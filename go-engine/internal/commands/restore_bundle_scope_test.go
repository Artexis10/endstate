// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
)

// bundleShapedManifest mimics what the bundle rewrite writes: restore entries
// carrying module provenance and rewritten ./configs/ sources.
func bundleShapedManifest(withProvenance bool) *manifest.Manifest {
	entry := func(module, source, target string) manifest.RestoreEntry {
		e := manifest.RestoreEntry{
			Type: "copy", Source: source, Target: target, Backup: true, Optional: true,
		}
		if withProvenance {
			e.FromModule = module
		}
		return e
	}
	return &manifest.Manifest{
		Version: 1,
		Apps: []manifest.App{
			{ID: "git-git", Refs: map[string]string{"windows": "Git.Git"}},
			{ID: "vscode", Refs: map[string]string{"windows": "Microsoft.VisualStudioCode"}},
		},
		ConfigModules: []string{"apps.git", "apps.vscode"},
		Restore: []manifest.RestoreEntry{
			entry("apps.git", "./configs/git/.gitconfig", `%USERPROFILE%\.gitconfig`),
			entry("apps.vscode", "./configs/vscode/settings.json", `%APPDATA%\Code\User\settings.json`),
		},
	}
}

// TestBundleRestoreEntries_RouteToScopableLanes is the behavioural half of the
// bundle-provenance fix.
//
// Restore input building sends an entry with an empty FromModule to
// `ordinaryRestores`, which is converted with an empty filter and is never
// touched by --only scoping. Entries carrying provenance instead group into
// legacy lanes keyed by module, which both --restore-filter and --only can
// scope. Without provenance a shared bundle restores every module's config
// regardless of what the recipient selected.
func TestBundleRestoreEntries_RouteToScopableLanes(t *testing.T) {
	t.Run("with provenance: scopable lanes", func(t *testing.T) {
		runtime, err := newConfigRestoreRuntime(configRestoreBuildRequest{
			Manifest:     bundleShapedManifest(true),
			ManifestPath: "manifest.jsonc",
		})
		if err != nil {
			t.Fatalf("newConfigRestoreRuntime: %v", err)
		}
		if len(runtime.inputs.ordinaryRestores) != 0 {
			t.Errorf("entries landed in ordinaryRestores (unscopable): %d", len(runtime.inputs.ordinaryRestores))
		}
		if len(runtime.inputs.legacyLanes) != 2 {
			t.Fatalf("expected one lane per module, got %d", len(runtime.inputs.legacyLanes))
		}
		byModule := map[string]bool{}
		for _, lane := range runtime.inputs.legacyLanes {
			byModule[lane.moduleID] = true
		}
		if !byModule["apps.git"] || !byModule["apps.vscode"] {
			t.Errorf("lanes not keyed by module id: %v", byModule)
		}
	})

	t.Run("without provenance: unscopable", func(t *testing.T) {
		runtime, err := newConfigRestoreRuntime(configRestoreBuildRequest{
			Manifest:     bundleShapedManifest(false),
			ManifestPath: "manifest.jsonc",
		})
		if err != nil {
			t.Fatalf("newConfigRestoreRuntime: %v", err)
		}
		// This is the defect: no lanes to scope, everything in the unfiltered bucket.
		if len(runtime.inputs.legacyLanes) != 0 {
			t.Errorf("expected no lanes without provenance, got %d", len(runtime.inputs.legacyLanes))
		}
		if len(runtime.inputs.ordinaryRestores) != 2 {
			t.Errorf("expected both entries in the unscopable bucket, got %d", len(runtime.inputs.ordinaryRestores))
		}
	})
}

// TestScopeConfigRestoreRuntimeForOnly_DeselectsUnmatchedLanes verifies the
// existing --only scoping reaches bundle-derived lanes once they carry
// provenance: a selection matching only apps.git must leave apps.vscode's lane
// unselected rather than restoring config the recipient did not ask for.
func TestScopeConfigRestoreRuntimeForOnly_DeselectsUnmatchedLanes(t *testing.T) {
	runtime, err := newConfigRestoreRuntime(configRestoreBuildRequest{
		Manifest:      bundleShapedManifest(true),
		ManifestPath:  "manifest.jsonc",
		RestoreFilter: "",
	})
	if err != nil {
		t.Fatalf("newConfigRestoreRuntime: %v", err)
	}

	// --only git-git resolves to just the git module.
	scopeConfigRestoreRuntimeForOnly(runtime, []*modules.Module{{ID: "apps.git"}})

	var gitSelected, vscodeSelected bool
	for _, lane := range runtime.inputs.legacyLanes {
		switch lane.moduleID {
		case "apps.git":
			gitSelected = lane.selected
		case "apps.vscode":
			vscodeSelected = lane.selected
		}
	}
	if !gitSelected {
		t.Error("the selected app's module lane should stay selected")
	}
	if vscodeSelected {
		t.Error("an unselected app's module lane must be deselected")
	}
}
