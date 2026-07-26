// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationharness

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/bundle"
	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
)

func TestV2ArtifactRejectsExtraCaptureForeignPayloadWrongProvenanceAndDigest(t *testing.T) {
	runtime, captured, entries, envelope := v2ArtifactFixture(t)
	validPath := writeV2ArtifactZip(t, runtime.Root, "valid.zip", entries)
	if _, failure := inspectV2CaptureArtifact(runtime, validPath, envelope); failure != nil {
		t.Fatalf("valid pure-v2 artifact: %+v", failure)
	}

	tests := []struct {
		name       string
		mutate     func(*manifest.Manifest, map[string][]byte)
		coordinate string
	}{
		{name: "extra config capture", coordinate: "manifest", mutate: func(value *manifest.Manifest, _ map[string][]byte) {
			value.ConfigCaptures = append(value.ConfigCaptures, value.ConfigCaptures[0])
		}},
		{name: "foreign payload", coordinate: "artifact", mutate: func(_ *manifest.Manifest, files map[string][]byte) {
			files["configs/foreign.txt"] = []byte("foreign")
		}},
		{name: "wrong provenance", coordinate: "configCaptures[0]", mutate: func(value *manifest.Manifest, _ map[string][]byte) {
			value.ConfigCaptures[0].CaptureModule.ContentHash = strings.Repeat("f", 64)
		}},
		{name: "wrong payload digest", coordinate: "configCaptures[0].payloadManifest", mutate: func(value *manifest.Manifest, _ map[string][]byte) {
			value.ConfigCaptures[0].PayloadManifest[0].SHA256 = strings.Repeat("0", 64)
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidateManifest := cloneV2Manifest(t, captured)
			candidateEntries := cloneV2Entries(entries)
			test.mutate(&candidateManifest, candidateEntries)
			candidateEntries["manifest.jsonc"] = mustV2JSON(t, candidateManifest)
			path := writeV2ArtifactZip(t, runtime.Root, strings.ReplaceAll(test.name, " ", "-")+".zip", candidateEntries)
			if _, failure := inspectV2CaptureArtifact(runtime, path, envelope); failure == nil || failure.Coordinate != test.coordinate {
				t.Fatalf("failure = %+v, want coordinate %q", failure, test.coordinate)
			}
		})
	}
}

func TestV2ArtifactRequiresLegacyManifestFieldsToBeAbsentNotEmpty(t *testing.T) {
	runtime, _, entries, envelope := v2ArtifactFixture(t)
	for _, field := range []string{"restore", "Restore", "CONFIGMODULES", "legacyConfigLanes", "LegacyConfigLANES"} {
		t.Run(field, func(t *testing.T) {
			candidate := cloneV2Entries(entries)
			var raw map[string]any
			if err := json.Unmarshal(candidate["manifest.jsonc"], &raw); err != nil {
				t.Fatal(err)
			}
			raw[field] = []any{}
			candidate["manifest.jsonc"] = mustV2JSON(t, raw)
			path := writeV2ArtifactZip(t, runtime.Root, "explicit-empty-"+field+".zip", candidate)
			if _, failure := inspectV2CaptureArtifact(runtime, path, envelope); failure == nil || failure.Coordinate != "manifest" {
				t.Fatalf("explicit empty %s accepted: %+v", field, failure)
			}
		})
	}
}

