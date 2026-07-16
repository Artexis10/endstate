// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package configrestore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/configvalidate"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
)

func TestPersistJournalIntentWritesCanonicalVerifiedContentWithoutTargetMutation(t *testing.T) {
	transactionRoot := t.TempDir()
	hostRoot := t.TempDir()
	stageRoot := t.TempDir()
	target := filepath.Join(hostRoot, "settings.json")
	source := filepath.Join(stageRoot, "settings.json")
	writeTestFile(t, target, `{"before":true}`)
	writeTestFile(t, source, `{"after":true}`)
	sourceInfo, err := os.Lstat(source)
	if err != nil {
		t.Fatal(err)
	}
	validation := configvalidate.ResolvedValidation{
		Definition: modules.ValidationDef{
			Type: "json-path-exists", Path: "settings.json", JSONPath: "$.after",
		},
		HostPath: target,
	}
	prepared, err := PrepareSnapshots(context.Background(), SnapshotRequest{
		Set: &MaterializedSet{
			Actions: []Action{{
				Kind: ActionCopy, Strategy: "copy", Source: source, Target: target,
				SourceMode: sourceInfo.Mode(), SnapshotRequired: true,
			}},
			Validations: []configvalidate.ResolvedValidation{validation},
		},
		TransactionRoot: transactionRoot,
	})
	if err != nil {
		t.Fatal(err)
	}
	lineage := testJournalLineage()

	intent, err := PersistJournalIntent(context.Background(), JournalIntentRequest{
		Prepared: prepared, TransactionRoot: transactionRoot, Lineage: lineage,
	})
	if err != nil {
		t.Fatalf("PersistJournalIntent() error = %v", err)
	}
	if intent.Path() != filepath.Join(transactionRoot, "journal", "intent.json") ||
		intent.State() != JournalPending || len(intent.Digest()) != 64 {
		t.Fatalf("intent identity = path %q state %q digest %q", intent.Path(), intent.State(), intent.Digest())
	}

	record := prepared.Actions()[0]
	identity := expectedIntentIdentityJSON(lineage, record, validation)
	digestBytes := sha256.Sum256([]byte(identity))
	wantDigest := hex.EncodeToString(digestBytes[:])
	wantBytes := []byte(strings.TrimSuffix(identity, "}") + `,"intentDigest":"` + wantDigest + `"}` + "\n")
	gotBytes, err := os.ReadFile(intent.Path())
	if err != nil {
		t.Fatal(err)
	}
	if string(gotBytes) != string(wantBytes) {
		t.Fatalf("intent bytes:\n got: %s\nwant: %s", gotBytes, wantBytes)
	}
	if intent.Digest() != wantDigest {
		t.Fatalf("intent digest = %q, want %q", intent.Digest(), wantDigest)
	}

	loaded, err := ReadJournalIntent(context.Background(), transactionRoot)
	if err != nil {
		t.Fatalf("ReadJournalIntent() error = %v", err)
	}
	if loaded.Digest() != intent.Digest() || loaded.State() != JournalPending {
		t.Fatalf("loaded intent = digest %q state %q", loaded.Digest(), loaded.State())
	}
	assertJournalIntentAccessorsAreDefensive(t, loaded)
	assertTestFile(t, target, `{"before":true}`)
	assertTestFile(t, source, `{"after":true}`)

	unrelated := filepath.Join(t.TempDir(), "settings.json")
	writeTestFile(t, unrelated, `{"after":true}`)
	rewriteCanonicalJournalIntent(t, intent.Path(), func(disk *journalIntentDisk) {
		disk.Validations[0].HostPath = unrelated
	})
	if redirected, err := ReadJournalIntent(context.Background(), transactionRoot); redirected != nil ||
		CodeOf(err) != CodeJournalIntentFailed {
		t.Fatalf("redirected validation result=%#v err=%v code=%q", redirected, err, CodeOf(err))
	}
}

