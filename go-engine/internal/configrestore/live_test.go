// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package configrestore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"testing"
	"time"
)

func TestBeginLiveCreatesVersionedTransactionRootWithImmutableDescriptor(t *testing.T) {
	stateDir := t.TempDir()
	guard, err := BeginLive(context.Background(), stateDir, "apply-20260716-120000-test", nil)
	if err != nil {
		t.Fatalf("BeginLive() error = %v", err)
	}
	t.Cleanup(func() { _ = guard.Close() })

	root, err := guard.CreateTransactionRoot("capture-example")
	if err != nil {
		t.Fatalf("CreateTransactionRoot() error = %v", err)
	}
	wantParent := filepath.Join(stateDir, "config-restore", "v1", "transactions")
	if filepath.Dir(root) != wantParent || !regexp.MustCompile(`^[0-9a-f]{32}$`).MatchString(filepath.Base(root)) {
		t.Fatalf("transaction root = %q, want opaque child beneath %q", root, wantParent)
	}

	data, err := os.ReadFile(filepath.Join(root, "transaction.json"))
	if err != nil {
		t.Fatalf("read descriptor: %v", err)
	}
	var disk transactionDescriptorDisk
	if err := json.Unmarshal(data, &disk); err != nil {
		t.Fatalf("decode descriptor: %v", err)
	}
	if disk.Format != transactionDescriptorFormat || disk.Version != transactionStoreVersion ||
		disk.TransactionID != filepath.Base(root) || disk.RunID != "apply-20260716-120000-test" ||
		disk.CaptureID != "capture-example" || disk.MutationOrdinal != 0 ||
		!regexp.MustCompile(`^[0-9a-f]{32}$`).MatchString(disk.RestoreRunID) || disk.DescriptorDigest == "" {
		t.Fatalf("descriptor = %+v", disk)
	}
	if _, canonical, err := decodeTransactionDescriptor(data); err != nil || string(canonical) != string(data) {
		t.Fatalf("descriptor canonical verification failed: err=%v", err)
	}
}

func TestDiscardTransactionRootRemovesOnlyNoIntentPreallocation(t *testing.T) {
	ctx := context.Background()
	guard, err := BeginLive(ctx, t.TempDir(), "discard-preallocation", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = guard.Close() })

	unused, err := guard.CreateTransactionRoot("capture-unused")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(unused, "snapshots", "large"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(unused, "snapshots", "large", "prefs.bin"), []byte("snapshot"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := guard.DiscardTransactionRoot(unused); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(unused); !os.IsNotExist(err) {
		t.Fatalf("unused transaction root survived discard: %v", err)
	}

	started, err := guard.CreateTransactionRoot("capture-started")
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "target.json")
	if err := os.WriteFile(target, []byte("prior"), 0o600); err != nil {
		t.Fatal(err)
	}
	prepared, err := PrepareSnapshots(ctx, SnapshotRequest{Set: &MaterializedSet{Actions: []Action{{
		Kind: ActionDeleteFile, Strategy: "delete-glob", Target: target, SnapshotRequired: true,
	}}}, TransactionRoot: started})
	if err != nil {
		t.Fatal(err)
	}
	lineage := testJournalLineage()
	lineage.RunID = "discard-preallocation"
	lineage.CaptureID = "capture-started"
	if _, err := PersistJournalIntent(ctx, JournalIntentRequest{Prepared: prepared, TransactionRoot: started, Lineage: lineage}); err != nil {
		t.Fatal(err)
	}
	if err := guard.DiscardTransactionRoot(started); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(started); err != nil {
		t.Fatalf("started transaction root was discarded: %v", err)
	}
}

func TestBeginLiveHoldsOneStoreLockAcrossGuardsUntilClosed(t *testing.T) {
	stateDir := t.TempDir()
	first, err := BeginLive(context.Background(), stateDir, "apply-first", nil)
	if err != nil {
		t.Fatalf("first BeginLive() error = %v", err)
	}

	waitContext, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	if second, err := BeginLive(waitContext, stateDir, "apply-second", nil); err == nil || second != nil ||
		!errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("contended BeginLive() = (%v, %v), want deadline error", second, err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("first Close() error = %v", err)
	}
	second, err := BeginLive(context.Background(), stateDir, "apply-second", nil)
	if err != nil {
		t.Fatalf("BeginLive() after release error = %v", err)
	}
	if err := second.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
	if _, err := os.Lstat(filepath.Join(stateDir, "config-restore", "mutation.lock")); err != nil {
		t.Fatalf("persistent mutation lock file: %v", err)
	}
}

