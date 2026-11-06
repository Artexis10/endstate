// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/bundle"
	"github.com/Artexis10/endstate/go-engine/internal/discovery"
	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/provision"
	"github.com/Artexis10/endstate/go-engine/internal/realizer"
)

func TestRunCaptureLinux_ConfigPresenceDoesNotAuthorizeCapture(t *testing.T) {
	root := t.TempDir()
	catalogDir := filepath.Join(root, "catalog", "packages")
	if err := os.MkdirAll(catalogDir, 0700); err != nil {
		t.Fatal(err)
	}
	executable := "rg"
	if runtime.GOOS == "windows" {
		executable += ".cmd"
	}
	for id, entry := range map[string]any{
		"ripgrep": map[string]any{"schemaVersion": 1, "id": "ripgrep", "displayName": "ripgrep", "nix": map[string]string{"attribute": "ripgrep"}, "aliases": map[string][]string{"executable": {executable}}},
		"editor":  map[string]any{"schemaVersion": 1, "id": "editor", "displayName": "Editor", "nix": map[string]string{"attribute": "editor"}, "aliases": map[string][]string{"config": {"${home}/editor.json"}}},
	} {
		body, err := json.Marshal(entry)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(catalogDir, id+".json"), body, 0600); err != nil {
			t.Fatal(err)
		}
	}
	bin, configRoot := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, executable), []byte("Never execute this file."), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configRoot, "editor.json"), []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ENDSTATE_ROOT", root)
	t.Setenv("PATH", bin)
	t.Setenv("HOME", configRoot)
	t.Setenv("USERPROFILE", configRoot)
	oldGOOS, oldRealizer, oldDiscovery := captureGOOSFn, newRealizerFn, discoverLinuxFn
	t.Cleanup(func() { captureGOOSFn, newRealizerFn, discoverLinuxFn = oldGOOS, oldRealizer, oldDiscovery })
	captureGOOSFn = func() string { return "linux" }
	newRealizerFn = func() (realizer.Realizer, error) { return nil, ErrNoRealizer }
	discoverLinuxFn = func(ctx context.Context, request discovery.Request, _ realizer.Realizer) (discovery.Result, error) {
		request.SelectedSources = []string{"catalog-executables", "catalog-config-paths"}
		return discovery.Discover(ctx, request)
	}
	out := filepath.Join(t.TempDir(), "selected.endstate")
	withFakeGenerations(nil, errors.New("no history"), func() {
		emptyCatalog(func() {
			_, captureErr := RunCapture(CaptureFlags{Out: out, Only: "ripgrep,editor"})
			if captureErr == nil || captureErr.Code != envelope.ErrManifestValidationError || !strings.Contains(captureErr.Message, "editor") {
				t.Fatalf("unsupported selected config must remain visible: %+v", captureErr)
			}
			if _, err := os.Stat(out); !os.IsNotExist(err) {
				t.Fatalf("unsupported selection published a partial artifact: %v", err)
			}
			raw, captureErr := RunCapture(CaptureFlags{Out: out})
			if captureErr != nil {
				t.Fatalf("capture supported inventory: %+v", captureErr)
			}
			result := raw.(*CaptureResult)
			if len(result.AppsIncluded) != 1 || len(result.Discovery.Settings) != 1 || len(result.Discovery.Settings[0].Actions) != 0 {
				t.Fatalf("capture lost supported inventory or review-only evidence: %+v", result)
			}
		})
	})
}

