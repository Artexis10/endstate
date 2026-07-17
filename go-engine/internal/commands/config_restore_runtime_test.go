// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/planner"
)

func TestNewConfigRestoreRuntimeLoadsAndPinsCatalogExactlyOnce(t *testing.T) {
	manifestDir := t.TempDir()
	capture := commandTestConfigCapture(t, manifestDir, "capture-pinned", "apps.pinned", "preferences")

	moduleFile := filepath.Join(t.TempDir(), "module.jsonc")
	if err := os.WriteFile(moduleFile, []byte(`{"revision":"original"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	module := &modules.Module{
		ModuleSchemaVersion: 2,
		ID:                  "apps.pinned",
		Revision:            "restore-revision",
		FilePath:            moduleFile,
		Matches:             modules.MatchCriteria{Exe: []string{"Pinned.exe"}},
		Config: &modules.ConfigDef{InstanceDetectors: []modules.InstanceDetectorDef{{
			ID: "profiles", Type: "path", Glob: "pinned/*",
		}}, Sets: []modules.ConfigSetDef{{
			ID: "preferences", Generations: []modules.GenerationDef{{
				ID: "g1", Order: 1, Fingerprint: capture.SourceGenerationFingerprint,
			}},
		}}},
	}
	catalog := map[string]*modules.Module{"apps.pinned": module}
	diagnostics := []modules.CatalogDiagnostic{{Code: "ORIGINAL", ModuleID: "apps.rejected", FilePath: "original"}}
	loadCount := 0
	originalLoader := loadConfigRestoreCatalogFn
	loadConfigRestoreCatalogFn = func(repoRoot string) (map[string]*modules.Module, []modules.CatalogDiagnostic, error) {
		loadCount++
		if repoRoot != "trusted-root" {
			t.Fatalf("repo root = %q", repoRoot)
		}
		return catalog, diagnostics, nil
	}
	t.Cleanup(func() { loadConfigRestoreCatalogFn = originalLoader })

	runtime, envErr := newConfigRestoreRuntime(configRestoreBuildRequest{
		Manifest:     &manifest.Manifest{Version: 2, ConfigCaptures: []manifest.ConfigCapture{capture}},
		ManifestPath: filepath.Join(manifestDir, "manifest.jsonc"), RepoRoot: "trusted-root",
	})
	if envErr != nil {
		t.Fatalf("newConfigRestoreRuntime: %+v", envErr)
	}
	if loadCount != 1 || runtime.catalog.resolver == nil || len(runtime.catalog.diagnostics) != 1 {
		t.Fatalf("runtime snapshot = %+v loadCount=%d", runtime.catalog, loadCount)
	}

	// Mutating the loader-owned map, module, diagnostics, and backing file after
	// construction must not change the command-scoped compatibility resolver.
	delete(catalog, "apps.pinned")
	module.Revision = "mutated-revision"
	module.Config.Sets[0].Generations[0].ID = "mutated-generation"
	module.Config.InstanceDetectors[0].Glob = "mutated/*"
	module.Matches.Exe[0] = "Mutated.exe"
	diagnostics[0].Code = "MUTATED"
	if err := os.WriteFile(moduleFile, []byte(`{"revision":"mutated"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	plan := runtime.catalog.resolver.ResolveCandidate(runtime.inputs.generationSources[0].source, planner.TargetInstance{
		ID: "target-instance", ModuleID: "apps.pinned", DetectorID: "installed", RawVersion: "99",
	})
	if plan.Resolution.Resolution != planner.ResolutionDirect || plan.Resolution.Reason != nil ||
		plan.Resolution.CaptureModuleRevision != capture.CaptureModule.ContentHash ||
		plan.Resolution.RestoreModuleRevision != "restore-revision" || loadCount != 1 {
		t.Fatalf("resolver was not pinned: resolution=%+v loadCount=%d", plan.Resolution, loadCount)
	}
	if runtime.catalog.diagnostics[0].Code != "ORIGINAL" {
		t.Fatalf("diagnostics alias loader memory: %+v", runtime.catalog.diagnostics)
	}
	if patterns := runtime.catalog.resolver.ProcessPatterns("apps.pinned"); len(patterns) != 1 || patterns[0] != "Pinned.exe" {
		t.Fatalf("process patterns were not pinned: %v", patterns)
	}
	seenGlob := ""
	if _, err := runtime.catalog.resolver.DiscoverTargets("apps.pinned", nil, modules.DiscoveryOptions{
		Glob: func(pattern string) ([]string, error) {
			seenGlob = pattern
			return []string{}, nil
		},
	}); err != nil || seenGlob != "pinned/*" || loadCount != 1 {
		t.Fatalf("detectors were not pinned: glob=%q err=%v loadCount=%d", seenGlob, err, loadCount)
	}
}

func TestNewConfigRestoreRuntimeMarksInvalidModuleSnapshotAsPayloadIntegrityFailure(t *testing.T) {
	tests := []struct {
		name       string
		invalidate func(string) error
		construct  func(configRestoreBuildRequest) (*configRestoreRuntime, *envelope.Error)
	}{
		{
			name: "edited snapshot through catalog source",
			invalidate: func(snapshotPath string) error {
				return os.WriteFile(snapshotPath, []byte("edited"), 0o644)
			},
			construct: func(request configRestoreBuildRequest) (*configRestoreRuntime, *envelope.Error) {
				return newConfigRestoreRuntimeWithCatalogSource(request, configRestoreCatalogLoader(
					func(string) (map[string]*modules.Module, []modules.CatalogDiagnostic, error) {
						return map[string]*modules.Module{}, nil, nil
					},
				))
			},
		},
		{
			name:       "missing snapshot through pinned catalog",
			invalidate: os.Remove,
			construct: func(request configRestoreBuildRequest) (*configRestoreRuntime, *envelope.Error) {
				return newConfigRestoreRuntimeWithCatalogSnapshot(request, newConfigCatalogSnapshot(map[string]*modules.Module{}, nil))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manifestDir := t.TempDir()
			capture := commandTestConfigCapture(t, manifestDir, "capture-pinned", "apps.pinned", "preferences")
			snapshotPath := filepath.Join(manifestDir, filepath.FromSlash(capture.CaptureModule.SnapshotPath))
			if err := tt.invalidate(snapshotPath); err != nil {
				t.Fatal(err)
			}

			runtime, envErr := tt.construct(configRestoreBuildRequest{
				Manifest:     &manifest.Manifest{Version: 2, ConfigCaptures: []manifest.ConfigCapture{capture}},
				ManifestPath: filepath.Join(manifestDir, "manifest.jsonc"),
			})
			if envErr != nil {
				t.Fatalf("runtime construction returned command error: %+v", envErr)
			}
			if len(runtime.inputs.generationSources) != 1 || !runtime.inputs.generationSources[0].source.PayloadIntegrityFailed {
				t.Fatalf("invalid snapshot was not isolated as payload integrity failure: %+v", runtime.inputs.generationSources)
			}

			plan := runtime.catalog.resolver.ResolveCandidate(runtime.inputs.generationSources[0].source, planner.TargetInstance{
				ID: "target-instance", ModuleID: "apps.pinned", DetectorID: "installed", RawVersion: "99",
			})
			if plan.Resolution.Status != planner.StatusFailed || plan.Resolution.Reason == nil ||
				*plan.Resolution.Reason != planner.ReasonPayloadIntegrityFailed {
				t.Fatalf("resolution = %+v, want failed/payload_integrity_failed", plan.Resolution)
			}
		})
	}
}

func TestNewConfigRestoreRuntimeLeavesConfigFreeInputPure(t *testing.T) {
	loadCount := 0
	originalLoader := loadConfigRestoreCatalogFn
	loadConfigRestoreCatalogFn = func(string) (map[string]*modules.Module, []modules.CatalogDiagnostic, error) {
		loadCount++
		return map[string]*modules.Module{}, nil, nil
	}
	t.Cleanup(func() { loadConfigRestoreCatalogFn = originalLoader })

	runtime, envErr := newConfigRestoreRuntime(configRestoreBuildRequest{
		Manifest: &manifest.Manifest{Version: 1}, ManifestPath: filepath.Join(t.TempDir(), "manifest.jsonc"),
	})
	if envErr != nil {
		t.Fatalf("newConfigRestoreRuntime: %+v", envErr)
	}
	if loadCount != 0 || runtime.catalog.resolver != nil || runtime.inputs.hasConfigPayloads {
		t.Fatalf("config-free runtime loaded catalog or invented payloads: runtime=%+v count=%d", runtime, loadCount)
	}
	if runtime.inputs.generationSources == nil || runtime.inputs.legacyLanes == nil ||
		runtime.inputs.ordinaryRestores == nil || runtime.inputs.targetMappings == nil || runtime.catalog.diagnostics == nil {
		t.Fatalf("config-free internals contain nil collections: %+v", runtime)
	}
}

func TestNewConfigRestoreRuntimeDoesNotRequireCatalogForLegacyOnlyInput(t *testing.T) {
	loadCount := 0
	originalLoader := loadConfigRestoreCatalogFn
	loadConfigRestoreCatalogFn = func(string) (map[string]*modules.Module, []modules.CatalogDiagnostic, error) {
		loadCount++
		return nil, nil, os.ErrNotExist
	}
	t.Cleanup(func() { loadConfigRestoreCatalogFn = originalLoader })

	runtime, envErr := newConfigRestoreRuntime(configRestoreBuildRequest{
		Manifest: &manifest.Manifest{Version: 1, Restore: []manifest.RestoreEntry{{
			Type: "copy", Source: "settings.json", Target: filepath.Join(t.TempDir(), "settings.json"),
			FromModule: "apps.legacy",
		}}},
		ManifestPath: filepath.Join(t.TempDir(), "manifest.jsonc"),
	})
	if envErr != nil {
		t.Fatalf("legacy-only runtime should not depend on current catalog: %+v", envErr)
	}
	if loadCount != 0 || !runtime.inputs.hasConfigPayloads || len(runtime.inputs.legacyLanes) != 1 || runtime.catalog.resolver != nil {
		t.Fatalf("runtime=%+v loadCount=%d", runtime, loadCount)
	}
}

func TestNewConfigRestoreRuntimeReturnsDeterministicNonNilCollections(t *testing.T) {
	manifestDir := t.TempDir()
	zeta := commandTestConfigCapture(t, manifestDir, "capture-zeta", "apps.zeta", "preferences")
	alpha := commandTestConfigCapture(t, manifestDir, "capture-alpha", "apps.alpha", "preferences")
	originalLoader := loadConfigRestoreCatalogFn
	loadConfigRestoreCatalogFn = func(string) (map[string]*modules.Module, []modules.CatalogDiagnostic, error) {
		return map[string]*modules.Module{}, []modules.CatalogDiagnostic{
			{Code: "Z", ModuleID: "apps.zeta", FilePath: "zeta"},
			{Code: "A", ModuleID: "apps.alpha", FilePath: "alpha"},
		}, nil
	}
	t.Cleanup(func() { loadConfigRestoreCatalogFn = originalLoader })

	runtime, envErr := newConfigRestoreRuntime(configRestoreBuildRequest{
		Manifest:     &manifest.Manifest{Version: 2, ConfigCaptures: []manifest.ConfigCapture{zeta, alpha}},
		ManifestPath: filepath.Join(manifestDir, "manifest.jsonc"),
	})
	if envErr != nil {
		t.Fatalf("newConfigRestoreRuntime: %+v", envErr)
	}
	if got := []string{
		runtime.inputs.generationSources[0].source.CaptureID,
		runtime.inputs.generationSources[1].source.CaptureID,
	}; strings.Join(got, ",") != "capture-alpha,capture-zeta" {
		t.Fatalf("source ordering = %v", got)
	}
	if runtime.inputs.legacyLanes == nil || runtime.inputs.ordinaryRestores == nil ||
		runtime.inputs.targetMappings == nil || runtime.catalog.diagnostics == nil {
		t.Fatalf("runtime internals contain nil collections: %+v", runtime)
	}
	if runtime.catalog.diagnostics[0].ModuleID != "apps.alpha" || runtime.catalog.diagnostics[1].ModuleID != "apps.zeta" {
		t.Fatalf("diagnostic ordering = %+v", runtime.catalog.diagnostics)
	}
}