func v2ArtifactFixture(t *testing.T) (*scenarioRuntime, manifest.Manifest, map[string][]byte, []byte) {
	t.Helper()
	runtime, _, _ := v2EvidenceFixture(t)
	mod, err := modules.ParseModuleJSON([]byte(`{
		"moduleSchemaVersion":2,"id":"apps.fixture","displayName":"Fixture","sensitivity":"low",
		"matches":{"pathExists":["%APPDATA%\\Vendor\\settings.ini"]},
		"config":{"instanceDetectors":[{"id":"package","type":"package"}],"sets":[{"id":"preferences","generations":[{"id":"g1","order":1,"matches":[{"versionPattern":"^1\\.2$"}],"capture":{"files":[{"source":"%APPDATA%\\Vendor\\settings.ini","dest":"settings.ini"}]},"restore":[{"type":"copy","source":"settings.ini","target":"%APPDATA%\\Vendor\\settings.ini","backup":true}],"validate":[{"type":"ini-parse","path":"settings.ini"}]}]}]}}
	`))
	if err != nil {
		t.Fatal(err)
	}
	runtime.Module = mod
	runtime.V2Plan.Compiled.Generation.Fingerprint = strings.Repeat("b", 64)
	capturedBytes := []byte("[endstate-validation]\nvalue=captured\n")
	runtime.V2Plan.CaptureTargets[0].Members = []V2FixtureFile{{Relative: ".", Path: runtime.V2Plan.CaptureTargets[0].Resolved, Captured: capturedBytes}}
	runtime.V2Plan.Targets[0].Members = []V2FixtureFile{{Relative: ".", Path: runtime.V2Plan.Targets[0].Resolved, Captured: capturedBytes}}
	digest := sha256.Sum256(capturedBytes)
	snapshotPath := "provenance/modules/apps.fixture-" + mod.Revision + ".json"
	capture := manifest.ConfigCapture{
		CaptureID: runtime.V2Plan.CaptureID, ModuleID: mod.ID, ConfigSetID: runtime.V2Plan.Compiled.Set.ID,
		SourceInstance: manifest.ConfigSourceInstance{
			ID: runtime.V2Plan.Instance.ID, DetectorID: runtime.V2Plan.Instance.DetectorID,
			RawVersion: runtime.V2Plan.Instance.Version.Raw, NormalizedVersion: runtime.V2Plan.Instance.Version.Normalized,
			Evidence: &manifest.ConfigSourceInstanceEvidence{
				Type: runtime.V2Plan.Instance.Evidence.Type, AppID: runtime.V2Plan.Instance.Evidence.AppID,
				Backend: runtime.V2Plan.Instance.Evidence.Backend, Platform: runtime.V2Plan.Instance.Evidence.Platform,
				Ref: runtime.V2Plan.Instance.Evidence.Ref, Driver: runtime.V2Plan.Instance.Evidence.Driver,
			},
		},
		SourceGeneration: runtime.V2Plan.Compiled.Generation.ID, SourceGenerationFingerprint: runtime.V2Plan.Compiled.Generation.Fingerprint,
		CaptureModule:   manifest.CaptureModuleProvenance{SchemaVersion: 2, ContentHash: mod.Revision, SnapshotPath: snapshotPath},
		PayloadRoot:     bundle.ConfigPayloadRoot(mod.ID, runtime.V2Plan.CaptureID),
		PayloadManifest: []manifest.PayloadManifestEntry{{RelativePath: "settings.ini", Size: int64(len(capturedBytes)), SHA256: hex.EncodeToString(digest[:])}},
	}
	captured := manifest.Manifest{
		Version: 2, Apps: []manifest.App{{ID: runtime.Inventory.AppID, Refs: map[string]string{"windows": runtime.Inventory.Ref}, Driver: runtime.Inventory.Driver}},
		ConfigCaptures: []manifest.ConfigCapture{capture},
	}
	metadata := bundle.BundleMetadata{
		SchemaVersion: "2.0", ManifestVersion: 2, CapturedAt: time.Now().UTC().Format(time.RFC3339), OS: "windows",
		ConfigCapturesIncluded: []string{capture.CaptureID}, ConfigModulesIncluded: []string{}, ConfigModulesSkipped: []string{},
	}
	entries := map[string][]byte{
		"manifest.jsonc": mustV2JSON(t, captured), "metadata.json": mustV2JSON(t, metadata), snapshotPath: mod.CanonicalSnapshot(),
		capture.PayloadRoot + "/settings.ini": capturedBytes,
	}
	return runtime, captured, entries, mustV2JSON(t, v2CaptureEnvelopeFixture(runtime, capture))
}

func writeV2ArtifactZip(t *testing.T, root, name string, entries map[string][]byte) string {
	t.Helper()
	path := filepath.Join(root, "manifests", name)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	for name, data := range entries {
		entry, err := writer.Create(filepath.ToSlash(name))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func cloneV2Manifest(t *testing.T, value manifest.Manifest) manifest.Manifest {
	t.Helper()
	raw := mustV2JSON(t, value)
	var cloned manifest.Manifest
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&cloned); err != nil {
		t.Fatal(err)
	}
	return cloned
}

func cloneV2Entries(values map[string][]byte) map[string][]byte {
	cloned := make(map[string][]byte, len(values))
	for name, data := range values {
		cloned[name] = append([]byte(nil), data...)
	}
	return cloned
}