func TestCapturePlatformRegressionMatrix(t *testing.T) {
	out := filepath.Join(t.TempDir(), "mixed-profile.jsonc")
	fr := &fakeRealizer{currentSet: nixSetWithVersion("ripgrep", "14.1.0")}
	fr.currentSet.Elements["fd"] = realizer.Element{Name: "fd"}
	originalGOOS := captureGOOSFn
	captureGOOSFn = func() string { return "linux" }
	t.Cleanup(func() { captureGOOSFn = originalGOOS })
	const ownerFlake = "github:example/settings#desktop"

	withFakeRealizer(fr, func() {
		discoverLinuxFn = func(ctx context.Context, request discovery.Request, r realizer.Realizer) (discovery.Result, error) {
			result, err := discoverTestRealizerOnly(ctx, request, r)
			if err != nil {
				return result, err
			}
			result.Resolved = append(result.Resolved, discovery.ResolvedIntent{
				ID: "jq", Installable: "github:NixOS/nixpkgs/1111111111111111111111111111111111111111#jq",
				Evidence: []discovery.Evidence{{Source: "debian-explicit", Ref: "jq", Version: "1.7-native"}},
			})
			return result, nil
		}
		withFakeGenerations([]*provision.Generation{hmGen(ownerFlake, 3)}, nil, func() {
			emptyCatalog(func() {
				if _, err := RunCapture(CaptureFlags{Out: out, Only: "ripgrep,jq"}); err != nil {
					t.Fatalf("capture managed and native inventory: %+v", err)
				}
			})
		})
	})

	mf := readCapturedManifest(t, out)
	if len(mf.Apps) != 2 {
		t.Fatalf("selected apps = %+v, want ripgrep and jq without fd", mf.Apps)
	}
	for _, app := range mf.Apps {
		switch app.ID {
		case "ripgrep":
			if app.Version != "14.1.0" {
				t.Errorf("managed Nix version = %q, want observed 14.1.0", app.Version)
			}
		case "jq":
			if app.Version != "" || app.Refs["linux"] != "github:NixOS/nixpkgs/1111111111111111111111111111111111111111#jq" {
				t.Errorf("native evidence became desired Nix version or lost its ref: %+v", app)
			}
		default:
			t.Errorf("unselected app captured: %s", app.ID)
		}
	}
	if mf.HomeManager != nil {
		t.Fatalf("selected capture retained an opaque settings graph that cannot be scoped: %+v", mf.HomeManager)
	}
}

func TestLinuxCaptureSanitizeOmitsHistoricalHomeManagerSettings(t *testing.T) {
	root := t.TempDir()
	out := filepath.Join(root, "sanitized.jsonc")
	fr := &fakeRealizer{currentSet: nixSet("ripgrep")}
	oldGOOS := captureGOOSFn
	captureGOOSFn = func() string { return "linux" }
	t.Cleanup(func() { captureGOOSFn = oldGOOS })
	history := hmGenSettingsSecrets(&manifest.HomeManagerSettings{
		Files: map[string]string{"${home}/.unselected": filepath.Join(root, "private-source")},
	}, []manifest.HomeManagerSecret{{Name: "~/.credential", Path: filepath.Join(root, "credential-reference")}}, "generated-flake", 3)
	withFakeRealizer(fr, func() {
		withFakeGenerations([]*provision.Generation{history}, nil, func() {
			emptyCatalog(func() {
				if _, err := RunCapture(CaptureFlags{Out: out, Sanitize: true}); err != nil {
					t.Fatalf("sanitized capture: %+v", err)
				}
			})
		})
	})
	mf := readCapturedManifest(t, out)
	if len(mf.Apps) != 1 || mf.Apps[0].ID != "ripgrep" {
		t.Fatalf("sanitization lost selected package state: %+v", mf.Apps)
	}
	if mf.HomeManager != nil {
		t.Fatal("sanitized capture retained historical settings or secret references")
	}
}

