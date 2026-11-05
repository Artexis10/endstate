// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package bundle

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
)

func TestWriteModuleSnapshotUsesPinnedCanonicalBytesHashAndSafePath(t *testing.T) {
	moduleFile := filepath.Join(t.TempDir(), "module.jsonc")
	original := []byte(`{
		// cosmetic source formatting is not snapshot formatting
		"id":"apps.example", "displayName":"Example", "sensitivity":"low",
		"matches":{"pathExists":["example"]},
		"moduleSchemaVersion":2,
		"config":{"sets":[{"id":"preferences","generations":[{"id":"g1","order":1}]}]}
	}`)
	if err := os.WriteFile(moduleFile, original, 0o644); err != nil {
		t.Fatal(err)
	}
	mod, err := modules.ParseModuleJSON(original)
	if err != nil {
		t.Fatal(err)
	}
	mod.FilePath = moduleFile
	if err := os.WriteFile(moduleFile, []byte(`{"id":"edited-on-disk"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	pinned := mod.CanonicalSnapshot()
	staging := t.TempDir()
	snapshot, err := WriteModuleSnapshot(staging, mod)
	if err != nil {
		t.Fatalf("WriteModuleSnapshot: %v", err)
	}
	wantPath := "provenance/modules/apps.example-" + mod.Revision + ".json"
	if snapshot.Path != wantPath || snapshot.ContentHash != mod.Revision {
		t.Fatalf("snapshot result = %+v, want path %q hash %q", snapshot, wantPath, mod.Revision)
	}
	written, err := os.ReadFile(filepath.Join(staging, filepath.FromSlash(snapshot.Path)))
	if err != nil {
		t.Fatal(err)
	}
	if string(written) != string(pinned) || testSHA256(written) != mod.Revision {
		t.Fatalf("snapshot bytes/hash not pinned canonical: bytes=%s hash=%s", written, testSHA256(written))
	}
	if string(mod.CanonicalSnapshot()) != string(pinned) {
		t.Fatal("WriteModuleSnapshot mutated pinned module bytes")
	}
}

func TestWriteModuleSnapshotDeduplicatesCosmeticallyEquivalentModules(t *testing.T) {
	left := parseSnapshotModule(t, []byte(`{
		"id":"apps.example","displayName":"Example","sensitivity":"low",
		"matches":{"pathExists":["example"]},"moduleSchemaVersion":2,
		"config":{"sets":[{"id":"preferences","generations":[{"id":"g1","order":1}]}]}
	}`))
	right := parseSnapshotModule(t, []byte(`{"config":{"sets":[{"generations":[{"order":1,"id":"g1"}],"id":"preferences"}]},"moduleSchemaVersion":2,"matches":{"pathExists":["example"]},"sensitivity":"low","displayName":"Example","id":"apps.example"}`))
	if left.Revision != right.Revision || string(left.CanonicalSnapshot()) != string(right.CanonicalSnapshot()) {
		t.Fatal("cosmetic module variants were not canonically equivalent")
	}
	staging := t.TempDir()
	first, err := WriteModuleSnapshot(staging, left)
	if err != nil {
		t.Fatal(err)
	}
	second, err := WriteModuleSnapshot(staging, right)
	if err != nil {
		t.Fatalf("deduplicated WriteModuleSnapshot: %v", err)
	}
	if first != second {
		t.Fatalf("deduplicated snapshot results differ: %+v %+v", first, second)
	}
	entries, err := os.ReadDir(filepath.Join(staging, "provenance", "modules"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("snapshot dedup entries=%v err=%v", entries, err)
	}
}

func TestWriteModuleSnapshotRejectsConflictingExistingFileWithoutOverwrite(t *testing.T) {
	mod := parseSnapshotModule(t, snapshotModuleJSON("apps.example"))
	staging := t.TempDir()
	first, err := WriteModuleSnapshot(staging, mod)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(staging, filepath.FromSlash(first.Path))
	conflict := []byte("conflict")
	if err := os.WriteFile(path, conflict, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteModuleSnapshot(staging, mod); IntegrityDiagnosticCode(err) != IntegritySnapshotConflict {
		t.Fatalf("snapshot conflict error = %T %v code=%q", err, err, IntegrityDiagnosticCode(err))
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != string(conflict) {
		t.Fatalf("conflicting snapshot overwritten: %q err=%v", got, err)
	}
}

func TestWriteModuleSnapshotSanitizesModuleID(t *testing.T) {
	mod := parseSnapshotModule(t, snapshotModuleJSON(`Apps Weird/../Example`))
	snapshot, err := WriteModuleSnapshot(t.TempDir(), mod)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(snapshot.Path, "provenance/modules/") || strings.Contains(snapshot.Path, "..") || strings.ContainsAny(snapshot.Path, `\ `) {
		t.Fatalf("unsafe snapshot path = %q", snapshot.Path)
	}
}

func TestVerifyModuleSnapshotAcceptsExactPinnedSnapshot(t *testing.T) {
	mod := parseSnapshotModule(t, snapshotModuleJSON("apps.example"))
	root := t.TempDir()
	snapshot, err := WriteModuleSnapshot(root, mod)
	if err != nil {
		t.Fatal(err)
	}
	capture := manifest.ConfigCapture{CaptureModule: manifest.CaptureModuleProvenance{
		SchemaVersion: 2, ContentHash: snapshot.ContentHash, SnapshotPath: snapshot.Path,
	}}
	if err := VerifyModuleSnapshot(root, capture); err != nil {
		t.Fatalf("VerifyModuleSnapshot: %v", err)
	}
}

func TestVerifyModuleSnapshotRejectsEditedMissingEscapingAndLinkedSnapshot(t *testing.T) {
	mod := parseSnapshotModule(t, snapshotModuleJSON("apps.example"))
	tests := []struct {
		name   string
		mutate func(t *testing.T, root string, snapshot ModuleSnapshot) manifest.ConfigCapture
		code   string
	}{
		{"edited", func(t *testing.T, root string, snapshot ModuleSnapshot) manifest.ConfigCapture {
			if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(snapshot.Path)), []byte("edited"), 0o644); err != nil {
				t.Fatal(err)
			}
			return snapshotCapture(snapshot)
		}, IntegritySnapshotHashMismatch},
		{"missing", func(t *testing.T, root string, snapshot ModuleSnapshot) manifest.ConfigCapture {
			if err := os.Remove(filepath.Join(root, filepath.FromSlash(snapshot.Path))); err != nil {
				t.Fatal(err)
			}
			return snapshotCapture(snapshot)
		}, IntegritySnapshotMissing},
		{"escaping", func(t *testing.T, root string, snapshot ModuleSnapshot) manifest.ConfigCapture {
			capture := snapshotCapture(snapshot)
			capture.CaptureModule.SnapshotPath = "../module.json"
			return capture
		}, IntegrityUnsafePath},
		{"outside provenance", func(t *testing.T, root string, snapshot ModuleSnapshot) manifest.ConfigCapture {
			capture := snapshotCapture(snapshot)
			capture.CaptureModule.SnapshotPath = "configs/module.json"
			return capture
		}, IntegrityUnsafePath},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			snapshot, err := WriteModuleSnapshot(root, mod)
			if err != nil {
				t.Fatal(err)
			}
			capture := tt.mutate(t, root, snapshot)
			err = VerifyModuleSnapshot(root, capture)
			if IntegrityDiagnosticCode(err) != tt.code {
				t.Fatalf("snapshot integrity code = %q, want %q: %T %v", IntegrityDiagnosticCode(err), tt.code, err, err)
			}
			var integrityErr *IntegrityError
			if !errors.As(err, &integrityErr) {
				t.Fatalf("snapshot error type = %T", err)
			}
		})
	}

	t.Run("linked", func(t *testing.T) {
		root := t.TempDir()
		snapshot, err := WriteModuleSnapshot(root, mod)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(root, filepath.FromSlash(snapshot.Path))
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		outside := filepath.Join(t.TempDir(), "snapshot.json")
		writeCaptureFile(t, outside, mod.CanonicalSnapshot())
		requireCaptureSymlink(t, outside, path)
		if err := VerifyModuleSnapshot(root, snapshotCapture(snapshot)); IntegrityDiagnosticCode(err) != IntegrityLinkUnsupported {
			t.Fatalf("linked snapshot error = %T %v code=%q", err, err, IntegrityDiagnosticCode(err))
		}
	})
}

func parseSnapshotModule(t *testing.T, data []byte) *modules.Module {
	t.Helper()
	mod, err := modules.ParseModuleJSON(data)
	if err != nil {
		t.Fatal(err)
	}
	return mod
}

func snapshotModuleJSON(id string) []byte {
	return []byte(`{"id":` + strconvQuote(id) + `,"displayName":"Example","sensitivity":"low","matches":{"pathExists":["example"]},"moduleSchemaVersion":2,"config":{"sets":[{"id":"preferences","generations":[{"id":"g1","order":1}]}]}}`)
}

func snapshotCapture(snapshot ModuleSnapshot) manifest.ConfigCapture {
	return manifest.ConfigCapture{CaptureModule: manifest.CaptureModuleProvenance{
		SchemaVersion: 2, ContentHash: snapshot.ContentHash, SnapshotPath: snapshot.Path,
	}}
}

func strconvQuote(value string) string {
	return `"` + strings.ReplaceAll(strings.ReplaceAll(value, `\`, `\\`), `"`, `\"`) + `"`
}

func TestSchemaV3SnapshotBindsCaptureToExactAuthoredSource(t *testing.T) {
	raw, err := os.ReadFile("../../../modules/apps/ripgrep/module.jsonc")
	if err != nil {
		t.Fatal(err)
	}
	authored := parseSnapshotModule(t, raw)
	source := modules.ProjectModuleForPlatform(authored, "linux")
	root := t.TempDir()
	snapshot, err := WriteModuleSnapshot(root, source)
	if err != nil {
		t.Fatal(err)
	}
	capture := manifest.ConfigCapture{ModuleID: source.ID, ConfigSetID: "default", SourcePlatform: "linux", SourceGeneration: "g1", SourceGenerationFingerprint: source.Config.Sets[0].Generations[0].Fingerprint, CaptureModule: manifest.CaptureModuleProvenance{SchemaVersion: 3, ContentHash: snapshot.ContentHash, SnapshotPath: snapshot.Path}}
	if err := VerifyModuleSnapshot(root, capture); err != nil {
		t.Fatalf("exact authored source: %v", err)
	}
	tests := []struct {
		name   string
		mutate func(*manifest.ConfigCapture)
	}{
		{"module", func(c *manifest.ConfigCapture) { c.ModuleID = "apps.foreign" }},
		{"variant", func(c *manifest.ConfigCapture) { c.SourcePlatform = "windows" }},
		{"missing variant", func(c *manifest.ConfigCapture) { c.SourcePlatform = "" }},
		{"inconsistent evidence platform", func(c *manifest.ConfigCapture) {
			c.SourceInstance.Evidence = &manifest.ConfigSourceInstanceEvidence{Type: "path", Platform: "windows"}
		}},
		{"set", func(c *manifest.ConfigCapture) { c.ConfigSetID = "foreign" }},
		{"generation", func(c *manifest.ConfigCapture) { c.SourceGeneration = "g2" }},
		{"fingerprint", func(c *manifest.ConfigCapture) { c.SourceGenerationFingerprint = strings.Repeat("a", 64) }},
		{"downgraded schema", func(c *manifest.ConfigCapture) { c.CaptureModule.SchemaVersion = 2; c.SourcePlatform = "" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			candidate := capture
			tt.mutate(&candidate)
			if err := VerifyModuleSnapshot(root, candidate); IntegrityDiagnosticCode(err) != IntegritySnapshotConflict {
				t.Fatalf("forged source accepted: %v", err)
			}
		})
	}
	legacy := parseSnapshotModule(t, snapshotModuleJSON("apps.legacy"))
	legacySnapshot, err := WriteModuleSnapshot(root, legacy)
	if err != nil {
		t.Fatal(err)
	}
	capture.CaptureModule = manifest.CaptureModuleProvenance{SchemaVersion: 3, ContentHash: legacySnapshot.ContentHash, SnapshotPath: legacySnapshot.Path}
	if err := VerifyModuleSnapshot(root, capture); IntegrityDiagnosticCode(err) != IntegritySnapshotConflict {
		t.Fatalf("synthetically upgraded source accepted: %v", err)
	}
}
