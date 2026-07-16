// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/realizer"
	"github.com/Artexis10/endstate/go-engine/internal/snapshot"
)

// ---------------------------------------------------------------------------
// Snapshot mock helpers
// ---------------------------------------------------------------------------

// withMockSnapshot replaces takeSnapshotFn (which defaults to WingetExport)
// with one that returns the given apps and error, calls f, then restores the
// original. It also forces the no-realizer path so RunCapture's realizer fork
// does not divert these winget-path tests to the Nix realizer on linux/darwin
// (mirrors withMockDriver); restored on exit.
func withMockSnapshot(apps []snapshot.SnapshotApp, err error, f func()) {
	orig := takeSnapshotFn
	takeSnapshotFn = func() ([]snapshot.SnapshotApp, error) {
		return apps, err
	}
	origRealizer := newRealizerFn
	newRealizerFn = func() (realizer.Realizer, error) { return nil, ErrNoRealizer }
	defer func() {
		takeSnapshotFn = orig
		newRealizerFn = origRealizer
	}()
	f()
}

// withMockDisplayNames replaces the installed-apps seam (listInstalledFn) with
// one that synthesizes name-only SnapshotApp entries from the given map (or
// returns err), calls f, then restores the original. Capture derives its
// display-name map from these entries; with no Version set, no versions leak,
// so display-name-only call sites behave exactly as before the seam swap.
func withMockDisplayNames(nameMap map[string]string, err error, f func()) {
	orig := listInstalledFn
	listInstalledFn = func() ([]snapshot.SnapshotApp, error) {
		if err != nil {
			return nil, err
		}
		apps := make([]snapshot.SnapshotApp, 0, len(nameMap))
		for id, name := range nameMap {
			apps = append(apps, snapshot.SnapshotApp{ID: id, Name: name})
		}
		return apps, nil
	}
	defer func() { listInstalledFn = orig }()
	f()
}

// withMockInstalledApps replaces the installed-apps seam (listInstalledFn) with
// one that returns the given version-bearing SnapshotApp entries. It registers a
// t.Cleanup to restore the original, so callers use it inline (typically before
// a withMockSnapshot closure) rather than wrapping a func.
func withMockInstalledApps(t *testing.T, apps []snapshot.SnapshotApp) {
	t.Helper()
	orig := listInstalledFn
	listInstalledFn = func() ([]snapshot.SnapshotApp, error) {
		return apps, nil
	}
	t.Cleanup(func() { listInstalledFn = orig })
}

// withMockCatalog replaces loadModuleCatalogFn and resolveRepoRootFn so the
// module-matching block is always entered (regardless of whether a real
// VERSION file is present in the test binary's directory hierarchy).
func withMockCatalog(catalog map[string]*modules.Module, err error, f func()) {
	origCatalog := loadModuleCatalogFn
	origRoot := resolveRepoRootFn
	loadModuleCatalogFn = func(_ string) (map[string]*modules.Module, error) {
		return catalog, err
	}
	// Return a non-empty sentinel so the "if repoRoot != """ guard passes.
	resolveRepoRootFn = func() string { return "/test-repo-root" }
	defer func() {
		loadModuleCatalogFn = origCatalog
		resolveRepoRootFn = origRoot
	}()
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

// sampleExportSet returns an ID-only export set (mirrors `winget export`, which
// populates package IDs but no versions) for the three sample apps. Pin tests
// use this as the export set so the conversion loop's sApp.Version fallback is
// empty and versions come solely from the installed-apps snapshot.
func sampleExportSet() []snapshot.SnapshotApp {
	return []snapshot.SnapshotApp{
		{Name: "Visual Studio Code", ID: "Microsoft.VisualStudioCode", Source: "winget"},
		{Name: "Git", ID: "Git.Git", Source: "winget"},
		{Name: "Google Chrome", ID: "Google.Chrome", Source: "winget"},
	}
}

// sampleInstalledApps returns the installed-apps snapshot (`winget list`) with
// versions for the three sample apps. Versions differ from sampleApps so pin
// tests prove the installed-version map is authoritative.
func sampleInstalledApps() []snapshot.SnapshotApp {
	return []snapshot.SnapshotApp{
		{Name: "Visual Studio Code", ID: "Microsoft.VisualStudioCode", Version: "1.99.0", Source: "winget"},
		{Name: "Git", ID: "Git.Git", Version: "2.44.0", Source: "winget"},
		{Name: "Google Chrome", ID: "Google.Chrome", Version: "130.0.1", Source: "winget"},
	}
}

// readManifestApps reads the written manifest and returns its apps as generic
// maps so tests can assert on the raw JSON shape (presence/absence of keys).
func readManifestApps(t *testing.T, path string) []map[string]interface{} {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read output: %v", err)
	}
	var mf struct {
		Apps []map[string]interface{} `json:"apps"`
	}
	if err := json.Unmarshal(data, &mf); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	return mf.Apps
}