func TestRunCaptureLinux_NoNixCapturesNativeIntentAndLiveSettings(t *testing.T) {
	dir := t.TempDir()
	liveConfig := filepath.Join(dir, "ripgreprc")
	if err := os.WriteFile(liveConfig, []byte("--hidden\n"), 0o600); err != nil {
		t.Fatalf("write live config: %v", err)
	}
	out := filepath.Join(dir, "captured.endstate")

	originalGOOS := captureGOOSFn
	originalRealizer := newRealizerFn
	originalDiscovery := discoverLinuxFn
	originalEnumerate := enumerateCapturePackagesFn
	captureGOOSFn = func() string { return "linux" }
	realizerCalls := 0
	newRealizerFn = func() (realizer.Realizer, error) {
		realizerCalls++
		return nil, ErrNoRealizer
	}
	discoverLinuxFn = func(context.Context, discovery.Request, realizer.Realizer) (discovery.Result, error) {
		return discovery.Result{
			SchemaVersion: discovery.SchemaVersion,
			Sources: []discovery.SourceStatus{
				{ID: "endstate-profile", State: discovery.SourceNotAvailable},
				{ID: "debian-explicit", State: discovery.SourceSucceeded, Discovered: 1},
			},
			Resolved: []discovery.ResolvedIntent{{
				ID:              "ripgrep",
				DisplayName:     "ripgrep",
				Attribute:       "ripgrep",
				Input:           "github:NixOS/nixpkgs/1111111111111111111111111111111111111111",
				Installable:     "github:NixOS/nixpkgs/1111111111111111111111111111111111111111#ripgrep",
				MappingRevision: "sha256:test-mapping",
				Evidence: []discovery.Evidence{{
					Source: "debian-explicit", Ref: "ripgrep", Version: "14.1.1", UserFacing: true,
				}},
			}},
			Settings: []discovery.SettingsCandidate{{
				ID: "apps.ripgrep", ModuleID: "apps.ripgrep", DisplayName: "ripgrep settings",
				PackageID: "ripgrep", Portability: discovery.PortabilityResolved,
				Actions: []discovery.SettingsAction{discovery.SettingsCapture, discovery.SettingsRestore},
			}},
			SettingsFiles: []discovery.SettingsFilePlan{{
				Realization: "home-manager",
				CandidateID: "apps.ripgrep", AdapterID: discovery.HomeManagerSettingsSource,
				Codec: "bounded-regular-file-v1", Target: "${xdg.config}/ripgrep/ripgreprc",
				Source: liveConfig, ObservedSize: int64(len("--hidden\n")),
			}},
			Counts: discovery.Counts{Discovered: 1, Resolved: 1, Selected: 1, Settings: 1},
		}, nil
	}
	enumerateCapturePackagesFn = func(CaptureFlags) ([]enumeratedCapturePackage, []CommandWarning, *envelope.Error) {
		return nil, nil, envelope.NewError(envelope.ErrCaptureFailed, "Windows package enumeration ran on Linux")
	}
	t.Cleanup(func() {
		captureGOOSFn = originalGOOS
		newRealizerFn = originalRealizer
		discoverLinuxFn = originalDiscovery
		enumerateCapturePackagesFn = originalEnumerate
	})

	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	moduleBytes := []byte(`{"moduleSchemaVersion":3,"id":"apps.ripgrep","displayName":"ripgrep","sensitivity":"low","platforms":{"linux":{"realization":"home-manager","matches":{"pathExists":["${home}/ripgreprc"]},"capture":{"files":[{"source":"${home}/ripgreprc","dest":"apps/ripgrep/ripgreprc"}]}}}}`)
	module, err := modules.ParseModuleJSON(moduleBytes)
	if err != nil {
		t.Fatal(err)
	}

	withFakeGenerations(nil, errors.New("no Endstate history"), func() {
		withMockCatalog(map[string]*modules.Module{module.ID: module}, nil, func() {
			raw, captureErr := RunCapture(CaptureFlags{Out: out})
			if captureErr != nil {
				t.Fatalf("RunCapture: %+v", captureErr)
			}
			result := raw.(*CaptureResult)
			if result.Discovery == nil || len(result.Discovery.Resolved) != 1 {
				t.Fatalf("discovery result = %+v, want one resolved native intent", result.Discovery)
			}
			if len(result.ConfigsIncluded) != 1 || result.ConfigsIncluded[0] != "ripgrep" {
				t.Fatalf("configsIncluded = %v, want live ripgrep config", result.ConfigsIncluded)
			}
		})
	})

	if realizerCalls != 1 {
		t.Fatalf("realizer availability checks = %d, want one non-mutating check", realizerCalls)
	}
	mf := readCapturedManifest(t, out)
	if len(mf.Apps) != 1 {
		t.Fatalf("manifest apps = %d, want one", len(mf.Apps))
	}
	const wantRef = "github:NixOS/nixpkgs/1111111111111111111111111111111111111111#ripgrep"
	if got := mf.Apps[0].Refs["linux"]; got != wantRef {
		t.Fatalf("linux ref = %q, want immutable reviewed intent %q", got, wantRef)
	}
	// A Home Manager file is captured once, as a generation payload; it is never copied into inline settings.
	if mf.HomeManager != nil {
		t.Fatalf("captured Home Manager file was also serialized inline: %+v", mf.HomeManager)
	}
	if len(mf.ConfigCaptures) != 1 || mf.ConfigCaptures[0].ModuleID != "apps.ripgrep" {
		t.Fatalf("configCaptures = %+v, want the ripgrep generation capture", mf.ConfigCaptures)
	}
}

