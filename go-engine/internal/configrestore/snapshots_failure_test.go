// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package configrestore

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrepareSnapshotsCancellationReturnsBackupFailedAndNoArtifacts(t *testing.T) {
	transactionRoot := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	writeTestFile(t, target, "prior")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := PrepareSnapshots(ctx, SnapshotRequest{
		Set:             &MaterializedSet{Actions: []Action{{Kind: ActionDeleteFile, Target: target, SnapshotRequired: true}}},
		TransactionRoot: transactionRoot,
	})
	if result != nil || CodeOf(err) != CodeBackupFailed || !errors.Is(err, context.Canceled) {
		t.Fatalf("result=%#v error=%v code=%q, want nil backup_failed wrapping cancellation", result, err, CodeOf(err))
	}
	assertNoSnapshotArtifacts(t, transactionRoot)
	assertTestFile(t, target, "prior")
}

func TestPrepareSnapshotsInjectedLaterActionFailureCleansEarlierBackups(t *testing.T) {
	transactionRoot := t.TempDir()
	hostRoot := t.TempDir()
	first := filepath.Join(hostRoot, "first")
	second := filepath.Join(hostRoot, "second")
	writeTestFile(t, first, "first")
	writeTestFile(t, second, "second")
	cause := errors.New("injected second-action backup failure")
	preparer := NewSnapshotPreparer()
	preparer.checkpoint = func(_ context.Context, phase snapshotPhase, index int) error {
		if phase == phaseBeforeAction && index == 1 {
			return cause
		}
		return nil
	}

	result, err := preparer.Prepare(context.Background(), SnapshotRequest{
		Set: &MaterializedSet{Actions: []Action{
			{Kind: ActionDeleteFile, Target: first, SnapshotRequired: true},
			{Kind: ActionDeleteFile, Target: second, SnapshotRequired: true},
		}},
		TransactionRoot: transactionRoot,
	})
	var typed *Error
	if result != nil || !errors.As(err, &typed) || typed.Code != CodeBackupFailed || typed.ActionIndex != 1 || !errors.Is(err, cause) {
		t.Fatalf("result=%#v error=%#v, want action[1] backup_failed", result, err)
	}
	assertNoSnapshotArtifacts(t, transactionRoot)
	assertTestFile(t, first, "first")
	assertTestFile(t, second, "second")
}

func TestPrepareSnapshotsDetectsTargetChangeBeforePublication(t *testing.T) {
	transactionRoot := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	writeTestFile(t, target, "prior")
	preparer := NewSnapshotPreparer()
	preparer.checkpoint = func(_ context.Context, phase snapshotPhase, index int) error {
		if phase == phaseAfterAction && index == 0 {
			return os.WriteFile(target, []byte("changed externally"), 0o600)
		}
		return nil
	}

	result, err := preparer.Prepare(context.Background(), SnapshotRequest{
		Set:             &MaterializedSet{Actions: []Action{{Kind: ActionDeleteFile, Target: target, SnapshotRequired: true}}},
		TransactionRoot: transactionRoot,
	})
	if result != nil || CodeOf(err) != CodeBackupFailed || !strings.Contains(err.Error(), "changed after snapshot") {
		t.Fatalf("result=%#v error=%v code=%q, want target-change backup_failed", result, err, CodeOf(err))
	}
	assertNoSnapshotArtifacts(t, transactionRoot)
}

func TestPrepareSnapshotsDetectsCopySourceChangeAndNeverWritesStage(t *testing.T) {
	transactionRoot := t.TempDir()
	stageRoot := t.TempDir()
	source := filepath.Join(stageRoot, "source")
	target := filepath.Join(t.TempDir(), "target")
	writeTestFile(t, source, "source")
	writeTestFile(t, target, "target")
	preparer := NewSnapshotPreparer()
	preparer.checkpoint = func(_ context.Context, phase snapshotPhase, index int) error {
		if phase == phaseAfterAction && index == 0 {
			return os.WriteFile(source, []byte("changed externally"), 0o600)
		}
		return nil
	}

	result, err := preparer.Prepare(context.Background(), SnapshotRequest{
		Set: &MaterializedSet{Actions: []Action{{
			Kind: ActionCopy, Source: source, Target: target, SourceMode: 0o600, SnapshotRequired: true,
		}}},
		TransactionRoot: transactionRoot,
	})
	if result != nil || CodeOf(err) != CodeBackupFailed || !strings.Contains(err.Error(), "copy source changed") {
		t.Fatalf("result=%#v error=%v code=%q, want source-change backup_failed", result, err, CodeOf(err))
	}
	assertNoSnapshotArtifacts(t, transactionRoot)
	assertTestFile(t, target, "target")
}

