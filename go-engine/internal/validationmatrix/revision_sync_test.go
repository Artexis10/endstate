// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationmatrix

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/safepath"
)

func TestSyncRevisionsCheckDetectsDriftWithoutChangingBytes(t *testing.T) {
	root := t.TempDir()
	mod := writeModule(t, root, "alpha", schemaV1Module("apps.alpha", true))
	record := validV1Validation(mod.ID, strings.Repeat("a", 64))
	writeValidation(t, root, "alpha", record)

	sidecarPath := filepath.Join(root, "modules", "apps", "alpha", "validation.jsonc")
	before, err := os.ReadFile(sidecarPath)
	if err != nil {
		t.Fatal(err)
	}
	result, err := SyncRevisions(root, false, time.Date(2026, 7, 22, 0, 0, 0, 0, time.UTC))
	if ErrorCode(err) != CodeStaleSidecar {
		t.Fatalf("SyncRevisions error = %v (code %q), want stale sidecar", err, ErrorCode(err))
	}
	if result.Stale != 1 || result.Updated != 0 {
		t.Fatalf("result = %+v, want one stale and no updates", result)
	}
	after, err := os.ReadFile(sidecarPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, before) {
		t.Fatal("check mode changed sidecar bytes")
	}
}

func TestSyncRevisionsWriteChangesOnlyRevisionTokenAndIsIdempotent(t *testing.T) {
	root := t.TempDir()
	mod := writeModule(t, root, "alpha", schemaV1Module("apps.alpha", true))
	record := validV1Validation(mod.ID, strings.Repeat("a", 64))
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	data = append([]byte("// moduleRevision in this comment is not a token\r\n"), bytes.ReplaceAll(data, []byte("\n"), []byte("\r\n"))...)
	writeRawValidation(t, root, "alpha", data)
	sidecarPath := filepath.Join(root, "modules", "apps", "alpha", "validation.jsonc")
	if err := os.Chmod(sidecarPath, 0o640); err != nil {
		t.Fatal(err)
	}
	modeBefore, err := os.Stat(sidecarPath)
	if err != nil {
		t.Fatal(err)
	}
	if modeBefore.Mode().Perm() != 0o640 {
		t.Skipf("mode preservation is unavailable on this filesystem: %o", modeBefore.Mode().Perm())
	}
	before, err := os.ReadFile(sidecarPath)
	if err != nil {
		t.Fatal(err)
	}
	debtPath := filepath.Join(root, "state", "known-failure-ledger.json")
	if err := os.MkdirAll(filepath.Dir(debtPath), 0o755); err != nil {
		t.Fatal(err)
	}
	debtBefore := []byte("sentinel debt authority\r\n")
	if err := os.WriteFile(debtPath, debtBefore, 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := SyncRevisions(root, true, time.Date(2026, 7, 22, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if result != (RevisionSyncResult{Stale: 1, Updated: 1}) {
		t.Fatalf("result = %+v", result)
	}
	after, err := os.ReadFile(sidecarPath)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Count(before, []byte("\r\n")) != bytes.Count(after, []byte("\r\n")) || len(before) != len(after) {
		t.Fatal("write changed line endings or byte length")
	}
	info, err := os.Stat(sidecarPath)
	if err != nil || info.Mode().Perm() != modeBefore.Mode().Perm() {
		t.Fatalf("write changed sidecar mode: %v, %v", info, err)
	}
	changes := 0
	for i := range before {
		if before[i] != after[i] {
			changes++
		}
	}
	if changes == 0 || changes > 64 {
		t.Fatalf("changed %d bytes, want only the 64-byte revision token", changes)
	}
	if _, err := LoadCatalog(root, time.Date(2026, 7, 22, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("LoadCatalog after write: %v", err)
	}
	debtAfter, err := os.ReadFile(debtPath)
	if err != nil || !bytes.Equal(debtAfter, debtBefore) {
		t.Fatal("revision synchronization changed debt authority")
	}
	second, err := SyncRevisions(root, true, time.Date(2026, 7, 22, 0, 0, 0, 0, time.UTC))
	if err != nil || second != (RevisionSyncResult{}) {
		t.Fatalf("second write = %+v, %v", second, err)
	}
	again, err := os.ReadFile(sidecarPath)
	if err != nil || !bytes.Equal(after, again) {
		t.Fatal("idempotent write changed bytes")
	}
}

func TestSyncRevisionsPreflightsBeforeWriting(t *testing.T) {
	root := t.TempDir()
	alpha := writeModule(t, root, "alpha", schemaV1Module("apps.alpha", true))
	writeValidation(t, root, "alpha", validV1Validation(alpha.ID, strings.Repeat("a", 64)))
	writeModule(t, root, "beta", schemaV1Module("apps.beta", true))
	alphaPath := filepath.Join(root, "modules", "apps", "alpha", "validation.jsonc")
	before, err := os.ReadFile(alphaPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := SyncRevisions(root, true, time.Date(2026, 7, 22, 0, 0, 0, 0, time.UTC)); err == nil {
		t.Fatal("write accepted a later missing sidecar")
	}
	after, err := os.ReadFile(alphaPath)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("preflight failure changed earlier sidecar")
	}
}

func TestSyncRevisionsPreflightsProductionModuleValidationBeforeWriting(t *testing.T) {
	root := t.TempDir()
	alpha := writeModule(t, root, "alpha", schemaV1Module("apps.alpha", true))
	writeValidation(t, root, "alpha", validV1Validation(alpha.ID, strings.Repeat("a", 64)))
	invalidModule := strings.Replace(schemaV1Module("apps.beta", true), `"displayName": "Fixture"`, `"displayName": ""`, 1)
	beta := writeModule(t, root, "beta", invalidModule)
	writeValidation(t, root, "beta", validV1Validation(beta.ID, strings.Repeat("b", 64)))
	alphaPath := filepath.Join(root, "modules", "apps", "alpha", "validation.jsonc")
	before, err := os.ReadFile(alphaPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := SyncRevisions(root, true, time.Date(2026, 7, 22, 0, 0, 0, 0, time.UTC)); ErrorCode(err) != CodeInvalidModuleCatalog {
		t.Fatalf("SyncRevisions error = %v (code %q)", err, ErrorCode(err))
	}
	after, err := os.ReadFile(alphaPath)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("production module preflight failure changed earlier sidecar")
	}
}

func TestSyncRevisionsPreflightsLaterInvalidSidecarsBeforeWriting(t *testing.T) {
	tests := []struct {
		name  string
		write func(t *testing.T, root string, betaRevision string)
	}{
		{"malformed", func(t *testing.T, root, _ string) { writeRawValidation(t, root, "beta", []byte(`{"schemaVersion":`)) }},
		{"duplicate token", func(t *testing.T, root, revision string) {
			record := validV1Validation("apps.beta", revision)
			data, err := json.Marshal(record)
			if err != nil {
				t.Fatal(err)
			}
			data = append(data[:len(data)-1], []byte(`,"moduleRevision":"`+revision+`"}`)...)
			writeRawValidation(t, root, "beta", data)
		}},
		{"semantic", func(t *testing.T, root, revision string) {
			record := validV1Validation("apps.beta", revision)
			record.Synthetic.Scenarios[0].Mode = "unknown"
			writeValidation(t, root, "beta", record)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			alpha := writeModule(t, root, "alpha", schemaV1Module("apps.alpha", true))
			writeValidation(t, root, "alpha", validV1Validation(alpha.ID, strings.Repeat("a", 64)))
			beta := writeModule(t, root, "beta", schemaV1Module("apps.beta", true))
			tt.write(t, root, beta.Revision)
			alphaPath := filepath.Join(root, "modules", "apps", "alpha", "validation.jsonc")
			before, err := os.ReadFile(alphaPath)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := SyncRevisions(root, true, time.Now().UTC()); err == nil {
				t.Fatal("SyncRevisions accepted invalid later sidecar")
			}
			after, err := os.ReadFile(alphaPath)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("invalid later sidecar changed earlier stale sidecar")
			}
		})
	}
}

func TestSyncRevisionsRejectsEscapedAndLinkedRevisionTargets(t *testing.T) {
	root := t.TempDir()
	mod := writeModule(t, root, "alpha", schemaV1Module("apps.alpha", true))
	writeValidation(t, root, "alpha", validV1Validation(mod.ID, mod.Revision))
	sidecarPath := filepath.Join(root, "modules", "apps", "alpha", "validation.jsonc")
	data, err := os.ReadFile(sidecarPath)
	if err != nil {
		t.Fatal(err)
	}
	escaped := bytes.Replace(data, []byte(mod.Revision), []byte(`\u0061`+mod.Revision[1:]), 1)
	if err := os.WriteFile(sidecarPath, escaped, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := SyncRevisions(root, true, time.Date(2026, 7, 22, 0, 0, 0, 0, time.UTC)); ErrorCode(err) != CodeInvalidSidecar {
		t.Fatalf("escaped token error = %v (code %q)", err, ErrorCode(err))
	}

	if err := os.WriteFile(sidecarPath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "sidecar-target.jsonc")
	if err := os.Rename(sidecarPath, target); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, sidecarPath); err != nil {
		t.Skipf("symlink unavailable on this Windows runner: %v", err)
	}
	if _, err := SyncRevisions(root, true, time.Date(2026, 7, 22, 0, 0, 0, 0, time.UTC)); ErrorCode(err) != CodeInvalidSidecar {
		t.Fatalf("linked target error = %v (code %q)", err, ErrorCode(err))
	}
}

func TestSyncRevisionsRejectsNonRegularSidecar(t *testing.T) {
	root := t.TempDir()
	mod := writeModule(t, root, "alpha", schemaV1Module("apps.alpha", true))
	writeValidation(t, root, "alpha", validV1Validation(mod.ID, mod.Revision))
	sidecarPath := filepath.Join(root, "modules", "apps", "alpha", "validation.jsonc")
	if err := os.Remove(sidecarPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(sidecarPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := SyncRevisions(root, true, time.Now().UTC()); ErrorCode(err) != CodeInvalidSidecar {
		t.Fatalf("non-regular target error = %v (code %q)", err, ErrorCode(err))
	}
}

func TestTopLevelRevisionTokenIgnoresCommentsStringsAndNestedObjects(t *testing.T) {
	value := strings.Repeat("a", 64)
	data := []byte(`{
  // "moduleRevision": "` + value + `"
  "note": "moduleRevision",
  "nested": {"moduleRevision": "` + value + `"},
  "moduleRevision": "` + value + `"
}`)
	start, got, err := topLevelRevisionToken(data)
	if err != nil || got != value || string(data[start:start+64]) != value {
		t.Fatalf("topLevelRevisionToken = %d, %q, %v", start, got, err)
	}
	for _, document := range [][]byte{[]byte(`{"moduleId":"apps.alpha"}`), []byte(`{"moduleRevision":"` + value + `","moduleRevision":"` + value + `"}`)} {
		if _, _, err := topLevelRevisionToken(document); err == nil {
			t.Fatalf("topLevelRevisionToken accepted %s", document)
		}
	}
}

func TestApplyRevisionSyncPlansReportsPartialWritesAndReloadFailure(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "first.jsonc")
	second := filepath.Join(root, "second.jsonc")
	before := []byte(`{"moduleRevision":"` + strings.Repeat("a", 64) + `"}`)
	if err := os.WriteFile(first, before, 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, before, 0o640); err != nil {
		t.Fatal(err)
	}
	plans := []revisionSyncPlan{
		{moduleID: "apps.first", path: first, before: before, start: 19, revision: strings.Repeat("b", 64)},
		{moduleID: "apps.second", path: second, before: before, start: 19, revision: strings.Repeat("c", 64)},
	}
	ops := revisionSyncOperations{
		resolve: func(root, path string) (string, error) { return filepath.Join(root, path), nil },
		read:    safepath.ReadRegularFile,
		write: func(path string, data []byte, mode os.FileMode) error {
			if path == second {
				return errors.New("forced write failure")
			}
			return safepath.AtomicWriteFile(path, data, mode)
		},
		reload: func(string, time.Time) (*Catalog, error) { return &Catalog{}, nil },
	}
	result, err := applyRevisionSyncPlans(root, plans, time.Now().UTC(), ops)
	if err == nil || result != (RevisionSyncResult{Stale: 2, Updated: 1}) {
		t.Fatalf("partial write result = %+v, %v", result, err)
	}
	firstAfter, err := os.ReadFile(first)
	if err != nil || !bytes.Contains(firstAfter, []byte(strings.Repeat("b", 64))) {
		t.Fatalf("first sidecar = %q, %v", firstAfter, err)
	}
	secondAfter, err := os.ReadFile(second)
	if err != nil || !bytes.Equal(secondAfter, before) {
		t.Fatalf("second sidecar = %q, %v", secondAfter, err)
	}

	if err := os.WriteFile(first, before, 0o640); err != nil {
		t.Fatal(err)
	}
	ops.write = safepath.AtomicWriteFile
	ops.reload = func(string, time.Time) (*Catalog, error) { return nil, errors.New("forced reload failure") }
	result, err = applyRevisionSyncPlans(root, plans[:1], time.Now().UTC(), ops)
	if err == nil || result != (RevisionSyncResult{Stale: 1, Updated: 1}) {
		t.Fatalf("reload failure result = %+v, %v", result, err)
	}
}
