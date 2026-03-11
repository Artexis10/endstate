// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/snapshot"
)

// ---------------------------------------------------------------------------
// Snapshot mock helpers
// ---------------------------------------------------------------------------

// withMockSnapshot replaces takeSnapshotFn with one that returns the given
// apps and error, calls f, then restores the original.
func withMockSnapshot(apps []snapshot.SnapshotApp, err error, f func()) {
	orig := takeSnapshotFn
	takeSnapshotFn = func() ([]snapshot.SnapshotApp, error) {
		return apps, err
	}
	defer func() { takeSnapshotFn = orig }()
	f()
}

// sampleApps returns a set of realistic snapshot apps for testing.
func sampleApps() []snapshot.SnapshotApp {
	return []snapshot.SnapshotApp{
		{Name: "Visual Studio Code", ID: "Microsoft.VisualStudioCode", Version: "1.85.0", Source: "winget"},
		{Name: "Git", ID: "Git.Git", Version: "2.43.0", Source: "winget"},
		{Name: "Google Chrome", ID: "Google.Chrome", Version: "120.0.6099.130", Source: "winget"},
	}
}

// sampleAppsWithRuntimesAndStore returns apps including runtime and store entries.
func sampleAppsWithRuntimesAndStore() []snapshot.SnapshotApp {
	return []snapshot.SnapshotApp{
		{Name: "Visual Studio Code", ID: "Microsoft.VisualStudioCode", Version: "1.85.0", Source: "winget"},
		{Name: "Git", ID: "Git.Git", Version: "2.43.0", Source: "winget"},
		{Name: "VC++ 2015 Redist", ID: "Microsoft.VCRedist.2015+.x64", Version: "14.38.0", Source: "winget"},
		{Name: "Some Store App", ID: "9NKSQGP7F2NH", Version: "1.0.0", Source: "winget"},
	}
}

// ---------------------------------------------------------------------------
// Capture tests
// ---------------------------------------------------------------------------

func TestRunCapture_BasicCapture_ReturnsCorrectResult(t *testing.T) {
	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "test-capture.jsonc")

	var result *CaptureResult
	withMockSnapshot(sampleApps(), nil, func() {
		r, err := RunCapture(CaptureFlags{
			Out: outPath,
		})
		if err != nil {
			t.Fatalf("RunCapture returned unexpected error: %+v", err)
		}
		result = r.(*CaptureResult)
	})

	if result.AppCount != 3 {
		t.Errorf("expected appCount=3, got %d", result.AppCount)
	}
	if result.Counts.TotalFound != 3 {
		t.Errorf("expected totalFound=3, got %d", result.Counts.TotalFound)
	}
	if result.Counts.Included != 3 {
		t.Errorf("expected included=3, got %d", result.Counts.Included)
	}
	if result.Counts.Skipped != 0 {
		t.Errorf("expected skipped=0, got %d", result.Counts.Skipped)
	}

	// Verify file was written and is non-empty.
	info, statErr := os.Stat(outPath)
	if statErr != nil {
		t.Fatalf("output file does not exist: %v", statErr)
	}
	if info.Size() == 0 {
		t.Error("output file is empty")
	}
}

