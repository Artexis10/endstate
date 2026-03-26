// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package bundle

import (
	"archive/zip"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
)

// ---------------------------------------------------------------------------
// IsBundle
// ---------------------------------------------------------------------------

func TestIsBundle(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"profile.zip", true},
		{"PROFILE.ZIP", true},
		{"profile.Zip", true},
		{"profile.jsonc", false},
		{"profile", false},
		{"path/to/bundle.zip", true},
		{"path/to/manifest.jsonc", false},
		{"", false},
	}

	for _, tc := range tests {
		got := IsBundle(tc.path)
		if got != tc.want {
			t.Errorf("IsBundle(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// rewriteSourcePath
// ---------------------------------------------------------------------------

func TestRewriteSourcePath(t *testing.T) {
	tests := []struct {
		source        string
		moduleDirName string
		want          string
	}{
		{
			source:        "./payload/apps/vscode/settings.json",
			moduleDirName: "vscode",
			want:          "./configs/vscode/settings.json",
		},
		{
			source:        "./payload/apps/git/.gitconfig",
			moduleDirName: "git",
			want:          "./configs/git/.gitconfig",
		},
		{
			source:        "./payload/apps/vscode/snippets",
			moduleDirName: "vscode",
			want:          "./configs/vscode/snippets",
		},
		{
			source:        "%APPDATA%\\Code\\User\\settings.json",
			moduleDirName: "vscode",
			want:          "%APPDATA%\\Code\\User\\settings.json", // Not a payload path — unchanged.
		},
		{
			source:        "relative/path/file.txt",
			moduleDirName: "test",
			want:          "relative/path/file.txt", // Not a payload path — unchanged.
		},
	}

	for _, tc := range tests {
		got := rewriteSourcePath(tc.source, tc.moduleDirName)
		if got != tc.want {
			t.Errorf("rewriteSourcePath(%q, %q) = %q, want %q", tc.source, tc.moduleDirName, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// CreateBundle + ExtractBundle round-trip
// ---------------------------------------------------------------------------

func TestCreateBundle_RoundTrip(t *testing.T) {
	dir := t.TempDir()

	// Create a fake manifest.
	manifestContent := `{
  "version": 1,
  "name": "test-profile",
  "apps": [
    {"id": "test-app", "refs": {"windows": "TestVendor.TestApp"}}
  ]
}`
	manifestPath := filepath.Join(dir, "manifest.jsonc")
	if err := os.WriteFile(manifestPath, []byte(manifestContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Create fake config files that the module will capture.
	configDir := filepath.Join(dir, "fake-config")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatal(err)
	}
	configFile := filepath.Join(configDir, "settings.json")
	if err := os.WriteFile(configFile, []byte(`{"key":"value"}`), 0644); err != nil {
		t.Fatal(err)
	}

	// Create a matched module that points to the fake config.
	matchedModules := []*modules.Module{
		{
			ID:          "apps.test-app",
			DisplayName: "Test App",
			Matches: modules.MatchCriteria{
				Winget: []string{"TestVendor.TestApp"},
			},
			Restore: []modules.RestoreDef{
				{
					Type:   "copy",
					Source: "./payload/apps/test-app/settings.json",
					Target: "%APPDATA%\\TestApp\\settings.json",
					Backup: true,
				},
			},
			Capture: &modules.CaptureDef{
				Files: []modules.CaptureFile{
					{Source: configFile, Dest: "apps/test-app/settings.json"},
				},
			},
		},
	}

	outputZip := filepath.Join(dir, "output.zip")
	err := CreateBundle(manifestPath, matchedModules, outputZip, "1.0.0-test")
	if err != nil {
		t.Fatalf("CreateBundle failed: %v", err)
	}

	// Verify the zip was created.
	if _, err := os.Stat(outputZip); err != nil {
		t.Fatalf("output zip not found: %v", err)
	}

	// Verify zip contents.
	zipReader, err := zip.OpenReader(outputZip)
	if err != nil {
		t.Fatalf("failed to open zip: %v", err)
	}
	defer zipReader.Close()

	fileNames := make(map[string]bool)
	for _, f := range zipReader.File {
		fileNames[f.Name] = true
	}

	if !fileNames["manifest.jsonc"] {
		t.Error("zip missing manifest.jsonc")
	}
	if !fileNames["metadata.json"] {
		t.Error("zip missing metadata.json")
	}

	// Check that configs directory was included.
	hasConfigs := false
	for name := range fileNames {
		if strings.HasPrefix(name, "configs/") {
			hasConfigs = true
			break
		}
	}
	if !hasConfigs {
		t.Error("zip missing configs/ directory")
	}

	// --- Extract and verify round-trip ---
	extractedManifestPath, err := ExtractBundle(outputZip)
	if err != nil {
		t.Fatalf("ExtractBundle failed: %v", err)
	}
	extractedDir := filepath.Dir(extractedManifestPath)
	defer os.RemoveAll(extractedDir)

	// Verify extracted manifest exists and has rewritten paths.
	data, err := os.ReadFile(extractedManifestPath)
	if err != nil {
		t.Fatalf("failed to read extracted manifest: %v", err)
	}

	var m manifest.Manifest
	if err := json.Unmarshal(manifest.StripJsoncComments(data), &m); err != nil {
		t.Fatalf("failed to parse extracted manifest: %v", err)
	}

	if m.Name != "test-profile" {
		t.Errorf("manifest.Name = %q, want %q", m.Name, "test-profile")
	}

	// Check path rewriting in restore entries.
	if len(m.Restore) == 0 {
		t.Fatal("expected restore entries in bundle manifest")
	}
	if m.Restore[0].Source != "./configs/test-app/settings.json" {
		t.Errorf("Restore[0].Source = %q, want %q", m.Restore[0].Source, "./configs/test-app/settings.json")
	}

	// Verify metadata.json was extracted.
	metadataPath := filepath.Join(extractedDir, "metadata.json")
	metadataData, err := os.ReadFile(metadataPath)
	if err != nil {
		t.Fatalf("failed to read metadata.json: %v", err)
	}

	var metadata BundleMetadata
	if err := json.Unmarshal(metadataData, &metadata); err != nil {
		t.Fatalf("failed to parse metadata.json: %v", err)
	}

	if metadata.SchemaVersion != "1.0" {
		t.Errorf("metadata.SchemaVersion = %q, want %q", metadata.SchemaVersion, "1.0")
	}
	if metadata.EndstateVersion != "1.0.0-test" {
		t.Errorf("metadata.EndstateVersion = %q, want %q", metadata.EndstateVersion, "1.0.0-test")
	}
	if len(metadata.ConfigModulesIncluded) != 1 || metadata.ConfigModulesIncluded[0] != "test-app" {
		t.Errorf("metadata.ConfigModulesIncluded = %v, want [test-app]", metadata.ConfigModulesIncluded)
	}

	// Verify config file was extracted.
	extractedConfig := filepath.Join(extractedDir, "configs", "test-app", "settings.json")
	if _, err := os.Stat(extractedConfig); err != nil {
		t.Errorf("extracted config file missing: %v", err)
	}
}

func TestCreateBundle_NoModules(t *testing.T) {
	dir := t.TempDir()

	manifestContent := `{"version": 1, "name": "bare", "apps": []}`
	manifestPath := filepath.Join(dir, "manifest.jsonc")
	if err := os.WriteFile(manifestPath, []byte(manifestContent), 0644); err != nil {
		t.Fatal(err)
	}

	outputZip := filepath.Join(dir, "bare.zip")
	err := CreateBundle(manifestPath, nil, outputZip, "1.0.0")
	if err != nil {
		t.Fatalf("CreateBundle with no modules failed: %v", err)
	}

	// Verify manifest and metadata are present.
	zipReader, err := zip.OpenReader(outputZip)
	if err != nil {
		t.Fatalf("failed to open zip: %v", err)
	}
	defer zipReader.Close()

	fileNames := make(map[string]bool)
	for _, f := range zipReader.File {
		fileNames[f.Name] = true
	}

	if !fileNames["manifest.jsonc"] {
		t.Error("zip missing manifest.jsonc")
	}
	if !fileNames["metadata.json"] {
		t.Error("zip missing metadata.json")
	}
}

func TestCreateBundle_AtomicWrite(t *testing.T) {
	dir := t.TempDir()

	manifestContent := `{"version": 1, "name": "atomic", "apps": []}`
	manifestPath := filepath.Join(dir, "manifest.jsonc")
	if err := os.WriteFile(manifestPath, []byte(manifestContent), 0644); err != nil {
		t.Fatal(err)
	}

	outputZip := filepath.Join(dir, "output.zip")
	err := CreateBundle(manifestPath, nil, outputZip, "1.0.0")
	if err != nil {
		t.Fatalf("CreateBundle failed: %v", err)
	}

	// Verify temp file was cleaned up.
	tempZip := outputZip + ".tmp"
	if _, err := os.Stat(tempZip); !os.IsNotExist(err) {
		t.Error("temp zip file was not cleaned up")
	}

	// Verify final file exists.
	if _, err := os.Stat(outputZip); err != nil {
		t.Errorf("output zip not found: %v", err)
	}
}

// ---------------------------------------------------------------------------
// CollectConfigFiles
// ---------------------------------------------------------------------------

func TestCollectConfigFiles_OptionalMissing(t *testing.T) {
	dir := t.TempDir()
	stagingDir := filepath.Join(dir, "staging")
	if err := os.MkdirAll(stagingDir, 0755); err != nil {
		t.Fatal(err)
	}

	mod := &modules.Module{
		ID: "apps.test",
		Capture: &modules.CaptureDef{
			Files: []modules.CaptureFile{
				{Source: filepath.Join(dir, "nonexistent.txt"), Dest: "apps/test/nonexistent.txt", Optional: true},
			},
		},
	}

	collected, err := CollectConfigFiles(mod, stagingDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(collected) != 0 {
		t.Errorf("expected 0 collected files for optional missing, got %d", len(collected))
	}
}

func TestCollectConfigFiles_RequiredMissing(t *testing.T) {
	dir := t.TempDir()
	stagingDir := filepath.Join(dir, "staging")
	if err := os.MkdirAll(stagingDir, 0755); err != nil {
		t.Fatal(err)
	}

	mod := &modules.Module{
		ID: "apps.test",
		Capture: &modules.CaptureDef{
			Files: []modules.CaptureFile{
				{Source: filepath.Join(dir, "nonexistent.txt"), Dest: "apps/test/nonexistent.txt", Optional: false},
			},
		},
	}

	_, err := CollectConfigFiles(mod, stagingDir)
	if err == nil {
		t.Fatal("expected error for required missing file, got nil")
	}
	if !strings.Contains(err.Error(), "missing required file") {
		t.Errorf("error = %q, want it to contain 'missing required file'", err.Error())
	}
}

func TestCollectConfigFiles_ExcludeGlobs(t *testing.T) {
	dir := t.TempDir()
	stagingDir := filepath.Join(dir, "staging")
	if err := os.MkdirAll(stagingDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create a source directory with some files.
	srcDir := filepath.Join(dir, "source")
	if err := os.MkdirAll(filepath.Join(srcDir, "Cache"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "config.json"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "Cache", "data.bin"), []byte("cache"), 0644); err != nil {
		t.Fatal(err)
	}

	mod := &modules.Module{
		ID: "apps.test",
		Capture: &modules.CaptureDef{
			Files: []modules.CaptureFile{
				{Source: srcDir, Dest: "apps/test/source"},
			},
			ExcludeGlobs: []string{"**/Cache/**"},
		},
	}

	collected, err := CollectConfigFiles(mod, stagingDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(collected) != 1 {
		t.Fatalf("expected 1 collected entry, got %d", len(collected))
	}

	// Verify config.json was copied but Cache/ was excluded.
	configPath := filepath.Join(stagingDir, "configs", "test", "source", "config.json")
	if _, err := os.Stat(configPath); err != nil {
		t.Errorf("config.json not copied: %v", err)
	}
	cachePath := filepath.Join(stagingDir, "configs", "test", "source", "Cache", "data.bin")
	if _, err := os.Stat(cachePath); !os.IsNotExist(err) {
		t.Error("Cache/data.bin should have been excluded")
	}
}

func TestCollectConfigFiles_SingleFile(t *testing.T) {
	dir := t.TempDir()
	stagingDir := filepath.Join(dir, "staging")
	if err := os.MkdirAll(stagingDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create source file.
	srcFile := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(srcFile, []byte(`{"setting":true}`), 0644); err != nil {
		t.Fatal(err)
	}

	mod := &modules.Module{
		ID: "apps.myapp",
		Capture: &modules.CaptureDef{
			Files: []modules.CaptureFile{
				{Source: srcFile, Dest: "apps/myapp/settings.json"},
			},
		},
	}

	collected, err := CollectConfigFiles(mod, stagingDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(collected) != 1 {
		t.Fatalf("expected 1 collected file, got %d", len(collected))
	}

	// Verify file was copied.
	destFile := filepath.Join(stagingDir, "configs", "myapp", "settings.json")
	data, err := os.ReadFile(destFile)
	if err != nil {
		t.Fatalf("collected file not found: %v", err)
	}
	if string(data) != `{"setting":true}` {
		t.Errorf("file content = %q, want %q", string(data), `{"setting":true}`)
	}
}

func TestCollectConfigFiles_NoCaptureSection(t *testing.T) {
	mod := &modules.Module{
		ID: "apps.no-capture",
	}

	collected, err := CollectConfigFiles(mod, t.TempDir())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(collected) != 0 {
		t.Errorf("expected 0 collected files for module without capture, got %d", len(collected))
	}
}

// ---------------------------------------------------------------------------
// matchesExcludeGlobs
// ---------------------------------------------------------------------------

func TestMatchesExcludeGlobs(t *testing.T) {
	tests := []struct {
		path    string
		globs   []string
		want    bool
	}{
		{"/path/to/Cache/data.bin", []string{"**/Cache/**"}, true},
		{"/path/to/config.json", []string{"**/Cache/**"}, false},
		{"/path/to/file.log", []string{"*.log"}, true},      // *.log matches file segment
		{"/path/to/debug.log", []string{"*.log"}, true},    // *.log matches any .log file
		{"/path/to/settings.json", []string{"*.log"}, false},
		{"/path/to/file.txt", nil, false},
		{"/path/to/file.txt", []string{}, false},
	}

	for _, tc := range tests {
		got := matchesExcludeGlobs(tc.path, tc.globs)
		if got != tc.want {
			t.Errorf("matchesExcludeGlobs(%q, %v) = %v, want %v", tc.path, tc.globs, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// ExtractBundle - error cases
// ---------------------------------------------------------------------------

func TestExtractBundle_MissingManifest(t *testing.T) {
	dir := t.TempDir()

	// Create a zip without manifest.jsonc.
	zipPath := filepath.Join(dir, "no-manifest.zip")
	zipFile, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(zipFile)
	f, _ := w.Create("metadata.json")
	f.Write([]byte("{}"))
	w.Close()
	zipFile.Close()

	_, err = ExtractBundle(zipPath)
	if err == nil {
		t.Fatal("expected error for zip without manifest, got nil")
	}
	if !strings.Contains(err.Error(), "manifest.jsonc") {
		t.Errorf("error = %q, want it to mention manifest.jsonc", err.Error())
	}
}

func TestExtractBundle_InvalidZip(t *testing.T) {
	dir := t.TempDir()

	notZip := filepath.Join(dir, "not-a-zip.zip")
	if err := os.WriteFile(notZip, []byte("not a zip file"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := ExtractBundle(notZip)
	if err == nil {
		t.Fatal("expected error for invalid zip, got nil")
	}
}

// ===========================================================================
// Additional tests ported from Pester Bundle.Tests.ps1
// ===========================================================================

// ---------------------------------------------------------------------------
// Metadata: empty slices serialize as [] not null
// Pester: Bundle.Metadata - New-CaptureMetadata with no config modules
// ---------------------------------------------------------------------------

func TestCreateBundle_MetadataEmptySlices(t *testing.T) {
	dir := t.TempDir()

	manifestContent := `{"version": 1, "name": "empty-slices", "apps": []}`
	manifestPath := filepath.Join(dir, "manifest.jsonc")
	if err := os.WriteFile(manifestPath, []byte(manifestContent), 0644); err != nil {
		t.Fatal(err)
	}

	outputZip := filepath.Join(dir, "output.zip")
	err := CreateBundle(manifestPath, nil, outputZip, "1.0.0")
	if err != nil {
		t.Fatalf("CreateBundle failed: %v", err)
	}

	// Extract and verify metadata has [] not null for empty slices.
	zipReader, err := zip.OpenReader(outputZip)
	if err != nil {
		t.Fatalf("failed to open zip: %v", err)
	}
	defer zipReader.Close()

	for _, f := range zipReader.File {
		if f.Name != "metadata.json" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("failed to open metadata.json: %v", err)
		}
		defer rc.Close()

		var raw map[string]json.RawMessage
		if err := json.NewDecoder(rc).Decode(&raw); err != nil {
			t.Fatalf("failed to parse metadata.json: %v", err)
		}

		// Verify empty slices serialize as [] not null.
		for _, field := range []string{"configModulesIncluded", "configModulesSkipped", "captureWarnings"} {
			val, ok := raw[field]
			if !ok {
				t.Errorf("metadata missing field %q", field)
				continue
			}
			trimmed := strings.TrimSpace(string(val))
			if trimmed == "null" {
				t.Errorf("metadata.%s serialized as null, want []", field)
			}
			if trimmed != "[]" {
				t.Errorf("metadata.%s = %s, want []", field, trimmed)
			}
		}

		return
	}

	t.Fatal("metadata.json not found in zip")
}

// ---------------------------------------------------------------------------
// Metadata: schemaVersion, machineName, capturedAt present
// Pester: Bundle.Metadata - New-CaptureMetadata required fields
// ---------------------------------------------------------------------------

func TestCreateBundle_MetadataRequiredFields(t *testing.T) {
	dir := t.TempDir()

	manifestContent := `{"version": 1, "name": "meta-fields", "apps": []}`
	manifestPath := filepath.Join(dir, "manifest.jsonc")
	if err := os.WriteFile(manifestPath, []byte(manifestContent), 0644); err != nil {
		t.Fatal(err)
	}

	outputZip := filepath.Join(dir, "output.zip")
	err := CreateBundle(manifestPath, nil, outputZip, "2.0.0-test")
	if err != nil {
		t.Fatalf("CreateBundle failed: %v", err)
	}

	zipReader, err := zip.OpenReader(outputZip)
	if err != nil {
		t.Fatalf("failed to open zip: %v", err)
	}
	defer zipReader.Close()

	for _, f := range zipReader.File {
		if f.Name != "metadata.json" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("failed to open metadata.json: %v", err)
		}
		defer rc.Close()

		var metadata BundleMetadata
		if err := json.NewDecoder(rc).Decode(&metadata); err != nil {
			t.Fatalf("failed to parse metadata.json: %v", err)
		}

		if metadata.SchemaVersion != "1.0" {
			t.Errorf("schemaVersion = %q, want %q", metadata.SchemaVersion, "1.0")
		}
		if metadata.CapturedAt == "" {
			t.Error("capturedAt is empty")
		}
		if metadata.MachineName == "" {
			t.Error("machineName is empty")
		}
		if metadata.EndstateVersion != "2.0.0-test" {
			t.Errorf("endstateVersion = %q, want %q", metadata.EndstateVersion, "2.0.0-test")
		}

		return
	}

	t.Fatal("metadata.json not found in zip")
}

// ---------------------------------------------------------------------------
// Metadata: includes config module lists
// Pester: Bundle.Metadata - configModulesIncluded / configModulesSkipped
// ---------------------------------------------------------------------------

func TestCreateBundle_MetadataIncludesModuleLists(t *testing.T) {
	dir := t.TempDir()

	manifestContent := `{"version": 1, "name": "mod-lists", "apps": []}`
	manifestPath := filepath.Join(dir, "manifest.jsonc")
	if err := os.WriteFile(manifestPath, []byte(manifestContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Create source file for one module.
	srcFile := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(srcFile, []byte(`{}`), 0644); err != nil {
		t.Fatal(err)
	}

	matchedModules := []*modules.Module{
		{
			ID:          "apps.included-app",
			DisplayName: "Included App",
			Matches:     modules.MatchCriteria{Winget: []string{"Included.App"}},
			Capture: &modules.CaptureDef{
				Files: []modules.CaptureFile{
					{Source: srcFile, Dest: "apps/included-app/settings.json"},
				},
			},
		},
		{
			ID:          "apps.skipped-app",
			DisplayName: "Skipped App",
			Matches:     modules.MatchCriteria{Winget: []string{"Skipped.App"}},
			Capture: &modules.CaptureDef{
				Files: []modules.CaptureFile{
					// Non-existent optional file — will be skipped.
					{Source: filepath.Join(dir, "nonexistent.txt"), Dest: "apps/skipped-app/x.txt", Optional: true},
				},
			},
		},
	}

	outputZip := filepath.Join(dir, "output.zip")
	err := CreateBundle(manifestPath, matchedModules, outputZip, "1.0.0")
	if err != nil {
		t.Fatalf("CreateBundle failed: %v", err)
	}

	zipReader, err := zip.OpenReader(outputZip)
	if err != nil {
		t.Fatalf("failed to open zip: %v", err)
	}
	defer zipReader.Close()

	for _, f := range zipReader.File {
		if f.Name != "metadata.json" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("failed to open metadata.json: %v", err)
		}
		defer rc.Close()

		var metadata BundleMetadata
		if err := json.NewDecoder(rc).Decode(&metadata); err != nil {
			t.Fatalf("failed to parse metadata.json: %v", err)
		}

		// included-app should be in configModulesIncluded.
		foundIncluded := false
		for _, m := range metadata.ConfigModulesIncluded {
			if m == "included-app" {
				foundIncluded = true
			}
		}
		if !foundIncluded {
			t.Errorf("configModulesIncluded = %v, want to contain 'included-app'", metadata.ConfigModulesIncluded)
		}

		// skipped-app should be in configModulesSkipped.
		foundSkipped := false
		for _, m := range metadata.ConfigModulesSkipped {
			if m == "skipped-app" {
				foundSkipped = true
			}
		}
		if !foundSkipped {
			t.Errorf("configModulesSkipped = %v, want to contain 'skipped-app'", metadata.ConfigModulesSkipped)
		}

		return
	}

	t.Fatal("metadata.json not found in zip")
}

// ---------------------------------------------------------------------------
// Preserve all restore entry fields through bundle round-trip
// Pester: Bundle.ZipCreation - Should preserve all restore entry fields
// ---------------------------------------------------------------------------

func TestCreateBundle_PreservesAllRestoreFields(t *testing.T) {
	dir := t.TempDir()

	manifestContent := `{"version": 1, "name": "field-test", "apps": []}`
	manifestPath := filepath.Join(dir, "manifest.jsonc")
	if err := os.WriteFile(manifestPath, []byte(manifestContent), 0644); err != nil {
		t.Fatal(err)
	}

	srcFile := filepath.Join(dir, "app.ini")
	if err := os.WriteFile(srcFile, []byte("content"), 0644); err != nil {
		t.Fatal(err)
	}

	matchedModules := []*modules.Module{
		{
			ID:          "apps.field-test",
			DisplayName: "Field Test",
			Matches:     modules.MatchCriteria{Winget: []string{"Field.Test"}},
			Restore: []modules.RestoreDef{
				{
					Type:     "merge-ini",
					Source:   "./payload/apps/field-test/app.ini",
					Target:   "C:\\FieldTest\\app.ini",
					Backup:   true,
					Optional: false,
					Exclude:  []string{"Section.Key"},
				},
			},
			Capture: &modules.CaptureDef{
				Files: []modules.CaptureFile{
					{Source: srcFile, Dest: "apps/field-test/app.ini"},
				},
			},
		},
	}

	outputZip := filepath.Join(dir, "output.zip")
	err := CreateBundle(manifestPath, matchedModules, outputZip, "1.0.0")
	if err != nil {
		t.Fatalf("CreateBundle failed: %v", err)
	}

	// Extract and verify all restore fields.
	extractedManifestPath, err := ExtractBundle(outputZip)
	if err != nil {
		t.Fatalf("ExtractBundle failed: %v", err)
	}
	defer os.RemoveAll(filepath.Dir(extractedManifestPath))

	data, err := os.ReadFile(extractedManifestPath)
	if err != nil {
		t.Fatalf("failed to read extracted manifest: %v", err)
	}

	var m manifest.Manifest
	if err := json.Unmarshal(manifest.StripJsoncComments(data), &m); err != nil {
		t.Fatalf("failed to parse extracted manifest: %v", err)
	}

	if len(m.Restore) != 1 {
		t.Fatalf("expected 1 restore entry, got %d", len(m.Restore))
	}

	r := m.Restore[0]
	if r.Type != "merge-ini" {
		t.Errorf("restore.type = %q, want %q", r.Type, "merge-ini")
	}
	if r.Source != "./configs/field-test/app.ini" {
		t.Errorf("restore.source = %q, want %q", r.Source, "./configs/field-test/app.ini")
	}
	if r.Target != "C:\\FieldTest\\app.ini" {
		t.Errorf("restore.target = %q, want %q", r.Target, "C:\\FieldTest\\app.ini")
	}
	if !r.Backup {
		t.Error("restore.backup should be true")
	}
	if r.Optional {
		t.Error("restore.optional should be false")
	}
	if len(r.Exclude) != 1 || r.Exclude[0] != "Section.Key" {
		t.Errorf("restore.exclude = %v, want [Section.Key]", r.Exclude)
	}
}

// ---------------------------------------------------------------------------
// Skipped modules should NOT inject restore entries
// Pester: Bundle.ZipCreation - Should not inject restore entries for skipped modules
// ---------------------------------------------------------------------------

func TestCreateBundle_NoRestoreEntriesForSkippedModules(t *testing.T) {
	dir := t.TempDir()

	manifestContent := `{"version": 1, "name": "skip-test", "apps": [], "restore": []}`
	manifestPath := filepath.Join(dir, "manifest.jsonc")
	if err := os.WriteFile(manifestPath, []byte(manifestContent), 0644); err != nil {
		t.Fatal(err)
	}

	matchedModules := []*modules.Module{
		{
			ID:          "apps.missing-app",
			DisplayName: "Missing App",
			Matches:     modules.MatchCriteria{Winget: []string{"Missing.App"}},
			Restore: []modules.RestoreDef{
				{
					Type:   "copy",
					Source: "./configs/missing-app/missing.cfg",
					Target: "C:\\MissingApp\\missing.cfg",
					Backup: true,
				},
			},
			Capture: &modules.CaptureDef{
				Files: []modules.CaptureFile{
					// Non-existent optional file — module will be skipped.
					{Source: filepath.Join(dir, "nonexistent.cfg"), Dest: "apps/missing-app/missing.cfg", Optional: true},
				},
			},
		},
	}

	outputZip := filepath.Join(dir, "output.zip")
	err := CreateBundle(manifestPath, matchedModules, outputZip, "1.0.0")
	if err != nil {
		t.Fatalf("CreateBundle failed: %v", err)
	}

	// Extract and verify manifest has no restore entries.
	extractedManifestPath, err := ExtractBundle(outputZip)
	if err != nil {
		t.Fatalf("ExtractBundle failed: %v", err)
	}
	defer os.RemoveAll(filepath.Dir(extractedManifestPath))

	data, err := os.ReadFile(extractedManifestPath)
	if err != nil {
		t.Fatalf("failed to read extracted manifest: %v", err)
	}

	var m manifest.Manifest
	if err := json.Unmarshal(manifest.StripJsoncComments(data), &m); err != nil {
		t.Fatalf("failed to parse extracted manifest: %v", err)
	}

	if len(m.Restore) != 0 {
		t.Errorf("expected 0 restore entries for skipped modules, got %d", len(m.Restore))
	}
}

// ---------------------------------------------------------------------------
// ExcludeGlobs: VEN_* pattern matching
// Pester: Bundle.ExcludeGlobs - VEN_* prefixed directory
// ---------------------------------------------------------------------------

func TestMatchesExcludeGlobs_VENPrefix(t *testing.T) {
	tests := []struct {
		path  string
		globs []string
		want  bool
	}{
		{"C:/profiles/VEN_10DE/DEV.cfg", []string{"**/VEN_*"}, true},
		{"C:/profiles/Global.cfg", []string{"**/VEN_*"}, false},
		{"C:/app/settings.json", []string{"**/VEN_*"}, false},
	}

	for _, tc := range tests {
		got := matchesExcludeGlobs(tc.path, tc.globs)
		if got != tc.want {
			t.Errorf("matchesExcludeGlobs(%q, %v) = %v, want %v", tc.path, tc.globs, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// ExcludeGlobs: directory capture prunes VEN_* subdirectories
// Pester: Bundle.ExcludeGlobs - Should prune excluded files from directory capture
// ---------------------------------------------------------------------------

func TestCollectConfigFiles_ExcludeGlobs_VENPrefix(t *testing.T) {
	dir := t.TempDir()
	stagingDir := filepath.Join(dir, "staging")
	if err := os.MkdirAll(stagingDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create source directory with Profiles/VEN_10DE and Profiles/Global.cfg.
	profilesDir := filepath.Join(dir, "source", "Profiles")
	venDir := filepath.Join(profilesDir, "VEN_10DE")
	if err := os.MkdirAll(venDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(venDir, "DEV_2684.cfg"), []byte("gpu-specific"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(profilesDir, "Global.cfg"), []byte("global-settings"), 0644); err != nil {
		t.Fatal(err)
	}

	mod := &modules.Module{
		ID: "apps.test-excl",
		Capture: &modules.CaptureDef{
			Files: []modules.CaptureFile{
				{Source: profilesDir, Dest: "apps/test-excl/Profiles"},
			},
			ExcludeGlobs: []string{"**/VEN_*"},
		},
	}

	collected, err := CollectConfigFiles(mod, stagingDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(collected) != 1 {
		t.Fatalf("expected 1 collected entry, got %d", len(collected))
	}

	// Global.cfg should be captured.
	globalFile := filepath.Join(stagingDir, "configs", "test-excl", "Profiles", "Global.cfg")
	if _, err := os.Stat(globalFile); err != nil {
		t.Error("Global.cfg should have been captured")
	}

	// VEN_10DE directory should have been pruned.
	venDestDir := filepath.Join(stagingDir, "configs", "test-excl", "Profiles", "VEN_10DE")
	if _, err := os.Stat(venDestDir); !os.IsNotExist(err) {
		t.Error("VEN_10DE directory should have been excluded")
	}
}

// ---------------------------------------------------------------------------
// CollectConfigFiles: strips "apps." prefix for directory name
// Pester: Bundle.ConfigCollection - Should strip 'apps.' prefix from module ID for directory name
// ---------------------------------------------------------------------------

func TestCollectConfigFiles_StripsAppsPrefix(t *testing.T) {
	dir := t.TempDir()
	stagingDir := filepath.Join(dir, "staging")
	if err := os.MkdirAll(stagingDir, 0755); err != nil {
		t.Fatal(err)
	}

	srcFile := filepath.Join(dir, "config.json")
	if err := os.WriteFile(srcFile, []byte(`{}`), 0644); err != nil {
		t.Fatal(err)
	}

	mod := &modules.Module{
		ID: "apps.my-app",
		Capture: &modules.CaptureDef{
			Files: []modules.CaptureFile{
				{Source: srcFile, Dest: "apps/my-app/config.json"},
			},
		},
	}

	_, err := CollectConfigFiles(mod, stagingDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should use "my-app" not "apps.my-app" as directory name.
	destFile := filepath.Join(stagingDir, "configs", "my-app", "config.json")
	if _, err := os.Stat(destFile); err != nil {
		t.Errorf("expected file at configs/my-app/config.json, got error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// CollectConfigFiles: multiple files for a single module
// Pester: Bundle.ConfigModules - Should track per-module file paths
// ---------------------------------------------------------------------------

func TestCollectConfigFiles_MultipleFiles(t *testing.T) {
	dir := t.TempDir()
	stagingDir := filepath.Join(dir, "staging")
	if err := os.MkdirAll(stagingDir, 0755); err != nil {
		t.Fatal(err)
	}

	file1 := filepath.Join(dir, "settings.json")
	file2 := filepath.Join(dir, "keybindings.json")
	if err := os.WriteFile(file1, []byte(`{}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file2, []byte(`{}`), 0644); err != nil {
		t.Fatal(err)
	}

	mod := &modules.Module{
		ID: "apps.path-app",
		Capture: &modules.CaptureDef{
			Files: []modules.CaptureFile{
				{Source: file1, Dest: "apps/path-app/settings.json"},
				{Source: file2, Dest: "apps/path-app/keybindings.json"},
			},
		},
	}

	collected, err := CollectConfigFiles(mod, stagingDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(collected) != 2 {
		t.Fatalf("expected 2 collected files, got %d", len(collected))
	}

	// Verify both files were copied and paths are correct.
	for _, p := range collected {
		if !strings.HasPrefix(p, "configs/path-app/") {
			t.Errorf("collected path %q does not start with configs/path-app/", p)
		}
	}

	// Verify actual files exist.
	dest1 := filepath.Join(stagingDir, "configs", "path-app", "settings.json")
	dest2 := filepath.Join(stagingDir, "configs", "path-app", "keybindings.json")
	if _, err := os.Stat(dest1); err != nil {
		t.Error("settings.json not found in staging")
	}
	if _, err := os.Stat(dest2); err != nil {
		t.Error("keybindings.json not found in staging")
	}
}

// ---------------------------------------------------------------------------
// ExtractBundle: extracted config file content matches original
// Pester: Bundle.ZipExtraction - Should extract zip and return manifest path / detect configs
// ---------------------------------------------------------------------------

func TestExtractBundle_ConfigFileContentPreserved(t *testing.T) {
	dir := t.TempDir()

	manifestContent := `{"version": 1, "name": "content-test", "apps": []}`
	manifestPath := filepath.Join(dir, "manifest.jsonc")
	if err := os.WriteFile(manifestPath, []byte(manifestContent), 0644); err != nil {
		t.Fatal(err)
	}

	configContent := `{"editor.fontSize": 14}`
	configDir := filepath.Join(dir, "vscode-config")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatal(err)
	}
	configFile := filepath.Join(configDir, "settings.json")
	if err := os.WriteFile(configFile, []byte(configContent), 0644); err != nil {
		t.Fatal(err)
	}

	matchedModules := []*modules.Module{
		{
			ID: "apps.vscode",
			Capture: &modules.CaptureDef{
				Files: []modules.CaptureFile{
					{Source: configFile, Dest: "apps/vscode/settings.json"},
				},
			},
		},
	}

	outputZip := filepath.Join(dir, "output.zip")
	if err := CreateBundle(manifestPath, matchedModules, outputZip, "1.0.0"); err != nil {
		t.Fatalf("CreateBundle failed: %v", err)
	}

	extractedManifestPath, err := ExtractBundle(outputZip)
	if err != nil {
		t.Fatalf("ExtractBundle failed: %v", err)
	}
	extractedDir := filepath.Dir(extractedManifestPath)
	defer os.RemoveAll(extractedDir)

	// Verify extracted config file content matches original.
	extractedConfig := filepath.Join(extractedDir, "configs", "vscode", "settings.json")
	data, err := os.ReadFile(extractedConfig)
	if err != nil {
		t.Fatalf("extracted config file missing: %v", err)
	}
	if string(data) != configContent {
		t.Errorf("extracted config content = %q, want %q", string(data), configContent)
	}

	// Verify configs directory exists (Pester: "Should detect configs directory").
	configsDir := filepath.Join(extractedDir, "configs")
	if _, err := os.Stat(configsDir); err != nil {
		t.Error("extracted bundle should have configs/ directory")
	}
}

// ---------------------------------------------------------------------------
// rewriteSourcePath: backslash-style paths also get rewritten
// Pester: Bundle.ZipCreation - Should rewrite restore source paths to match zip configs/ layout
// ---------------------------------------------------------------------------

func TestRewriteSourcePath_BackslashVariant(t *testing.T) {
	// Windows-style backslash path should also be rewritten.
	got := rewriteSourcePath(".\\payload\\apps\\rewrite-app\\settings.json", "rewrite-app")
	want := "./configs/rewrite-app/settings.json"
	if got != want {
		t.Errorf("rewriteSourcePath with backslashes = %q, want %q", got, want)
	}
}
