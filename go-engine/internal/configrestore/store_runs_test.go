// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package configrestore

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestActiveStoreRunsOrdersGenerationAndRegisteredLegacyMembers(t *testing.T) {
	ctx := context.Background()
	stateDir := t.TempDir()
	guard, err := BeginLive(ctx, stateDir, "apply-mixed", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = guard.Close() })

	firstTarget := commitStoredDelete(t, guard, "apply-mixed", "capture-first", "first")
	legacyPath := filepath.Join(t.TempDir(), "restore-legacy.json")
	if err := os.WriteFile(legacyPath, []byte("{\"runId\":\"legacy\"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	registered, err := guard.RegisterLegacyJournal(legacyPath)
	if err != nil {
		t.Fatalf("RegisterLegacyJournal() error = %v", err)
	}
	secondTarget := commitStoredDelete(t, guard, "apply-mixed", "capture-second", "second")

	runs, err := guard.ActiveStoreRuns(ctx)
	if err != nil {
		t.Fatalf("ActiveStoreRuns() error = %v", err)
	}
	if len(runs) != 1 || runs[0].ID() == "" || runs[0].RunID() != "apply-mixed" || runs[0].StartedAt().IsZero() {
		t.Fatalf("active runs = %+v", runs)
	}
	members := runs[0].Members()
	if len(members) != 3 {
		t.Fatalf("members = %+v, want three", members)
	}
	if members[0].Kind() != StoreMemberGeneration || members[0].Ordinal() != 0 || members[0].CaptureID() != "capture-first" ||
		members[1].Kind() != StoreMemberLegacy || members[1].Ordinal() != 1 || members[1].LegacyJournalPath() != legacyPath ||
		members[2].Kind() != StoreMemberGeneration || members[2].Ordinal() != 2 || members[2].CaptureID() != "capture-second" {
		t.Fatalf("ordered members = [%+v, %+v, %+v]", members[0], members[1], members[2])
	}
	if registered.Kind() != StoreMemberLegacy || registered.Ordinal() != members[1].Ordinal() {
		t.Fatalf("registered member = %+v", registered)
	}

	if _, err := guard.RevertGenerationMember(ctx, members[2]); err != nil {
		t.Fatalf("RevertGenerationMember(second) error = %v", err)
	}
	if data, err := os.ReadFile(secondTarget); err != nil || string(data) != "second" {
		t.Fatalf("second target after revert = %q, %v", data, err)
	}
	if _, err := guard.RevertGenerationMember(ctx, members[2]); !errors.Is(err, ErrStoreMemberReverted) {
		t.Fatalf("second revert error = %v, want ErrStoreMemberReverted", err)
	}
	if err := guard.MarkLegacyMemberReverted(ctx, members[1]); err != nil {
		t.Fatalf("MarkLegacyMemberReverted() error = %v", err)
	}

	runs, err = guard.ActiveStoreRuns(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 || len(runs[0].Members()) != 1 || runs[0].Members()[0].CaptureID() != "capture-first" {
		t.Fatalf("active runs after consumption = %+v", runs)
	}
	if _, err := os.Lstat(firstTarget); !os.IsNotExist(err) {
		t.Fatalf("unreverted first target should remain deleted, err=%v", err)
	}
}

func TestRegisterLegacyJournalIsIdempotentAndRevertWorkIsOneShot(t *testing.T) {
	ctx := context.Background()
	guard, err := BeginLive(ctx, t.TempDir(), "apply-legacy-idempotent", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = guard.Close() })
	journalPath := filepath.Join(t.TempDir(), "restore-legacy.json")
	if err := os.WriteFile(journalPath, []byte("{\"runId\":\"legacy\"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	first, err := guard.RegisterLegacyJournal(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	second, err := guard.RegisterLegacyJournal(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	if first.Ordinal() != second.Ordinal() || first.LegacyJournalPath() != second.LegacyJournalPath() {
		t.Fatalf("duplicate registration: first=%+v second=%+v", first, second)
	}
	runs, err := guard.ActiveStoreRuns(ctx)
	if err != nil || len(runs) != 1 || len(runs[0].Members()) != 1 {
		t.Fatalf("active runs = %+v, %v", runs, err)
	}
	firstRoot, err := guard.LegacyMemberRevertRoot(ctx, first)
	if err != nil {
		t.Fatal(err)
	}
	secondRoot, err := guard.LegacyMemberRevertRoot(ctx, second)
	if err != nil || firstRoot != secondRoot {
		t.Fatalf("revert roots = %q, %q, %v", firstRoot, secondRoot, err)
	}
	if err := guard.MarkLegacyMemberReverted(ctx, first); err != nil {
		t.Fatal(err)
	}
	if _, err := guard.LegacyMemberRevertRoot(ctx, second); !errors.Is(err, ErrStoreMemberReverted) {
		t.Fatalf("consumed revert root error = %v", err)
	}
}

func TestRevertGenerationMemberRejectsUnrelatedDriftWithoutConsumption(t *testing.T) {
	ctx := context.Background()
	guard, err := BeginLive(ctx, t.TempDir(), "apply-drift", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = guard.Close() })
	target := commitStoredDelete(t, guard, "apply-drift", "capture-drift", "before")
	if err := os.WriteFile(target, []byte("unrelated"), 0o600); err != nil {
		t.Fatal(err)
	}
	runs, err := guard.ActiveStoreRuns(ctx)
	if err != nil || len(runs) != 1 || len(runs[0].Members()) != 1 {
		t.Fatalf("ActiveStoreRuns() = %+v, %v", runs, err)
	}
	if _, err := guard.RevertGenerationMember(ctx, runs[0].Members()[0]); err == nil {
		t.Fatal("RevertGenerationMember() accepted unrelated drift")
	}
	if data, err := os.ReadFile(target); err != nil || string(data) != "unrelated" {
		t.Fatalf("drift changed = %q, %v", data, err)
	}
	active, err := guard.ActiveStoreRuns(ctx)
	if err != nil || len(active) != 1 || len(active[0].Members()) != 1 {
		t.Fatalf("member was consumed after failed revert: %+v, %v", active, err)
	}
}

func TestRevertGenerationMemberPreflightsAllActionsBeforeFirstMutation(t *testing.T) {
	ctx := context.Background()
	guard, err := BeginLive(ctx, t.TempDir(), "apply-preflight", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = guard.Close() })
	root, err := guard.CreateTransactionRoot("capture-preflight")
	if err != nil {
		t.Fatal(err)
	}
	targets := []string{filepath.Join(t.TempDir(), "first.json"), filepath.Join(t.TempDir(), "second.json")}
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
	lineage.RunID, lineage.CaptureID = "apply-preflight", "capture-preflight"
	intent, err := PersistJournalIntent(ctx, JournalIntentRequest{Prepared: prepared, TransactionRoot: root, Lineage: lineage})
	if err != nil {
		t.Fatal(err)
	}
	if result, err := ExecuteConfigSetTransaction(ctx, TransactionRequest{Prepared: prepared, Intent: intent}); err != nil || result.Status() != TransactionRestored {
		t.Fatalf("commit transaction = %+v, %v", result, err)
	}
	if err := os.WriteFile(targets[0], []byte("unrelated"), 0o600); err != nil {
		t.Fatal(err)
	}
	runs, err := guard.ActiveStoreRuns(ctx)
	if err != nil || len(runs) != 1 || len(runs[0].Members()) != 1 {
		t.Fatalf("ActiveStoreRuns() = %+v, %v", runs, err)
	}
	member := runs[0].Members()[0]
	if _, err := guard.RevertGenerationMember(ctx, member); err == nil {
		t.Fatal("RevertGenerationMember() accepted unrelated drift")
	}
	if _, err := os.Lstat(targets[1]); !os.IsNotExist(err) {
		t.Fatalf("later action was partially reverted before drift discovery, err=%v", err)
	}
	if _, err := os.Lstat(filepath.Join(root, "reverted.json")); !os.IsNotExist(err) {
		t.Fatalf("failed revert published consumption sidecar, err=%v", err)
	}
}