func TestPersistJournalIntentIdenticalRetryIsIdempotent(t *testing.T) {
	transactionRoot := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	writeTestFile(t, target, "prior")
	prepared, err := PrepareSnapshots(context.Background(), SnapshotRequest{
		Set: &MaterializedSet{Actions: []Action{{
			Kind: ActionDeleteFile, Strategy: "delete-glob", Target: target, SnapshotRequired: true,
		}}},
		TransactionRoot: transactionRoot,
	})
	if err != nil {
		t.Fatal(err)
	}
	request := JournalIntentRequest{Prepared: prepared, TransactionRoot: transactionRoot, Lineage: testJournalLineage()}
	first, err := PersistJournalIntent(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	firstBytes, err := os.ReadFile(first.Path())
	if err != nil {
		t.Fatal(err)
	}
	second, err := PersistJournalIntent(context.Background(), request)
	if err != nil {
		t.Fatalf("identical retry: %v", err)
	}
	secondBytes, err := os.ReadFile(second.Path())
	if err != nil {
		t.Fatal(err)
	}
	if first.Digest() != second.Digest() || string(firstBytes) != string(secondBytes) {
		t.Fatalf("idempotent intent changed: first=%q second=%q", first.Digest(), second.Digest())
	}
}

func TestPersistJournalIntentRecordsExactRegistryIdentityAndRawPriorSnapshot(t *testing.T) {
	transactionRoot := t.TempDir()
	key := `HKCU\Software\Vendor\App`
	valueName := `Theme\Variant`
	identity := key + "\x00" + valueName
	priorData := []byte{0x41, 0x00, 0x00, 0x00}
	reader := &mapRegistryReader{values: map[string]RegistryReadResult{
		identity: {Exists: true, ValueType: RegistryTypeSZ, Data: priorData},
	}}
	prepared, err := PrepareSnapshots(context.Background(), SnapshotRequest{
		Set: &MaterializedSet{Actions: []Action{{
			Kind: ActionRegistrySet, Strategy: "registry-set", Target: key + `\` + valueName,
			SnapshotRequired: true,
			RegistryValue: &RegistryValue{
				Key: key, ValueName: valueName, ValueType: "REG_SZ", Data: "dark",
			},
		}}},
		TransactionRoot: transactionRoot, RegistryReader: reader,
	})
	if err != nil {
		t.Fatal(err)
	}
	intent, err := PersistJournalIntent(context.Background(), JournalIntentRequest{
		Prepared: prepared, TransactionRoot: transactionRoot, Lineage: testJournalLineage(),
	})
	if err != nil {
		t.Fatal(err)
	}
	action := intent.Actions()[0]
	if action.RegistryKey != key || action.RegistryValueName != valueName {
		t.Fatalf("registry identity = key %q value %q", action.RegistryKey, action.RegistryValueName)
	}
	snapshot, err := loadRegistrySnapshot(action.Prior.BackupPath)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Key != key || snapshot.ValueName != valueName || snapshot.ValueType != RegistryTypeSZ ||
		string(snapshot.Data) != string(priorData) {
		t.Fatalf("raw prior registry snapshot = %#v", snapshot)
	}
	action.RegistryKey = "changed"
	if intent.Actions()[0].RegistryKey != key {
		t.Fatal("journal registry identity was mutated through an accessor")
	}

	original, err := os.ReadFile(intent.Path())
	if err != nil {
		t.Fatal(err)
	}
	mutations := []struct {
		name   string
		mutate func(*JournalAction)
	}{
		{"invalid prior kind", func(action *JournalAction) { action.Prior.Kind = StateFile }},
		{"invalid desired kind", func(action *JournalAction) { action.Desired.Kind = StateAbsent }},
		{"non-exact registry key", func(action *JournalAction) { action.RegistryKey = `hkcu\Software\Vendor\App` }},
		{"registry state has mode", func(action *JournalAction) { action.Prior.Mode = 0o600 }},
	}
	for _, tt := range mutations {
		t.Run(tt.name, func(t *testing.T) {
			if err := os.WriteFile(intent.Path(), original, 0o600); err != nil {
				t.Fatal(err)
			}
			rewriteCanonicalJournalIntent(t, intent.Path(), func(disk *journalIntentDisk) {
				tt.mutate(&disk.Actions[0])
			})
			if result, err := ReadJournalIntent(context.Background(), transactionRoot); result != nil ||
				CodeOf(err) != CodeJournalIntentFailed {
				t.Fatalf("invalid registry result=%#v err=%v code=%q", result, err, CodeOf(err))
			}
		})
	}
}

func TestPersistJournalIntentAcceptsAbsentRegistryPriorState(t *testing.T) {
	transactionRoot := t.TempDir()
	key := `HKCU\Software\Vendor\App`
	valueName := "Missing"
	reader := &mapRegistryReader{values: map[string]RegistryReadResult{
		key + "\x00" + valueName: {Exists: false},
	}}
	prepared, err := PrepareSnapshots(context.Background(), SnapshotRequest{
		Set: &MaterializedSet{Actions: []Action{{
			Kind: ActionRegistrySet, Strategy: "registry-set", Target: key + `\` + valueName,
			SnapshotRequired: true,
			RegistryValue: &RegistryValue{
				Key: key, ValueName: valueName, ValueType: "REG_DWORD", Data: "1",
			},
		}}},
		TransactionRoot: transactionRoot, RegistryReader: reader,
	})
	if err != nil {
		t.Fatal(err)
	}
	intent, err := PersistJournalIntent(context.Background(), JournalIntentRequest{
		Prepared: prepared, TransactionRoot: transactionRoot, Lineage: testJournalLineage(),
	})
	if err != nil {
		t.Fatalf("PersistJournalIntent() absent registry prior: %v", err)
	}
	action := intent.Actions()[0]
	if action.Prior.Kind != StateAbsent || action.Desired.Kind != StateRegistryValue {
		t.Fatalf("registry state kinds = prior %q desired %q", action.Prior.Kind, action.Desired.Kind)
	}
}

func TestJournalValidationBindsToRecordedDirectoryCopyMapping(t *testing.T) {
	transactionRoot := t.TempDir()
	source := filepath.Join(t.TempDir(), "prefs")
	target := filepath.Join(t.TempDir(), "restored-prefs")
	writeTestFile(t, filepath.Join(source, "nested", "settings.json"), `{"valid":true}`)
	writeTestFile(t, filepath.Join(source, "other.json"), `{"valid":true}`)
	sourceInfo, err := os.Lstat(source)
	if err != nil {
		t.Fatal(err)
	}
	validation := configvalidate.ResolvedValidation{
		Definition: modules.ValidationDef{
			Type: "json-path-exists", Path: "prefs/nested/settings.json", JSONPath: "$.valid",
		},
		HostPath: filepath.Join(target, "nested", "settings.json"),
	}
	prepared, err := PrepareSnapshots(context.Background(), SnapshotRequest{
		Set: &MaterializedSet{
			Actions: []Action{{
				Kind: ActionCopy, Strategy: "copy", Source: source, Target: target,
				SourceMode: sourceInfo.Mode(), SourceIsDirectory: true, SnapshotRequired: true,
			}},
			Validations: []configvalidate.ResolvedValidation{validation},
		},
		TransactionRoot: transactionRoot,
	})
	if err != nil {
		t.Fatal(err)
	}
	intent, err := PersistJournalIntent(context.Background(), JournalIntentRequest{
		Prepared: prepared, TransactionRoot: transactionRoot, Lineage: testJournalLineage(),
	})
	if err != nil {
		t.Fatalf("PersistJournalIntent() directory validation: %v", err)
	}
	rewriteCanonicalJournalIntent(t, intent.Path(), func(disk *journalIntentDisk) {
		// This is still inside the same copy target and names a desired file,
		// but it does not correspond to the portable validation path.
		disk.Validations[0].HostPath = filepath.Join(target, "other.json")
	})
	if redirected, err := ReadJournalIntent(context.Background(), transactionRoot); redirected != nil ||
		CodeOf(err) != CodeJournalIntentFailed {
		t.Fatalf("misbound directory validation result=%#v err=%v code=%q", redirected, err, CodeOf(err))
	}
}

func expectedIntentIdentityJSON(
	lineage JournalLineage,
	record PreparedAction,
	validation configvalidate.ResolvedValidation,
) string {
	prior := record.Prior()
	desired := record.Desired()
	action := record.Action()
	quote := strconv.Quote
	return fmt.Sprintf(
		`{"format":"endstate.config-restore-intent","version":1,"state":"pending","lineage":{"runId":%s,"captureId":%s,"moduleId":%s,"configSetId":%s,"targetInstanceId":%s,"sourceGeneration":%s,"targetGeneration":%s,"migrationPath":[%s,%s],"sourceGenerationFingerprint":%s,"captureModuleRevision":%s,"restoreModuleRevision":%s},"actions":[{"index":0,"kind":%s,"strategy":%s,"target":%s,"registryKey":"","registryValueName":"","prior":{"kind":%s,"digest":%s,"mode":%d,"backupPath":%s,"entries":%s},"desired":{"kind":%s,"digest":%s,"mode":%d,"backupPath":%s,"entries":%s},"sourceDigest":%s}],"validations":[{"type":%s,"path":%s,"jsonPath":%s,"section":"","key":"","hostPath":%s}],"validationStatus":"pending","rollbackOutcome":"not_attempted"}`,
		quote(lineage.RunID), quote(lineage.CaptureID), quote(lineage.ModuleID), quote(lineage.ConfigSetID),
		quote(lineage.TargetInstanceID), quote(lineage.SourceGeneration), quote(lineage.TargetGeneration),
		quote(lineage.MigrationPath[0]), quote(lineage.MigrationPath[1]), quote(lineage.SourceGenerationFingerprint),
		quote(lineage.CaptureModuleRevision), quote(lineage.RestoreModuleRevision), quote(string(action.Kind)),
		quote(action.Strategy), quote(action.Target), quote(string(prior.Kind)), quote(prior.Digest), uint32(prior.Mode.Perm()),
		quote(prior.BackupPath), expectedStateEntriesJSON(prior.Entries()), quote(string(desired.Kind)), quote(desired.Digest),
		uint32(desired.Mode.Perm()), quote(desired.BackupPath), expectedStateEntriesJSON(desired.Entries()),
		quote(record.SourceDigest()), quote(validation.Definition.Type),
		quote(validation.Definition.Path), quote(validation.Definition.JSONPath), quote(validation.HostPath),
	)
}

func expectedStateEntriesJSON(entries []StateEntry) string {
	if len(entries) == 0 {
		return "[]"
	}
	encoded := make([]string, len(entries))
	for index, entry := range entries {
		encoded[index] = fmt.Sprintf(
			`{"path":%s,"kind":%s,"mode":%d,"size":%d,"contentHash":%s}`,
			strconv.Quote(entry.Path), strconv.Quote(string(entry.Kind)), uint32(entry.Mode.Perm()),
			entry.Size, strconv.Quote(entry.ContentHash),
		)
	}
	return "[" + strings.Join(encoded, ",") + "]"
}

func assertJournalIntentAccessorsAreDefensive(t *testing.T, intent *JournalIntent) {
	t.Helper()
	lineage := intent.Lineage()
	lineage.MigrationPath[0] = "changed"
	actions := intent.Actions()
	actions[0].Prior.Entries[0] = JournalFilesystemEntry{}
	actions[0] = JournalAction{}
	validations := intent.Validations()
	validations[0] = JournalValidation{}
	if intent.Lineage().MigrationPath[0] != "g1" || intent.Actions()[0].Kind != ActionCopy ||
		intent.Actions()[0].Prior.Entries[0].Path != "." ||
		intent.Validations()[0].Type != "json-path-exists" {
		t.Fatal("journal intent was mutated through an accessor")
	}
}

func testJournalLineage() JournalLineage {
	return JournalLineage{
		RunID: "run-20260716", CaptureID: "capture-preferences", ModuleID: "vendor.app",
		ConfigSetID: "preferences", TargetInstanceID: "target-side-by-side-1",
		SourceGeneration: "g1", TargetGeneration: "g2", MigrationPath: []string{"g1", "g2"},
		SourceGenerationFingerprint: strings.Repeat("1", 64),
		CaptureModuleRevision:       strings.Repeat("2", 64),
		RestoreModuleRevision:       strings.Repeat("3", 64),
	}
}