func TestPrepareSnapshotsFinalVerificationRejectsCorruptedBackup(t *testing.T) {
	transactionRoot := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	writeTestFile(t, target, "prior")
	preparer := NewSnapshotPreparer()
	preparer.checkpoint = func(_ context.Context, phase snapshotPhase, index int) error {
		if phase != phaseAfterAction || index != 0 {
			return nil
		}
		matches, err := filepath.Glob(filepath.Join(transactionRoot, ".snapshots-preparing-*", "000000", "prior"))
		if err != nil || len(matches) != 1 {
			return fmt.Errorf("locate temporary backup: matches=%v err=%v", matches, err)
		}
		return os.WriteFile(matches[0], []byte("corrupted backup"), 0o600)
	}

	result, err := preparer.Prepare(context.Background(), SnapshotRequest{
		Set:             &MaterializedSet{Actions: []Action{{Kind: ActionDeleteFile, Target: target, SnapshotRequired: true}}},
		TransactionRoot: transactionRoot,
	})
	if result != nil || CodeOf(err) != CodeBackupFailed || !strings.Contains(err.Error(), "backup") {
		t.Fatalf("result=%#v error=%v code=%q, want corrupted-backup failure", result, err, CodeOf(err))
	}
	assertNoSnapshotArtifacts(t, transactionRoot)
	assertTestFile(t, target, "prior")
}

func TestPrepareSnapshotsDetectsRegistryChangeBeforePublication(t *testing.T) {
	transactionRoot := t.TempDir()
	identity := `HKCU\Software\Endstate` + "\x00" + "Theme"
	reader := &mapRegistryReader{values: map[string]RegistryReadResult{
		identity: {Exists: true, ValueType: RegistryTypeDWORD, Data: []byte{0, 0, 0, 0}},
	}}
	preparer := NewSnapshotPreparer()
	preparer.checkpoint = func(_ context.Context, phase snapshotPhase, index int) error {
		if phase == phaseAfterAction && index == 0 {
			reader.values[identity] = RegistryReadResult{Exists: true, ValueType: RegistryTypeDWORD, Data: []byte{1, 0, 0, 0}}
		}
		return nil
	}

	result, err := preparer.Prepare(context.Background(), SnapshotRequest{
		Set: &MaterializedSet{Actions: []Action{{
			Kind: ActionRegistrySet, Target: `HKCU\Software\Endstate\Theme`, SnapshotRequired: true,
			RegistryValue: &RegistryValue{Key: `HKCU\Software\Endstate`, ValueName: "Theme", ValueType: "REG_DWORD", Data: "1"},
		}}},
		TransactionRoot: transactionRoot, RegistryReader: reader,
	})
	if result != nil || CodeOf(err) != CodeBackupFailed || !strings.Contains(err.Error(), "registry value changed") {
		t.Fatalf("result=%#v error=%v code=%q, want registry-change backup_failed", result, err, CodeOf(err))
	}
	assertNoSnapshotArtifacts(t, transactionRoot)
}

