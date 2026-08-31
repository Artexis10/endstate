// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/discovery"
	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/realizer"
)

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
				Target: "${xdg.config}/ripgrep/ripgreprc", Source: liveConfig, ObservedSize: int64(len("--hidden\n")),
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
				Target: "${xdg.config}/atuin/config.toml", Source: liveConfig,
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