func TestActiveStoreRunsClassifiesRevertedLegacyBeforeReadingOldJournal(t *testing.T) {
	ctx := context.Background()
	guard, err := BeginLive(ctx, t.TempDir(), "apply-legacy", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = guard.Close() })
	journalPath := filepath.Join(t.TempDir(), "restore-legacy.json")
	if err := os.WriteFile(journalPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	member, err := guard.RegisterLegacyJournal(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := guard.MarkLegacyMemberReverted(ctx, member); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(journalPath); err != nil {
		t.Fatal(err)
	}
	runs, err := guard.ActiveStoreRuns(ctx)
	if err != nil {
		t.Fatalf("ActiveStoreRuns() read consumed legacy journal: %v", err)
	}
	if len(runs) != 0 {
		t.Fatalf("active runs = %+v, want none", runs)
	}
}

func TestActiveStoreRunsClassifiesRevertedGenerationBeforeReadingBackup(t *testing.T) {
	ctx := context.Background()
	guard, err := BeginLive(ctx, t.TempDir(), "apply-generation", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = guard.Close() })
	commitStoredDelete(t, guard, "apply-generation", "capture-generation", "before")
	runs, err := guard.ActiveStoreRuns(ctx)
	if err != nil || len(runs) != 1 || len(runs[0].Members()) != 1 {
		t.Fatalf("ActiveStoreRuns() = %+v, %v", runs, err)
	}
	member := runs[0].Members()[0]
	root := filepath.Join(guard.transactions, member.memberID)
	intent, err := ReadJournalIntent(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	backup := intent.Actions()[0].Prior.BackupPath
	if _, err := guard.RevertGenerationMember(ctx, member); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(backup); err != nil {
		t.Fatal(err)
	}
	runs, err = guard.ActiveStoreRuns(ctx)
	if err != nil {
		t.Fatalf("ActiveStoreRuns() read consumed generation backup: %v", err)
	}
	if len(runs) != 0 {
		t.Fatalf("active runs = %+v, want none", runs)
	}
}

func TestActiveStoreRunsRemovesPrePublicationStoreTempOrphan(t *testing.T) {
	ctx := context.Background()
	guard, err := BeginLive(ctx, t.TempDir(), "apply-temp-orphan", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = guard.Close() })
	tempPath := filepath.Join(guard.legacyMembers, ".store-record-123456.tmp")
	if err := os.WriteFile(tempPath, []byte("partial"), 0o600); err != nil {
		t.Fatal(err)
	}
	runs, err := guard.ActiveStoreRuns(ctx)
	if err != nil {
		t.Fatalf("ActiveStoreRuns() with pre-publication temp: %v", err)
	}
	if len(runs) != 0 {
		t.Fatalf("active runs = %+v, want none", runs)
	}
	if _, err := os.Lstat(tempPath); !os.IsNotExist(err) {
		t.Fatalf("pre-publication temp still exists, err=%v", err)
	}
}