func TestBeginLiveMutationLockExcludesSecondProcessUntilGuardClose(t *testing.T) {
	stateDir := t.TempDir()
	signalDir := t.TempDir()
	readyPath := filepath.Join(signalDir, "ready")
	releasePath := filepath.Join(signalDir, "release")
	command := exec.Command(os.Args[0], "-test.run=^TestConfigRestoreLockHolderHelper$")
	command.Env = append(os.Environ(),
		"ENDSTATE_CONFIG_RESTORE_LOCK_HELPER=1",
		"ENDSTATE_CONFIG_RESTORE_LOCK_STATE="+stateDir,
		"ENDSTATE_CONFIG_RESTORE_LOCK_READY="+readyPath,
		"ENDSTATE_CONFIG_RESTORE_LOCK_RELEASE="+releasePath,
	)
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if command.Process != nil {
			_ = command.Process.Kill()
		}
	})
	if err := waitForTestPath(readyPath, 5*time.Second); err != nil {
		_ = command.Process.Kill()
		_ = command.Wait()
		t.Fatalf("lock helper did not become ready: %v", err)
	}

	waitContext, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	second, err := BeginLive(waitContext, stateDir, "apply-parent-contended", nil)
	cancel()
	if err == nil || second != nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("contended cross-process BeginLive() = (%v, %v), want deadline error", second, err)
	}
	if err := os.WriteFile(releasePath, []byte("release"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := command.Wait(); err != nil {
		t.Fatalf("lock helper exit: %v", err)
	}

	afterClose, err := BeginLive(context.Background(), stateDir, "apply-parent-after-close", nil)
	if err != nil {
		t.Fatalf("BeginLive() after child Guard.Close: %v", err)
	}
	if err := afterClose.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestConfigRestoreLockHolderHelper(t *testing.T) {
	if os.Getenv("ENDSTATE_CONFIG_RESTORE_LOCK_HELPER") != "1" {
		return
	}
	guard, err := BeginLive(
		context.Background(), os.Getenv("ENDSTATE_CONFIG_RESTORE_LOCK_STATE"), "apply-child-holder", nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(os.Getenv("ENDSTATE_CONFIG_RESTORE_LOCK_READY"), []byte("ready"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := waitForTestPath(os.Getenv("ENDSTATE_CONFIG_RESTORE_LOCK_RELEASE"), 10*time.Second); err != nil {
		t.Fatal(err)
	}
	if err := guard.Close(); err != nil {
		t.Fatal(err)
	}
}

func waitForTestPath(path string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return nil
		} else if !os.IsNotExist(err) {
			return err
		}
		time.Sleep(10 * time.Millisecond)
	}
	return fmt.Errorf("timed out waiting for %q", path)
}

func TestBeginLiveRecoversPendingIntentBeforeReturningGuard(t *testing.T) {
	ctx := context.Background()
	stateDir := t.TempDir()
	runID := "apply-20260716-120000-test"
	first, err := BeginLive(ctx, stateDir, runID, nil)
	if err != nil {
		t.Fatalf("first BeginLive() error = %v", err)
	}
	root, err := first.CreateTransactionRoot("capture-example")
	if err != nil {
		t.Fatalf("CreateTransactionRoot() error = %v", err)
	}
	target := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(target, []byte("before"), 0o600); err != nil {
		t.Fatal(err)
	}
	prepared, err := PrepareSnapshots(ctx, SnapshotRequest{
		Set: &MaterializedSet{Actions: []Action{{
			Kind: ActionDeleteFile, Strategy: "delete-glob", Target: target, SnapshotRequired: true,
		}}},
		TransactionRoot: root,
	})
	if err != nil {
		t.Fatalf("PrepareSnapshots() error = %v", err)
	}
	lineage := testJournalLineage()
	lineage.RunID = runID
	lineage.CaptureID = "capture-example"
	intent, err := PersistJournalIntent(ctx, JournalIntentRequest{
		Prepared: prepared, TransactionRoot: root, Lineage: lineage,
	})
	if err != nil {
		t.Fatalf("PersistJournalIntent() error = %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("first guard Close() error = %v", err)
	}

	second, err := BeginLive(ctx, stateDir, "restore-20260716-120100-test", nil)
	if err != nil {
		t.Fatalf("second BeginLive() recovery error = %v", err)
	}
	t.Cleanup(func() { _ = second.Close() })
	markerPath := journalMarkerPath(filepath.Join(root, "journal"), JournalRolledBack, intent.Digest())
	marker, err := readJournalMarkerFile(root, markerPath, intent)
	if err != nil {
		t.Fatalf("read recovered marker: %v", err)
	}
	if marker.State() != JournalRolledBack || marker.RollbackOutcome() != RollbackSucceeded ||
		marker.ValidationStatus() != ValidationNotRun {
		t.Fatalf("recovered marker = state %q rollback %q validation %q",
			marker.State(), marker.RollbackOutcome(), marker.ValidationStatus())
	}
	if data, err := os.ReadFile(target); err != nil || string(data) != "before" {
		t.Fatalf("target after recovery = %q, %v", data, err)
	}
}

func TestBeginLiveRecoversProcessDeathAfterTargetMutation(t *testing.T) {
	fixture := prepareStoredDelete(t, "before")
	preparedAction := fixture.prepared.Actions()[0]
	journalAction := fixture.intent.Actions()[0]
	if err := executeTransactionAction(
		context.Background(), preparedAction, journalAction, nil, func() {}, nil,
	); err != nil {
		t.Fatalf("simulate committed target action: %v", err)
	}
	if _, err := os.Lstat(fixture.target); !os.IsNotExist(err) {
		t.Fatalf("target should be absent at simulated process death, err=%v", err)
	}
	if err := fixture.guard.Close(); err != nil {
		t.Fatal(err)
	}

	recovered, err := BeginLive(context.Background(), fixture.stateDir, "restore-after-crash", nil)
	if err != nil {
		t.Fatalf("BeginLive() recovery error = %v", err)
	}
	t.Cleanup(func() { _ = recovered.Close() })
	if data, err := os.ReadFile(fixture.target); err != nil || string(data) != "before" {
		t.Fatalf("target after next-run recovery = %q, %v", data, err)
	}
	markerPath := journalMarkerPath(filepath.Join(fixture.root, "journal"), JournalRolledBack, fixture.intent.Digest())
	if marker, err := readJournalMarkerFile(fixture.root, markerPath, fixture.intent); err != nil || marker.State() != JournalRolledBack {
		t.Fatalf("recovered terminal marker = %+v, %v", marker, err)
	}
}

func TestBeginLiveRecoversProcessDeathBetweenConfigSetActions(t *testing.T) {
	ctx := context.Background()
	stateDir := t.TempDir()
	guard, err := BeginLive(ctx, stateDir, "apply-crash", nil)
	if err != nil {
		t.Fatal(err)
	}
	root, err := guard.CreateTransactionRoot("capture-crash")
	if err != nil {
		t.Fatal(err)
	}
	parent := t.TempDir()
	targets := []string{filepath.Join(parent, "one"), filepath.Join(parent, "two")}
	for index, target := range targets {
		if err := os.WriteFile(target, []byte{byte('a' + index)}, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	prepared, err := PrepareSnapshots(ctx, SnapshotRequest{
		Set: &MaterializedSet{Actions: []Action{
			{Kind: ActionDeleteFile, Strategy: "delete-glob", Target: targets[0], SnapshotRequired: true},
			{Kind: ActionDeleteFile, Strategy: "delete-glob", Target: targets[1], SnapshotRequired: true},
		}}, TransactionRoot: root,
	})
	if err != nil {
		t.Fatal(err)
	}
	lineage := testJournalLineage()
	lineage.RunID, lineage.CaptureID = "apply-crash", "capture-crash"
	intent, err := PersistJournalIntent(ctx, JournalIntentRequest{Prepared: prepared, TransactionRoot: root, Lineage: lineage})
	if err != nil {
		t.Fatal(err)
	}
	if err := executeTransactionAction(ctx, prepared.Actions()[0], intent.Actions()[0], nil, func() {}, nil); err != nil {
		t.Fatal(err)
	}
	if err := guard.Close(); err != nil {
		t.Fatal(err)
	}

	recovered, err := BeginLive(ctx, stateDir, "restore-after-crash", nil)
	if err != nil {
		t.Fatalf("recover between actions: %v", err)
	}
	t.Cleanup(func() { _ = recovered.Close() })
	for index, target := range targets {
		data, readErr := os.ReadFile(target)
		if readErr != nil || len(data) != 1 || data[0] != byte('a'+index) {
			t.Fatalf("target[%d] after recovery = %q, %v", index, data, readErr)
		}
	}
}

func TestBeginLiveDoesNotRollbackDurablyCommittedTransaction(t *testing.T) {
	fixture := prepareStoredDelete(t, "before")
	result, err := ExecuteConfigSetTransaction(context.Background(), TransactionRequest{
		Prepared: fixture.prepared, Intent: fixture.intent,
	})
	if err != nil || result.Status() != TransactionRestored {
		t.Fatalf("ExecuteConfigSetTransaction() = %+v, %v", result, err)
	}
	if err := fixture.guard.Close(); err != nil {
		t.Fatal(err)
	}

	next, err := BeginLive(context.Background(), fixture.stateDir, "restore-next", nil)
	if err != nil {
		t.Fatalf("BeginLive() after committed transaction: %v", err)
	}
	t.Cleanup(func() { _ = next.Close() })
	if _, err := os.Lstat(fixture.target); !os.IsNotExist(err) {
		t.Fatalf("committed target should remain absent, err=%v", err)
	}
}

func TestBeginLiveClassifiesCommittedTransactionBeforeMissingBackupVerification(t *testing.T) {
	fixture := prepareStoredDelete(t, "before")
	result, err := ExecuteConfigSetTransaction(context.Background(), TransactionRequest{
		Prepared: fixture.prepared, Intent: fixture.intent,
	})
	if err != nil || result.Status() != TransactionRestored {
		t.Fatalf("ExecuteConfigSetTransaction() = %+v, %v", result, err)
	}
	backup := fixture.intent.Actions()[0].Prior.BackupPath
	if err := os.Remove(backup); err != nil {
		t.Fatalf("remove committed backup: %v", err)
	}
	if err := fixture.guard.Close(); err != nil {
		t.Fatal(err)
	}

	next, err := BeginLive(context.Background(), fixture.stateDir, "restore-next", nil)
	if err != nil {
		t.Fatalf("BeginLive() classified terminal history as pending: %v", err)
	}
	t.Cleanup(func() { _ = next.Close() })
	if _, err := os.Lstat(fixture.target); !os.IsNotExist(err) {
		t.Fatalf("committed target should remain absent, err=%v", err)
	}
}

func TestBeginLiveClassifiesRolledBackTransactionBeforeMissingBackupVerification(t *testing.T) {
	fixture := prepareStoredDelete(t, "before")
	if _, err := PersistRolledBackMarker(context.Background(), fixture.intent, ValidationNotRun); err != nil {
		t.Fatalf("PersistRolledBackMarker() error = %v", err)
	}
	if err := os.Remove(fixture.intent.Actions()[0].Prior.BackupPath); err != nil {
		t.Fatalf("remove rolled-back backup: %v", err)
	}
	if err := fixture.guard.Close(); err != nil {
		t.Fatal(err)
	}
	next, err := BeginLive(context.Background(), fixture.stateDir, "restore-next", nil)
	if err != nil {
		t.Fatalf("BeginLive() classified rolled-back history as pending: %v", err)
	}
	t.Cleanup(func() { _ = next.Close() })
}

func TestBeginLiveRejectsPendingTransactionWithMissingBackup(t *testing.T) {
	fixture := prepareStoredDelete(t, "before")
	if err := os.Remove(fixture.intent.Actions()[0].Prior.BackupPath); err != nil {
		t.Fatal(err)
	}
	if err := fixture.guard.Close(); err != nil {
		t.Fatal(err)
	}
	guard, err := BeginLive(context.Background(), fixture.stateDir, "restore-blocked", nil)
	if guard != nil || !errors.Is(err, ErrRecoveryRequired) {
		t.Fatalf("BeginLive() = (%v, %v), want recovery required", guard, err)
	}
}

func TestBeginLiveRejectsCorruptTerminalWithoutChangingTarget(t *testing.T) {
	fixture := prepareStoredDelete(t, "before")
	terminalPath := journalMarkerPath(filepath.Join(fixture.root, "journal"), JournalCommitted, fixture.intent.Digest())
	if err := os.WriteFile(terminalPath, []byte("not-json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := fixture.guard.Close(); err != nil {
		t.Fatal(err)
	}
	guard, err := BeginLive(context.Background(), fixture.stateDir, "restore-blocked", nil)
	if guard != nil || !errors.Is(err, ErrRecoveryRequired) {
		t.Fatalf("BeginLive() = (%v, %v), want recovery required", guard, err)
	}
	if data, err := os.ReadFile(fixture.target); err != nil || string(data) != "before" {
		t.Fatalf("target changed while terminal was untrusted = %q, %v", data, err)
	}
}

func TestBeginLiveRecoversRealProcessDeathAfterDurableMutation(t *testing.T) {
	stateDir := t.TempDir()
	target := filepath.Join(t.TempDir(), "settings.json")
	command := exec.Command(os.Args[0], "-test.run=^TestConfigRestoreCrashHelper$")
	command.Env = append(os.Environ(),
		"ENDSTATE_CONFIG_RESTORE_CRASH_HELPER=1",
		"ENDSTATE_CONFIG_RESTORE_CRASH_STATE="+stateDir,
		"ENDSTATE_CONFIG_RESTORE_CRASH_TARGET="+target,
	)
	output, err := command.CombinedOutput()
	var exitError *exec.ExitError
	if !errors.As(err, &exitError) || exitError.ExitCode() != 23 {
		t.Fatalf("crash helper = %v, output=%s", err, output)
	}
	if _, err := os.Lstat(target); !os.IsNotExist(err) {
		t.Fatalf("target should be absent at process death, err=%v", err)
	}
	guard, err := BeginLive(context.Background(), stateDir, "restore-after-real-crash", nil)
	if err != nil {
		t.Fatalf("BeginLive() after real process death: %v", err)
	}
	t.Cleanup(func() { _ = guard.Close() })
	if data, err := os.ReadFile(target); err != nil || string(data) != "before" {
		t.Fatalf("target after real process recovery = %q, %v", data, err)
	}
}

func TestConfigRestoreCrashHelper(t *testing.T) {
	if os.Getenv("ENDSTATE_CONFIG_RESTORE_CRASH_HELPER") != "1" {
		return
	}
	ctx := context.Background()
	stateDir := os.Getenv("ENDSTATE_CONFIG_RESTORE_CRASH_STATE")
	target := os.Getenv("ENDSTATE_CONFIG_RESTORE_CRASH_TARGET")
	guard, err := BeginLive(ctx, stateDir, "apply-real-crash", nil)
	if err != nil {
		t.Fatal(err)
	}
	root, err := guard.CreateTransactionRoot("capture-real-crash")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("before"), 0o600); err != nil {
		t.Fatal(err)
	}
	prepared, err := PrepareSnapshots(ctx, SnapshotRequest{
		Set: &MaterializedSet{Actions: []Action{{
			Kind: ActionDeleteFile, Strategy: "delete-glob", Target: target, SnapshotRequired: true,
		}}}, TransactionRoot: root,
	})
	if err != nil {
		t.Fatal(err)
	}
	lineage := testJournalLineage()
	lineage.RunID, lineage.CaptureID = "apply-real-crash", "capture-real-crash"
	intent, err := PersistJournalIntent(ctx, JournalIntentRequest{Prepared: prepared, TransactionRoot: root, Lineage: lineage})
	if err != nil {
		t.Fatal(err)
	}
	err = executeTransactionAction(ctx, prepared.Actions()[0], intent.Actions()[0], nil, func() {}, func() error {
		os.Exit(23)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Fatal("mutation checkpoint did not terminate process")
}

func TestBeginLiveRejectsLinkedMutationLock(t *testing.T) {
	stateDir := t.TempDir()
	configRoot := filepath.Join(stateDir, "config-restore")
	if err := os.Mkdir(configRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "unrelated.lock")
	if err := os.WriteFile(target, []byte("untouched"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(configRoot, "mutation.lock")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if guard, err := BeginLive(context.Background(), stateDir, "apply-linked", nil); err == nil || guard != nil {
		t.Fatalf("BeginLive() = (%v, %v), want unsafe lock error", guard, err)
	}
	if data, err := os.ReadFile(target); err != nil || string(data) != "untouched" {
		t.Fatalf("lock target changed = %q, %v", data, err)
	}
}

func TestBeginLiveLeavesPreIntentTransactionRootAsHarmlessOrphan(t *testing.T) {
	stateDir := t.TempDir()
	first, err := BeginLive(context.Background(), stateDir, "apply-orphan", nil)
	if err != nil {
		t.Fatal(err)
	}
	root, err := first.CreateTransactionRoot("capture-orphan")
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	second, err := BeginLive(context.Background(), stateDir, "apply-next", nil)
	if err != nil {
		t.Fatalf("BeginLive() with pre-intent orphan: %v", err)
	}
	t.Cleanup(func() { _ = second.Close() })
	if _, err := os.Stat(filepath.Join(root, "transaction.json")); err != nil {
		t.Fatalf("orphan descriptor was removed: %v", err)
	}
}

func TestBeginLiveBlocksNewMutationWhenPendingTargetHasUnrelatedDrift(t *testing.T) {
	fixture := prepareStoredDelete(t, "before")
	if err := os.WriteFile(fixture.target, []byte("unrelated"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := fixture.guard.Close(); err != nil {
		t.Fatal(err)
	}

	guard, err := BeginLive(context.Background(), fixture.stateDir, "restore-blocked", nil)
	if guard != nil || !errors.Is(err, ErrRecoveryRequired) {
		t.Fatalf("BeginLive() = (%v, %v), want recovery_required", guard, err)
	}
	if data, readErr := os.ReadFile(fixture.target); readErr != nil || string(data) != "unrelated" {
		t.Fatalf("unrelated drift was changed: %q, %v", data, readErr)
	}
}

type storedDeleteFixture struct {
	stateDir string
	root     string
	target   string
	guard    *Guard
	prepared *PreparedSet
	intent   *JournalIntent
}

func prepareStoredDelete(t *testing.T, prior string) storedDeleteFixture {
	t.Helper()
	ctx := context.Background()
	stateDir := t.TempDir()
	guard, err := BeginLive(ctx, stateDir, "apply-crash", nil)
	if err != nil {
		t.Fatal(err)
	}
	root, err := guard.CreateTransactionRoot("capture-crash")
	if err != nil {
		_ = guard.Close()
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(target, []byte(prior), 0o600); err != nil {
		_ = guard.Close()
		t.Fatal(err)
	}
	prepared, err := PrepareSnapshots(ctx, SnapshotRequest{
		Set: &MaterializedSet{Actions: []Action{{
			Kind: ActionDeleteFile, Strategy: "delete-glob", Target: target, SnapshotRequired: true,
		}}}, TransactionRoot: root,
	})
	if err != nil {
		_ = guard.Close()
		t.Fatal(err)
	}
	lineage := testJournalLineage()
	lineage.RunID, lineage.CaptureID = "apply-crash", "capture-crash"
	intent, err := PersistJournalIntent(ctx, JournalIntentRequest{Prepared: prepared, TransactionRoot: root, Lineage: lineage})
	if err != nil {
		_ = guard.Close()
		t.Fatal(err)
	}
	return storedDeleteFixture{
		stateDir: stateDir, root: root, target: target, guard: guard, prepared: prepared, intent: intent,
	}
}
