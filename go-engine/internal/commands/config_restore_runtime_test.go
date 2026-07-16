// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/planner"
)

func TestNewConfigRestoreRuntimeLoadsAndPinsCatalogExactlyOnce(t *testing.T) {
	manifestDir := t.TempDir()
	capture := commandTestConfigCapture(t, manifestDir, "capture-pinned", "apps.pinned", "preferences")
	capture.CaptureModule.ContentHash = "capture-revision"

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
