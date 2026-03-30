// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package modules

import (
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/manifest"
)

// ---------------------------------------------------------------------------
// ExpandConfigModules — FromModule tagging
// ---------------------------------------------------------------------------

// TestExpandConfigModules_SetsFromModule verifies that injected restore entries
// have their FromModule field set to the source module ID.
func TestExpandConfigModules_SetsFromModule(t *testing.T) {
	catalog := map[string]*Module{
		"apps.git": {
			ID:          "apps.git",
			DisplayName: "Git",
			Matches:     MatchCriteria{Winget: []string{"Git.Git"}},
			Restore: []RestoreDef{
				{Type: "copy", Source: "./payload/apps/git/.gitconfig", Target: "%USERPROFILE%\\.gitconfig"},
			},
		},
		"apps.vscode": {
			ID:          "apps.vscode",
			DisplayName: "Visual Studio Code",
			Matches:     MatchCriteria{Winget: []string{"Microsoft.VisualStudioCode"}},
			Restore: []RestoreDef{
				{Type: "copy", Source: "./payload/apps/vscode/settings.json", Target: "%APPDATA%\\Code\\User\\settings.json"},
				{Type: "copy", Source: "./payload/apps/vscode/keybindings.json", Target: "%APPDATA%\\Code\\User\\keybindings.json"},
			},
		},
	}

	mf := &manifest.Manifest{
		ConfigModules: []string{"apps.git", "apps.vscode"},
	}

	err := ExpandConfigModules(mf, catalog)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(mf.Restore) != 3 {
		t.Fatalf("expected 3 restore entries, got %d", len(mf.Restore))
	}

	if mf.Restore[0].FromModule != "apps.git" {
		t.Errorf("entry 0: expected FromModule=%q, got %q", "apps.git", mf.Restore[0].FromModule)
	}
	if mf.Restore[1].FromModule != "apps.vscode" {
		t.Errorf("entry 1: expected FromModule=%q, got %q", "apps.vscode", mf.Restore[1].FromModule)
	}
	if mf.Restore[2].FromModule != "apps.vscode" {
		t.Errorf("entry 2: expected FromModule=%q, got %q", "apps.vscode", mf.Restore[2].FromModule)
	}
}

// ---------------------------------------------------------------------------
// ExpandConfigModules — excludeConfigs filtering
// ---------------------------------------------------------------------------

// TestExpandConfigModules_ExcludeConfigsSkipsMatchingModule verifies that a
// module listed in excludeConfigs is not expanded into the manifest.
func TestExpandConfigModules_ExcludeConfigsSkipsMatchingModule(t *testing.T) {
	catalog := map[string]*Module{
		"apps.git": {
			ID:      "apps.git",
			Matches: MatchCriteria{Winget: []string{"Git.Git"}},
			Restore: []RestoreDef{
				{Type: "copy", Source: "a", Target: "b"},
			},
		},
		"apps.vscode": {
			ID:      "apps.vscode",
			Matches: MatchCriteria{Winget: []string{"Microsoft.VisualStudioCode"}},
			Restore: []RestoreDef{
				{Type: "copy", Source: "c", Target: "d"},
			},
		},
	}

	mf := &manifest.Manifest{
		ConfigModules:  []string{"apps.git", "apps.vscode"},
		ExcludeConfigs: []string{"apps.vscode"},
	}

	err := ExpandConfigModules(mf, catalog)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Only apps.git entries should be injected.
	if len(mf.Restore) != 1 {
		t.Fatalf("expected 1 restore entry (vscode excluded), got %d", len(mf.Restore))
	}
	if mf.Restore[0].FromModule != "apps.git" {
		t.Errorf("expected FromModule=apps.git, got %q", mf.Restore[0].FromModule)
	}
}