func TestRunCapture_OutputManifestContainsCorrectStructure(t *testing.T) {
	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "test-structure.jsonc")

	withMockSnapshot(sampleApps(), nil, func() {
		_, err := RunCapture(CaptureFlags{
			Out:  outPath,
			Name: "test-capture",
		})
		if err != nil {
			t.Fatalf("RunCapture returned unexpected error: %+v", err)
		}
	})

	data, readErr := os.ReadFile(outPath)
	if readErr != nil {
		t.Fatalf("failed to read output file: %v", readErr)
	}

	var manifest map[string]interface{}
	if jsonErr := json.Unmarshal(data, &manifest); jsonErr != nil {
		t.Fatalf("output is not valid JSON: %v", jsonErr)
	}

	// Check version.
	if v, ok := manifest["version"].(float64); !ok || v != 1 {
		t.Errorf("expected version=1, got %v", manifest["version"])
	}

	// Check name.
	if n, ok := manifest["name"].(string); !ok || n != "test-capture" {
		t.Errorf("expected name=%q, got %v", "test-capture", manifest["name"])
	}

	// Check captured timestamp exists.
	if _, ok := manifest["captured"].(string); !ok {
		t.Error("expected captured timestamp to be present")
	}

	// Check apps array.
	apps, ok := manifest["apps"].([]interface{})
	if !ok {
		t.Fatalf("expected apps to be an array, got %T", manifest["apps"])
	}
	if len(apps) != 3 {
		t.Errorf("expected 3 apps, got %d", len(apps))
	}
}

func TestRunCapture_FiltersRuntimesByDefault(t *testing.T) {
	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "test-filter-runtimes.jsonc")

	var result *CaptureResult
	withMockSnapshot(sampleAppsWithRuntimesAndStore(), nil, func() {
		r, err := RunCapture(CaptureFlags{
			Out: outPath,
		})
		if err != nil {
			t.Fatalf("RunCapture returned unexpected error: %+v", err)
		}
		result = r.(*CaptureResult)
	})

	// 4 total found, but runtime and store should be filtered.
	if result.Counts.TotalFound != 4 {
		t.Errorf("expected totalFound=4, got %d", result.Counts.TotalFound)
	}
	if result.Counts.FilteredRuntimes != 1 {
		t.Errorf("expected filteredRuntimes=1, got %d", result.Counts.FilteredRuntimes)
	}
	if result.Counts.FilteredStore != 1 {
		t.Errorf("expected filteredStoreApps=1, got %d", result.Counts.FilteredStore)
	}
	if result.Counts.Included != 2 {
		t.Errorf("expected included=2, got %d", result.Counts.Included)
	}
	if result.Counts.Skipped != 2 {
		t.Errorf("expected skipped=2, got %d", result.Counts.Skipped)
	}
}

func TestRunCapture_IncludeRuntimes_KeepsRuntimePackages(t *testing.T) {
	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "test-include-runtimes.jsonc")

	var result *CaptureResult
	withMockSnapshot(sampleAppsWithRuntimesAndStore(), nil, func() {
		r, err := RunCapture(CaptureFlags{
			Out:             outPath,
			IncludeRuntimes: true,
		})
		if err != nil {
			t.Fatalf("RunCapture returned unexpected error: %+v", err)
		}
		result = r.(*CaptureResult)
	})

	// Runtime should be included now; store still filtered.
	if result.Counts.FilteredRuntimes != 0 {
		t.Errorf("expected filteredRuntimes=0 with --include-runtimes, got %d", result.Counts.FilteredRuntimes)
	}
	if result.Counts.Included != 3 {
		t.Errorf("expected included=3, got %d", result.Counts.Included)
	}
}

func TestRunCapture_IncludeStoreApps_KeepsStoreIDs(t *testing.T) {
	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "test-include-store.jsonc")

	var result *CaptureResult
	withMockSnapshot(sampleAppsWithRuntimesAndStore(), nil, func() {
		r, err := RunCapture(CaptureFlags{
			Out:              outPath,
			IncludeStoreApps: true,
		})
		if err != nil {
			t.Fatalf("RunCapture returned unexpected error: %+v", err)
		}
		result = r.(*CaptureResult)
	})

	// Store should be included now; runtime still filtered.
	if result.Counts.FilteredStore != 0 {
		t.Errorf("expected filteredStore=0 with --include-store-apps, got %d", result.Counts.FilteredStore)
	}
	if result.Counts.Included != 3 {
		t.Errorf("expected included=3, got %d", result.Counts.Included)
	}
}