// manifestAppVersion returns the version field for the app whose refs.windows
// equals winRef, and whether such an app was found.
func manifestAppVersion(apps []map[string]interface{}, winRef string) (string, bool) {
	for _, app := range apps {
		refs, _ := app["refs"].(map[string]interface{})
		if r, _ := refs["windows"].(string); r == winRef {
			v, _ := app["version"].(string)
			return v, true
		}
	}
	return "", false
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

// noopDisplayNames installs a display-name mock that returns an empty map.
// Tests that don't care about display names use this to avoid winget calls.
func noopDisplayNames(f func()) {
	withMockDisplayNames(map[string]string{}, nil, f)
}

// emptyCatalog installs a module catalog mock that returns an empty catalog.
// Tests that don't care about module matching use this.
func emptyCatalog(f func()) {
	withMockCatalog(map[string]*modules.Module{}, nil, f)
}

// snapshotCall describes one return value for withMockSnapshotSequence.
type snapshotCall struct {
	apps []snapshot.SnapshotApp
	err  error
}

// withMockSnapshotSequence replaces takeSnapshotFn with one that returns
// successive results from calls. If more calls are made than entries, the last
// entry is repeated.
func withMockSnapshotSequence(calls []snapshotCall, f func()) {
	orig := takeSnapshotFn
	callIdx := 0
	takeSnapshotFn = func() ([]snapshot.SnapshotApp, error) {
		idx := callIdx
		if idx >= len(calls) {
			idx = len(calls) - 1
		}
		callIdx++
		return calls[idx].apps, calls[idx].err
	}
	// Force the no-realizer path (see withMockSnapshot) so the capture fork does
	// not divert these winget-path tests to the Nix realizer on linux/darwin.
	origRealizer := newRealizerFn
	newRealizerFn = func() (realizer.Realizer, error) { return nil, ErrNoRealizer }
	defer func() {
		takeSnapshotFn = orig
		newRealizerFn = origRealizer
	}()
	f()
}

// withRetryDelay overrides snapshotRetryDelay for the duration of f.
func withRetryDelay(d time.Duration, f func()) {
	orig := snapshotRetryDelay
	snapshotRetryDelay = d
	defer func() { snapshotRetryDelay = orig }()
	f()
}

func assertCaptureEventStreamIsNDJSON(t *testing.T, stderr string) {
	t.Helper()
	lineNumber := 0
	eventCount := 0
	for _, line := range strings.Split(stderr, "\n") {
		lineNumber++
		if strings.TrimSpace(line) == "" {
			continue
		}
		var event map[string]interface{}
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("stderr line %d is not valid event JSON: %v\n%s", lineNumber, err, line)
		}
		if event["event"] == nil {
			t.Fatalf("stderr line %d is JSON but not an event: %s", lineNumber, line)
		}
		eventCount++
	}
	if eventCount == 0 {
		t.Fatal("expected capture to emit structured events")
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
		noopDisplayNames(func() {
			emptyCatalog(func() {
				r, err := RunCapture(CaptureFlags{
					Out: outPath,
				})
				if err != nil {
					t.Fatalf("RunCapture returned unexpected error: %+v", err)
				}
				result = r.(*CaptureResult)
			})
		})
	})

	if result.Counts.Included != 3 {
		t.Errorf("expected Counts.Included=3, got %d", result.Counts.Included)
	}
	if result.Counts.TotalFound != 3 {
		t.Errorf("expected totalFound=3, got %d", result.Counts.TotalFound)
	}
	if result.Counts.Skipped != 0 {
		t.Errorf("expected skipped=0, got %d", result.Counts.Skipped)
	}
	if result.OutputFormat != "jsonc" {
		t.Errorf("expected outputFormat=jsonc, got %q", result.OutputFormat)
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
		noopDisplayNames(func() {
			emptyCatalog(func() {
				_, err := RunCapture(CaptureFlags{
					Out:  outPath,
					Name: "test-capture",
				})
				if err != nil {
					t.Fatalf("RunCapture returned unexpected error: %+v", err)
				}
			})
		})
	})

	data, readErr := os.ReadFile(outPath)
	if readErr != nil {
		t.Fatalf("failed to read output file: %v", readErr)
	}

	var mf map[string]interface{}
	if jsonErr := json.Unmarshal(data, &mf); jsonErr != nil {
		t.Fatalf("output is not valid JSON: %v", jsonErr)
	}

	// Check version.
	if v, ok := mf["version"].(float64); !ok || v != 1 {
		t.Errorf("expected version=1, got %v", mf["version"])
	}

	// Check name.
	if n, ok := mf["name"].(string); !ok || n != "test-capture" {
		t.Errorf("expected name=%q, got %v", "test-capture", mf["name"])
	}

	// Check captured timestamp exists.
	if _, ok := mf["captured"].(string); !ok {
		t.Error("expected captured timestamp to be present")
	}

	// Check apps array.
	apps, ok := mf["apps"].([]interface{})
	if !ok {
		t.Fatalf("expected apps to be an array, got %T", mf["apps"])
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
		noopDisplayNames(func() {
			emptyCatalog(func() {
				r, err := RunCapture(CaptureFlags{
					Out: outPath,
				})
				if err != nil {
					t.Fatalf("RunCapture returned unexpected error: %+v", err)
				}
				result = r.(*CaptureResult)
			})
		})
	})

	// 4 total found, but runtime and store should be filtered.
	if result.Counts.TotalFound != 4 {
		t.Errorf("expected totalFound=4, got %d", result.Counts.TotalFound)
	}
	if result.Counts.FilteredRuntimes != 1 {
		t.Errorf("expected filteredRuntimes=1, got %d", result.Counts.FilteredRuntimes)
	}
	if result.Counts.FilteredStoreApps != 1 {
		t.Errorf("expected filteredStoreApps=1, got %d", result.Counts.FilteredStoreApps)
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
		noopDisplayNames(func() {
			emptyCatalog(func() {
				r, err := RunCapture(CaptureFlags{
					Out:             outPath,
					IncludeRuntimes: true,
				})
				if err != nil {
					t.Fatalf("RunCapture returned unexpected error: %+v", err)
				}
				result = r.(*CaptureResult)
			})
		})
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
		noopDisplayNames(func() {
			emptyCatalog(func() {
				r, err := RunCapture(CaptureFlags{
					Out:              outPath,
					IncludeStoreApps: true,
				})
				if err != nil {
					t.Fatalf("RunCapture returned unexpected error: %+v", err)
				}
				result = r.(*CaptureResult)
			})
		})
	})

	// Store should be included now; runtime still filtered.
	if result.Counts.FilteredStoreApps != 0 {
		t.Errorf("expected filteredStoreApps=0 with --include-store-apps, got %d", result.Counts.FilteredStoreApps)
	}
	if result.Counts.Included != 3 {
		t.Errorf("expected included=3, got %d", result.Counts.Included)
	}
}

func TestRunCapture_Sanitize_StripsMeta_SortsByID(t *testing.T) {
	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "test-sanitize.jsonc")

	withMockSnapshot(sampleApps(), nil, func() {
		noopDisplayNames(func() {
			emptyCatalog(func() {
				_, err := RunCapture(CaptureFlags{
					Out:      outPath,
					Sanitize: true,
				})
				if err != nil {
					t.Fatalf("RunCapture returned unexpected error: %+v", err)
				}
			})
		})
	})

	data, readErr := os.ReadFile(outPath)
	if readErr != nil {
		t.Fatalf("failed to read output: %v", readErr)
	}

	var mf map[string]json.RawMessage
	if jsonErr := json.Unmarshal(data, &mf); jsonErr != nil {
		t.Fatalf("output is not valid JSON: %v", jsonErr)
	}

	var apps []map[string]interface{}
	if jsonErr := json.Unmarshal(mf["apps"], &apps); jsonErr != nil {
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

func TestRunCapture_Sanitize_OutputFormat_IsJsonc(t *testing.T) {
	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "test-sanitize-format.jsonc")

	var result *CaptureResult
	withMockSnapshot(sampleApps(), nil, func() {
		noopDisplayNames(func() {
			emptyCatalog(func() {
				r, err := RunCapture(CaptureFlags{
					Out:      outPath,
					Sanitize: true,
				})
				if err != nil {
					t.Fatalf("RunCapture returned unexpected error: %+v", err)
				}
				result = r.(*CaptureResult)
			})
		})
	})

	if result.OutputFormat != "jsonc" {
		t.Errorf("expected outputFormat=jsonc for sanitized capture, got %q", result.OutputFormat)
	}
	if len(result.ConfigModules) != 0 {
		t.Errorf("expected no configModules for sanitized capture, got %d", len(result.ConfigModules))
	}
}

