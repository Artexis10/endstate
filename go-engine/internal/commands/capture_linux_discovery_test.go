// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/discovery"
	"github.com/Artexis10/endstate/go-engine/internal/envelope"
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
		"editor":  map[string]any{"schemaVersion": 1, "id": "editor", "displayName": "Editor", "nix": map[string]string{"attribute": "editor"}, "aliases": map[string][]string{"config": {"${xdg.config}/editor.json"}}},
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
	t.Setenv("XDG_CONFIG_HOME", configRoot)
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
	if mf.HomeManager == nil || mf.HomeManager.Flake != ownerFlake {
		t.Fatalf("Home Manager history lost during ordinary discovery: %+v", mf.HomeManager)
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

	module := &modules.Module{
		ModuleSchemaVersion: 3,
		ID:                  "apps.ripgrep", DisplayName: "ripgrep", Sensitivity: "low",
		Platforms: map[string]modules.PlatformVariant{"linux": {
			Realization: "home-manager",
			Matches:     modules.MatchCriteria{PathExists: []string{liveConfig}},
			Capture: &modules.CaptureDef{Files: []modules.CaptureFile{{
				Source: liveConfig, Dest: "apps/ripgrep/ripgreprc",
			}}},
		}},
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
	var homeSettings struct {
		Files map[string]string `json:"files"`
	}
	if mf.HomeManager == nil || len(mf.HomeManager.Settings) == 0 {
		t.Fatalf("captured manifest has no Home Manager settings: %+v", mf.HomeManager)
	}
	if err := json.Unmarshal(mf.HomeManager.Settings, &homeSettings); err != nil {
		t.Fatal(err)
	}
	if ref := homeSettings.Files["${xdg.config}/ripgrep/ripgreprc"]; !strings.HasPrefix(ref, "./configs/home-manager/ripgrep/") {
		t.Fatalf("captured Home Manager file ref = %q", ref)
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
		if len(request.Only) != 1 || request.Only[0] != "atuin" {
			t.Fatalf("discovery selection = %v", request.Only)
		}
		return discovery.Result{
			SchemaVersion: discovery.SchemaVersion,
			Sources:       []discovery.SourceStatus{{ID: discovery.HomeManagerSettingsSource, State: discovery.SourceSucceeded, Discovered: 1}},
			Settings: []discovery.SettingsCandidate{{
				ID: "home-manager.atuin", DisplayName: "Atuin settings", PackageID: "atuin",
				Portability: discovery.PortabilityConfigOnly,
				Actions:     []discovery.SettingsAction{discovery.SettingsCapture, discovery.SettingsRestore},
			}},
			SettingsFiles: []discovery.SettingsFilePlan{{
				CandidateID: "home-manager.atuin", AdapterID: discovery.HomeManagerSettingsSource,
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

	withFakeGenerations(nil, errors.New("no Endstate history"), func() {
		withMockCatalog(map[string]*modules.Module{}, nil, func() {
			raw, captureErr := RunCapture(CaptureFlags{Out: out, Only: "atuin"})
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
	if len(mf.Apps) != 0 || mf.HomeManager == nil || mf.HomeManager.Settings == nil {
		t.Fatalf("settings-only manifest = %+v", mf)
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