// TestExpandConfigModules_ExcludeConfigsShortIDMatchesQualified verifies that
// a short ID in excludeConfigs (e.g. "vscode") excludes the qualified module
// ("apps.vscode").
func TestExpandConfigModules_ExcludeConfigsShortIDMatchesQualified(t *testing.T) {
	catalog := map[string]*Module{
		"apps.vscode": {
			ID:      "apps.vscode",
			Matches: MatchCriteria{Winget: []string{"Microsoft.VisualStudioCode"}},
			Restore: []RestoreDef{
				{Type: "copy", Source: "a", Target: "b"},
			},
		},
	}

	mf := &manifest.Manifest{
		ConfigModules:  []string{"apps.vscode"},
		ExcludeConfigs: []string{"vscode"}, // short ID
	}

	err := ExpandConfigModules(mf, catalog)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(mf.Restore) != 0 {
		t.Errorf("expected 0 restore entries (vscode excluded by short ID), got %d", len(mf.Restore))
	}
}

// TestExpandConfigModules_NonExcludedModulesStillExpand verifies that only the
// listed excluded module is skipped, and others still expand normally.
func TestExpandConfigModules_NonExcludedModulesStillExpand(t *testing.T) {
	catalog := map[string]*Module{
		"apps.git": {
			ID:      "apps.git",
			Matches: MatchCriteria{Winget: []string{"Git.Git"}},
			Restore: []RestoreDef{
				{Type: "copy", Source: "git-src", Target: "git-tgt"},
			},
		},
		"apps.vscode": {
			ID:      "apps.vscode",
			Matches: MatchCriteria{Winget: []string{"Microsoft.VisualStudioCode"}},
			Restore: []RestoreDef{
				{Type: "copy", Source: "vscode-src", Target: "vscode-tgt"},
			},
		},
	}

	mf := &manifest.Manifest{
		ConfigModules:  []string{"apps.git", "apps.vscode"},
		ExcludeConfigs: []string{"apps.git"},
	}

	err := ExpandConfigModules(mf, catalog)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Only vscode should be expanded.
	if len(mf.Restore) != 1 {
		t.Fatalf("expected 1 restore entry (only vscode), got %d", len(mf.Restore))
	}
	if mf.Restore[0].FromModule != "apps.vscode" {
		t.Errorf("expected FromModule=apps.vscode, got %q", mf.Restore[0].FromModule)
	}
}

// TestExpandConfigModules_EmptyExcludeConfigsHasNoEffect verifies that an
// empty excludeConfigs list does not affect expansion.
func TestExpandConfigModules_EmptyExcludeConfigsHasNoEffect(t *testing.T) {
	catalog := map[string]*Module{
		"apps.git": {
			ID:      "apps.git",
			Matches: MatchCriteria{Winget: []string{"Git.Git"}},
			Restore: []RestoreDef{
				{Type: "copy", Source: "a", Target: "b"},
			},
		},
	}

	mf := &manifest.Manifest{
		ConfigModules:  []string{"apps.git"},
		ExcludeConfigs: []string{}, // empty — no effect
	}

	err := ExpandConfigModules(mf, catalog)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(mf.Restore) != 1 {
		t.Errorf("expected 1 restore entry with empty excludeConfigs, got %d", len(mf.Restore))
	}
}

// TestExpandConfigModules_InlineEntriesUntagged verifies that restore entries
// already present in the manifest (not from modules) retain an empty FromModule.
func TestExpandConfigModules_InlineEntriesUntagged(t *testing.T) {
	catalog := map[string]*Module{
		"apps.git": {
			ID:          "apps.git",
			DisplayName: "Git",
			Matches:     MatchCriteria{Winget: []string{"Git.Git"}},
			Restore: []RestoreDef{
				{Type: "copy", Source: "a", Target: "b"},
			},
		},
	}

	mf := &manifest.Manifest{
		ConfigModules: []string{"apps.git"},
		Restore: []manifest.RestoreEntry{
			{Type: "copy", Source: "inline-src", Target: "inline-tgt"},
		},
	}

	err := ExpandConfigModules(mf, catalog)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(mf.Restore) != 2 {
		t.Fatalf("expected 2 restore entries, got %d", len(mf.Restore))
	}

	// First entry is the pre-existing inline one.
	if mf.Restore[0].FromModule != "" {
		t.Errorf("inline entry: expected empty FromModule, got %q", mf.Restore[0].FromModule)
	}
	// Second entry is from the module.
	if mf.Restore[1].FromModule != "apps.git" {
		t.Errorf("module entry: expected FromModule=%q, got %q", "apps.git", mf.Restore[1].FromModule)
	}
}