func TestRunCapture_NonSanitized_IncludesDisplayName(t *testing.T) {
	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "test-non-sanitized.jsonc")

	withMockSnapshot(sampleApps(), nil, func() {
		noopDisplayNames(func() {
			emptyCatalog(func() {
				_, err := RunCapture(CaptureFlags{
					Out: outPath,
				})
				if err != nil {
					t.Fatalf("RunCapture returned unexpected error: %+v", err)
				}
			})
		})
	})

	data, readErr := os.ReadFile(outPath)
	if readErr != nil {
		t.Fatalf("failed to read output: %v", readErr)
	}

	var mf struct {
		Apps []map[string]interface{} `json:"apps"`
	}
	if jsonErr := json.Unmarshal(data, &mf); jsonErr != nil {
		t.Fatalf("output is not valid JSON: %v", jsonErr)
	}

	// At least one app should have _name.
	hasName := false
	for _, app := range mf.Apps {
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

func TestRunCapture_EmptySnapshot_FailsAfterRetry(t *testing.T) {
	withRetryDelay(0, func() {
		withMockSnapshot([]snapshot.SnapshotApp{}, nil, func() {
			noopDisplayNames(func() {
				_, err := RunCapture(CaptureFlags{Out: "test-empty.jsonc"})
				if err == nil {
					t.Fatal("expected CAPTURE_FAILED when snapshot returns 0 apps after retry")
				}
				if string(err.Code) != "CAPTURE_FAILED" {
					t.Errorf("expected code CAPTURE_FAILED, got %q", err.Code)
				}
			})
		})
	})
}

func TestRunCapture_NameFlag_SetsManifestName(t *testing.T) {
	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "test-name.jsonc")

	var result *CaptureResult
	withMockSnapshot(sampleApps(), nil, func() {
		noopDisplayNames(func() {
			emptyCatalog(func() {
				r, err := RunCapture(CaptureFlags{
					Out:  outPath,
					Name: "my-machine",
				})
				if err != nil {
					t.Fatalf("RunCapture returned unexpected error: %+v", err)
				}
				result = r.(*CaptureResult)
			})
		})
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
		noopDisplayNames(func() {
			emptyCatalog(func() {
				r, err := RunCapture(CaptureFlags{
					Out:      outPath,
					Sanitize: true,
				})
				if err != nil {
					t.Fatalf("RunCapture returned unexpected error: %+v", err)
				}
				result = r.(*CaptureResult)
			})
		})
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
		noopDisplayNames(func() {
			emptyCatalog(func() {
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
		})
	})

	// Existing had 1 app, snapshot has 3. VSCode is a duplicate.
	// So merged should be 1 (existing) + 2 (new) = 3.
	if result.Counts.Included != 3 {
		t.Errorf("expected Counts.Included=3 after merge, got %d", result.Counts.Included)
	}
}

func TestRunCapture_AppID_LowercasedDotsToHyphens(t *testing.T) {
	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "test-app-id.jsonc")

	withMockSnapshot(sampleApps(), nil, func() {
		noopDisplayNames(func() {
			emptyCatalog(func() {
				_, err := RunCapture(CaptureFlags{Out: outPath})
				if err != nil {
					t.Fatalf("RunCapture returned unexpected error: %+v", err)
				}
			})
		})
	})

	data, readErr := os.ReadFile(outPath)
	if readErr != nil {
		t.Fatalf("failed to read output: %v", readErr)
	}

	var mf struct {
		Apps []struct {
			ID   string            `json:"id"`
			Refs map[string]string `json:"refs"`
		} `json:"apps"`
	}
	if jsonErr := json.Unmarshal(data, &mf); jsonErr != nil {
		t.Fatalf("output is not valid JSON: %v", jsonErr)
	}

	// Find the Git.Git entry and check its id.
	found := false
	for _, app := range mf.Apps {
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
		noopDisplayNames(func() {
			emptyCatalog(func() {
				_, err := RunCapture(CaptureFlags{Out: outPath})
				if err != nil {
					t.Fatalf("RunCapture returned unexpected error: %+v", err)
				}
			})
		})
	})

	data, readErr := os.ReadFile(outPath)
	if readErr != nil {
		t.Fatalf("failed to read output: %v", readErr)
	}

	var mf struct {
		Apps []struct {
			Refs map[string]string `json:"refs"`
		} `json:"apps"`
	}
	if jsonErr := json.Unmarshal(data, &mf); jsonErr != nil {
		t.Fatalf("output is not valid JSON: %v", jsonErr)
	}

	// Verify refs.windows preserves original casing.
	expectedRefs := map[string]bool{
		"Microsoft.VisualStudioCode": true,
		"Git.Git":                    true,
		"Google.Chrome":              true,
	}
	for _, app := range mf.Apps {
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
		noopDisplayNames(func() {
			emptyCatalog(func() {
				_, err := RunCapture(CaptureFlags{Out: outPath})
				if err != nil {
					t.Fatalf("RunCapture returned unexpected error: %+v", err)
				}
			})
		})
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
		noopDisplayNames(func() {
			emptyCatalog(func() {
				r, err := RunCapture(CaptureFlags{Out: outPath})
				if err != nil {
					t.Fatalf("RunCapture returned unexpected error: %+v", err)
				}
				result = r.(*CaptureResult)
			})
		})
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
		noopDisplayNames(func() {
			emptyCatalog(func() {
				r, err := RunCapture(CaptureFlags{
					Out:     outPath,
					Profile: "work-laptop",
				})
				if err != nil {
					t.Fatalf("RunCapture returned unexpected error: %+v", err)
				}
				result = r.(*CaptureResult)
			})
		})
	})

	if result.Manifest.Name != "work-laptop" {
		t.Errorf("expected manifest name=%q from profile, got %q", "work-laptop", result.Manifest.Name)
	}
}

// ---------------------------------------------------------------------------
// New envelope shape tests
// ---------------------------------------------------------------------------

func TestRunCapture_AppsIncluded_HasSourceNameID(t *testing.T) {
	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "test-apps-included.jsonc")

	displayNames := map[string]string{
		"Microsoft.VisualStudioCode": "Visual Studio Code",
		"Git.Git":                    "Git",
		"Google.Chrome":              "Google Chrome",
	}

	var result *CaptureResult
	withMockSnapshot(sampleApps(), nil, func() {
		withMockDisplayNames(displayNames, nil, func() {
			emptyCatalog(func() {
				r, err := RunCapture(CaptureFlags{Out: outPath})
				if err != nil {
					t.Fatalf("RunCapture returned unexpected error: %+v", err)
				}
				result = r.(*CaptureResult)
			})
		})
	})

	if len(result.AppsIncluded) != 3 {
		t.Fatalf("expected 3 appsIncluded, got %d", len(result.AppsIncluded))
	}

	for _, app := range result.AppsIncluded {
		if app.Source != "winget" {
			t.Errorf("expected source=winget for app %q, got %q", app.ID, app.Source)
		}
		if app.ID == "" {
			t.Error("expected ID to be set")
		}
		if app.Name == "" {
			t.Errorf("expected name to be set for app %q (from display name map)", app.ID)
		}
	}
}

func TestRunCapture_ConfigModuleMap_EmptyWhenNoCatalog(t *testing.T) {
	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "test-no-catalog.jsonc")

	var result *CaptureResult
	withMockSnapshot(sampleApps(), nil, func() {
		noopDisplayNames(func() {
			emptyCatalog(func() {
				r, err := RunCapture(CaptureFlags{Out: outPath})
				if err != nil {
					t.Fatalf("RunCapture returned unexpected error: %+v", err)
				}
				result = r.(*CaptureResult)
			})
		})
	})

	// All slice/map fields must be non-nil.
	if result.ConfigModuleMap == nil {
		t.Error("expected configModuleMap to be non-nil")
	}
	if result.ConfigModules == nil {
		t.Error("expected configModules to be non-nil")
	}
	if result.ConfigsIncluded == nil {
		t.Error("expected configsIncluded to be non-nil")
	}
	if result.ConfigsSkipped == nil {
		t.Error("expected configsSkipped to be non-nil")
	}
	if result.ConfigsCaptureErrors == nil {
		t.Error("expected configsCaptureErrors to be non-nil")
	}

	if len(result.ConfigModules) != 0 {
		t.Errorf("expected no configModules when catalog is empty, got %d", len(result.ConfigModules))
	}
}

func TestRunCapture_Sanitized_NoModulesMatched(t *testing.T) {
	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "test-sanitize-no-modules.jsonc")

	var result *CaptureResult
	withMockSnapshot(sampleApps(), nil, func() {
		noopDisplayNames(func() {
			// Module matching is intentionally skipped for sanitized captures,
			// so even though the fn is not replaced here it should not be called.
			r, err := RunCapture(CaptureFlags{
				Out:      outPath,
				Sanitize: true,
			})
			if err != nil {
				t.Fatalf("RunCapture returned unexpected error: %+v", err)
			}
			result = r.(*CaptureResult)
		})
	})

	if result.OutputFormat != "jsonc" {
		t.Errorf("sanitized capture must have outputFormat=jsonc, got %q", result.OutputFormat)
	}
	if len(result.ConfigModules) != 0 {
		t.Errorf("sanitized capture must have no configModules, got %d", len(result.ConfigModules))
	}
	if len(result.ConfigModuleMap) != 0 {
		t.Errorf("sanitized capture must have empty configModuleMap, got %v", result.ConfigModuleMap)
	}
}

func TestRunCapture_EnvelopeShape_RequiredFields(t *testing.T) {
	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "test-envelope.jsonc")

	var raw interface{}
	withMockSnapshot(sampleApps(), nil, func() {
		noopDisplayNames(func() {
			emptyCatalog(func() {
				r, err := RunCapture(CaptureFlags{Out: outPath})
				if err != nil {
					t.Fatalf("RunCapture returned unexpected error: %+v", err)
				}
				raw = r
			})
		})
	})

	result, ok := raw.(*CaptureResult)
	if !ok {
		t.Fatalf("expected *CaptureResult, got %T", raw)
	}

	if result.OutputPath == "" {
		t.Error("expected outputPath to be non-empty")
	}
	if result.OutputFormat == "" {
		t.Error("expected outputFormat to be non-empty")
	}
	if result.AppsIncluded == nil {
		t.Error("expected appsIncluded to be non-nil")
	}
	if result.ConfigModules == nil {
		t.Error("expected configModules to be non-nil")
	}
	if result.ConfigModuleMap == nil {
		t.Error("expected configModuleMap to be non-nil")
	}
	if result.ConfigsIncluded == nil {
		t.Error("expected configsIncluded to be non-nil")
	}
	if result.ConfigsSkipped == nil {
		t.Error("expected configsSkipped to be non-nil")
	}
	if result.ConfigsCaptureErrors == nil {
		t.Error("expected configsCaptureErrors to be non-nil")
	}
	if result.Manifest.Name == "" {
		t.Error("expected manifest.name to be non-empty")
	}
	if result.Manifest.Path == "" {
		t.Error("expected manifest.path to be non-empty")
	}
}

