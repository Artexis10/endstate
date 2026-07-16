// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package configrestore

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/configvalidate"
)

func TestPrepareSnapshotsCapturesFilePriorAndDesiredStatesWithoutMutation(t *testing.T) {
	stageRoot := t.TempDir()
	hostRoot := t.TempDir()
	transactionRoot := t.TempDir()
	copySource := filepath.Join(stageRoot, "copy.txt")
	copyTarget := filepath.Join(hostRoot, "copy.txt")
	writeTarget := filepath.Join(hostRoot, "created.txt")
	deleteTarget := filepath.Join(hostRoot, "delete.txt")
	writeTestFile(t, copySource, "replacement")
	writeTestFile(t, copyTarget, "original")
	writeTestFile(t, deleteTarget, "remove me")
	chmodTestPath(t, copySource, 0o640)
	chmodTestPath(t, copyTarget, 0o600)
	chmodTestPath(t, deleteTarget, 0o644)

	set := &MaterializedSet{
		Actions: []Action{
			{
				Kind: ActionCopy, Strategy: "copy", Source: copySource, Target: copyTarget,
				SourceMode: 0o640, SnapshotRequired: true,
			},
			{
				Kind: ActionWriteFile, Strategy: "merge-json", Source: filepath.Join(stageRoot, "merge.json"),
				Target: writeTarget, DesiredContent: []byte("created\n"), SnapshotRequired: true,
			},
			{Kind: ActionDeleteFile, Strategy: "delete-glob", Target: deleteTarget, SnapshotRequired: true},
		},
		Validations: []configvalidate.ResolvedValidation{},
	}

	prepared, err := PrepareSnapshots(context.Background(), SnapshotRequest{
		Set: set, TransactionRoot: transactionRoot,
	})
	if err != nil {
		t.Fatalf("PrepareSnapshots() error = %v", err)
	}
	if prepared.SnapshotRoot() != filepath.Join(transactionRoot, "snapshots") {
		t.Fatalf("snapshot root = %q", prepared.SnapshotRoot())
	}
	records := prepared.Actions()
	if len(records) != 3 {
		t.Fatalf("prepared actions = %#v, want 3", records)
	}

	copyPrior, copyDesired := records[0].Prior(), records[0].Desired()
	if copyPrior.Kind != StateFile || copyDesired.Kind != StateFile || copyPrior.Digest == copyDesired.Digest {
		t.Fatalf("copy states prior=%+v desired=%+v", copyPrior, copyDesired)
	}
	wantCopyBackup := filepath.Join(transactionRoot, "snapshots", "000000", "prior")
	if copyPrior.BackupPath != wantCopyBackup {
		t.Fatalf("copy backup = %q, want %q", copyPrior.BackupPath, wantCopyBackup)
	}
	assertTestFile(t, wantCopyBackup, "original")
	if runtime.GOOS != "windows" {
		if copyPrior.Mode.Perm() != 0o600 || copyDesired.Mode.Perm() != 0o600 {
			t.Fatalf("copy modes prior=%#o desired=%#o, want existing target mode", copyPrior.Mode.Perm(), copyDesired.Mode.Perm())
		}
	}
	if records[0].SourceDigest() == "" {
		t.Fatal("copy source digest is empty")
	}

	writePrior, writeDesired := records[1].Prior(), records[1].Desired()
	if writePrior.Kind != StateAbsent || writePrior.BackupPath != "" || writeDesired.Kind != StateFile {
		t.Fatalf("write states prior=%+v desired=%+v", writePrior, writeDesired)
	}
	deletePrior, deleteDesired := records[2].Prior(), records[2].Desired()
	if deletePrior.Kind != StateFile || deleteDesired.Kind != StateAbsent {
		t.Fatalf("delete states prior=%+v desired=%+v", deletePrior, deleteDesired)
	}
	assertTestFile(t, deletePrior.BackupPath, "remove me")

	assertTestFile(t, copySource, "replacement")
	assertTestFile(t, copyTarget, "original")
	assertTestFile(t, deleteTarget, "remove me")
	if _, err := os.Lstat(writeTarget); !os.IsNotExist(err) {
		t.Fatalf("write target was mutated during snapshot: %v", err)
	}

	// Prepared records own deep clones rather than exposing mutable materialized
	// action slices or desired bytes.
	firstAction := records[1].Action()
	firstAction.DesiredContent[0] = 'X'
	records[1] = PreparedAction{}
	again := prepared.Actions()
	if string(again[1].Action().DesiredContent) != "created\n" || again[1].Desired().Digest != writeDesired.Digest {
		t.Fatalf("prepared records were mutated through accessors: %#v", again[1])
	}
}