func TestActiveStoreRunsRemovesPostPublicationStoreTempBesideValidMember(t *testing.T) {
	ctx := context.Background()
	guard, err := BeginLive(ctx, t.TempDir(), "apply-temp-published", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = guard.Close() })
	journalPath := filepath.Join(t.TempDir(), "legacy.json")
	if err := os.WriteFile(journalPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	member, err := guard.RegisterLegacyJournal(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	tempPath := filepath.Join(guard.legacyMembers, ".store-record-654321.tmp")
	memberPath := filepath.Join(guard.legacyMembers, member.memberID+".json")
	if err := os.Link(memberPath, tempPath); err != nil {
		t.Fatal(err)
	}
	runs, err := guard.ActiveStoreRuns(ctx)
	if err != nil {
		t.Fatalf("ActiveStoreRuns() with post-publication temp: %v", err)
	}
	if len(runs) != 1 || len(runs[0].Members()) != 1 || runs[0].Members()[0].memberID != member.memberID {
		t.Fatalf("active runs = %+v, want registered legacy member", runs)
	}
	if _, err := os.Lstat(tempPath); !os.IsNotExist(err) {
		t.Fatalf("post-publication temp still exists, err=%v", err)
	}
}

func TestActiveStoreRunsRejectsLinkedStoreTempFailClosed(t *testing.T) {
	ctx := context.Background()
	guard, err := BeginLive(ctx, t.TempDir(), "apply-temp-link", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = guard.Close() })
	target := filepath.Join(t.TempDir(), "unrelated")
	if err := os.WriteFile(target, []byte("untouched"), 0o600); err != nil {
		t.Fatal(err)
	}
	tempPath := filepath.Join(guard.legacyMembers, ".store-record-777777.tmp")
	if err := os.Symlink(target, tempPath); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := guard.ActiveStoreRuns(ctx); err == nil {
		t.Fatal("ActiveStoreRuns() removed or ignored linked store temp")
	}
	if info, err := os.Lstat(tempPath); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("linked temp was changed: info=%v err=%v", info, err)
	}
	if data, err := os.ReadFile(target); err != nil || string(data) != "untouched" {
		t.Fatalf("linked temp target changed = %q, %v", data, err)
	}
}

func commitStoredDelete(t *testing.T, guard *Guard, runID, captureID, prior string) string {
	t.Helper()
	ctx := context.Background()
	root, err := guard.CreateTransactionRoot(captureID)
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(target, []byte(prior), 0o600); err != nil {
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
	lineage.RunID, lineage.CaptureID = runID, captureID
	intent, err := PersistJournalIntent(ctx, JournalIntentRequest{Prepared: prepared, TransactionRoot: root, Lineage: lineage})
	if err != nil {
		t.Fatal(err)
	}
	result, err := ExecuteConfigSetTransaction(ctx, TransactionRequest{Prepared: prepared, Intent: intent})
	if err != nil || result.Status() != TransactionRestored {
		t.Fatalf("commit transaction = %+v, %v", result, err)
	}
	return target
}