func TestRunCapture_ConfigModuleMap_PopulatedFromMatchedModules(t *testing.T) {
	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "test-module-map.jsonc")

	// A module that matches Microsoft.VisualStudioCode with a capture section.
	// The source file is optional so capture proceeds without error even if the
	// file doesn't exist on the test machine.
	matchingModule := &modules.Module{
		ID:          "apps.vscode",
		DisplayName: "Visual Studio Code",
		Matches: modules.MatchCriteria{
			Winget: []string{"Microsoft.VisualStudioCode"},
		},
		Capture: &modules.CaptureDef{
			Files: []modules.CaptureFile{
				{
					Source:   "/nonexistent/settings.json",
					Dest:     "./payload/apps/vscode/settings.json",
					Optional: true,
				},
			},
		},
	}

	catalog := map[string]*modules.Module{
		"apps.vscode": matchingModule,
	}

	var result *CaptureResult
	withMockSnapshot(sampleApps(), nil, func() {
		noopDisplayNames(func() {
			withMockCatalog(catalog, nil, func() {
				r, err := RunCapture(CaptureFlags{Out: outPath})
				if err != nil {
					t.Fatalf("RunCapture returned unexpected error: %+v", err)
				}
				result = r.(*CaptureResult)
			})
		})
	})

	// configModuleMap must contain the winget ref -> module ID mapping.
	if result.ConfigModuleMap["Microsoft.VisualStudioCode"] != "apps.vscode" {
		t.Errorf("expected configModuleMap[%q]=%q, got %q",
			"Microsoft.VisualStudioCode", "apps.vscode",
			result.ConfigModuleMap["Microsoft.VisualStudioCode"])
	}

	// configModules must have one entry.
	if len(result.ConfigModules) != 1 {
		t.Fatalf("expected 1 configModule, got %d", len(result.ConfigModules))
	}
	mod := result.ConfigModules[0]
	if mod.ID != "apps.vscode" {
		t.Errorf("expected configModules[0].id=%q, got %q", "apps.vscode", mod.ID)
	}
	if mod.AppID != "vscode" {
		t.Errorf("expected configModules[0].appId=%q, got %q", "vscode", mod.AppID)
	}
	if mod.DisplayName != "Visual Studio Code" {
		t.Errorf("expected configModules[0].displayName=%q, got %q",
			"Visual Studio Code", mod.DisplayName)
	}
	if len(mod.WingetRefs) == 0 || mod.WingetRefs[0] != "Microsoft.VisualStudioCode" {
		t.Errorf("expected wingetRefs=[%q], got %v", "Microsoft.VisualStudioCode", mod.WingetRefs)
	}
	// Source is optional and non-existent, so module reports skipped.
	if mod.Status != "skipped" {
		t.Errorf("expected status=skipped for optional non-existent file, got %q", mod.Status)
	}
}

