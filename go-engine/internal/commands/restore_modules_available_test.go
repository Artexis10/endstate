// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
)

// TestConfigModuleRestorable_OnlyWhatTheManifestCarries is the phantom-checkbox
// regression.
//
// `restoreModulesAvailable` was built from catalog matching alone, answering
// "which modules could exist for these apps" while clients ask "which settings
// does this profile have". Measured on a real profile: 41 modules offered, 8
// actually present — 33 controls that restore nothing when selected. A one-app
// profile declaring `"configModules": []` still advertised 16.
//
// Module restore is driven entirely by mf.ConfigModules (ExpandConfigModules
// returns immediately when it is empty), so that list is the authority.
func TestConfigModuleRestorable_OnlyWhatTheManifestCarries(t *testing.T) {
	tests := []struct {
		name     string
		manifest *manifest.Manifest
		moduleID string
		want     bool
	}{
		{
			name:     "declared module is restorable",
			manifest: &manifest.Manifest{ConfigModules: []string{"apps.vscode", "apps.git"}},
			moduleID: "apps.git",
			want:     true,
		},
		{
			name:     "matched but undeclared module is not",
			manifest: &manifest.Manifest{ConfigModules: []string{"apps.vscode"}},
			moduleID: "apps.git",
			want:     false,
		},
		{
			name:     "empty configModules means nothing restores",
			manifest: &manifest.Manifest{ConfigModules: []string{}},
			moduleID: "apps.git",
			want:     false,
		},
		{
			name:     "nil configModules means nothing restores",
			manifest: &manifest.Manifest{},
			moduleID: "apps.git",
			want:     false,
		},
		{
			name: "excluded module is not restorable even when declared",
			manifest: &manifest.Manifest{
				ConfigModules:  []string{"apps.git"},
				ExcludeConfigs: []string{"git"},
			},
			moduleID: "apps.git",
			want:     false,
		},
		{
			name:     "short declared id matches qualified module id",
			manifest: &manifest.Manifest{ConfigModules: []string{"git"}},
			moduleID: "apps.git",
			want:     true,
		},
		{
			name:     "nil manifest is not restorable",
			manifest: nil,
			moduleID: "apps.git",
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := configModuleRestorable(tt.manifest, tt.moduleID); got != tt.want {
				t.Errorf("configModuleRestorable = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestRunApply_RestoreModulesAvailable_EmptyWithoutConfigModules is the
// end-of-pipeline guard for the phantom-checkbox defect.
//
// A manifest whose apps match catalog modules but which declares no
// configModules restores nothing, so it must advertise nothing. Before this,
// matching alone populated the list and a client rendered controls that did
// nothing when selected.
func TestRunApply_RestoreModulesAvailable_EmptyWithoutConfigModules(t *testing.T) {
	md := &mockDriver{installed: map[string]bool{
		"Microsoft.VisualStudioCode": true,
		"Git.Git":                    true,
	}}

	catalog := map[string]*modules.Module{
		"apps.vscode": {
			ID:          "apps.vscode",
			DisplayName: "Visual Studio Code",
			Matches:     modules.MatchCriteria{Winget: []string{"Microsoft.VisualStudioCode"}},
			Capture:     &modules.CaptureDef{Files: []modules.CaptureFile{{Source: "a", Dest: "b"}}},
		},
		"apps.git": {
			ID:          "apps.git",
			DisplayName: "Git",
			Matches:     modules.MatchCriteria{Winget: []string{"Git.Git"}},
			Capture:     &modules.CaptureDef{Files: []modules.CaptureFile{{Source: "a", Dest: "b"}}},
		},
	}

	var result *ApplyResult
	withMockDriver(md, func() {
		withMockCatalog(catalog, nil, func() {
			// two-apps.jsonc declares no configModules.
			r, err := RunApply(ApplyFlags{
				Manifest: fixtureManifest("two-apps.jsonc"),
				DryRun:   true,
			})
			if err != nil {
				t.Fatalf("RunApply: %v", err)
			}
			result = r.(*ApplyResult)
		})
	})

	if len(result.RestoreModulesAvailable) != 0 {
		t.Fatalf("both modules match the apps but the manifest carries no config, so none should be offered; got %d: %+v",
			len(result.RestoreModulesAvailable), result.RestoreModulesAvailable)
	}
}
