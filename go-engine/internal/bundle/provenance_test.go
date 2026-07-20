// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package bundle

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
)

// TestCreateCaptureBundle_LegacyRestoreCarriesFromModule locks module provenance
// into every bundle.
//
// Restore input building routes an entry with an empty FromModule into
// `ordinaryRestores`, which is converted with an empty filter and is never
// touched by --only scoping. Provenance used to be attached only for mixed-v2
// bundles, so a plain v1 bundle's restore entries were unfilterable: a recipient
// running `apply --only <app> --enable-restore` against a shared bundle received
// every module's config rather than the selection, and `--restore-filter` was a
// silent no-op.
//
// This exercises CreateCaptureBundle — the function capture actually calls.
// CreateBundle is legacy and has no production caller, so a test against it
// would prove nothing about shipped behaviour.
func TestCreateCaptureBundle_LegacyRestoreCarriesFromModule(t *testing.T) {
	dir := t.TempDir()

	manifestPath := filepath.Join(dir, "manifest.jsonc")
	if err := os.WriteFile(manifestPath, []byte(`{
  "version": 1,
  "name": "provenance-test",
  "apps": [
    {"id": "test-app", "refs": {"windows": "TestVendor.TestApp"}}
  ]
}`), 0644); err != nil {
		t.Fatal(err)
	}

	configDir := filepath.Join(dir, "fake-config")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatal(err)
	}
	configFile := filepath.Join(configDir, "settings.json")
	if err := os.WriteFile(configFile, []byte(`{"key":"value"}`), 0644); err != nil {
		t.Fatal(err)
	}

	mods := []*modules.Module{
		{
			ID:          "apps.test-app",
			DisplayName: "Test App",
			Matches:     modules.MatchCriteria{Winget: []string{"TestVendor.TestApp"}},
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
	if _, err := CreateCaptureBundle(CaptureBundleRequest{
		ManifestPath:    manifestPath,
		OutputPath:      outputZip,
		EndstateVersion: "1.0.0-test",
		Modules:         mods,
	}); err != nil {
		t.Fatalf("CreateCaptureBundle failed: %v", err)
	}

	extractedManifestPath, err := ExtractBundle(outputZip)
	if err != nil {
		t.Fatalf("ExtractBundle failed: %v", err)
	}
	defer os.RemoveAll(filepath.Dir(extractedManifestPath))

	data, err := os.ReadFile(extractedManifestPath)
	if err != nil {
		t.Fatalf("read extracted manifest: %v", err)
	}
	var m manifest.Manifest
	if err := json.Unmarshal(manifest.StripJsoncComments(data), &m); err != nil {
		t.Fatalf("parse extracted manifest: %v", err)
	}

	if len(m.Restore) == 0 {
		t.Fatal("extracted manifest has no restore entries")
	}
	for i, e := range m.Restore {
		if e.FromModule != "apps.test-app" {
			t.Errorf("restore[%d]: FromModule = %q, want %q (empty routes the entry to ordinaryRestores, where no filter or selection can reach it)",
				i, e.FromModule, "apps.test-app")
		}
		// v1 bundles must NOT carry v2 legacy identity — the v1 input validator
		// rejects a manifest that does.
		if e.LegacyCaptureID != "" {
			t.Errorf("restore[%d]: LegacyCaptureID = %q, want empty on a v1 bundle", i, e.LegacyCaptureID)
		}
	}
}