func TestRunCapture_ModuleDirName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"apps.vscode", "vscode"},
		{"apps.git", "git"},
		{"vscode", "vscode"},
		{"apps.", ""},
	}
	for _, tt := range tests {
		got := moduleDirName(tt.input)
		if got != tt.expected {
			t.Errorf("moduleDirName(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

// ---------------------------------------------------------------------------
// INV-CONTINUITY-1: counts.included must equal len(appsIncluded)
// (mirrors Pester INV-CONTINUITY-1)
// ---------------------------------------------------------------------------

func TestRunCapture_ContinuityInvariant_CountsEqualsAppsIncluded(t *testing.T) {
	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "test-continuity.jsonc")

	var result *CaptureResult
	withMockSnapshot(sampleApps(), nil, func() {
		noopDisplayNames(func() {
			emptyCatalog(func() {
				r, err := RunCapture(CaptureFlags{Out: outPath})
				if err != nil {
					t.Fatalf("RunCapture returned unexpected error: %+v", err)
				}
				result = r.(*CaptureResult)
			})
		})
	})

	if result.Counts.Included != len(result.AppsIncluded) {
		t.Errorf("INV-CONTINUITY-1 violated: counts.included=%d but len(appsIncluded)=%d",
			result.Counts.Included, len(result.AppsIncluded))
	}
}

// ---------------------------------------------------------------------------
// Update merge: existing + new deduped by windows ref
// (mirrors Pester Merge-ManifestsForUpdate)
// ---------------------------------------------------------------------------

func TestRunCapture_Update_DeduplicatesByWindowsRef(t *testing.T) {
	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "test-dedup.jsonc")

	// Existing manifest has Git and VSCode.
	existingManifest := `{
  "version": 1,
  "name": "existing",
  "apps": [
    {"id": "git-git", "refs": {"windows": "Git.Git"}},
    {"id": "microsoft-visualstudiocode", "refs": {"windows": "Microsoft.VisualStudioCode"}}
  ]
}`
	existingPath := filepath.Join(tmpDir, "existing.jsonc")
	if err := os.WriteFile(existingPath, []byte(existingManifest), 0644); err != nil {
		t.Fatal(err)
	}

	// Snapshot returns Git, VSCode, and Chrome. Git and VSCode are dupes.
	var result *CaptureResult
	withMockSnapshot(sampleApps(), nil, func() {
		noopDisplayNames(func() {
			emptyCatalog(func() {
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
		})
	})

	// 2 existing + 1 new (Chrome) = 3 total. No duplicates.
	if result.Counts.Included != 3 {
		t.Errorf("expected 3 included after dedup, got %d", result.Counts.Included)
	}
}

func TestRunCapture_Update_PreservesExistingApps(t *testing.T) {
	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "test-update-preserve.jsonc")

	// Existing manifest has an app NOT in the current snapshot.
	existingManifest := `{
  "version": 1,
  "name": "existing",
  "apps": [
    {"id": "removed-app", "refs": {"windows": "Removed.App"}}
  ]
}`
	existingPath := filepath.Join(tmpDir, "existing.jsonc")
	if err := os.WriteFile(existingPath, []byte(existingManifest), 0644); err != nil {
		t.Fatal(err)
	}

	var result *CaptureResult
	withMockSnapshot(sampleApps(), nil, func() {
		noopDisplayNames(func() {
			emptyCatalog(func() {
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
		})
	})

	// 1 existing (Removed.App) + 3 new = 4 total (merge without prune keeps existing).
	if result.Counts.Included != 4 {
		t.Errorf("expected 4 included (existing preserved without prune), got %d", result.Counts.Included)
	}
}

// ---------------------------------------------------------------------------
// App sorting: output sorted by id (mirrors Pester Capture.DeterministicOutput)
// ---------------------------------------------------------------------------

func TestRunCapture_AppsSortedByID(t *testing.T) {
	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "test-sorted.jsonc")

	withMockSnapshot(sampleApps(), nil, func() {
		noopDisplayNames(func() {
			emptyCatalog(func() {
				_, err := RunCapture(CaptureFlags{Out: outPath})
				if err != nil {
					t.Fatalf("RunCapture returned unexpected error: %+v", err)
				}
			})
		})
	})

	data, readErr := os.ReadFile(outPath)
	if readErr != nil {
		t.Fatalf("failed to read output: %v", readErr)
	}

	var mf struct {
		Apps []struct {
			ID string `json:"id"`
		} `json:"apps"`
	}
	if jsonErr := json.Unmarshal(data, &mf); jsonErr != nil {
		t.Fatalf("output is not valid JSON: %v", jsonErr)
	}

	for i := 1; i < len(mf.Apps); i++ {
		if mf.Apps[i-1].ID > mf.Apps[i].ID {
			t.Errorf("apps not sorted by id: %q > %q", mf.Apps[i-1].ID, mf.Apps[i].ID)
		}
	}
}

// ---------------------------------------------------------------------------
// buildAppsIncluded: display name from map and fallback
// (mirrors Pester Capture.ArtifactContract display name tests)
// ---------------------------------------------------------------------------

func TestBuildAppsIncluded_IncludesDisplayNameFromMap(t *testing.T) {
	apps := []capturedApp{
		{ID: "git-git", Refs: map[string]string{"windows": "Git.Git"}, Name: ""},
	}
	nameMap := map[string]string{"Git.Git": "Git"}

	result := buildAppsIncluded(apps, nameMap)
	if len(result) != 1 {
		t.Fatalf("expected 1 app, got %d", len(result))
	}
	if result[0].Name != "Git" {
		t.Errorf("expected name=%q from display map, got %q", "Git", result[0].Name)
	}
	if result[0].ID != "Git.Git" {
		t.Errorf("expected id=%q, got %q", "Git.Git", result[0].ID)
	}
	if result[0].Source != "winget" {
		t.Errorf("expected source=winget, got %q", result[0].Source)
	}
}

func TestBuildAppsIncluded_FallbackToSnapshotName(t *testing.T) {
	apps := []capturedApp{
		{ID: "git-git", Refs: map[string]string{"windows": "Git.Git"}, Name: "Git from snapshot"},
	}
	// Display name map does NOT contain Git.Git.
	nameMap := map[string]string{}

	result := buildAppsIncluded(apps, nameMap)
	if result[0].Name != "Git from snapshot" {
		t.Errorf("expected fallback to snapshot name %q, got %q", "Git from snapshot", result[0].Name)
	}
}

func TestBuildAppsIncluded_OmitsNameWhenBothEmpty(t *testing.T) {
	apps := []capturedApp{
		{ID: "git-git", Refs: map[string]string{"windows": "Git.Git"}, Name: ""},
	}
	nameMap := map[string]string{}

	result := buildAppsIncluded(apps, nameMap)
	if result[0].Name != "" {
		t.Errorf("expected empty name when both sources empty, got %q", result[0].Name)
	}
}

// ---------------------------------------------------------------------------
// buildConfigModuleMap tests
// (mirrors Pester Build-ConfigModuleMap)
// ---------------------------------------------------------------------------

func TestBuildConfigModuleMap_SingleWingetRef(t *testing.T) {
	mods := []*modules.Module{
		{
			ID:          "apps.git",
			DisplayName: "Git",
			Matches:     modules.MatchCriteria{Winget: []string{"Git.Git"}},
		},
	}
	m := buildConfigModuleMap(mods)
	if m["Git.Git"] != "apps.git" {
		t.Errorf("expected map[Git.Git]=apps.git, got %q", m["Git.Git"])
	}
	if len(m) != 1 {
		t.Errorf("expected 1 entry, got %d", len(m))
	}
}

func TestBuildConfigModuleMap_MultipleWingetRefs(t *testing.T) {
	mods := []*modules.Module{
		{
			ID:          "apps.vscode",
			DisplayName: "VS Code",
			Matches: modules.MatchCriteria{
				Winget: []string{"Microsoft.VisualStudioCode", "Microsoft.VisualStudioCode.Insiders"},
			},
		},
	}
	m := buildConfigModuleMap(mods)
	if m["Microsoft.VisualStudioCode"] != "apps.vscode" {
		t.Errorf("expected map entry for VSCode, got %q", m["Microsoft.VisualStudioCode"])
	}
	if m["Microsoft.VisualStudioCode.Insiders"] != "apps.vscode" {
		t.Errorf("expected map entry for VSCode.Insiders, got %q", m["Microsoft.VisualStudioCode.Insiders"])
	}
	if len(m) != 2 {
		t.Errorf("expected 2 entries, got %d", len(m))
	}
}

func TestBuildConfigModuleMap_NoWingetRefs_UsesModuleID(t *testing.T) {
	mods := []*modules.Module{
		{
			ID:          "apps.nowinget",
			DisplayName: "No Winget",
			Matches:     modules.MatchCriteria{Exe: []string{"nowinget.exe"}},
		},
	}
	m := buildConfigModuleMap(mods)
	if m["apps.nowinget"] != "apps.nowinget" {
		t.Errorf("expected map[apps.nowinget]=apps.nowinget, got %q", m["apps.nowinget"])
	}
	if len(m) != 1 {
		t.Errorf("expected 1 entry, got %d", len(m))
	}
}

func TestBuildConfigModuleMap_MixedWingetAndNonWinget(t *testing.T) {
	mods := []*modules.Module{
		{
			ID:      "apps.git",
			Matches: modules.MatchCriteria{Winget: []string{"Git.Git"}},
		},
		{
			ID:      "apps.nowinget",
			Matches: modules.MatchCriteria{Exe: []string{"nowinget.exe"}},
		},
	}
	m := buildConfigModuleMap(mods)
	if m["Git.Git"] != "apps.git" {
		t.Errorf("expected Git.Git mapping")
	}
	if m["apps.nowinget"] != "apps.nowinget" {
		t.Errorf("expected apps.nowinget mapping")
	}
	if len(m) != 2 {
		t.Errorf("expected 2 entries, got %d", len(m))
	}
}

func TestBuildConfigModuleMap_DuplicateWingetRef_LastWins(t *testing.T) {
	// When two modules share the same winget ref, last module in slice wins.
	mods := []*modules.Module{
		{
			ID:      "apps.first",
			Matches: modules.MatchCriteria{Winget: []string{"Shared.PackageId"}},
		},
		{
			ID:      "apps.second",
			Matches: modules.MatchCriteria{Winget: []string{"Shared.PackageId"}},
		},
	}
	m := buildConfigModuleMap(mods)
	if m["Shared.PackageId"] != "apps.second" {
		t.Errorf("expected last module to win, got %q", m["Shared.PackageId"])
	}
	if len(m) != 1 {
		t.Errorf("expected 1 entry (deduped), got %d", len(m))
	}
}

// ---------------------------------------------------------------------------
// Update merge: apps sorted alphabetically by id in output
// (mirrors Pester Merge-ManifestsForUpdate — "Should sort apps by id")
// ---------------------------------------------------------------------------

func TestRunCapture_Update_MergedAppsSortedByID(t *testing.T) {
	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "test-update-sorted.jsonc")

	existingManifest := `{
  "version": 1,
  "name": "test",
  "apps": [
    {"id": "zebra-app", "refs": {"windows": "Zebra.App"}}
  ]
}`
	existingPath := filepath.Join(tmpDir, "existing.jsonc")
	if err := os.WriteFile(existingPath, []byte(existingManifest), 0644); err != nil {
		t.Fatal(err)
	}

	appsWithAlpha := []snapshot.SnapshotApp{
		{Name: "Alpha App", ID: "Alpha.App", Version: "1.0", Source: "winget"},
		{Name: "Beta App", ID: "Beta.App", Version: "1.0", Source: "winget"},
	}

	withMockSnapshot(appsWithAlpha, nil, func() {
		noopDisplayNames(func() {
			emptyCatalog(func() {
				_, err := RunCapture(CaptureFlags{
					Out:      outPath,
					Manifest: existingPath,
					Update:   true,
				})
				if err != nil {
					t.Fatalf("RunCapture returned unexpected error: %+v", err)
				}
			})
		})
	})

	data, readErr := os.ReadFile(outPath)
	if readErr != nil {
		t.Fatalf("failed to read output: %v", readErr)
	}

	var mf struct {
		Apps []struct {
			ID string `json:"id"`
		} `json:"apps"`
	}
	if jsonErr := json.Unmarshal(data, &mf); jsonErr != nil {
		t.Fatalf("output is not valid JSON: %v", jsonErr)
	}

	if len(mf.Apps) != 3 {
		t.Fatalf("expected 3 apps, got %d", len(mf.Apps))
	}

	// Verify sorted order.
	for i := 1; i < len(mf.Apps); i++ {
		if mf.Apps[i-1].ID > mf.Apps[i].ID {
			t.Errorf("apps not sorted by id: %q > %q", mf.Apps[i-1].ID, mf.Apps[i].ID)
		}
	}
}

// ---------------------------------------------------------------------------
// Update mode: non-existent manifest behaves like normal capture
// (mirrors Pester "Should behave like normal capture when manifest doesn't exist")
// ---------------------------------------------------------------------------

func TestRunCapture_Update_NonExistentManifest_CreatesNew(t *testing.T) {
	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "test-new-update.jsonc")
	nonExistentPath := filepath.Join(tmpDir, "nonexistent.jsonc")

	// When --update is set but manifest doesn't exist, should return error.
	withMockSnapshot(sampleApps(), nil, func() {
		noopDisplayNames(func() {
			emptyCatalog(func() {
				_, err := RunCapture(CaptureFlags{
					Out:      outPath,
					Manifest: nonExistentPath,
					Update:   true,
				})
				// The Go implementation returns MANIFEST_NOT_FOUND for non-existent manifest.
				if err == nil {
					t.Fatal("expected error for non-existent manifest in update mode, got nil")
				}
			})
		})
	})
}

// ---------------------------------------------------------------------------
// Deterministic output: same input produces same app order
// (mirrors Pester Deterministic output)
// ---------------------------------------------------------------------------

func TestRunCapture_DeterministicOutput_SameInputSameOrder(t *testing.T) {
	tmpDir := t.TempDir()

	// Run capture twice with the same apps.
	var ids1, ids2 []string
	for run, outSlice := range map[int]*[]string{0: &ids1, 1: &ids2} {
		outPath := filepath.Join(tmpDir, "run"+string(rune('0'+run))+".jsonc")
		withMockSnapshot(sampleApps(), nil, func() {
			noopDisplayNames(func() {
				emptyCatalog(func() {
					_, err := RunCapture(CaptureFlags{Out: outPath})
					if err != nil {
						t.Fatalf("RunCapture run %d returned unexpected error: %+v", run, err)
					}
				})
			})
		})

		data, readErr := os.ReadFile(outPath)
		if readErr != nil {
			t.Fatalf("failed to read output: %v", readErr)
		}

		var mf struct {
			Apps []struct {
				ID string `json:"id"`
			} `json:"apps"`
		}
		if jsonErr := json.Unmarshal(data, &mf); jsonErr != nil {
			t.Fatalf("output is not valid JSON: %v", jsonErr)
		}

		for _, app := range mf.Apps {
			*outSlice = append(*outSlice, app.ID)
		}
	}

	if len(ids1) != len(ids2) {
		t.Fatalf("expected same number of apps: %d vs %d", len(ids1), len(ids2))
	}
	for i := range ids1 {
		if ids1[i] != ids2[i] {
			t.Errorf("app order differs at index %d: %q vs %q", i, ids1[i], ids2[i])
		}
	}
}

// ---------------------------------------------------------------------------
// Manifest structure: captured timestamp is RFC3339
// (mirrors Pester "Should update captured timestamp")
// ---------------------------------------------------------------------------

func TestRunCapture_CapturedTimestamp_IsRFC3339(t *testing.T) {
	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "test-timestamp.jsonc")

	withMockSnapshot(sampleApps(), nil, func() {
		noopDisplayNames(func() {
			emptyCatalog(func() {
				_, err := RunCapture(CaptureFlags{Out: outPath, Name: "ts-test"})
				if err != nil {
					t.Fatalf("RunCapture returned unexpected error: %+v", err)
				}
			})
		})
	})

	data, readErr := os.ReadFile(outPath)
	if readErr != nil {
		t.Fatalf("failed to read output: %v", readErr)
	}

	var mf map[string]interface{}
	if jsonErr := json.Unmarshal(data, &mf); jsonErr != nil {
		t.Fatalf("output is not valid JSON: %v", jsonErr)
	}

	captured, ok := mf["captured"].(string)
	if !ok || captured == "" {
		t.Fatal("expected captured timestamp string")
	}

	// Must contain "T" separator (RFC3339 format).
	if !strings.Contains(captured, "T") {
		t.Errorf("expected RFC3339 timestamp, got %q", captured)
	}
}

