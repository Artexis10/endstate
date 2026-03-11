// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package modules

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/manifest"
)

// ---------------------------------------------------------------------------
// LoadCatalog
// ---------------------------------------------------------------------------

func TestLoadCatalog_ValidModules(t *testing.T) {
	catalog, err := LoadCatalog("testdata")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// valid-module and no-capture-module should load; invalid-module should be skipped.
	if _, ok := catalog["apps.test-app"]; !ok {
		t.Error("expected catalog to contain 'apps.test-app'")
	}
	if _, ok := catalog["apps.no-capture"]; !ok {
		t.Error("expected catalog to contain 'apps.no-capture'")
	}

	// Verify valid module fields.
	mod := catalog["apps.test-app"]
	if mod.DisplayName != "Test Application" {
		t.Errorf("DisplayName = %q, want %q", mod.DisplayName, "Test Application")
	}
	if mod.Sensitivity != "low" {
		t.Errorf("Sensitivity = %q, want %q", mod.Sensitivity, "low")
	}
	if len(mod.Matches.Winget) != 1 || mod.Matches.Winget[0] != "TestVendor.TestApp" {
		t.Errorf("Matches.Winget = %v, want [TestVendor.TestApp]", mod.Matches.Winget)
	}
	if len(mod.Restore) != 2 {
		t.Errorf("len(Restore) = %d, want 2", len(mod.Restore))
	}
	if len(mod.Verify) != 1 {
		t.Errorf("len(Verify) = %d, want 1", len(mod.Verify))
	}
	if mod.Capture == nil {
		t.Fatal("Capture is nil, want non-nil")
	}
	if len(mod.Capture.Files) != 2 {
		t.Errorf("len(Capture.Files) = %d, want 2", len(mod.Capture.Files))
	}
	if len(mod.Capture.ExcludeGlobs) != 2 {
		t.Errorf("len(Capture.ExcludeGlobs) = %d, want 2", len(mod.Capture.ExcludeGlobs))
	}
}

func TestLoadCatalog_InvalidModuleSkipped(t *testing.T) {
	catalog, err := LoadCatalog("testdata")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The invalid-module directory should be skipped entirely due to JSON parse error.
	// We should still have the valid modules.
	if len(catalog) < 2 {
		t.Errorf("expected at least 2 modules in catalog, got %d", len(catalog))
	}
}

func TestLoadCatalog_MissingDirectory(t *testing.T) {
	catalog, err := LoadCatalog("testdata/nonexistent-dir")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(catalog) != 0 {
		t.Errorf("expected empty catalog for missing directory, got %d entries", len(catalog))
	}
}

