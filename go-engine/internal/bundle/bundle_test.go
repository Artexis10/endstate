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