// ---------------------------------------------------------------------------
// resolveOutputPath tests
// ---------------------------------------------------------------------------

func TestResolveOutputPath_DefaultTimestampBased(t *testing.T) {
	path := resolveOutputPath(CaptureFlags{})
	if !strings.HasPrefix(path, "captured-") {
		t.Errorf("expected default path to start with 'captured-', got %q", path)
	}
	if !strings.HasSuffix(path, ".jsonc") {
		t.Errorf("expected default path to end with '.jsonc', got %q", path)
	}
}

func TestResolveOutputPath_OutFlagTakesPrecedence(t *testing.T) {
	path := resolveOutputPath(CaptureFlags{Out: "/custom/output.jsonc"})
	if path != "/custom/output.jsonc" {
		t.Errorf("expected /custom/output.jsonc, got %q", path)
	}
}

// ---------------------------------------------------------------------------
// INV-MANIFEST-NEVER-EMPTY: output always has version, name, apps
// (mirrors Pester INV-MANIFEST-NEVER-EMPTY)
// ---------------------------------------------------------------------------

func TestRunCapture_ManifestNeverEmptyObject(t *testing.T) {
	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "test-never-empty.jsonc")

	// Discover mode allows an empty snapshot (fresh machine) — use it here
	// since this test verifies manifest structure, not the empty-guard.
	withRetryDelay(0, func() {
		withMockSnapshot([]snapshot.SnapshotApp{}, nil, func() {
			noopDisplayNames(func() {
				emptyCatalog(func() {
					_, err := RunCapture(CaptureFlags{Out: outPath, Discover: true})
					if err != nil {
						t.Fatalf("RunCapture returned unexpected error: %+v", err)
					}
				})
			})
		})
	})

	data, readErr := os.ReadFile(outPath)
	if readErr != nil {
		t.Fatalf("failed to read output: %v", readErr)
	}

	var mf map[string]interface{}
	if jsonErr := json.Unmarshal(data, &mf); jsonErr != nil {
		t.Fatalf("output is not valid JSON: %v", jsonErr)
	}

	// Must not be empty object {}.
	if len(mf) == 0 {
		t.Fatal("INV-MANIFEST-NEVER-EMPTY violated: output is empty object {}")
	}

	// Must contain version field.
	if _, ok := mf["version"]; !ok {
		t.Error("expected manifest to contain 'version' field")
	}

	// Must contain apps array.
	if _, ok := mf["apps"]; !ok {
		t.Error("expected manifest to contain 'apps' field")
	}
}

// ---------------------------------------------------------------------------
// Empty snapshot retry (winget lock contention guard)
// ---------------------------------------------------------------------------

func TestRunCapture_EmptySnapshot_RetriesAndSucceeds(t *testing.T) {
	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "test-retry-success.jsonc")

	calls := []snapshotCall{
		{apps: []snapshot.SnapshotApp{}, err: nil}, // first call: empty (lock contention)
		{apps: sampleApps(), err: nil},             // retry: success
	}

	var result *CaptureResult
	withRetryDelay(0, func() {
		withMockSnapshotSequence(calls, func() {
			noopDisplayNames(func() {
				emptyCatalog(func() {
					r, err := RunCapture(CaptureFlags{Out: outPath})
					if err != nil {
						t.Fatalf("RunCapture should succeed after retry: %+v", err)
					}
					result = r.(*CaptureResult)
				})
			})
		})
	})

	if result.Counts.Included != 3 {
		t.Errorf("expected 3 included after retry, got %d", result.Counts.Included)
	}
	if result.Counts.TotalFound != 3 {
		t.Errorf("expected totalFound=3 after retry, got %d", result.Counts.TotalFound)
	}
}

