// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package manifest

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const bundleFixtureManifest = `{
  // capture bundles ship JSONC
  "version": 1,
  "name": "captured",
  "apps": [{ "id": "7zip-7zip", "refs": { "windows": "7zip.7zip" } }]
}`

// writeBundle builds a zip whose entries are the given name→content pairs.
func writeBundle(t *testing.T, entries map[string]string) string {
	t.Helper()
	bundlePath := filepath.Join(t.TempDir(), "endstate-capture.zip")
	file, err := os.Create(bundlePath)
	if err != nil {
		t.Fatal(err)
	}
	archive := zip.NewWriter(file)
	for name, content := range entries {
		entry, err := archive.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return bundlePath
}

// A capture bundle is a complete source for a manifest reader, which is what
// lets it be a schedule baseline without a side-written sidecar file.
func TestLoadManifestOrBundleReadsACaptureBundle(t *testing.T) {
	bundlePath := writeBundle(t, map[string]string{
		BundleManifestEntry:               bundleFixtureManifest,
		"configs/7zip-135f78ef/7-Zip.reg": "payload",
		"metadata.json":                   `{"schemaVersion":"2.0"}`,
	})

	loaded, err := LoadManifestOrBundle(bundlePath)
	if err != nil {
		t.Fatalf("LoadManifestOrBundle: %v", err)
	}
	if loaded.Name != "captured" {
		t.Fatalf("name = %q, want %q", loaded.Name, "captured")
	}
	if len(loaded.Apps) != 1 || loaded.Apps[0].ID != "7zip-7zip" {
		t.Fatalf("apps = %+v, want the bundle's single app", loaded.Apps)
	}
}

// Only the archive ROOT manifest counts. A config payload that happens to carry
// its own manifest.jsonc must never be mistaken for the bundle's.
func TestLoadManifestOrBundleIgnoresNestedManifests(t *testing.T) {
	bundlePath := writeBundle(t, map[string]string{
		"configs/some-app-135f78ef/manifest.jsonc": `{"version":1,"name":"payload","apps":[]}`,
	})

	_, err := LoadManifestOrBundle(bundlePath)
	if err == nil {
		t.Fatal("a bundle with no root manifest.jsonc must not resolve a nested one")
	}
	if !strings.Contains(err.Error(), "archive root") {
		t.Fatalf("error %q should say the bundle has no manifest at its archive root", err)
	}
}

// Plain manifest files keep loading exactly as before — the bundle path is
// additive, not a replacement.
func TestLoadManifestOrBundleLeavesPlainManifestsAlone(t *testing.T) {
	manifestPath := filepath.Join(t.TempDir(), "manifest.jsonc")
	if err := os.WriteFile(manifestPath, []byte(bundleFixtureManifest), 0o644); err != nil {
		t.Fatal(err)
	}

	viaHelper, err := LoadManifestOrBundle(manifestPath)
	if err != nil {
		t.Fatalf("LoadManifestOrBundle on a manifest file: %v", err)
	}
	direct, err := LoadManifest(manifestPath)
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if viaHelper.Name != direct.Name || len(viaHelper.Apps) != len(direct.Apps) {
		t.Fatalf("bundle-aware load diverged from LoadManifest: %+v vs %+v", viaHelper, direct)
	}
}

func TestLoadManifestOrBundleRejectsAnUnreadableBundle(t *testing.T) {
	notAZip := filepath.Join(t.TempDir(), "endstate-capture.zip")
	if err := os.WriteFile(notAZip, []byte("this is not a zip"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := LoadManifestOrBundle(notAZip); err == nil {
		t.Fatal("a .zip that is not a zip must be rejected, not parsed as JSONC")
	}
}

func TestIsBundlePathMatchesExtensionCaseInsensitively(t *testing.T) {
	for _, bundle := range []string{"a.zip", "a.ZIP", `C:\x\endstate-capture.Zip`} {
		if !IsBundlePath(bundle) {
			t.Fatalf("%q should be treated as a bundle", bundle)
		}
	}
	for _, plain := range []string{"manifest.jsonc", "a.zip.jsonc", "zip"} {
		if IsBundlePath(plain) {
			t.Fatalf("%q should not be treated as a bundle", plain)
		}
	}
}