func TestRunCapture_Sanitize_StripsMeta_SortsByID(t *testing.T) {
	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "test-sanitize.jsonc")

	withMockSnapshot(sampleApps(), nil, func() {
		_, err := RunCapture(CaptureFlags{
			Out:      outPath,
			Sanitize: true,
		})
		if err != nil {
			t.Fatalf("RunCapture returned unexpected error: %+v", err)
		}
	})

	data, readErr := os.ReadFile(outPath)
	if readErr != nil {
		t.Fatalf("failed to read output: %v", readErr)
	}

	var manifest map[string]json.RawMessage
	if jsonErr := json.Unmarshal(data, &manifest); jsonErr != nil {
		t.Fatalf("output is not valid JSON: %v", jsonErr)
	}

	var apps []map[string]interface{}
	if jsonErr := json.Unmarshal(manifest["apps"], &apps); jsonErr != nil {
		t.Fatalf("failed to parse apps: %v", jsonErr)
	}

	// Verify _name is not present in sanitized output.
	for _, app := range apps {
		if _, has := app["_name"]; has {
			t.Errorf("sanitized app should not have _name field: %v", app)
		}
	}

	// Verify sorted by id.
	if len(apps) >= 2 {
		for i := 1; i < len(apps); i++ {
			prevID := apps[i-1]["id"].(string)
			currID := apps[i]["id"].(string)
			if prevID > currID {
				t.Errorf("apps not sorted by id: %q > %q", prevID, currID)
			}
		}
	}
}

func TestRunCapture_NonSanitized_IncludesDisplayName(t *testing.T) {
	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "test-non-sanitized.jsonc")

	withMockSnapshot(sampleApps(), nil, func() {
		_, err := RunCapture(CaptureFlags{
			Out: outPath,
		})
		if err != nil {
			t.Fatalf("RunCapture returned unexpected error: %+v", err)
		}
	})

	data, readErr := os.ReadFile(outPath)
	if readErr != nil {
		t.Fatalf("failed to read output: %v", readErr)
	}

	var manifest struct {
		Apps []map[string]interface{} `json:"apps"`
	}
	if jsonErr := json.Unmarshal(data, &manifest); jsonErr != nil {
		t.Fatalf("output is not valid JSON: %v", jsonErr)
	}

	// At least one app should have _name.
	hasName := false
	for _, app := range manifest.Apps {
		if _, ok := app["_name"]; ok {
			hasName = true
			break
		}
	}
	if !hasName {
		t.Error("expected _name field in non-sanitized output")
	}
}

