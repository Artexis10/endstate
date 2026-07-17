// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package bundle

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
)

func TestConfigGenerationModuleFixtures(t *testing.T) {
	root := configGenerationFixtureRoot(t)
	acceptedRoot := filepath.Join(root, "modules", "accepted")

	catalog, diagnostics, err := modules.LoadCatalogWithDiagnostics(acceptedRoot)
	if err != nil {
		t.Fatalf("load accepted fixtures: %v", err)
	}
	if len(diagnostics) != 0 {
		t.Fatalf("accepted fixture diagnostics = %+v, want none", diagnostics)
	}
	wantModules := map[string]int{
		"apps.fixture-forward-json": 2,
		"apps.fixture-legacy":       1,
		"apps.fixture-side-by-side": 2,
		"apps.fixture-stable":       2,
	}
	if len(catalog) != len(wantModules) {
		t.Fatalf("accepted catalog size = %d, want %d: %+v", len(catalog), len(wantModules), catalog)
	}
	for moduleID, wantSchema := range wantModules {
		mod := catalog[moduleID]
		if mod == nil {
			t.Errorf("accepted catalog missing %q", moduleID)
			continue
		}
		if got := mod.EffectiveSchemaVersion(); got != wantSchema {
			t.Errorf("%s schema = %d, want %d", moduleID, got, wantSchema)
		}
		if wantSchema == 1 && !mod.Unversioned {
			t.Errorf("%s is schema v1 but not marked unversioned", moduleID)
		}
		if wantSchema == 2 && (mod.Unversioned || mod.Config == nil) {
			t.Errorf("%s is not a generation-aware schema-v2 fixture: %+v", moduleID, mod)
		}
	}

	rejectedRoot := filepath.Join(root, "modules", "rejected")
	rejected, rejectedDiagnostics, err := modules.LoadCatalogWithDiagnostics(rejectedRoot)
	if err != nil {
		t.Fatalf("load rejected fixtures: %v", err)
	}
	if len(rejected) != 0 {
		t.Fatalf("rejected fixture entered catalog: %+v", rejected)
	}
	if len(rejectedDiagnostics) != 1 || rejectedDiagnostics[0].Code != modules.DiagnosticAmbiguousMigrationRoute {
		t.Fatalf("rejected fixture diagnostics = %+v, want one %s", rejectedDiagnostics, modules.DiagnosticAmbiguousMigrationRoute)
	}
}

func TestConfigGenerationBundleFixturesRoundTrip(t *testing.T) {
	tests := []struct {
		name            string
		wantManifest    int
		wantSchema      string
		wantGenerations bool
	}{
		{name: "legacy-v1", wantManifest: 1, wantSchema: "1.0"},
		{name: "generation-v2", wantManifest: 2, wantSchema: "2.0", wantGenerations: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixtureTree := filepath.Join(configGenerationFixtureRoot(t), "bundles", tt.name)
			assertNoOpaqueZipFixtures(t, fixtureTree)
			zipPath := filepath.Join(t.TempDir(), tt.name+".zip")
			materializeFixtureZip(t, fixtureTree, zipPath)

			manifestPath, err := ExtractBundle(zipPath)
			if err != nil {
				t.Fatalf("ExtractBundle: %v", err)
			}
			extractedRoot := filepath.Dir(manifestPath)
			t.Cleanup(func() { _ = os.RemoveAll(extractedRoot) })
			assertFixtureTreesEqual(t, fixtureTree, extractedRoot)

			loaded, err := manifest.LoadManifest(manifestPath)
			if err != nil {
				t.Fatalf("LoadManifest: %v", err)
			}
			if loaded.Version != tt.wantManifest {
				t.Fatalf("manifest version = %#v, want %d", loaded.Version, tt.wantManifest)
			}
			metadata := readFixtureMetadata(t, extractedRoot)
			if metadata.SchemaVersion != tt.wantSchema {
				t.Fatalf("metadata schema = %q, want %q", metadata.SchemaVersion, tt.wantSchema)
			}

			manifestKeys := readFixtureObject(t, manifestPath)
			metadataKeys := readFixtureObject(t, filepath.Join(extractedRoot, "metadata.json"))
			if !tt.wantGenerations {
				if len(loaded.ConfigCaptures) != 0 || len(loaded.Restore) == 0 || len(loaded.ConfigModules) == 0 {
					t.Fatalf("legacy structure = captures %+v restore %+v modules %+v", loaded.ConfigCaptures, loaded.Restore, loaded.ConfigModules)
				}
				if hasAnyKey(manifestKeys, "configCaptures", "legacyConfigLanes") || hasAnyKey(metadataKeys, "manifestVersion", "configCapturesIncluded") {
					t.Fatalf("legacy fixture leaked generation fields: manifest=%v metadata=%v", sortedObjectKeys(manifestKeys), sortedObjectKeys(metadataKeys))
				}
				return
			}

			if len(loaded.ConfigCaptures) != 1 || len(loaded.Restore) != 0 || len(loaded.ConfigModules) != 0 || len(loaded.LegacyConfigLanes) != 0 {
				t.Fatalf("generation structure = captures %+v restore %+v modules %+v legacy lanes %+v", loaded.ConfigCaptures, loaded.Restore, loaded.ConfigModules, loaded.LegacyConfigLanes)
			}
			if !hasAnyKey(manifestKeys, "configCaptures") || hasAnyKey(manifestKeys, "restore", "configModules", "legacyConfigLanes") {
				t.Fatalf("generation fixture crossed structural boundary: %v", sortedObjectKeys(manifestKeys))
			}
			if metadata.ManifestVersion != 2 || len(metadata.ConfigCapturesIncluded) != 1 || !hasAnyKey(metadataKeys, "manifestVersion", "configCapturesIncluded") {
				t.Fatalf("generation metadata = %+v keys=%v", metadata, sortedObjectKeys(metadataKeys))
			}

			capture := loaded.ConfigCaptures[0]
			payloadRoot := filepath.Join(extractedRoot, filepath.FromSlash(capture.PayloadRoot))
			if err := VerifyPayloadManifest(payloadRoot, capture.PayloadManifest); err != nil {
				t.Fatalf("VerifyPayloadManifest: %v", err)
			}
			assertFrozenModuleFixture(t, extractedRoot, capture)
			if err := VerifyModuleSnapshot(extractedRoot, capture); err != nil {
				t.Fatalf("VerifyModuleSnapshot: %v", err)
			}
		})
	}
}

