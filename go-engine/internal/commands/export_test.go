// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ===========================================================================
// Tests for RunExport (export-config command)
// Ported from Pester ExportConfig.Tests.ps1
// ===========================================================================

// ---------------------------------------------------------------------------
// Default export path: manifestDir/export
// Pester: Get-ExportPath - Returns default export path when no ExportPath specified
// ---------------------------------------------------------------------------

func TestRunExport_DefaultExportPath(t *testing.T) {
	tmp := t.TempDir()

	// Create a manifest with one restore entry whose target exists on disk.
	srcFile := filepath.Join(tmp, "system-config.txt")
	if err := os.WriteFile(srcFile, []byte("config data"), 0644); err != nil {
		t.Fatal(err)
	}

	manifestDir := filepath.Join(tmp, "manifests")
	if err := os.MkdirAll(manifestDir, 0755); err != nil {
		t.Fatal(err)
	}

	manifestContent := `{
  "version": 1,
  "name": "test",
  "apps": [],
  "restore": [
    {
      "type": "copy",
      "source": "./configs/test-config.txt",
      "target": "` + strings.ReplaceAll(srcFile, `\`, `\\`) + `"
    }
  ]
}`
	manifestPath := filepath.Join(manifestDir, "test.jsonc")
	if err := os.WriteFile(manifestPath, []byte(manifestContent), 0644); err != nil {
		t.Fatal(err)
	}

	result, envErr := RunExport(ExportFlags{
		Manifest: manifestPath,
		// Export path intentionally empty — should default to manifestDir/export
	})
	if envErr != nil {
		t.Fatalf("RunExport failed: %s", envErr.Message)
	}

	data := result.(*ExportData)
	expectedDir := filepath.Join(manifestDir, "export")
	absExpected, _ := filepath.Abs(expectedDir)

	if data.ExportPath != absExpected {
		t.Errorf("ExportPath = %q, want %q", data.ExportPath, absExpected)
	}
}

// ---------------------------------------------------------------------------
// Custom export path
// Pester: Get-ExportPath - Returns custom export path when ExportPath specified
// ---------------------------------------------------------------------------

func TestRunExport_CustomExportPath(t *testing.T) {
	tmp := t.TempDir()

	srcFile := filepath.Join(tmp, "system-config.txt")
	if err := os.WriteFile(srcFile, []byte("config data"), 0644); err != nil {
		t.Fatal(err)
	}

	manifestDir := filepath.Join(tmp, "manifests")
	if err := os.MkdirAll(manifestDir, 0755); err != nil {
		t.Fatal(err)
	}

	manifestContent := `{
  "version": 1,
  "name": "test",
  "apps": [],
  "restore": [
    {
      "type": "copy",
      "source": "./configs/test-config.txt",
      "target": "` + strings.ReplaceAll(srcFile, `\`, `\\`) + `"
    }
  ]
}`
	manifestPath := filepath.Join(manifestDir, "test.jsonc")
	if err := os.WriteFile(manifestPath, []byte(manifestContent), 0644); err != nil {
		t.Fatal(err)
	}

	customExport := filepath.Join(tmp, "custom-export")

	result, envErr := RunExport(ExportFlags{
		Manifest: manifestPath,
		Export:   customExport,
	})
	if envErr != nil {
		t.Fatalf("RunExport failed: %s", envErr.Message)
	}

	data := result.(*ExportData)
	absCustom, _ := filepath.Abs(customExport)

	if data.ExportPath != absCustom {
		t.Errorf("ExportPath = %q, want %q", data.ExportPath, absCustom)
	}
}

// ---------------------------------------------------------------------------
// DryRun does not copy files
// Pester: Invoke-ExportCapture DryRun Mode - DryRun does not copy files
// ---------------------------------------------------------------------------

func TestRunExport_DryRunDoesNotCopyFiles(t *testing.T) {
	tmp := t.TempDir()

	srcFile := filepath.Join(tmp, "system-config.txt")
	if err := os.WriteFile(srcFile, []byte("test content"), 0644); err != nil {
		t.Fatal(err)
	}

	manifestDir := filepath.Join(tmp, "manifests")
	if err := os.MkdirAll(manifestDir, 0755); err != nil {
		t.Fatal(err)
	}

	manifestContent := `{
  "version": 1,
  "name": "test",
  "apps": [],
  "restore": [
    {
      "type": "copy",
      "source": "./configs/test-config.txt",
      "target": "` + strings.ReplaceAll(srcFile, `\`, `\\`) + `"
    }
  ]
}`
	manifestPath := filepath.Join(manifestDir, "test.jsonc")
	if err := os.WriteFile(manifestPath, []byte(manifestContent), 0644); err != nil {
		t.Fatal(err)
	}

	result, envErr := RunExport(ExportFlags{
		Manifest: manifestPath,
		DryRun:   true,
	})
	if envErr != nil {
		t.Fatalf("RunExport DryRun failed: %s", envErr.Message)
	}

	data := result.(*ExportData)

	// Should report 1 export (would export).
	if data.ExportCount != 1 {
		t.Errorf("ExportCount = %d, want 1", data.ExportCount)
	}

	// But the file should NOT actually exist.
	exportFile := filepath.Join(data.ExportPath, "configs", "test-config.txt")
	if _, err := os.Stat(exportFile); !os.IsNotExist(err) {
		t.Error("file should NOT be copied in dry-run mode")
	}
}

// ---------------------------------------------------------------------------
// Real export copies file to correct location
// Pester: Invoke-ExportCapture Real Export - Exports file to correct location
// ---------------------------------------------------------------------------

func TestRunExport_RealExportCopiesFile(t *testing.T) {
	tmp := t.TempDir()

	srcFile := filepath.Join(tmp, "system-config.txt")
	if err := os.WriteFile(srcFile, []byte("test content for export"), 0644); err != nil {
		t.Fatal(err)
	}

	manifestDir := filepath.Join(tmp, "manifests")
	if err := os.MkdirAll(manifestDir, 0755); err != nil {
		t.Fatal(err)
	}

	manifestContent := `{
  "version": 1,
  "name": "test",
  "apps": [],
  "restore": [
    {
      "type": "copy",
      "source": "./configs/test-config.txt",
      "target": "` + strings.ReplaceAll(srcFile, `\`, `\\`) + `"
    }
  ]
}`
	manifestPath := filepath.Join(manifestDir, "test.jsonc")
	if err := os.WriteFile(manifestPath, []byte(manifestContent), 0644); err != nil {
		t.Fatal(err)
	}

	result, envErr := RunExport(ExportFlags{
		Manifest: manifestPath,
	})
	if envErr != nil {
		t.Fatalf("RunExport failed: %s", envErr.Message)
	}

	data := result.(*ExportData)

	if data.ExportCount != 1 {
		t.Errorf("ExportCount = %d, want 1", data.ExportCount)
	}

	// Verify the exported file exists with correct content.
	exportFile := filepath.Join(data.ExportPath, "configs", "test-config.txt")
	exported, err := os.ReadFile(exportFile)
	if err != nil {
		t.Fatalf("exported file not found: %v", err)
	}
	if !strings.Contains(string(exported), "test content for export") {
		t.Errorf("exported content = %q, want to contain %q", string(exported), "test content for export")
	}
}

// ---------------------------------------------------------------------------
// Creates manifest snapshot in export folder
// Pester: Invoke-ExportCapture Real Export - Creates manifest snapshot in export folder
// ---------------------------------------------------------------------------

func TestRunExport_CreatesManifestSnapshot(t *testing.T) {
	tmp := t.TempDir()

	srcFile := filepath.Join(tmp, "system-config.txt")
	if err := os.WriteFile(srcFile, []byte("content"), 0644); err != nil {
		t.Fatal(err)
	}

	manifestDir := filepath.Join(tmp, "manifests")
	if err := os.MkdirAll(manifestDir, 0755); err != nil {
		t.Fatal(err)
	}

	manifestContent := `{
  "version": 1,
  "name": "test",
  "apps": [],
  "restore": [
    {
      "type": "copy",
      "source": "./configs/test-config.txt",
      "target": "` + strings.ReplaceAll(srcFile, `\`, `\\`) + `"
    }
  ]
}`
	manifestPath := filepath.Join(manifestDir, "test.jsonc")
	if err := os.WriteFile(manifestPath, []byte(manifestContent), 0644); err != nil {
		t.Fatal(err)
	}

	result, envErr := RunExport(ExportFlags{
		Manifest: manifestPath,
	})
	if envErr != nil {
		t.Fatalf("RunExport failed: %s", envErr.Message)
	}

	data := result.(*ExportData)
	snapshotPath := filepath.Join(data.ExportPath, "manifest.snapshot.jsonc")

	if _, err := os.Stat(snapshotPath); err != nil {
		t.Errorf("manifest.snapshot.jsonc not found in export folder: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Skips files that don't exist on system
// Pester: Invoke-ExportCapture Real Export - Skips files that don't exist on system
// ---------------------------------------------------------------------------

func TestRunExport_SkipsMissingTargetFiles(t *testing.T) {
	tmp := t.TempDir()

	manifestDir := filepath.Join(tmp, "manifests")
	if err := os.MkdirAll(manifestDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Target path does NOT exist on the system.
	manifestContent := `{
  "version": 1,
  "name": "test",
  "apps": [],
  "restore": [
    {
      "type": "copy",
      "source": "./configs/missing.txt",
      "target": "` + strings.ReplaceAll(filepath.Join(tmp, "nonexistent", "missing.txt"), `\`, `\\`) + `"
    }
  ]
}`
	manifestPath := filepath.Join(manifestDir, "test-missing.jsonc")
	if err := os.WriteFile(manifestPath, []byte(manifestContent), 0644); err != nil {
		t.Fatal(err)
	}

	result, envErr := RunExport(ExportFlags{
		Manifest: manifestPath,
	})
	if envErr != nil {
		t.Fatalf("RunExport failed: %s", envErr.Message)
	}

	data := result.(*ExportData)
	if data.SkipCount != 1 {
		t.Errorf("SkipCount = %d, want 1", data.SkipCount)
	}
	if data.ExportCount != 0 {
		t.Errorf("ExportCount = %d, want 0", data.ExportCount)
	}
}

// ---------------------------------------------------------------------------
// DryRun does not create manifest snapshot
// Pester: Invoke-ExportCapture DryRun Mode - DryRun does not create export directory
// ---------------------------------------------------------------------------

func TestRunExport_DryRunNoManifestSnapshot(t *testing.T) {
	tmp := t.TempDir()

	srcFile := filepath.Join(tmp, "system-config.txt")
	if err := os.WriteFile(srcFile, []byte("content"), 0644); err != nil {
		t.Fatal(err)
	}

	manifestDir := filepath.Join(tmp, "manifests")
	if err := os.MkdirAll(manifestDir, 0755); err != nil {
		t.Fatal(err)
	}

	manifestContent := `{
  "version": 1,
  "name": "test",
  "apps": [],
  "restore": [
    {
      "type": "copy",
      "source": "./configs/test-config.txt",
      "target": "` + strings.ReplaceAll(srcFile, `\`, `\\`) + `"
    }
  ]
}`
	manifestPath := filepath.Join(manifestDir, "test.jsonc")
	if err := os.WriteFile(manifestPath, []byte(manifestContent), 0644); err != nil {
		t.Fatal(err)
	}

	result, envErr := RunExport(ExportFlags{
		Manifest: manifestPath,
		DryRun:   true,
	})
	if envErr != nil {
		t.Fatalf("RunExport DryRun failed: %s", envErr.Message)
	}

	data := result.(*ExportData)

	// In dry-run mode, manifest.snapshot.jsonc should NOT be created.
	snapshotPath := filepath.Join(data.ExportPath, "manifest.snapshot.jsonc")
	if _, err := os.Stat(snapshotPath); !os.IsNotExist(err) {
		t.Error("manifest.snapshot.jsonc should NOT exist in dry-run mode")
	}
}