func TestRunCapture_EventsJSONL_ZeroPackageRetryKeepsStderrValidNDJSON(t *testing.T) {
	outPath := filepath.Join(t.TempDir(), "captured.jsonc")
	calls := []snapshotCall{
		{apps: []snapshot.SnapshotApp{}},
		{apps: sampleExportSet()},
	}
	origGOOS := captureGOOSFn
	captureGOOSFn = func() string { return "windows" }
	defer func() { captureGOOSFn = origGOOS }()

	stderr := captureStderr(t, func() {
		withRetryDelay(0, func() {
			withMockSnapshotSequence(calls, func() {
				noopDisplayNames(func() {
					emptyCatalog(func() {
						_, eerr := RunCapture(CaptureFlags{Out: outPath, Drivers: []string{"winget"}, Events: "jsonl"})
						if eerr != nil {
							t.Fatalf("RunCapture: %v", eerr)
						}
					})
				})
			})
		})
	})

	assertCaptureEventStreamIsNDJSON(t, stderr)
}

func TestRunCapture_EmptySnapshot_RetryAlsoEmpty_FailsWithCaptureFailed(t *testing.T) {
	calls := []snapshotCall{
		{apps: []snapshot.SnapshotApp{}, err: nil},
		{apps: []snapshot.SnapshotApp{}, err: nil},
	}

	withRetryDelay(0, func() {
		withMockSnapshotSequence(calls, func() {
			noopDisplayNames(func() {
				_, err := RunCapture(CaptureFlags{Out: "test.jsonc"})
				if err == nil {
					t.Fatal("expected CAPTURE_FAILED when retry also returns 0 apps")
				}
				if string(err.Code) != "CAPTURE_FAILED" {
					t.Errorf("expected code CAPTURE_FAILED, got %q", err.Code)
				}
				if !strings.Contains(err.Message, "no packages after retry") {
					t.Errorf("expected message about retry, got %q", err.Message)
				}
			})
		})
	})
}

func TestRunCapture_NormalCapture_NoRetry(t *testing.T) {
	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "test-no-retry.jsonc")

	callCount := 0
	orig := takeSnapshotFn
	takeSnapshotFn = func() ([]snapshot.SnapshotApp, error) {
		callCount++
		return sampleApps(), nil
	}
	defer func() { takeSnapshotFn = orig }()

	// Force the no-realizer path so the capture fork does not divert this
	// winget-path test to the Nix realizer on linux/darwin.
	origRealizer := newRealizerFn
	newRealizerFn = func() (realizer.Realizer, error) { return nil, ErrNoRealizer }
	defer func() { newRealizerFn = origRealizer }()

	withRetryDelay(0, func() {
		noopDisplayNames(func() {
			emptyCatalog(func() {
				_, err := RunCapture(CaptureFlags{Out: outPath})
				if err != nil {
					t.Fatalf("RunCapture returned unexpected error: %+v", err)
				}
			})
		})
	})

	if callCount != 1 {
		t.Errorf("expected takeSnapshotFn called once (no retry), got %d", callCount)
	}
}

// ---------------------------------------------------------------------------
// capture --pin: installed-version pinning (windows-version-capture-pinning)
// ---------------------------------------------------------------------------

// Without --pin, no app carries a version field even when the installed-apps
// snapshot exposes versions (byte-regression guard).
func TestRunCapture_NoPin_OmitsVersion(t *testing.T) {
	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "test-no-pin.jsonc")

	withMockInstalledApps(t, sampleInstalledApps())
	withMockSnapshot(sampleExportSet(), nil, func() {
		emptyCatalog(func() {
			_, err := RunCapture(CaptureFlags{Out: outPath})
			if err != nil {
				t.Fatalf("RunCapture returned unexpected error: %+v", err)
			}
		})
	})

	for _, app := range readManifestApps(t, outPath) {
		if _, has := app["version"]; has {
			t.Errorf("expected no version field without --pin, got %v", app["version"])
		}
	}
}

// With --pin, each app's version matches the mocked installed version (not the
// export-set version).
func TestRunCapture_Pin_WritesInstalledVersions(t *testing.T) {
	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "test-pin.jsonc")

	want := map[string]string{
		"Microsoft.VisualStudioCode": "1.99.0",
		"Git.Git":                    "2.44.0",
		"Google.Chrome":              "130.0.1",
	}

	withMockInstalledApps(t, sampleInstalledApps())
	withMockSnapshot(sampleExportSet(), nil, func() {
		emptyCatalog(func() {
			_, err := RunCapture(CaptureFlags{Out: outPath, Pin: true})
			if err != nil {
				t.Fatalf("RunCapture returned unexpected error: %+v", err)
			}
		})
	})

	apps := readManifestApps(t, outPath)
	if len(apps) != len(want) {
		t.Fatalf("expected %d apps, got %d", len(want), len(apps))
	}
	for ref, wantVer := range want {
		gotVer, found := manifestAppVersion(apps, ref)
		if !found {
			t.Errorf("app %q missing from output", ref)
			continue
		}
		if gotVer != wantVer {
			t.Errorf("app %q: version=%q, want %q", ref, gotVer, wantVer)
		}
	}
}

// With --pin, an app present in the export set but absent from the installed
// snapshot (skew) is emitted without a version field; capture still succeeds.
func TestRunCapture_Pin_SkewedApp_OmitsVersion(t *testing.T) {
	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "test-pin-skew.jsonc")

	// Installed snapshot covers only two of the three export-set apps; Chrome
	// is absent and must be emitted without a version.
	installed := []snapshot.SnapshotApp{
		{Name: "Visual Studio Code", ID: "Microsoft.VisualStudioCode", Version: "1.99.0"},
		{Name: "Git", ID: "Git.Git", Version: "2.44.0"},
	}

	withMockInstalledApps(t, installed)
	withMockSnapshot(sampleExportSet(), nil, func() {
		emptyCatalog(func() {
			_, err := RunCapture(CaptureFlags{Out: outPath, Pin: true})
			if err != nil {
				t.Fatalf("RunCapture returned unexpected error: %+v", err)
			}
		})
	})

	apps := readManifestApps(t, outPath)
	ver, found := manifestAppVersion(apps, "Google.Chrome")
	if !found {
		t.Fatal("expected Google.Chrome in output")
	}
	if ver != "" {
		t.Errorf("expected no version for skewed app Google.Chrome, got %q", ver)
	}
}

// With --pin, a total failure of the installed-apps snapshot degrades to no
// versions; capture still succeeds (display-name posture preserved).
func TestRunCapture_Pin_SnapshotError_SucceedsWithoutVersions(t *testing.T) {
	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "test-pin-snap-err.jsonc")

	withMockSnapshot(sampleExportSet(), nil, func() {
		withMockDisplayNames(nil, errors.New("winget list failed"), func() {
			emptyCatalog(func() {
				_, err := RunCapture(CaptureFlags{Out: outPath, Pin: true})
				if err != nil {
					t.Fatalf("capture should succeed when installed-apps snapshot fails: %+v", err)
				}
			})
		})
	})

	for _, app := range readManifestApps(t, outPath) {
		if _, has := app["version"]; has {
			t.Errorf("expected no version when installed-apps snapshot failed, got %v", app["version"])
		}
	}
}

func TestRunCapture_EventsJSONL_PinWithoutVersionsKeepsStderrValidNDJSON(t *testing.T) {
	outPath := filepath.Join(t.TempDir(), "captured.jsonc")
	origGOOS := captureGOOSFn
	captureGOOSFn = func() string { return "windows" }
	defer func() { captureGOOSFn = origGOOS }()

	stderr := captureStderr(t, func() {
		withMockSnapshot(sampleExportSet(), nil, func() {
			withMockDisplayNames(nil, errors.New("winget list failed"), func() {
				emptyCatalog(func() {
					_, eerr := RunCapture(CaptureFlags{
						Out: outPath, Pin: true, Drivers: []string{"winget"}, Events: "jsonl",
					})
					if eerr != nil {
						t.Fatalf("RunCapture: %v", eerr)
					}
				})
			})
		})
	})

	assertCaptureEventStreamIsNDJSON(t, stderr)
}