func TestRunCaptureLinux_NoNixAllowsSelectedSettingsOnlyArtifact(t *testing.T) {
	dir := t.TempDir()
	liveConfig := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(liveConfig, []byte("sync_address = 'https://sync.example'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "settings-only.endstate")

	originalGOOS := captureGOOSFn
	originalRealizer := newRealizerFn
	originalDiscovery := discoverLinuxFn
	captureGOOSFn = func() string { return "linux" }
	newRealizerFn = func() (realizer.Realizer, error) { return nil, ErrNoRealizer }
	discoverLinuxFn = func(_ context.Context, request discovery.Request, _ realizer.Realizer) (discovery.Result, error) {
		if len(request.Only) != 1 || request.Only[0] != "apps.atuin" {
			t.Fatalf("discovery selection = %v", request.Only)
		}
		return discovery.Result{
			SchemaVersion: discovery.SchemaVersion,
			Sources:       []discovery.SourceStatus{{ID: discovery.HomeManagerSettingsSource, State: discovery.SourceSucceeded, Discovered: 1}},
			Settings: []discovery.SettingsCandidate{{
				ID: "apps.atuin", ModuleID: "apps.atuin", DisplayName: "Atuin settings", PackageID: "atuin",
				Portability: discovery.PortabilityConfigOnly,
				Actions:     []discovery.SettingsAction{discovery.SettingsCapture, discovery.SettingsRestore},
			}},
			SettingsFiles: []discovery.SettingsFilePlan{{
				Realization: "home-manager",
				CandidateID: "apps.atuin", AdapterID: discovery.HomeManagerSettingsSource,
				Codec: "bounded-regular-file-v1", Target: "${xdg.config}/atuin/config.toml", Source: liveConfig,
				ObservedSize: int64(len("sync_address = 'https://sync.example'\n")),
			}},
			Counts: discovery.Counts{Discovered: 1, Settings: 1},
		}, nil
	}
	t.Cleanup(func() {
		captureGOOSFn = originalGOOS
		newRealizerFn = originalRealizer
		discoverLinuxFn = originalDiscovery
	})

	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	module, err := modules.ParseModuleJSON([]byte(`{"moduleSchemaVersion":3,"id":"apps.atuin","displayName":"Atuin","sensitivity":"low","platforms":{"linux":{"realization":"home-manager","matches":{"pathExists":["${home}/config.toml"]},"capture":{"files":[{"source":"${home}/config.toml","dest":"apps/atuin/config.toml"}]}}}}`))
	if err != nil {
		t.Fatal(err)
	}
	withFakeGenerations(nil, errors.New("no Endstate history"), func() {
		withMockCatalog(map[string]*modules.Module{module.ID: module}, nil, func() {
			raw, captureErr := RunCapture(CaptureFlags{Out: out, Only: "apps.atuin"})
			if captureErr != nil {
				t.Fatalf("RunCapture: %+v", captureErr)
			}
			result := raw.(*CaptureResult)
			if len(result.AppsIncluded) != 0 || result.Discovery == nil || result.Discovery.Counts.Settings != 1 {
				t.Fatalf("settings-only result = %+v", result)
			}
		})
	})

	mf := readCapturedManifest(t, out)
	if len(mf.Apps) != 0 || mf.HomeManager != nil || len(mf.ConfigCaptures) != 1 || mf.ConfigCaptures[0].ModuleID != "apps.atuin" {
		t.Fatalf("settings-only manifest = %+v", mf)
	}
}

type captureDiscoveryAdapter struct{ inventory discovery.AdapterInventory }

func (captureDiscoveryAdapter) ID() string { return "fixture" }
func (a captureDiscoveryAdapter) Inventory(context.Context) (discovery.AdapterInventory, error) {
	return a.inventory, nil
}

func TestLinuxCaptureSelectionPreservesInventoryAndScopesPayload(t *testing.T) {
	for _, mode := range []string{"selected", "update", "share", "sanitize"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			live := filepath.Join(dir, "selected.json")
			if err := os.WriteFile(live, []byte(`{"theme":"dark"}`), 0600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("HOME", dir)
			t.Setenv("USERPROFILE", dir)
			inventory := discovery.AdapterInventory{}
			catalog := map[string]*modules.Module{}
			for _, id := range []string{"selected", "unselected"} {
				inventory.Resolved = append(inventory.Resolved, discovery.ResolvedIntent{
					ID: id, DisplayName: id, Attribute: id, Input: "github:example/packages/revision",
					Installable: "github:example/packages/revision#" + id,
				})
				inventory.Settings = append(inventory.Settings, discovery.SettingsCandidate{
					ID: "apps." + id, ModuleID: "apps." + id, PackageID: id, Actions: []discovery.SettingsAction{discovery.SettingsCapture},
				})
				source := live
				if id == "unselected" {
					// This source must never be opened during a selected capture.
					source = filepath.Join(dir, "missing-unselected.json")
				}
				inventory.SettingsFiles = append(inventory.SettingsFiles, discovery.SettingsFilePlan{
					Realization: "home-manager",
					CandidateID: "apps." + id, Codec: "bounded-regular-file-v1",
					Target: "${xdg.config}/" + id + "/settings.json", Source: source,
				})
				module, err := modules.ParseModuleJSON([]byte(`{"moduleSchemaVersion":3,"id":"apps.` + id + `","displayName":"` + id + `","sensitivity":"low","platforms":{"linux":{"realization":"home-manager","matches":{"pathExists":["${home}/` + filepath.Base(source) + `"]},"capture":{"files":[{"source":"${home}/` + filepath.Base(source) + `","dest":"apps/` + id + `/settings.json"}]}}}}`))
				if err != nil {
					t.Fatal(err)
				}
				catalog[module.ID] = module
			}
			oldGOOS, oldRealizer, oldDiscovery := captureGOOSFn, newRealizerFn, discoverLinuxFn
			t.Cleanup(func() { captureGOOSFn, newRealizerFn, discoverLinuxFn = oldGOOS, oldRealizer, oldDiscovery })
			captureGOOSFn = func() string { return "linux" }
			newRealizerFn = func() (realizer.Realizer, error) { return nil, ErrNoRealizer }
			discoverLinuxFn = func(ctx context.Context, request discovery.Request, _ realizer.Realizer) (discovery.Result, error) {
				return (discovery.Orchestrator{Adapters: []discovery.Adapter{captureDiscoveryAdapter{inventory: inventory}}}).Discover(ctx, request)
			}
			flags := CaptureFlags{Out: filepath.Join(dir, "selection.endstate"), Only: "selected,apps.selected", Share: mode == "share", Sanitize: mode == "sanitize"}
			if flags.Sanitize {
				flags.Out = filepath.Join(dir, "selection.jsonc")
			}
			if mode == "update" {
				flags.Update = true
				flags.Manifest = filepath.Join(dir, "previous.jsonc")
				if err := os.WriteFile(flags.Manifest, []byte(`{"version":1,"name":"previous","apps":[{"id":"unselected","refs":{"linux":"github:example/packages/revision#unselected"}}]}`), 0600); err != nil {
					t.Fatal(err)
				}
			}
			withFakeGenerations(nil, errors.New("no history"), func() {
				withMockCatalog(catalog, nil, func() {
					raw, err := RunCapture(flags)
					if err != nil {
						t.Fatalf("selected capture read an unselected source or failed: %+v", err)
					}
					result := raw.(*CaptureResult)
					if result.Discovery == nil || len(result.Discovery.Resolved) != 2 || len(result.Discovery.Settings) != 2 || result.Discovery.Counts.Selected != 1 {
						t.Fatalf("capture hid unselected inventory or miscounted selection: %+v", result.Discovery)
					}
				})
			})
			mf := readCapturedManifest(t, flags.Out)
			wantApps := 1
			if mode == "update" {
				wantApps = 2
			}
			if len(mf.Apps) != wantApps {
				t.Fatalf("capture apps = %+v, want %d", mf.Apps, wantApps)
			}
			if mode == "sanitize" {
				if mf.HomeManager != nil {
					t.Fatalf("sanitized capture retained settings: %+v", mf.HomeManager)
				}
				return
			}
			if mf.HomeManager != nil || len(mf.ConfigCaptures) != 1 || mf.ConfigCaptures[0].ModuleID != "apps.selected" {
				t.Fatalf("capture did not scope settings to the selected generation: %+v %+v", mf.HomeManager, mf.ConfigCaptures)
			}
		})
	}
}

func TestRunCaptureLinux_FailsBeforeWritingWhenNothingUsefulWasFound(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "empty.endstate")

	originalGOOS := captureGOOSFn
	originalRealizer := newRealizerFn
	originalDiscovery := discoverLinuxFn
	captureGOOSFn = func() string { return "linux" }
	newRealizerFn = func() (realizer.Realizer, error) { return nil, ErrNoRealizer }
	discoverLinuxFn = func(context.Context, discovery.Request, realizer.Realizer) (discovery.Result, error) {
		return discovery.Result{
			SchemaVersion: discovery.SchemaVersion,
			Sources:       []discovery.SourceStatus{{ID: "debian-explicit", State: discovery.SourceSucceeded}},
			Unresolved: []discovery.UnresolvedItem{{
				ID: "debian:unknown", DisplayName: "Unknown", Reason: "mapping_missing",
				Message: "No reviewed portable package mapping exists for this installed application.",
			}},
			Counts: discovery.Counts{Discovered: 1, Unresolved: 1},
		}, nil
	}
	t.Cleanup(func() {
		captureGOOSFn = originalGOOS
		newRealizerFn = originalRealizer
		discoverLinuxFn = originalDiscovery
	})

	_, captureErr := RunCapture(CaptureFlags{Out: out})
	if captureErr == nil || captureErr.Code != envelope.ErrCaptureFailed {
		t.Fatalf("capture error = %+v, want CAPTURE_FAILED", captureErr)
	}
	if !strings.Contains(captureErr.Message, "1 unresolved") {
		t.Fatalf("capture error lacks discovery counts: %+v", captureErr)
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatalf("empty artifact was written before failure: %v", err)
	}
}

