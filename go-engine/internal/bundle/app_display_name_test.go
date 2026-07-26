// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package bundle

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/manifest"
)

// An app's display name is the only identity it keeps once its package id stops
// resolving — a browser that self-updates out of winget's tracking still has a
// name in ARP. The bundle writer round-trips the source manifest through
// manifest.App, so a name the writer does not bind is silently dropped here and
// every bundle ships apps with no display name at all. That is exactly what
// happened while capture serialized the field as "_name": written by capture,
// lost on re-serialization, invisible until a profile was applied months later
// and reported installed software as missing.
func TestCaptureBundlePreservesAppDisplayNames(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "input.jsonc")
	source := `{
      "version": 1,
      "name": "capture",
      "apps": [
        { "id": "7zip-7zip", "refs": { "windows": "7zip.7zip" }, "displayName": "7-Zip 25.01 (x64)" },
        { "id": "no-name", "refs": { "windows": "Some.Package" } }
      ]
    }`
	if err := os.WriteFile(manifestPath, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}

	request := CaptureBundleRequest{
		ManifestPath:    manifestPath,
		OutputPath:      filepath.Join(dir, "capture.zip"),
		EndstateVersion: "test-version",
	}
	if _, err := CreateCaptureBundle(request); err != nil {
		t.Fatalf("CreateCaptureBundle: %v", err)
	}

	loaded, _ := loadCaptureBundle(t, request.OutputPath)
	byID := make(map[string]manifest.App, len(loaded.Apps))
	for _, app := range loaded.Apps {
		byID[app.ID] = app
	}

	if got := byID["7zip-7zip"].DisplayName; got != "7-Zip 25.01 (x64)" {
		t.Fatalf("display name did not survive the bundle round-trip: got %q", got)
	}
	// An app whose enumeration supplied no name stays nameless rather than
	// gaining an empty key.
	if got := byID["no-name"].DisplayName; got != "" {
		t.Fatalf("app without a captured name gained one: %q", got)
	}

	// And it is actually on disk under the key readers bind, not merely in the
	// parsed struct.
	extracted := extractCaptureBundle(t, request.OutputPath)
	raw, err := os.ReadFile(extracted)
	if err != nil {
		t.Fatal(err)
	}
	var written struct {
		Apps []map[string]json.RawMessage `json:"apps"`
	}
	if err := json.Unmarshal(manifest.StripJsoncComments(raw), &written); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, app := range written.Apps {
		if _, has := app["displayName"]; has {
			found = true
		}
		if _, has := app["_name"]; has {
			t.Fatalf("bundle manifest still carries the legacy _name key: %v", app)
		}
	}
	if !found {
		t.Fatal("no app in the written bundle manifest carries displayName")
	}
}