// With --pin --sanitize, versions are kept, _name is stripped, and apps are
// sorted by id.
func TestRunCapture_PinSanitize_KeepsVersions(t *testing.T) {
	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "test-pin-sanitize.jsonc")

	withMockInstalledApps(t, sampleInstalledApps())
	withMockSnapshot(sampleExportSet(), nil, func() {
		emptyCatalog(func() {
			_, err := RunCapture(CaptureFlags{Out: outPath, Pin: true, Sanitize: true})
			if err != nil {
				t.Fatalf("RunCapture returned unexpected error: %+v", err)
			}
		})
	})

	apps := readManifestApps(t, outPath)
	if len(apps) != 3 {
		t.Fatalf("expected 3 apps, got %d", len(apps))
	}
	prevID := ""
	for i, app := range apps {
		if _, has := app["_name"]; has {
			t.Errorf("sanitized app should not have _name field: %v", app)
		}
		if v, _ := app["version"].(string); v == "" {
			t.Errorf("expected version present under --pin --sanitize for %v", app["id"])
		}
		id, _ := app["id"].(string)
		if i > 0 && prevID > id {
			t.Errorf("apps not sorted by id: %q > %q", prevID, id)
		}
		prevID = id
	}
}

// --update without --pin preserves an existing app's version AND driver through
// the merge (previously both were silently dropped).
func TestRunCapture_Update_NoPin_PreservesVersionAndDriver(t *testing.T) {
	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "test-update-preserve-version.jsonc")

	existing := `{
  "version": 1,
  "name": "existing",
  "apps": [
    {"id": "microsoft-visualstudiocode", "version": "1.85.0", "driver": "winget", "refs": {"windows": "Microsoft.VisualStudioCode"}}
  ]
}`
	existingPath := filepath.Join(tmpDir, "existing.jsonc")
	if err := os.WriteFile(existingPath, []byte(existing), 0644); err != nil {
		t.Fatal(err)
	}

	withMockInstalledApps(t, sampleInstalledApps())
	withMockSnapshot(sampleExportSet(), nil, func() {
		emptyCatalog(func() {
			_, err := RunCapture(CaptureFlags{Out: outPath, Manifest: existingPath, Update: true})
			if err != nil {
				t.Fatalf("RunCapture returned unexpected error: %+v", err)
			}
		})
	})

	found := false
	for _, app := range readManifestApps(t, outPath) {
		if id, _ := app["id"].(string); id == "microsoft-visualstudiocode" {
			found = true
			if v, _ := app["version"].(string); v != "1.85.0" {
				t.Errorf("expected version 1.85.0 preserved through --update, got %q", v)
			}
			if d, _ := app["driver"].(string); d != "winget" {
				t.Errorf("expected driver winget preserved through --update, got %q", d)
			}
		}
	}
	if !found {
		t.Fatal("expected microsoft-visualstudiocode in merged output")
	}
}

// --update --pin refreshes an existing pin (1.0.0) to the drifted installed
// version (2.0.0).
func TestRunCapture_UpdatePin_RefreshesDriftedVersion(t *testing.T) {
	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "test-update-pin-refresh.jsonc")

	existing := `{
  "version": 1,
  "name": "existing",
  "apps": [
    {"id": "microsoft-visualstudiocode", "version": "1.0.0", "refs": {"windows": "Microsoft.VisualStudioCode"}}
  ]
}`
	existingPath := filepath.Join(tmpDir, "existing.jsonc")
	if err := os.WriteFile(existingPath, []byte(existing), 0644); err != nil {
		t.Fatal(err)
	}

	installed := []snapshot.SnapshotApp{
		{Name: "Visual Studio Code", ID: "Microsoft.VisualStudioCode", Version: "2.0.0"},
	}

	withMockInstalledApps(t, installed)
	withMockSnapshot(sampleExportSet(), nil, func() {
		emptyCatalog(func() {
			_, err := RunCapture(CaptureFlags{Out: outPath, Manifest: existingPath, Update: true, Pin: true})
			if err != nil {
				t.Fatalf("RunCapture returned unexpected error: %+v", err)
			}
		})
	})

	ver, found := manifestAppVersion(readManifestApps(t, outPath), "Microsoft.VisualStudioCode")
	if !found {
		t.Fatal("expected Microsoft.VisualStudioCode in merged output")
	}
	if ver != "2.0.0" {
		t.Errorf("expected refreshed version 2.0.0, got %q", ver)
	}
}

// --update --pin keeps an existing pin when the app is absent from the
// installed snapshot (a missing version never blanks a declared pin).
func TestRunCapture_UpdatePin_MissingVersion_RetainsPin(t *testing.T) {
	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "test-update-pin-retain.jsonc")

	existing := `{
  "version": 1,
  "name": "existing",
  "apps": [
    {"id": "microsoft-visualstudiocode", "version": "1.0.0", "refs": {"windows": "Microsoft.VisualStudioCode"}}
  ]
}`
	existingPath := filepath.Join(tmpDir, "existing.jsonc")
	if err := os.WriteFile(existingPath, []byte(existing), 0644); err != nil {
		t.Fatal(err)
	}

	// Installed snapshot exposes a version for a DIFFERENT app only — so the
	// pin is retained specifically because the pinned app's key missed, not
	// because the map was empty (guards the merge's lookup key).
	withMockInstalledApps(t, []snapshot.SnapshotApp{
		{ID: "Google.Chrome", Name: "Google Chrome", Version: "126.0.6478.127"},
	})
	withMockSnapshot(sampleExportSet(), nil, func() {
		emptyCatalog(func() {
			_, err := RunCapture(CaptureFlags{Out: outPath, Manifest: existingPath, Update: true, Pin: true})
			if err != nil {
				t.Fatalf("RunCapture returned unexpected error: %+v", err)
			}
		})
	})

	ver, found := manifestAppVersion(readManifestApps(t, outPath), "Microsoft.VisualStudioCode")
	if !found {
		t.Fatal("expected Microsoft.VisualStudioCode in merged output")
	}
	if ver != "1.0.0" {
		t.Errorf("expected retained pin 1.0.0, got %q", ver)
	}
}

// --update --pin gives a newly discovered app its installed version (new apps
// flow through the conversion loop).
func TestRunCapture_UpdatePin_NewApp_CarriesVersion(t *testing.T) {
	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "test-update-pin-new.jsonc")

	// Existing manifest has only VSCode; Chrome is newly discovered.
	existing := `{
  "version": 1,
  "name": "existing",
  "apps": [
    {"id": "microsoft-visualstudiocode", "refs": {"windows": "Microsoft.VisualStudioCode"}}
  ]
}`
	existingPath := filepath.Join(tmpDir, "existing.jsonc")
	if err := os.WriteFile(existingPath, []byte(existing), 0644); err != nil {
		t.Fatal(err)
	}

	withMockInstalledApps(t, sampleInstalledApps())
	withMockSnapshot(sampleExportSet(), nil, func() {
		emptyCatalog(func() {
			_, err := RunCapture(CaptureFlags{Out: outPath, Manifest: existingPath, Update: true, Pin: true})
			if err != nil {
				t.Fatalf("RunCapture returned unexpected error: %+v", err)
			}
		})
	})

	ver, found := manifestAppVersion(readManifestApps(t, outPath), "Google.Chrome")
	if !found {
		t.Fatal("expected newly discovered Google.Chrome in merged output")
	}
	if ver != "130.0.1" {
		t.Errorf("expected new app version 130.0.1, got %q", ver)
	}
}

func TestRunCapture_Discover_EmptySnapshot_Succeeds(t *testing.T) {
	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "test-discover-empty.jsonc")

	withRetryDelay(0, func() {
		withMockSnapshot([]snapshot.SnapshotApp{}, nil, func() {
			noopDisplayNames(func() {
				emptyCatalog(func() {
					r, err := RunCapture(CaptureFlags{
						Out:      outPath,
						Discover: true,
					})
					if err != nil {
						t.Fatalf("Discover mode should allow empty snapshot: %+v", err)
					}
					result := r.(*CaptureResult)
					if result.Counts.Included != 0 {
						t.Errorf("expected 0 included in discover mode with empty snapshot, got %d", result.Counts.Included)
					}
				})
			})
		})
	})
}