func TestLoadCatalog_EmptyDirectory(t *testing.T) {
	dir := t.TempDir()
	catalog, err := LoadCatalog(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(catalog) != 0 {
		t.Errorf("expected empty catalog for empty directory, got %d entries", len(catalog))
	}
}

func TestLoadCatalog_MissingRequiredFields(t *testing.T) {
	dir := t.TempDir()

	// Module missing displayName.
	modDir := filepath.Join(dir, "bad-module")
	if err := os.MkdirAll(modDir, 0755); err != nil {
		t.Fatal(err)
	}
	content := `{"id": "test", "matches": {"winget": ["X.Y"]}}`
	if err := os.WriteFile(filepath.Join(modDir, "module.jsonc"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	catalog, err := LoadCatalog(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(catalog) != 0 {
		t.Errorf("expected module with missing displayName to be skipped, got %d entries", len(catalog))
	}
}

func TestLoadCatalog_DuplicateIDs(t *testing.T) {
	dir := t.TempDir()

	// Create two modules with the same ID.
	for _, name := range []string{"mod-a", "mod-b"} {
		modDir := filepath.Join(dir, name)
		if err := os.MkdirAll(modDir, 0755); err != nil {
			t.Fatal(err)
		}
		content := `{"id":"same-id","displayName":"Dup","sensitivity":"low","matches":{"winget":["X.Y"]}}`
		if err := os.WriteFile(filepath.Join(modDir, "module.jsonc"), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	catalog, err := LoadCatalog(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Only one should survive; the duplicate is skipped.
	if len(catalog) != 1 {
		t.Errorf("expected 1 module after dedup, got %d", len(catalog))
	}
}

// ---------------------------------------------------------------------------
// MatchModulesForApps
// ---------------------------------------------------------------------------

func TestMatchModulesForApps_WingetMatch(t *testing.T) {
	catalog := map[string]*Module{
		"apps.test": {
			ID:          "apps.test",
			DisplayName: "Test",
			Matches: MatchCriteria{
				Winget: []string{"TestVendor.TestApp"},
			},
			Capture: &CaptureDef{
				Files: []CaptureFile{
					{Source: "%APPDATA%\\test", Dest: "apps/test/config"},
				},
			},
		},
	}

	apps := []manifest.App{
		{
			ID:   "test-app",
			Refs: map[string]string{"windows": "TestVendor.TestApp"},
		},
	}

	matched := MatchModulesForApps(catalog, apps)
	if len(matched) != 1 {
		t.Fatalf("expected 1 match, got %d", len(matched))
	}
	if matched[0].ID != "apps.test" {
		t.Errorf("matched[0].ID = %q, want %q", matched[0].ID, "apps.test")
	}
}

func TestMatchModulesForApps_NoMatch(t *testing.T) {
	catalog := map[string]*Module{
		"apps.test": {
			ID:          "apps.test",
			DisplayName: "Test",
			Matches: MatchCriteria{
				Winget: []string{"TestVendor.TestApp"},
			},
			Capture: &CaptureDef{
				Files: []CaptureFile{
					{Source: "%APPDATA%\\test", Dest: "apps/test/config"},
				},
			},
		},
	}

	apps := []manifest.App{
		{
			ID:   "other-app",
			Refs: map[string]string{"windows": "Other.App"},
		},
	}

	matched := MatchModulesForApps(catalog, apps)
	if len(matched) != 0 {
		t.Errorf("expected 0 matches, got %d", len(matched))
	}
}

func TestMatchModulesForApps_NoCaptureSkipped(t *testing.T) {
	catalog := map[string]*Module{
		"apps.no-capture": {
			ID:          "apps.no-capture",
			DisplayName: "No Capture",
			Matches: MatchCriteria{
				Winget: []string{"Vendor.App"},
			},
			// No Capture section — should not be matched.
		},
	}

	apps := []manifest.App{
		{
			ID:   "app",
			Refs: map[string]string{"windows": "Vendor.App"},
		},
	}

	matched := MatchModulesForApps(catalog, apps)
	if len(matched) != 0 {
		t.Errorf("expected 0 matches for module without capture, got %d", len(matched))
	}
}

func TestMatchModulesForApps_EmptyCatalog(t *testing.T) {
	matched := MatchModulesForApps(map[string]*Module{}, []manifest.App{{ID: "x"}})
	if len(matched) != 0 {
		t.Errorf("expected 0 matches for empty catalog, got %d", len(matched))
	}
}

func TestMatchModulesForApps_EmptyApps(t *testing.T) {
	catalog := map[string]*Module{
		"apps.test": {
			ID: "apps.test",
			Matches: MatchCriteria{
				Winget: []string{"X.Y"},
			},
			Capture: &CaptureDef{Files: []CaptureFile{{Source: "s", Dest: "d"}}},
		},
	}
	matched := MatchModulesForApps(catalog, nil)
	if len(matched) != 0 {
		t.Errorf("expected 0 matches for nil apps, got %d", len(matched))
	}
}

func TestMatchModulesForApps_PathExists(t *testing.T) {
	// Create a temporary file to match against.
	dir := t.TempDir()
	testFile := filepath.Join(dir, "testapp.exe")
	if err := os.WriteFile(testFile, []byte("fake"), 0644); err != nil {
		t.Fatal(err)
	}

	catalog := map[string]*Module{
		"apps.pathtest": {
			ID:          "apps.pathtest",
			DisplayName: "Path Test",
			Matches: MatchCriteria{
				PathExists: []string{testFile},
			},
			Capture: &CaptureDef{
				Files: []CaptureFile{
					{Source: testFile, Dest: "apps/pathtest/config"},
				},
			},
		},
	}

	// No winget match, but pathExists should match.
	apps := []manifest.App{
		{
			ID:   "some-app",
			Refs: map[string]string{"windows": "Unrelated.App"},
		},
	}

	matched := MatchModulesForApps(catalog, apps)
	if len(matched) != 1 {
		t.Fatalf("expected 1 match via pathExists, got %d", len(matched))
	}
	if matched[0].ID != "apps.pathtest" {
		t.Errorf("matched[0].ID = %q, want %q", matched[0].ID, "apps.pathtest")
	}
}

// ---------------------------------------------------------------------------
// ExpandConfigModules
// ---------------------------------------------------------------------------

func TestExpandConfigModules_InjectsEntries(t *testing.T) {
	catalog := map[string]*Module{
		"apps.test": {
			ID:          "apps.test",
			DisplayName: "Test",
			Restore: []RestoreDef{
				{Type: "copy", Source: "./payload/apps/test/config.json", Target: "%APPDATA%\\Test\\config.json", Backup: true},
				{Type: "copy", Source: "./payload/apps/test/settings.ini", Target: "%APPDATA%\\Test\\settings.ini"},
			},
			Verify: []VerifyDef{
				{Type: "command-exists", Command: "testapp"},
			},
		},
	}

	m := &manifest.Manifest{
		ConfigModules: []string{"apps.test"},
		Restore:       []manifest.RestoreEntry{},
		Verify:        []manifest.VerifyEntry{},
	}

	err := ExpandConfigModules(m, catalog)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(m.Restore) != 2 {
		t.Errorf("len(Restore) = %d, want 2", len(m.Restore))
	}
	if len(m.Verify) != 1 {
		t.Errorf("len(Verify) = %d, want 1", len(m.Verify))
	}

	// Verify source paths are preserved as-is.
	if m.Restore[0].Source != "./payload/apps/test/config.json" {
		t.Errorf("Restore[0].Source = %q, want %q", m.Restore[0].Source, "./payload/apps/test/config.json")
	}
	if m.Restore[0].Backup != true {
		t.Error("Restore[0].Backup = false, want true")
	}
}

func TestExpandConfigModules_AppendsToExisting(t *testing.T) {
	catalog := map[string]*Module{
		"apps.test": {
			ID:          "apps.test",
			DisplayName: "Test",
			Restore: []RestoreDef{
				{Type: "copy", Source: "s1", Target: "t1"},
			},
			Verify: []VerifyDef{
				{Type: "file-exists", Path: "/tmp/test"},
			},
		},
	}

	m := &manifest.Manifest{
		ConfigModules: []string{"apps.test"},
		Restore: []manifest.RestoreEntry{
			{Type: "copy", Source: "existing-source", Target: "existing-target"},
		},
		Verify: []manifest.VerifyEntry{
			{Type: "command-exists", Command: "existing"},
		},
	}

	err := ExpandConfigModules(m, catalog)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(m.Restore) != 2 {
		t.Errorf("len(Restore) = %d, want 2 (1 existing + 1 injected)", len(m.Restore))
	}
	if len(m.Verify) != 2 {
		t.Errorf("len(Verify) = %d, want 2 (1 existing + 1 injected)", len(m.Verify))
	}

	// First entry should be the existing one.
	if m.Restore[0].Source != "existing-source" {
		t.Errorf("Restore[0].Source = %q, want existing entry first", m.Restore[0].Source)
	}
}

func TestExpandConfigModules_UnknownModuleSkipped(t *testing.T) {
	catalog := map[string]*Module{}

	m := &manifest.Manifest{
		ConfigModules: []string{"apps.nonexistent"},
	}

	err := ExpandConfigModules(m, catalog)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Unknown modules should be silently skipped (with stderr warning).
	if len(m.Restore) != 0 {
		t.Errorf("len(Restore) = %d, want 0", len(m.Restore))
	}
}

func TestExpandConfigModules_EmptyConfigModules(t *testing.T) {
	m := &manifest.Manifest{}
	err := ExpandConfigModules(m, map[string]*Module{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