func TestLinuxCaptureOptionalSettingsDisappearanceKeepsDestination(t *testing.T) {
	for _, existing := range []bool{false, true} {
		name := "new destination"
		if existing {
			name = "existing artifact"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			out := filepath.Join(root, "capture.endstate")
			oldGOOS := captureGOOSFn
			captureGOOSFn = func() string { return "linux" }
			t.Cleanup(func() { captureGOOSFn = oldGOOS })
			var previous []byte
			withFakeRealizer(&fakeRealizer{currentSet: nixSet("ripgrep")}, func() {
				withFakeGenerations(nil, nil, func() {
					emptyCatalog(func() {
						if existing {
							if _, err := RunCapture(CaptureFlags{Out: out}); err != nil {
								t.Fatalf("capture original artifact: %+v", err)
							}
							var err error
							previous, err = os.ReadFile(out)
							if err != nil {
								t.Fatal(err)
							}
						}
						newRealizerFn = func() (realizer.Realizer, error) { return nil, ErrNoRealizer }
						discoverLinuxFn = func(context.Context, discovery.Request, realizer.Realizer) (discovery.Result, error) {
							return discovery.Result{
								SchemaVersion: discovery.SchemaVersion,
								Settings:      []discovery.SettingsCandidate{{ID: "apps.ripgrep", ModuleID: "apps.ripgrep", Actions: []discovery.SettingsAction{discovery.SettingsCapture}}},
								SettingsFiles: []discovery.SettingsFilePlan{{
									Realization: "home-manager", CandidateID: "apps.ripgrep", Codec: "bounded-regular-file-v1",
									Target: "${xdg.config}/ripgrep/ripgreprc", Source: filepath.Join(root, "disappeared-source"), Optional: true,
								}},
								Counts: discovery.Counts{Discovered: 1, Selected: 1, Settings: 1, SettingsOnly: 1},
							}, nil
						}
						_, err := RunCapture(CaptureFlags{Out: out})
						if err == nil || err.Code != envelope.ErrCaptureFailed {
							t.Fatalf("disappeared optional payload reported successful capture: %+v", err)
						}
					})
				})
			})
			got, err := os.ReadFile(out)
			if existing {
				if err != nil || !bytes.Equal(got, previous) {
					t.Fatal("failed capture changed the prior artifact")
				}
			} else if !os.IsNotExist(err) {
				t.Fatal("failed capture left an empty or intermediate artifact")
			}
		})
	}
}