func TestPrepareSnapshotsRejectsUnsafeSourceTargetTransactionAndBackupTrees(t *testing.T) {
	t.Run("linked transaction root", func(t *testing.T) {
		realRoot := t.TempDir()
		linkedRoot := filepath.Join(t.TempDir(), "linked")
		if err := os.Symlink(realRoot, linkedRoot); err != nil {
			t.Skipf("creating test symlink is unavailable: %v", err)
		}
		result, err := PrepareSnapshots(context.Background(), SnapshotRequest{
			Set: &MaterializedSet{Actions: []Action{}}, TransactionRoot: linkedRoot,
		})
		if result != nil || CodeOf(err) != CodeBackupFailed {
			t.Fatalf("result=%#v error=%v code=%q", result, err, CodeOf(err))
		}
	})

	t.Run("linked target", func(t *testing.T) {
		transactionRoot := t.TempDir()
		realTarget := filepath.Join(t.TempDir(), "real")
		linkedTarget := filepath.Join(t.TempDir(), "linked")
		writeTestFile(t, realTarget, "prior")
		if err := os.Symlink(realTarget, linkedTarget); err != nil {
			t.Skipf("creating test symlink is unavailable: %v", err)
		}
		result, err := PrepareSnapshots(context.Background(), SnapshotRequest{
			Set:             &MaterializedSet{Actions: []Action{{Kind: ActionDeleteFile, Target: linkedTarget, SnapshotRequired: true}}},
			TransactionRoot: transactionRoot,
		})
		if result != nil || CodeOf(err) != CodeBackupFailed {
			t.Fatalf("result=%#v error=%v code=%q", result, err, CodeOf(err))
		}
		assertNoSnapshotArtifacts(t, transactionRoot)
		assertTestFile(t, realTarget, "prior")
	})

	t.Run("linked source tree entry", func(t *testing.T) {
		transactionRoot := t.TempDir()
		source := filepath.Join(t.TempDir(), "source")
		target := filepath.Join(t.TempDir(), "target")
		outside := filepath.Join(t.TempDir(), "outside")
		writeTestFile(t, filepath.Join(source, "safe"), "safe")
		writeTestFile(t, outside, "outside")
		if err := os.Symlink(outside, filepath.Join(source, "linked")); err != nil {
			t.Skipf("creating test symlink is unavailable: %v", err)
		}
		result, err := PrepareSnapshots(context.Background(), SnapshotRequest{
			Set: &MaterializedSet{Actions: []Action{{
				Kind: ActionCopy, Source: source, Target: target, SourceIsDirectory: true, SnapshotRequired: true,
			}}},
			TransactionRoot: transactionRoot,
		})
		if result != nil || CodeOf(err) != CodeBackupFailed {
			t.Fatalf("result=%#v error=%v code=%q", result, err, CodeOf(err))
		}
		assertNoSnapshotArtifacts(t, transactionRoot)
	})

	t.Run("linked target tree entry", func(t *testing.T) {
		transactionRoot := t.TempDir()
		source := filepath.Join(t.TempDir(), "source")
		target := filepath.Join(t.TempDir(), "target")
		outside := filepath.Join(t.TempDir(), "outside")
		writeTestFile(t, filepath.Join(source, "safe"), "safe")
		writeTestFile(t, filepath.Join(target, "prior"), "prior")
		writeTestFile(t, outside, "outside")
		if err := os.Symlink(outside, filepath.Join(target, "linked")); err != nil {
			t.Skipf("creating test symlink is unavailable: %v", err)
		}
		result, err := PrepareSnapshots(context.Background(), SnapshotRequest{
			Set: &MaterializedSet{Actions: []Action{{
				Kind: ActionCopy, Source: source, Target: target, SourceIsDirectory: true, SnapshotRequired: true,
			}}},
			TransactionRoot: transactionRoot,
		})
		if result != nil || CodeOf(err) != CodeBackupFailed {
			t.Fatalf("result=%#v error=%v code=%q", result, err, CodeOf(err))
		}
		assertNoSnapshotArtifacts(t, transactionRoot)
		assertTestFile(t, outside, "outside")
	})

	t.Run("write target changed to directory", func(t *testing.T) {
		transactionRoot := t.TempDir()
		target := filepath.Join(t.TempDir(), "target")
		if err := os.Mkdir(target, 0o700); err != nil {
			t.Fatal(err)
		}
		result, err := PrepareSnapshots(context.Background(), SnapshotRequest{
			Set: &MaterializedSet{Actions: []Action{{
				Kind: ActionWriteFile, Target: target, DesiredContent: []byte("desired"), SnapshotRequired: true,
			}}},
			TransactionRoot: transactionRoot,
		})
		if result != nil || CodeOf(err) != CodeBackupFailed {
			t.Fatalf("result=%#v error=%v code=%q", result, err, CodeOf(err))
		}
		assertNoSnapshotArtifacts(t, transactionRoot)
	})

	t.Run("backup replaced by link", func(t *testing.T) {
		transactionRoot := t.TempDir()
		target := filepath.Join(t.TempDir(), "target")
		outside := filepath.Join(t.TempDir(), "outside")
		writeTestFile(t, target, "prior")
		writeTestFile(t, outside, "outside")
		preparer := NewSnapshotPreparer()
		preparer.checkpoint = func(_ context.Context, phase snapshotPhase, index int) error {
			if phase != phaseAfterAction || index != 0 {
				return nil
			}
			matches, err := filepath.Glob(filepath.Join(transactionRoot, ".snapshots-preparing-*", "000000", "prior"))
			if err != nil || len(matches) != 1 {
				return fmt.Errorf("locate backup: matches=%v err=%v", matches, err)
			}
			if err := os.Remove(matches[0]); err != nil {
				return err
			}
			return os.Symlink(outside, matches[0])
		}
		result, err := preparer.Prepare(context.Background(), SnapshotRequest{
			Set:             &MaterializedSet{Actions: []Action{{Kind: ActionDeleteFile, Target: target, SnapshotRequired: true}}},
			TransactionRoot: transactionRoot,
		})
		if result != nil || CodeOf(err) != CodeBackupFailed {
			t.Fatalf("result=%#v error=%v code=%q", result, err, CodeOf(err))
		}
		assertNoSnapshotArtifacts(t, transactionRoot)
		assertTestFile(t, outside, "outside")
	})

	t.Run("backup tree entry replaced by link", func(t *testing.T) {
		transactionRoot := t.TempDir()
		source := filepath.Join(t.TempDir(), "source")
		target := filepath.Join(t.TempDir(), "target")
		outside := filepath.Join(t.TempDir(), "outside")
		if err := os.Mkdir(source, 0o700); err != nil {
			t.Fatal(err)
		}
		writeTestFile(t, filepath.Join(target, "prior"), "prior")
		writeTestFile(t, outside, "outside")
		preparer := NewSnapshotPreparer()
		preparer.checkpoint = func(_ context.Context, phase snapshotPhase, index int) error {
			if phase != phaseAfterAction || index != 0 {
				return nil
			}
			matches, err := filepath.Glob(filepath.Join(transactionRoot, ".snapshots-preparing-*", "000000", "prior", "prior"))
			if err != nil || len(matches) != 1 {
				return fmt.Errorf("locate backup tree entry: matches=%v err=%v", matches, err)
			}
			if err := os.Remove(matches[0]); err != nil {
				return err
			}
			return os.Symlink(outside, matches[0])
		}
		result, err := preparer.Prepare(context.Background(), SnapshotRequest{
			Set: &MaterializedSet{Actions: []Action{{
				Kind: ActionCopy, Source: source, Target: target, SourceIsDirectory: true, SnapshotRequired: true,
			}}},
			TransactionRoot: transactionRoot,
		})
		if result != nil || CodeOf(err) != CodeBackupFailed {
			t.Fatalf("result=%#v error=%v code=%q", result, err, CodeOf(err))
		}
		assertNoSnapshotArtifacts(t, transactionRoot)
		assertTestFile(t, outside, "outside")
	})
}

func TestPrepareSnapshotsRejectsPreexistingPublicationPath(t *testing.T) {
	transactionRoot := t.TempDir()
	if err := os.Mkdir(filepath.Join(transactionRoot, "snapshots"), 0o700); err != nil {
		t.Fatal(err)
	}
	result, err := PrepareSnapshots(context.Background(), SnapshotRequest{
		Set: &MaterializedSet{Actions: []Action{}}, TransactionRoot: transactionRoot,
	})
	if result != nil || CodeOf(err) != CodeBackupFailed {
		t.Fatalf("result=%#v error=%v code=%q", result, err, CodeOf(err))
	}
}

func assertNoSnapshotArtifacts(t *testing.T, transactionRoot string) {
	t.Helper()
	entries, err := os.ReadDir(transactionRoot)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name() == "snapshots" || strings.HasPrefix(entry.Name(), ".snapshots-preparing-") {
			t.Fatalf("incomplete snapshot artifact remains: %s", entry.Name())
		}
	}
}