func TestRunCapture_WingetIDToManifestID(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Microsoft.VisualStudioCode", "microsoft-visualstudiocode"},
		{"Git.Git", "git-git"},
		{"Google.Chrome", "google-chrome"},
	}
	for _, tt := range tests {
		got := wingetIDToManifestID(tt.input)
		if got != tt.expected {
			t.Errorf("wingetIDToManifestID(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestRunCapture_WingetNotAvailable_ReturnsCorrectError(t *testing.T) {
	notFoundErr := &exec.Error{Name: "winget", Err: exec.ErrNotFound}

	withMockSnapshot(nil, notFoundErr, func() {
		_, err := RunCapture(CaptureFlags{Out: "test.jsonc"})
		if err == nil {
			t.Fatal("expected error when winget not available, got nil")
		}
		if string(err.Code) != "WINGET_NOT_AVAILABLE" {
			t.Errorf("expected code WINGET_NOT_AVAILABLE, got %q", err.Code)
		}
	})
}

func TestRunCapture_SnapshotError_ReturnsCaptureFailedError(t *testing.T) {
	withMockSnapshot(nil, errors.New("some snapshot error"), func() {
		_, err := RunCapture(CaptureFlags{Out: "test.jsonc"})
		if err == nil {
			t.Fatal("expected error for snapshot failure, got nil")
		}
		if string(err.Code) != "CAPTURE_FAILED" {
			t.Errorf("expected code CAPTURE_FAILED, got %q", err.Code)
		}
	})
}

func TestRunCapture_EmptySnapshot_WritesEmptyAppsArray(t *testing.T) {
	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "test-empty.jsonc")

	var result *CaptureResult
	withMockSnapshot([]snapshot.SnapshotApp{}, nil, func() {
		r, err := RunCapture(CaptureFlags{Out: outPath})
		if err != nil {
			t.Fatalf("RunCapture returned unexpected error: %+v", err)
		}
		result = r.(*CaptureResult)
	})

	if result.AppCount != 0 {
		t.Errorf("expected appCount=0, got %d", result.AppCount)
	}

	// Verify file exists and contains valid JSON.
	data, readErr := os.ReadFile(outPath)
	if readErr != nil {
		t.Fatalf("failed to read output: %v", readErr)
	}
	var manifest map[string]interface{}
	if jsonErr := json.Unmarshal(data, &manifest); jsonErr != nil {
		t.Fatalf("output is not valid JSON: %v", jsonErr)
	}
}

func TestRunCapture_NameFlag_SetsManifestName(t *testing.T) {
	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "test-name.jsonc")

	var result *CaptureResult
	withMockSnapshot(sampleApps(), nil, func() {
		r, err := RunCapture(CaptureFlags{
			Out:  outPath,
			Name: "my-machine",
		})
		if err != nil {
			t.Fatalf("RunCapture returned unexpected error: %+v", err)
		}
		result = r.(*CaptureResult)
	})

	if result.Manifest.Name != "my-machine" {
		t.Errorf("expected manifest name=%q, got %q", "my-machine", result.Manifest.Name)
	}
}

func TestRunCapture_SanitizedFlag_ReflectedInResult(t *testing.T) {
	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "test-sanitized-flag.jsonc")

	var result *CaptureResult
	withMockSnapshot(sampleApps(), nil, func() {
		r, err := RunCapture(CaptureFlags{
			Out:      outPath,
			Sanitize: true,
		})
		if err != nil {
			t.Fatalf("RunCapture returned unexpected error: %+v", err)
		}
		result = r.(*CaptureResult)
	})

	if !result.Sanitized {
		t.Error("expected sanitized=true in result")
	}
}

func TestRunCapture_Update_MergesWithExistingManifest(t *testing.T) {
	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "test-update.jsonc")

	// Create an existing manifest with one app already present.
	existingManifest := `{
  "version": 1,
  "name": "existing",
  "apps": [
    {
      "id": "microsoft-visualstudiocode",
      "refs": {
        "windows": "Microsoft.VisualStudioCode"
      }
    }
  ]
}`
	existingPath := filepath.Join(tmpDir, "existing.jsonc")
	if writeErr := os.WriteFile(existingPath, []byte(existingManifest), 0644); writeErr != nil {
		t.Fatalf("failed to write existing manifest: %v", writeErr)
	}

	var result *CaptureResult
	withMockSnapshot(sampleApps(), nil, func() {
		r, err := RunCapture(CaptureFlags{
			Out:      outPath,
			Manifest: existingPath,
			Update:   true,
		})
		if err != nil {
			t.Fatalf("RunCapture returned unexpected error: %+v", err)
		}
		result = r.(*CaptureResult)
	})

	// Existing had 1 app, snapshot has 3. VSCode is a duplicate.
	// So merged should be 1 (existing) + 2 (new) = 3.
	if result.AppCount != 3 {
		t.Errorf("expected appCount=3 after merge, got %d", result.AppCount)
	}
}