func TestPrepareSnapshotsCopiesCompleteDirectoryAndDigestsOverlayDesiredState(t *testing.T) {
	stageRoot := t.TempDir()
	hostRoot := t.TempDir()
	transactionRoot := t.TempDir()
	source := filepath.Join(stageRoot, "source")
	target := filepath.Join(hostRoot, "target")
	writeTestFile(t, filepath.Join(source, "replace.txt"), "new")
	writeTestFile(t, filepath.Join(source, "new.txt"), "added")
	writeTestFile(t, filepath.Join(source, "excluded.tmp"), "source excluded")
	writeTestFile(t, filepath.Join(source, "nested", "source.txt"), "nested")
	writeTestFile(t, filepath.Join(target, "replace.txt"), "old")
	writeTestFile(t, filepath.Join(target, "unrelated.txt"), "preserved")
	writeTestFile(t, filepath.Join(target, "excluded.tmp"), "target preserved")
	if err := os.MkdirAll(filepath.Join(target, "empty"), 0o711); err != nil {
		t.Fatal(err)
	}
	chmodTestPath(t, filepath.Join(target, "replace.txt"), 0o600)

	set := &MaterializedSet{Actions: []Action{{
		Kind: ActionCopy, Strategy: "copy", Source: source, Target: target,
		SourceIsDirectory: true, SourceMode: 0o755, Exclude: []string{"**/excluded.tmp"}, SnapshotRequired: true,
	}}}
	prepared, err := PrepareSnapshots(context.Background(), SnapshotRequest{Set: set, TransactionRoot: transactionRoot})
	if err != nil {
		t.Fatalf("PrepareSnapshots() error = %v", err)
	}
	record := prepared.Actions()[0]
	if record.Prior().Kind != StateDirectory || record.Desired().Kind != StateDirectory || record.SourceDigest() == "" {
		t.Fatalf("directory record = %#v", record)
	}
	backup := record.Prior().BackupPath
	assertTestFile(t, filepath.Join(backup, "replace.txt"), "old")
	assertTestFile(t, filepath.Join(backup, "unrelated.txt"), "preserved")
	assertTestFile(t, filepath.Join(backup, "excluded.tmp"), "target preserved")
	if info, err := os.Lstat(filepath.Join(backup, "empty")); err != nil || !info.IsDir() {
		t.Fatalf("empty directory was not preserved: info=%v err=%v", info, err)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Lstat(filepath.Join(backup, "replace.txt"))
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("backup file mode = %v err=%v", info, err)
		}
	}

	// The desired digest is the prior tree plus an overlay. It must depend on
	// unrelated target entries, while excluded source bytes must not affect it.
	secondSource := filepath.Join(t.TempDir(), "source")
	secondTarget := filepath.Join(t.TempDir(), "target")
	copyTestTree(t, source, secondSource)
	copyTestTree(t, target, secondTarget)
	writeTestFile(t, filepath.Join(secondSource, "excluded.tmp"), "different ignored bytes")
	secondPrepared, err := PrepareSnapshots(context.Background(), SnapshotRequest{
		Set: &MaterializedSet{Actions: []Action{{
			Kind: ActionCopy, Strategy: "copy", Source: secondSource, Target: secondTarget,
			SourceIsDirectory: true, SourceMode: 0o755, Exclude: []string{"**/excluded.tmp"}, SnapshotRequired: true,
		}}},
		TransactionRoot: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if secondPrepared.Actions()[0].Desired().Digest != record.Desired().Digest {
		t.Fatalf("excluded source changed desired digest: %q != %q", secondPrepared.Actions()[0].Desired().Digest, record.Desired().Digest)
	}
	if err := os.Remove(filepath.Join(secondTarget, "unrelated.txt")); err != nil {
		t.Fatal(err)
	}
	thirdPrepared, err := PrepareSnapshots(context.Background(), SnapshotRequest{
		Set: &MaterializedSet{Actions: []Action{{
			Kind: ActionCopy, Strategy: "copy", Source: secondSource, Target: secondTarget,
			SourceIsDirectory: true, SourceMode: 0o755, Exclude: []string{"**/excluded.tmp"}, SnapshotRequired: true,
		}}},
		TransactionRoot: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if thirdPrepared.Actions()[0].Desired().Digest == record.Desired().Digest {
		t.Fatal("unrelated preserved target entry did not affect desired overlay digest")
	}

	assertTestFile(t, filepath.Join(target, "replace.txt"), "old")
	assertTestFile(t, filepath.Join(source, "replace.txt"), "new")
}

func TestDesiredWriteStatePreservesExistingModeAndUsesCreateMode(t *testing.T) {
	content := []byte("desired\n")
	existing := filesystemState{Kind: StateFile, Mode: 0o600}

	preserved := desiredWriteState(existing, content)
	created := desiredWriteState(absentFilesystemState(), content)
	if preserved.Mode.Perm() != 0o600 || preserved.Entries["."].Mode.Perm() != 0o600 {
		t.Fatalf("existing target desired mode = %#o, want 0600", preserved.Mode.Perm())
	}
	if created.Mode.Perm() != 0o644 || created.Entries["."].Mode.Perm() != 0o644 {
		t.Fatalf("new target desired mode = %#o, want 0644", created.Mode.Perm())
	}
	if preserved.Digest == created.Digest {
		t.Fatal("desired write digest does not encode preserved versus create mode")
	}
}

func TestDesiredDirectoryCopyUsesCommitRootModeRatherThanSourceRootMode(t *testing.T) {
	source := filesystemState{
		Kind: StateDirectory,
		Mode: 0o700,
		Entries: map[string]filesystemEntry{
			".": {Path: ".", Kind: StateDirectory, Mode: 0o700},
		},
	}
	source.Digest = digestFilesystemState(source)

	desired, err := desiredCopyState(absentFilesystemState(), source, nil)
	if err != nil {
		t.Fatal(err)
	}
	if desired.Mode.Perm() != 0o755 || desired.Entries["."].Mode.Perm() != 0o755 {
		t.Fatalf("new directory target root mode = %#o, want copy commit mode 0755", desired.Mode.Perm())
	}
}

type mapRegistryReader struct {
	values map[string]RegistryReadResult
	calls  []string
}

func (r *mapRegistryReader) ReadValue(_ context.Context, key, valueName string) (RegistryReadResult, error) {
	identity := key + "\x00" + valueName
	r.calls = append(r.calls, identity)
	value := r.values[identity]
	value.Data = append([]byte(nil), value.Data...)
	return value, nil
}

func TestPrepareSnapshotsCapturesExactRegistryValueAndAbsence(t *testing.T) {
	transactionRoot := t.TempDir()
	reader := &mapRegistryReader{values: map[string]RegistryReadResult{
		`HKCU\Software\Endstate` + "\x00" + "Theme": {Exists: true, ValueType: RegistryTypeSZ, Data: []byte{0x64, 0x00, 0x61, 0x00, 0x72, 0x00, 0x6b, 0x00, 0, 0}},
	}}
	set := &MaterializedSet{Actions: []Action{
		{
			Kind: ActionRegistrySet, Strategy: "registry-set", Target: `HKCU\Software\Endstate\Theme`,
			RegistryValue:    &RegistryValue{Key: `HKCU\Software\Endstate`, ValueName: "Theme", ValueType: "REG_SZ", Data: "light"},
			SnapshotRequired: true,
		},
		{
			Kind: ActionRegistrySet, Strategy: "registry-set", Target: `HKCU\Software\Endstate\Missing`,
			RegistryValue:    &RegistryValue{Key: `HKCU\Software\Endstate`, ValueName: "Missing", ValueType: "REG_DWORD", Data: "1"},
			SnapshotRequired: true,
		},
	}}

	prepared, err := PrepareSnapshots(context.Background(), SnapshotRequest{
		Set: set, TransactionRoot: transactionRoot, RegistryReader: reader,
	})
	if err != nil {
		t.Fatalf("PrepareSnapshots() error = %v", err)
	}
	records := prepared.Actions()
	if records[0].Prior().Kind != StateRegistryValue || records[0].Desired().Kind != StateRegistryValue {
		t.Fatalf("existing registry states = prior=%+v desired=%+v", records[0].Prior(), records[0].Desired())
	}
	if records[1].Prior().Kind != StateAbsent || records[1].Desired().Kind != StateRegistryValue {
		t.Fatalf("absent registry states = prior=%+v desired=%+v", records[1].Prior(), records[1].Desired())
	}
	for index, record := range records {
		want := filepath.Join(transactionRoot, "snapshots", formatActionIndex(index), "prior.registry")
		if record.Prior().BackupPath != want {
			t.Errorf("record[%d] backup = %q, want %q", index, record.Prior().BackupPath, want)
		}
		if info, err := os.Lstat(want); err != nil || !info.Mode().IsRegular() {
			t.Errorf("registry snapshot[%d] info=%v err=%v", index, info, err)
		}
	}
	if len(reader.calls) != 6 {
		t.Fatalf("registry reader calls = %d, want snapshot, per-action verify, and final recheck per action", len(reader.calls))
	}
}

func TestPrepareSnapshotsIsDeterministicAcrossTransactionRoots(t *testing.T) {
	hostRoot := t.TempDir()
	target := filepath.Join(hostRoot, "settings.txt")
	writeTestFile(t, target, "prior")
	set := &MaterializedSet{Actions: []Action{{
		Kind: ActionWriteFile, Strategy: "append", Target: target,
		DesiredContent: []byte("desired\n"), SnapshotRequired: true,
	}}}

	first, err := PrepareSnapshots(context.Background(), SnapshotRequest{Set: set, TransactionRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	second, err := PrepareSnapshots(context.Background(), SnapshotRequest{Set: set, TransactionRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	firstRecord, secondRecord := first.Actions()[0], second.Actions()[0]
	if firstRecord.Prior().Digest != secondRecord.Prior().Digest || firstRecord.Desired().Digest != secondRecord.Desired().Digest {
		t.Fatalf("digests changed across roots: first=%+v second=%+v", firstRecord, secondRecord)
	}
	if filepath.Base(filepath.Dir(firstRecord.Prior().BackupPath)) != "000000" || filepath.Base(filepath.Dir(secondRecord.Prior().BackupPath)) != "000000" {
		t.Fatalf("backup paths are not deterministically indexed: %q %q", firstRecord.Prior().BackupPath, secondRecord.Prior().BackupPath)
	}
}

func TestPrepareSnapshotsZeroActionsReturnsImmutableEmptySetWithoutArtifacts(t *testing.T) {
	transactionRoot := t.TempDir()
	prepared, err := PrepareSnapshots(context.Background(), SnapshotRequest{
		Set:             &MaterializedSet{Actions: []Action{}, Validations: []configvalidate.ResolvedValidation{}},
		TransactionRoot: transactionRoot,
	})
	if err != nil {
		t.Fatalf("PrepareSnapshots() error = %v", err)
	}
	if prepared == nil || prepared.Actions() == nil || len(prepared.Actions()) != 0 || prepared.SnapshotRoot() != "" {
		t.Fatalf("zero-action result = %#v actions=%#v", prepared, prepared.Actions())
	}
	if _, err := os.Lstat(filepath.Join(transactionRoot, "snapshots")); !os.IsNotExist(err) {
		t.Fatalf("zero-action preparation created artifacts: %v", err)
	}
}

func TestPrepareSnapshotsRejectsActionWithoutSnapshotRequirement(t *testing.T) {
	target := filepath.Join(t.TempDir(), "target")
	writeTestFile(t, target, "prior")
	prepared, err := PrepareSnapshots(context.Background(), SnapshotRequest{
		Set: &MaterializedSet{Actions: []Action{{
			Kind: ActionDeleteFile, Target: target, SnapshotRequired: false,
		}}},
		TransactionRoot: t.TempDir(),
	})
	if prepared != nil || CodeOf(err) != CodeBackupFailed {
		t.Fatalf("result=%#v error=%v code=%q, want nil typed backup_failed", prepared, err, CodeOf(err))
	}
}

func chmodTestPath(t *testing.T, path string, mode os.FileMode) {
	t.Helper()
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func copyTestTree(t *testing.T, source, target string) {
	t.Helper()
	err := filepath.Walk(source, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		destination := filepath.Join(target, relative)
		if info.IsDir() {
			return os.MkdirAll(destination, info.Mode())
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
			return err
		}
		return os.WriteFile(destination, data, info.Mode())
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestPreparedSetValidationAccessorsReturnCopies(t *testing.T) {
	target := filepath.Join(t.TempDir(), "target")
	writeTestFile(t, target, "prior")
	validation := configvalidate.ResolvedValidation{HostPath: target}
	prepared, err := PrepareSnapshots(context.Background(), SnapshotRequest{
		Set: &MaterializedSet{
			Actions:     []Action{{Kind: ActionDeleteFile, Target: target, SnapshotRequired: true}},
			Validations: []configvalidate.ResolvedValidation{validation},
		},
		TransactionRoot: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	first := prepared.Validations()
	first[0].HostPath = "mutated"
	if !reflect.DeepEqual(prepared.Validations(), []configvalidate.ResolvedValidation{validation}) {
		t.Fatalf("validations were mutable: %#v", prepared.Validations())
	}
}

func TestSnapshotErrorRetainsCause(t *testing.T) {
	cause := errors.New("sentinel")
	err := newError(CodeBackupFailed, 1, "target", cause)
	if !errors.Is(err, cause) {
		t.Fatalf("backup error does not unwrap cause: %v", err)
	}
}