func TestRunCaptureLinuxPackageMatchCannotCollectIneligibleAdapter(t *testing.T) {
	registry, err := os.ReadFile(filepath.Join("..", "..", "..", "catalog", "home-manager", "registry.jsonc"))
	if err != nil {
		t.Fatal(err)
	}
	root, home, bin := t.TempDir(), t.TempDir(), t.TempDir()
	executable := "rg"
	if runtime.GOOS == "windows" {
		executable += ".cmd"
	}
	files := map[string][]byte{
		"catalog/home-manager/registry.jsonc": registry,
		"catalog/packages/ripgrep.jsonc":      []byte(`{"schemaVersion":1,"id":"ripgrep","displayName":"ripgrep","nix":{"attribute":"ripgrep"},"aliases":{"executable":["` + executable + `"]},"configModule":"apps.ripgrep"}`),
		"modules/apps/ripgrep/module.jsonc":   []byte(`{"moduleSchemaVersion":3,"id":"apps.ripgrep","displayName":"ripgrep","sensitivity":"low","platforms":{"linux":{"realization":"endstate-restore","matches":{"pathExists":["${xdg.config}/ripgrep/ripgreprc"]},"capture":{"files":[{"source":"${xdg.config}/ripgrep/ripgreprc","dest":"apps/ripgrep/ripgreprc"}]},"restore":[{"type":"copy","source":"./payload/apps/ripgrep/ripgreprc","target":"${xdg.config}/ripgrep/ripgreprc","backup":true}]}}}`),
	}
	for relative, body := range files {
		target := filepath.Join(root, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, body, 0600); err != nil {
			t.Fatal(err)
		}
	}
	source := filepath.Join(home, ".config", "ripgrep", "ripgreprc")
	if err := os.MkdirAll(filepath.Dir(source), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte("payload must not be collected"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, executable), []byte("Never execute this file."), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ENDSTATE_ROOT", root)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("PATH", bin)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	oldGOOS, oldRealizer, oldDiscovery, oldBundle := captureGOOSFn, newRealizerFn, discoverLinuxFn, createCaptureBundleFn
	t.Cleanup(func() {
		captureGOOSFn, newRealizerFn, discoverLinuxFn, createCaptureBundleFn = oldGOOS, oldRealizer, oldDiscovery, oldBundle
	})
	captureGOOSFn = func() string { return "linux" }
	newRealizerFn = func() (realizer.Realizer, error) { return nil, ErrNoRealizer }
	discoverLinuxFn = func(ctx context.Context, request discovery.Request, _ realizer.Realizer) (discovery.Result, error) {
		request.SelectedSources = []string{"catalog-executables", discovery.HomeManagerSettingsSource}
		return discovery.Discover(ctx, request)
	}
	publications := 0
	createCaptureBundleFn = func(request bundle.CaptureBundleRequest) (*bundle.CaptureBundleResult, error) {
		publications++
		if len(request.Modules) != 0 || len(request.GenerationPlans) != 0 || len(request.HomeManagerFiles) != 0 {
			t.Fatalf("ineligible payload reached collection: %+v", request)
		}
		return bundle.CreateCaptureBundle(request)
	}
	withFakeGenerations(nil, errors.New("no history"), func() {
		out := filepath.Join(t.TempDir(), "package.endstate")
		raw, captureErr := RunCapture(CaptureFlags{Out: out})
		if captureErr != nil {
			t.Fatalf("independent package capture failed: %+v", captureErr)
		}
		result := raw.(*CaptureResult)
		if publications != 1 || len(result.AppsIncluded) != 1 || len(result.Discovery.Settings) != 1 || len(result.Discovery.Settings[0].Actions) != 0 {
			t.Fatalf("package inventory or inert settings evidence lost: %+v", result)
		}
		selected := filepath.Join(t.TempDir(), "selected.endstate")
		_, captureErr = RunCapture(CaptureFlags{Out: selected, Only: "ripgrep,apps.ripgrep"})
		if captureErr == nil || publications != 1 {
			t.Fatalf("explicit module selection bypassed adapter authority: %+v publications=%d", captureErr, publications)
		}
		if _, err := os.Stat(selected); !os.IsNotExist(err) {
			t.Fatalf("ineligible explicit selection published output: %v", err)
		}
	})
}

func TestLinuxSettingsSelectionCannotBorrowPackageAuthority(t *testing.T) {
	oldTiers := captureCatalogTiersFn
	captureCatalogTiersFn = func() ([]string, error) { return []string{"apps"}, nil }
	t.Cleanup(func() { captureCatalogTiersFn = oldTiers })
	for _, test := range []struct {
		name      string
		settings  map[string]bool
		wantError bool
	}{
		{"Windows retains package-backed selection", nil, false},
		{"independent module retains selection", map[string]bool{}, false},
		{"known ineligible adapter", map[string]bool{"apps.example": false}, true},
		{"capturable settings", map[string]bool{"apps.example": true}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, _, captureErr := validateCaptureOnlyWithSettings("example,apps.example", []capturedApp{{ID: "example"}}, nil, test.settings)
			if (captureErr != nil) != test.wantError {
				t.Fatalf("selection error=%+v wantError=%v", captureErr, test.wantError)
			}
		})
	}
}