func TestRunCapture_AppID_LowercasedDotsToHyphens(t *testing.T) {
	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "test-app-id.jsonc")

	withMockSnapshot(sampleApps(), nil, func() {
		_, err := RunCapture(CaptureFlags{Out: outPath})
		if err != nil {
			t.Fatalf("RunCapture returned unexpected error: %+v", err)
		}
	})

	data, readErr := os.ReadFile(outPath)
	if readErr != nil {
		t.Fatalf("failed to read output: %v", readErr)
	}

	var manifest struct {
		Apps []struct {
			ID   string            `json:"id"`
			Refs map[string]string `json:"refs"`
		} `json:"apps"`
	}
	if jsonErr := json.Unmarshal(data, &manifest); jsonErr != nil {
		t.Fatalf("output is not valid JSON: %v", jsonErr)
	}

	// Find the Git.Git entry and check its id.
	found := false
	for _, app := range manifest.Apps {
		if app.Refs["windows"] == "Git.Git" {
			found = true
			if app.ID != "git-git" {
				t.Errorf("expected id=%q for Git.Git, got %q", "git-git", app.ID)
			}
		}
	}
	if !found {
		t.Error("expected to find an app with refs.windows=Git.Git")
	}
}

func TestRunCapture_RefsWindowsPreservesOriginalCasing(t *testing.T) {
	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "test-refs-casing.jsonc")

	withMockSnapshot(sampleApps(), nil, func() {
		_, err := RunCapture(CaptureFlags{Out: outPath})
		if err != nil {
			t.Fatalf("RunCapture returned unexpected error: %+v", err)
		}
	})

	data, readErr := os.ReadFile(outPath)
	if readErr != nil {
		t.Fatalf("failed to read output: %v", readErr)
	}

	var manifest struct {
		Apps []struct {
			Refs map[string]string `json:"refs"`
		} `json:"apps"`
	}
	if jsonErr := json.Unmarshal(data, &manifest); jsonErr != nil {
		t.Fatalf("output is not valid JSON: %v", jsonErr)
	}

	// Verify refs.windows preserves original casing.
	expectedRefs := map[string]bool{
		"Microsoft.VisualStudioCode": true,
		"Git.Git":                    true,
		"Google.Chrome":              true,
	}
	for _, app := range manifest.Apps {
		ref := app.Refs["windows"]
		if !expectedRefs[ref] {
			t.Errorf("unexpected refs.windows=%q", ref)
		}
	}
}

func TestRunCapture_INV_CAPTURE_2_FileExistsAndNonEmpty(t *testing.T) {
	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "test-inv2.jsonc")

	withMockSnapshot(sampleApps(), nil, func() {
		_, err := RunCapture(CaptureFlags{Out: outPath})
		if err != nil {
			t.Fatalf("RunCapture returned unexpected error: %+v", err)
		}
	})

	info, statErr := os.Stat(outPath)
	if statErr != nil {
		t.Fatalf("INV-CAPTURE-2 violated: output file does not exist: %v", statErr)
	}
	if info.Size() == 0 {
		t.Fatal("INV-CAPTURE-2 violated: output file is empty")
	}
}

func TestRunCapture_DefaultName_IsCaptured(t *testing.T) {
	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "test-default-name.jsonc")

	var result *CaptureResult
	withMockSnapshot(sampleApps(), nil, func() {
		r, err := RunCapture(CaptureFlags{Out: outPath})
		if err != nil {
			t.Fatalf("RunCapture returned unexpected error: %+v", err)
		}
		result = r.(*CaptureResult)
	})

	if result.Manifest.Name != "captured" {
		t.Errorf("expected default manifest name=%q, got %q", "captured", result.Manifest.Name)
	}
}

func TestRunCapture_ProfileName_UsedAsManifestName(t *testing.T) {
	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "test-profile-name.jsonc")

	var result *CaptureResult
	withMockSnapshot(sampleApps(), nil, func() {
		r, err := RunCapture(CaptureFlags{
			Out:     outPath,
			Profile: "work-laptop",
		})
		if err != nil {
			t.Fatalf("RunCapture returned unexpected error: %+v", err)
		}
		result = r.(*CaptureResult)
	})

	if result.Manifest.Name != "work-laptop" {
		t.Errorf("expected manifest name=%q from profile, got %q", "work-laptop", result.Manifest.Name)
	}
}