func assertFrozenModuleFixture(t *testing.T, bundleRoot string, capture manifest.ConfigCapture) {
	t.Helper()
	snapshotPath := filepath.Join(bundleRoot, filepath.FromSlash(capture.CaptureModule.SnapshotPath))
	snapshot, err := os.ReadFile(snapshotPath)
	if err != nil {
		t.Fatal(err)
	}
	frozen, err := modules.ParseModuleJSON(snapshot)
	if err != nil {
		t.Fatalf("parse frozen module: %v", err)
	}
	if !bytes.Equal(bytes.TrimSpace(snapshot), frozen.CanonicalSnapshot()) {
		t.Fatalf("frozen module snapshot is not canonical JSON: %s", capture.CaptureModule.SnapshotPath)
	}
	if frozen.Config == nil || len(frozen.Config.Sets) != 1 || len(frozen.Config.Sets[0].Generations) != 1 {
		t.Fatalf("frozen module has unexpected generation shape: %+v", frozen.Config)
	}
	if got := frozen.Config.Sets[0].Generations[0].Fingerprint; got != capture.SourceGenerationFingerprint {
		t.Fatalf("source generation fingerprint = %s, want frozen generation %s", capture.SourceGenerationFingerprint, got)
	}
}

func configGenerationFixtureRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "testdata", "config-generations"))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func assertNoOpaqueZipFixtures(t *testing.T, root string) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && strings.EqualFold(filepath.Ext(entry.Name()), ".zip") {
			t.Fatalf("opaque zip fixture must not be committed: %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("inspect fixture tree: %v", err)
	}
}

func materializeFixtureZip(t *testing.T, root, zipPath string) {
	t.Helper()
	output, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(output)
	walkErr := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == root || entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			t.Fatalf("fixture entry is not a regular file: %s", path)
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(relative)
		header.Method = zip.Deflate
		entryWriter, err := writer.CreateHeader(header)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		_, err = entryWriter.Write(data)
		return err
	})
	closeErr := writer.Close()
	fileCloseErr := output.Close()
	if walkErr != nil {
		t.Fatalf("materialize zip: %v", walkErr)
	}
	if closeErr != nil {
		t.Fatalf("close zip: %v", closeErr)
	}
	if fileCloseErr != nil {
		t.Fatalf("close zip file: %v", fileCloseErr)
	}
}

func assertFixtureTreesEqual(t *testing.T, wantRoot, gotRoot string) {
	t.Helper()
	want := readFixtureTree(t, wantRoot)
	got := readFixtureTree(t, gotRoot)
	if len(got) != len(want) {
		t.Fatalf("round-trip file count = %d, want %d; got=%v want=%v", len(got), len(want), sortedByteMapKeys(got), sortedByteMapKeys(want))
	}
	for path, wantBytes := range want {
		gotBytes, exists := got[path]
		if !exists || !bytes.Equal(gotBytes, wantBytes) {
			t.Errorf("round-trip mismatch for %q: exists=%v got=%q want=%q", path, exists, gotBytes, wantBytes)
		}
	}
}

func readFixtureTree(t *testing.T, root string) map[string][]byte {
	t.Helper()
	files := make(map[string][]byte)
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == root || entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(relative)] = data
		return nil
	})
	if err != nil {
		t.Fatalf("read fixture tree %s: %v", root, err)
	}
	return files
}

func readFixtureMetadata(t *testing.T, root string) BundleMetadata {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "metadata.json"))
	if err != nil {
		t.Fatal(err)
	}
	var metadata BundleMetadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		t.Fatal(err)
	}
	return metadata
}

func readFixtureObject(t *testing.T, path string) map[string]json.RawMessage {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(manifest.StripJsoncComments(data), &object); err != nil {
		t.Fatal(err)
	}
	return object
}

func hasAnyKey(object map[string]json.RawMessage, keys ...string) bool {
	for _, key := range keys {
		if _, exists := object[key]; exists {
			return true
		}
	}
	return false
}

func sortedObjectKeys(object map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sortedByteMapKeys(object map[string][]byte) []string {
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
