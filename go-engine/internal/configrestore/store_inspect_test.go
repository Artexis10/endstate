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
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestInspectStoreReturnsClosedImmutableGenerationRecord(t *testing.T) {
	stateDir := t.TempDir()
	guard, err := BeginLive(context.Background(), stateDir, "apply-inspection", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = guard.Close() })
	commitStoredDelete(t, guard, "apply-inspection", "capture-inspection", "before")

	root := filepath.Join(stateDir, "config-restore", "v1")
	before := snapshotInspectionTree(t, root)
	inspection, err := InspectStore(root, true)
	after := snapshotInspectionTree(t, root)
	if err != nil {
		t.Fatalf("InspectStore() error = %v", err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("InspectStore() changed the store\nbefore=%#v\nafter=%#v", before, after)
	}
	if inspection.TransactionCount() != 1 || inspection.MemberCount() != 1 {
		t.Fatalf("counts = transactions %d members %d, want 1/1", inspection.TransactionCount(), inspection.MemberCount())
	}
	runs := inspection.Runs()
	if len(runs) != 1 || runs[0].RunID != "apply-inspection" || runs[0].ID == "" || runs[0].StartedAtUTC == "" {
		t.Fatalf("runs = %#v", runs)
	}
	members := runs[0].Members()
	if len(members) != 1 {
		t.Fatalf("members = %#v", members)
	}
	member := members[0]
	if member.Kind != StoreMemberGeneration || member.ID == "" || member.Ordinal != 0 || member.CaptureID != "capture-inspection" ||
		member.MemberDigest == "" || member.IntentDigest == "" || member.TerminalDigest == "" || member.TerminalState != JournalCommitted ||
		member.Reverted || !member.HasLineage || member.Lineage.RunID != "apply-inspection" ||
		member.Lineage.CaptureID != "capture-inspection" || member.Lineage.ModuleID == "" ||
		member.LegacyJournalIdentity != "" || member.LegacyJournalDigest != "" {
		t.Fatalf("member = %#v", member)
	}
}

func TestInspectStoreRejectsUnsafeOrIncompleteStateWithoutChangingIt(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*testing.T, string, *Guard)
	}{
		{name: "pending transaction", mutate: func(t *testing.T, _ string, guard *Guard) {
			if _, err := guard.CreateTransactionRoot("capture-pending"); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "rolled back transaction", mutate: func(t *testing.T, _ string, guard *Guard) {
			root, err := guard.CreateTransactionRoot("capture-rolled-back")
			if err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(t.TempDir(), "settings.json")
			if err := os.WriteFile(target, []byte("before"), 0o600); err != nil {
				t.Fatal(err)
			}
			prepared, err := PrepareSnapshots(context.Background(), SnapshotRequest{Set: &MaterializedSet{Actions: []Action{{
				Kind: ActionDeleteFile, Strategy: "delete-glob", Target: target, SnapshotRequired: true,
			}}}, TransactionRoot: root})
			if err != nil {
				t.Fatal(err)
			}
			lineage := testJournalLineage()
			lineage.RunID, lineage.CaptureID = "apply-reject", "capture-rolled-back"
			intent, err := PersistJournalIntent(context.Background(), JournalIntentRequest{Prepared: prepared, TransactionRoot: root, Lineage: lineage})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := PersistAbortedMarker(context.Background(), intent); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "publication temp", mutate: func(t *testing.T, root string, _ *Guard) {
			if err := os.WriteFile(filepath.Join(root, "legacy-members", ".store-record-123.tmp"), []byte("partial"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "unknown transaction entry", mutate: func(t *testing.T, root string, _ *Guard) {
			if err := os.WriteFile(filepath.Join(root, "transactions", "unexpected"), []byte("unexpected"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "corrupt terminal", mutate: func(t *testing.T, root string, _ *Guard) {
			transaction := inspectionOnlyTransactionRoot(t, root)
			intent, err := ReadJournalIntent(context.Background(), transaction)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(journalMarkerPath(filepath.Join(transaction, "journal"), JournalCommitted, intent.Digest()), []byte("{}\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "foreign legacy path", mutate: func(t *testing.T, _ string, guard *Guard) {
			journal := filepath.Join(t.TempDir(), "legacy.json")
			if err := os.WriteFile(journal, []byte("{}\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := guard.RegisterLegacyJournal(journal); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			stateDir := t.TempDir()
			guard, err := BeginLive(context.Background(), stateDir, "apply-reject", nil)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = guard.Close() })
			root := filepath.Join(stateDir, "config-restore", "v1")
			if test.name == "corrupt terminal" {
				commitStoredDelete(t, guard, "apply-reject", "capture-reject", "before")
			}
			test.mutate(t, root, guard)
			before := snapshotInspectionTree(t, root)
			if _, err := InspectStore(root, true); err == nil {
				t.Fatal("InspectStore() accepted unsafe or incomplete state")
			}
			after := snapshotInspectionTree(t, root)
			if !reflect.DeepEqual(before, after) {
				t.Fatalf("failed InspectStore() changed the store\nbefore=%#v\nafter=%#v", before, after)
			}
		})
	}
}

func TestInspectStoreRejectsLinksDuplicateOrdinalsAndMismatchedRevert(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*testing.T, string)
	}{
		{name: "link", mutate: func(t *testing.T, root string) {
			target := filepath.Join(t.TempDir(), "target")
			if err := os.WriteFile(target, []byte("safe"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, filepath.Join(root, "legacy-members", "linked.json")); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
		}},
		{name: "duplicate ordinal", mutate: func(t *testing.T, root string) {
			entries, err := os.ReadDir(filepath.Join(root, "transactions"))
			if err != nil || len(entries) != 2 {
				t.Fatalf("transaction entries = %#v, %v", entries, err)
			}
			var second string
			var disk transactionDescriptorDisk
			var started time.Time
			for _, entry := range entries {
				candidate := filepath.Join(root, "transactions", entry.Name())
				candidateDisk, candidateStarted, err := readStoredTransactionDescriptor(candidate)
				if err != nil {
					t.Fatal(err)
				}
				if candidateDisk.MutationOrdinal == 1 {
					second, disk, started = candidate, candidateDisk, candidateStarted
					break
				}
			}
			if second == "" {
				t.Fatal("second transaction was not found")
			}
			_, encoded, err := newTransactionDescriptor(disk.TransactionID, disk.RestoreRunID, disk.RunID, started, 0, disk.CaptureID)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(second, "transaction.json"), encoded, 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "mismatched reverted marker", mutate: func(t *testing.T, root string) {
			transaction := inspectionOnlyTransactionRoot(t, root)
			intent, err := ReadJournalIntent(context.Background(), transaction)
			if err != nil {
				t.Fatal(err)
			}
			_, encoded, err := newMemberRevert(StoreMemberGeneration, filepath.Base(transaction), strings.Repeat("0", 64))
			if err != nil {
				t.Fatal(err)
			}
			if intent.Digest() == strings.Repeat("0", 64) {
				t.Fatal("test digest collision")
			}
			if err := os.WriteFile(filepath.Join(transaction, "reverted.json"), encoded, 0o600); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			stateDir := t.TempDir()
			guard, err := BeginLive(context.Background(), stateDir, "apply-hostile", nil)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = guard.Close() })
			commitStoredDelete(t, guard, "apply-hostile", "capture-one", "before")
			if test.name == "duplicate ordinal" {
				commitStoredDelete(t, guard, "apply-hostile", "capture-two", "before")
			}
			root := filepath.Join(stateDir, "config-restore", "v1")
			test.mutate(t, root)
			before := snapshotInspectionTree(t, root)
			if _, err := InspectStore(root, true); err == nil {
				t.Fatal("InspectStore() accepted hostile store state")
			}
			after := snapshotInspectionTree(t, root)
			if !reflect.DeepEqual(before, after) {
				t.Fatalf("failed InspectStore() changed the store\nbefore=%#v\nafter=%#v", before, after)
			}
		})
	}
}

func TestInspectStoreFailsWhenStoreChangesBetweenScansAndDefensivelyCopies(t *testing.T) {
	stateDir := t.TempDir()
	guard, err := BeginLive(context.Background(), stateDir, "apply-race", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = guard.Close() })
	commitStoredDelete(t, guard, "apply-race", "capture-race", "before")
	root := filepath.Join(stateDir, "config-restore", "v1")
	transaction := inspectionOnlyTransactionRoot(t, root)
	descriptor := filepath.Join(transaction, "transaction.json")

	storeInspectionAfterFirstScan = func() {
		now := time.Now().Add(time.Second)
		if err := os.Chtimes(descriptor, now, now); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { storeInspectionAfterFirstScan = nil })
	before := snapshotInspectionTree(t, root)
	if _, err := InspectStore(root, true); err == nil {
		t.Fatal("InspectStore() accepted a mid-scan change")
	}
	after := snapshotInspectionTree(t, root)
	if reflect.DeepEqual(before, after) {
		t.Fatal("test seam did not change store metadata")
	}

	storeInspectionAfterFirstScan = nil
	inspection, err := InspectStore(root, true)
	if err != nil {
		t.Fatal(err)
	}
	runs := inspection.Runs()
	runs[0].RunID = "mutated"
	members := runs[0].Members()
	members[0].ID = "mutated"
	if fresh := inspection.Runs(); fresh[0].RunID != "apply-race" || fresh[0].Members()[0].ID == "mutated" {
		t.Fatalf("inspection returned mutable internal records: %#v", fresh)
	}
}

func inspectionOnlyTransactionRoot(t *testing.T, root string) string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(root, "transactions"))
	if err != nil || len(entries) == 0 {
		t.Fatalf("transaction entries = %#v, %v", entries, err)
	}
	return filepath.Join(root, "transactions", entries[0].Name())
}

func TestInspectStoreRejectsMissingRootAndLeaseWithoutWriting(t *testing.T) {
	root := filepath.Join(t.TempDir(), "missing", "config-restore", "v1")
	if _, err := InspectStore(root, true); err == nil {
		t.Fatal("InspectStore() accepted a nonexistent root")
	}
	if _, err := os.Lstat(root); !os.IsNotExist(err) {
		t.Fatalf("InspectStore() created missing root: %v", err)
	}

	stateDir := t.TempDir()
	guard, err := BeginLive(context.Background(), stateDir, "apply-no-lease", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = guard.Close() })
	root = filepath.Join(stateDir, "config-restore", "v1")
	before := snapshotInspectionTree(t, root)
	if _, err := InspectStore(root, false); err == nil {
		t.Fatal("InspectStore() accepted inspection without exclusive lease")
	}
	after := snapshotInspectionTree(t, root)
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("failed InspectStore() changed the store\nbefore=%#v\nafter=%#v", before, after)
	}
}

func TestInspectStoreVerifiesAbsoluteAndProjectedLegacyJournalBytes(t *testing.T) {
	journalBytes := []byte(`{"runId":"apply-legacy","timestamp":"2026-01-01T00:00:00Z","manifestPath":"manifest","manifestDir":"manifest","entries":[{"resolvedSourcePath":"source","targetPath":"target","action":"restored"}]}`)
	for _, test := range []struct {
		name      string
		projected bool
	}{{"absolute", false}, {"projected", true}} {
		t.Run(test.name, func(t *testing.T) {
			authority := t.TempDir()
			stateDir := filepath.Join(authority, "state")
			journalPath := filepath.Join(authority, "logs", "restore-journal.json")
			if err := os.MkdirAll(filepath.Dir(journalPath), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(journalPath, journalBytes, 0o600); err != nil {
				t.Fatal(err)
			}
			var guard *Guard
			var boundary HostBoundary
			var err error
			if test.projected {
				boundary = &recordingHostBoundary{root: authority, resolved: map[string]string{}, projectRootToken: "$ENDSTATE_ROOT/"}
				guard, err = BeginLiveWithBoundary(context.Background(), stateDir, "apply-legacy", nil, boundary)
			} else {
				guard, err = BeginLive(context.Background(), stateDir, "apply-legacy", nil)
			}
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = guard.Close() })
			if _, err := guard.RegisterLegacyJournal(journalPath); err != nil {
				t.Fatal(err)
			}
			root := filepath.Join(stateDir, "config-restore", "v1")
			before := snapshotInspectionTree(t, root)
			if test.projected {
				if _, err := InspectStore(root, true); err == nil {
					t.Fatal("unresolved projected journal was accepted")
				}
			} else if _, err := InspectStore(root, true); err == nil {
				t.Fatal("absolute legacy journal was accepted without an authorized boundary")
			}
			if !test.projected {
				boundary = &recordingHostBoundary{root: authority, resolved: map[string]string{}}
			}
			inspection, err := InspectStoreWithBoundary(root, true, boundary)
			after := snapshotInspectionTree(t, root)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, after) {
				t.Fatal("legacy inspection changed the store")
			}
			identity := inspection.Runs()[0].Members()[0].LegacyJournalIdentity
			if test.projected {
				if !strings.HasPrefix(identity, "$ENDSTATE_ROOT/") {
					t.Fatalf("projected identity = %q", identity)
				}
			} else if !strings.HasPrefix(identity, "sha256:") || strings.Contains(identity, authority) {
				t.Fatalf("absolute identity leaked: %q", identity)
			}
			if err := os.WriteFile(journalPath, append(journalBytes, ' '), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := InspectStoreWithBoundary(root, true, boundary); err == nil {
				t.Fatal("tampered registered journal was accepted")
			}
		})
	}
}

func TestInspectStoreRetainsLegacyJournalActions(t *testing.T) {
	authority := t.TempDir()
	stateDir := filepath.Join(authority, "state")
	journal := filepath.Join(authority, "logs", "restore-journal.json")
	if err := os.MkdirAll(filepath.Dir(journal), 0o700); err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(authority, "state", "backups", "apply-legacy-actions", "backup-one")
	if err := os.MkdirAll(filepath.Dir(backup), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(backup, []byte("prior"), 0o600); err != nil {
		t.Fatal(err)
	}
	journalBytes := []byte(`{"runId":"apply-legacy-actions","timestamp":"2026-01-01T00:00:00Z","manifestPath":"manifest","manifestDir":"manifest","entries":[{"resolvedSourcePath":"source-one","targetPath":"target-one","backupRequested":true,"backupCreated":true,"backupPath":"$ENDSTATE_ROOT/state/backups/apply-legacy-actions/backup-one","action":"restored"},{"resolvedSourcePath":"source-two","targetPath":"target-two","action":"skipped"}]}`)
	if err := os.WriteFile(journal, journalBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	boundary := &recordingHostBoundary{root: authority, resolved: map[string]string{}, projectRootToken: "$ENDSTATE_ROOT/"}
	guard, err := BeginLiveWithBoundary(context.Background(), stateDir, "apply-legacy-actions", nil, boundary)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = guard.Close() })
	if _, err := guard.RegisterLegacyJournal(journal); err != nil {
		t.Fatal(err)
	}
	spy := &auditingInspectionFS{storeInspectionFS: osStoreInspectionFS{}}
	storeInspectionFilesystem = spy
	t.Cleanup(func() { storeInspectionFilesystem = osStoreInspectionFS{} })
	inspection, err := InspectStoreWithBoundary(filepath.Join(stateDir, "config-restore", "v1"), true, boundary)
	if err != nil {
		t.Fatal(err)
	}
	actions := inspection.Runs()[0].Members()[0].Actions()
	if len(actions) != 2 || actions[0].Index != 0 || actions[1].Index != 1 || actions[0].TargetIdentity == "" || actions[0].SourceIdentity == "" || actions[0].TargetIdentity == actions[1].TargetIdentity || !actions[0].Backup.Exists || actions[0].Backup.Digest == "" || actions[0].Backup.Kind != StateFile {
		t.Fatalf("legacy actions = %#v", actions)
	}
	journalOpens := 0
	for _, path := range spy.opened {
		if path == journal {
			journalOpens++
		}
	}
	if journalOpens != 1 {
		t.Fatalf("legacy journal opens = %d, want 1", journalOpens)
	}
	if err := os.Remove(backup); err != nil {
		t.Fatal(err)
	}
	if _, err := InspectStoreWithBoundary(filepath.Join(stateDir, "config-restore", "v1"), true, boundary); err == nil {
		t.Fatal("missing legacy backup was accepted")
	}
	linkedTarget := filepath.Join(authority, "state", "backups", "apply-legacy-actions", "linked-target")
	if err := os.WriteFile(linkedTarget, []byte("replacement"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(linkedTarget, backup); err == nil {
		if _, err := InspectStoreWithBoundary(filepath.Join(stateDir, "config-restore", "v1"), true, boundary); err == nil {
			t.Fatal("linked legacy backup was accepted")
		}
	}
}

func TestInspectStoreRejectsInvalidLegacyActionEvidence(t *testing.T) {
	for _, test := range []struct {
		name   string
		entry  string
		backup bool
	}{
		{name: "missing created backup", entry: `{"resolvedSourcePath":"source","targetPath":"target","backupRequested":true,"backupCreated":true,"backupPath":"$ENDSTATE_ROOT/state/backups/apply-legacy-invalid/backup","action":"restored"}`},
		{name: "backup path without creation", entry: `{"resolvedSourcePath":"source","targetPath":"target","backupCreated":false,"backupPath":"$ENDSTATE_ROOT/state/backups/apply-legacy-invalid/backup","action":"restored"}`, backup: true},
		{name: "unknown action", entry: `{"resolvedSourcePath":"source","targetPath":"target","action":"../../invented"}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			authority := t.TempDir()
			stateDir := filepath.Join(authority, "state")
			journal := filepath.Join(authority, "logs", "restore-journal.json")
			if err := os.MkdirAll(filepath.Dir(journal), 0o700); err != nil {
				t.Fatal(err)
			}
			if test.backup {
				backup := filepath.Join(authority, "state", "backups", "apply-legacy-invalid", "backup")
				if err := os.MkdirAll(filepath.Dir(backup), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(backup, []byte("prior"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			journalBytes := []byte(`{"runId":"apply-legacy-invalid","timestamp":"2026-01-01T00:00:00Z","manifestPath":"manifest","manifestDir":"manifest","entries":[` + test.entry + `]}`)
			if err := os.WriteFile(journal, journalBytes, 0o600); err != nil {
				t.Fatal(err)
			}
			boundary := &recordingHostBoundary{root: authority, resolved: map[string]string{}, projectRootToken: "$ENDSTATE_ROOT/"}
			guard, err := BeginLiveWithBoundary(context.Background(), stateDir, "apply-legacy-invalid", nil, boundary)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = guard.Close() })
			if _, err := guard.RegisterLegacyJournal(journal); err != nil {
				t.Fatal(err)
			}
			if _, err := InspectStoreWithBoundary(guard.storeRoot, true, boundary); err == nil {
				t.Fatal("invalid legacy action evidence was accepted")
			}
		})
	}
}

func TestInspectLegacyJournalsEnforceAggregateBudgets(t *testing.T) {
	writeJournal := func(t *testing.T, root, runID string, actionCount int) (string, string, int64) {
		t.Helper()
		entry := `{"resolvedSourcePath":"source","targetPath":"target","action":"restored"}`
		data := []byte(`{"runId":"` + runID + `","timestamp":"2026-01-01T00:00:00Z","manifestPath":"manifest","manifestDir":"manifest","entries":[` + strings.TrimSuffix(strings.Repeat(entry+`,`, actionCount), `,`) + `]}`)
		path := filepath.Join(root, runID+".json")
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(data)
		return path, hex.EncodeToString(digest[:]), int64(len(data))
	}

	t.Run("journal count", func(t *testing.T) {
		root := t.TempDir()
		budget := legacyInspectionBudget{}
		for index := 0; index <= maxStoreInspectionJournals; index++ {
			path, digest, _ := writeJournal(t, root, fmt.Sprintf("journal-%03d", index), 1)
			_, err := inspectLegacyJournal(storeInspectionFilesystem, path, digest, &budget)
			if index < maxStoreInspectionJournals && err != nil {
				t.Fatalf("journal %d: %v", index, err)
			}
			if index == maxStoreInspectionJournals && err == nil {
				t.Fatal("aggregate legacy journal count was accepted")
			}
		}
	})

	t.Run("action entries", func(t *testing.T) {
		root := t.TempDir()
		budget := legacyInspectionBudget{}
		for index := 0; ; index++ {
			path, digest, _ := writeJournal(t, root, fmt.Sprintf("actions-%03d", index), 17)
			_, err := inspectLegacyJournal(storeInspectionFilesystem, path, digest, &budget)
			if err != nil {
				break
			}
		}
		if budget.entries != maxStoreInspectionEntries-16 {
			t.Fatalf("retained legacy entries = %d, want %d", budget.entries, maxStoreInspectionEntries-16)
		}
	})

	t.Run("journal bytes", func(t *testing.T) {
		root := t.TempDir()
		path, digest, size := writeJournal(t, root, "bytes", 1)
		budget := legacyInspectionBudget{bytes: maxStoreInspectionBytes - size + 1}
		if _, err := inspectLegacyJournal(storeInspectionFilesystem, path, digest, &budget); err == nil {
			t.Fatal("aggregate legacy journal bytes were accepted")
		}
	})
}

func TestInspectStoreRejectsForeignAbsoluteLegacyJournalBeforeRead(t *testing.T) {
	authority := t.TempDir()
	foreign := filepath.Join(t.TempDir(), "restore-journal.json")
	journalBytes := []byte(`{"runId":"apply-foreign","timestamp":"2026-01-01T00:00:00Z","manifestPath":"manifest","manifestDir":"manifest","entries":[{"resolvedSourcePath":"source","targetPath":"target","action":"restored"}]}`)
	if err := os.WriteFile(foreign, journalBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	boundary := &recordingHostBoundary{root: authority, resolved: map[string]string{}}
	guard, err := BeginLiveWithBoundary(context.Background(), filepath.Join(authority, "state"), "apply-foreign", nil, boundary)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = guard.Close() })
	read, err := readInspectionBoundedFile(storeInspectionFilesystem, foreign)
	if err != nil {
		t.Fatal(err)
	}
	memberID, err := newOpaqueStoreID()
	if err != nil {
		t.Fatal(err)
	}
	_, record, err := newLegacyMember(memberID, guard.restoreRunID, guard.runID, guard.runStartedAt, 0, foreign, read.digest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(guard.legacyMembers, memberID+".json"), record, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := InspectStoreWithBoundary(guard.storeRoot, true, boundary); err == nil {
		t.Fatal("foreign absolute legacy journal was accepted")
	}
	validated := false
	for _, path := range boundary.validateCalls {
		if path == foreign {
			validated = true
			break
		}
	}
	if !validated {
		t.Fatal("foreign legacy journal was read before the boundary validated it")
	}
}

func TestInspectStoreRejectsLinkedAncestorBeforeEmptyStoreSuccess(t *testing.T) {
	stateDir := t.TempDir()
	guard, err := BeginLive(context.Background(), stateDir, "apply-ancestor", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = guard.Close() })
	link := filepath.Join(t.TempDir(), "linked-state")
	if err := os.Symlink(stateDir, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := InspectStore(filepath.Join(link, "config-restore", "v1"), true); err == nil {
		t.Fatal("linked store ancestor was accepted")
	}
}

func TestInspectStoreReturnsOneToOneActionAndBackupRecords(t *testing.T) {
	stateDir := t.TempDir()
	guard, err := BeginLive(context.Background(), stateDir, "apply-actions", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = guard.Close() })
	root, err := guard.CreateTransactionRoot("capture-actions")
	if err != nil {
		t.Fatal(err)
	}
	targets := []string{filepath.Join(t.TempDir(), "one.json"), filepath.Join(t.TempDir(), "two.json")}
	for _, target := range targets {
		if err := os.WriteFile(target, []byte("before"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	prepared, err := PrepareSnapshots(context.Background(), SnapshotRequest{Set: &MaterializedSet{Actions: []Action{
		{Kind: ActionDeleteFile, Strategy: "delete-glob", Target: targets[0], SnapshotRequired: true},
		{Kind: ActionDeleteFile, Strategy: "delete-glob", Target: targets[1], SnapshotRequired: true},
	}}, TransactionRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	lineage := testJournalLineage()
	lineage.RunID, lineage.CaptureID = "apply-actions", "capture-actions"
	intent, err := PersistJournalIntent(context.Background(), JournalIntentRequest{Prepared: prepared, TransactionRoot: root, Lineage: lineage})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ExecuteConfigSetTransaction(context.Background(), TransactionRequest{Prepared: prepared, Intent: intent}); err != nil {
		t.Fatal(err)
	}
	storeRoot := filepath.Join(stateDir, "config-restore", "v1")
	inspection, err := InspectStore(storeRoot, true)
	if err != nil {
		t.Fatal(err)
	}
	actions := inspection.Runs()[0].Members()[0].Actions()
	if len(actions) != 2 || actions[0].Index != 0 || actions[1].Index != 1 || actions[0].TargetIdentity == "" ||
		actions[0].TargetIdentity == targets[0] || !actions[0].Backup.Exists || actions[0].Backup.Digest != actions[0].PriorDigest {
		t.Fatalf("action inspections = %#v", actions)
	}
	if err := os.Remove(filepath.Join(root, "snapshots", "000000", "prior")); err != nil {
		t.Fatal(err)
	}
	if _, err := InspectStore(storeRoot, true); err == nil {
		t.Fatal("missing physical backup was accepted")
	}
}

func TestInspectStoreVerifiesBoundedDirectoryBackup(t *testing.T) {
	stateDir := t.TempDir()
	guard, err := BeginLive(context.Background(), stateDir, "apply-directory", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = guard.Close() })
	transaction, err := guard.CreateTransactionRoot("capture-directory")
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "prefs")
	if err := os.MkdirAll(filepath.Join(target, "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "nested", "settings.json"), []byte("before"), 0o600); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(t.TempDir(), "source")
	if err := os.MkdirAll(source, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "settings.json"), []byte("desired"), 0o600); err != nil {
		t.Fatal(err)
	}
	sourceInfo, err := os.Lstat(source)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := PrepareSnapshots(context.Background(), SnapshotRequest{Set: &MaterializedSet{Actions: []Action{{Kind: ActionCopy, Strategy: "copy", Source: source, Target: target, SourceMode: sourceInfo.Mode(), SourceIsDirectory: true, SnapshotRequired: true}}}, TransactionRoot: transaction})
	if err != nil {
		t.Fatal(err)
	}
	lineage := testJournalLineage()
	lineage.RunID, lineage.CaptureID = "apply-directory", "capture-directory"
	intent, err := PersistJournalIntent(context.Background(), JournalIntentRequest{Prepared: prepared, TransactionRoot: transaction, Lineage: lineage})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ExecuteConfigSetTransaction(context.Background(), TransactionRequest{Prepared: prepared, Intent: intent}); err != nil {
		t.Fatal(err)
	}
	storeRoot := filepath.Join(stateDir, "config-restore", "v1")
	inspection, err := InspectStore(storeRoot, true)
	if err != nil {
		t.Fatal(err)
	}
	backup := inspection.Runs()[0].Members()[0].Actions()[0].Backup
	if !backup.Exists || backup.Kind != StateDirectory || backup.Digest == "" {
		t.Fatalf("directory backup = %#v", backup)
	}
	backupRoot := filepath.Join(transaction, "snapshots", "000000", "prior")
	if err := os.WriteFile(filepath.Join(backupRoot, "extra"), []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := InspectStore(storeRoot, true); err == nil {
		t.Fatal("extra directory backup file was accepted")
	}
	if err := os.Remove(filepath.Join(backupRoot, "extra")); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(backupRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(backupRoot)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() == 0o755 {
		if _, err := InspectStore(storeRoot, true); err == nil {
			t.Fatal("directory backup mode change was accepted")
		}
	}
	if err := os.Chmod(backupRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(backupRoot, "linked")
	if err := os.Symlink(filepath.Join(backupRoot, "nested", "settings.json"), link); err == nil {
		if _, err := InspectStore(storeRoot, true); err == nil {
			t.Fatal("linked directory backup child was accepted")
		}
		if err := os.Remove(link); err != nil {
			t.Fatal(err)
		}
	}
	deep := backupRoot
	for range maxStoreInspectionDepth + 1 {
		deep = filepath.Join(deep, "deep")
		if err := os.Mkdir(deep, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := InspectStore(storeRoot, true); err == nil {
		t.Fatal("too-deep directory backup was accepted")
	}
}

func TestInspectStoreRejectsTamperedOrReorderedJournalActions(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func([]JournalAction)
	}{
		{"swapped", func(actions []JournalAction) { actions[0], actions[1] = actions[1], actions[0] }},
		{"prior digest", func(actions []JournalAction) { actions[0].Prior.Digest = strings.Repeat("0", 64) }},
		{"desired digest", func(actions []JournalAction) { actions[0].Desired.Digest = strings.Repeat("0", 64) }},
		{"foreign target", func(actions []JournalAction) { actions[0].Target = "../escape" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			stateDir := t.TempDir()
			guard, err := BeginLive(context.Background(), stateDir, "apply-tamper", nil)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = guard.Close() })
			storeRoot := filepath.Join(stateDir, "config-restore", "v1")
			transaction, err := guard.CreateTransactionRoot("capture-tamper")
			if err != nil {
				t.Fatal(err)
			}
			targets := []string{filepath.Join(t.TempDir(), "one.json"), filepath.Join(t.TempDir(), "two.json")}
			for _, target := range targets {
				if err := os.WriteFile(target, []byte("before"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			prepared, err := PrepareSnapshots(context.Background(), SnapshotRequest{Set: &MaterializedSet{Actions: []Action{
				{Kind: ActionDeleteFile, Strategy: "delete-glob", Target: targets[0], SnapshotRequired: true},
				{Kind: ActionDeleteFile, Strategy: "delete-glob", Target: targets[1], SnapshotRequired: true},
			}}, TransactionRoot: transaction})
			if err != nil {
				t.Fatal(err)
			}
			lineage := testJournalLineage()
			lineage.RunID, lineage.CaptureID = "apply-tamper", "capture-tamper"
			intent, err := PersistJournalIntent(context.Background(), JournalIntentRequest{Prepared: prepared, TransactionRoot: transaction, Lineage: lineage})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := ExecuteConfigSetTransaction(context.Background(), TransactionRequest{Prepared: prepared, Intent: intent}); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(filepath.Join(transaction, "journal", "intent.json"))
			if err != nil {
				t.Fatal(err)
			}
			disk, err := decodeJournalIntent(data)
			if err != nil {
				t.Fatal(err)
			}
			test.mutate(disk.Actions)
			_, encoded, err := newJournalIntentDisk(disk.Lineage, disk.Actions, disk.Validations)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(transaction, "journal", "intent.json"), encoded, 0o600); err != nil {
				t.Fatal(err)
			}
			before := snapshotInspectionTree(t, storeRoot)
			if _, err := InspectStore(storeRoot, true); err == nil {
				t.Fatal("tampered journal actions were accepted")
			}
			if after := snapshotInspectionTree(t, storeRoot); !reflect.DeepEqual(before, after) {
				t.Fatal("failed inspection changed tampered store")
			}
		})
	}
}

type auditingInspectionFS struct {
	storeInspectionFS
	operations []string
	opened     []string
}

func (fs *auditingInspectionFS) Lstat(path string) (os.FileInfo, error) {
	fs.operations = append(fs.operations, "lstat")
	return fs.storeInspectionFS.Lstat(path)
}
func (fs *auditingInspectionFS) ReadDir(path string) ([]os.DirEntry, error) {
	fs.operations = append(fs.operations, "readdir")
	return fs.storeInspectionFS.ReadDir(path)
}
func (fs *auditingInspectionFS) Open(path string) (storeInspectionFile, error) {
	fs.operations = append(fs.operations, "open")
	fs.opened = append(fs.opened, path)
	return fs.storeInspectionFS.Open(path)
}

func TestInspectStoreUsesOnlyReadOnlyFilesystemSurface(t *testing.T) {
	stateDir := t.TempDir()
	guard, err := BeginLive(context.Background(), stateDir, "apply-audit", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = guard.Close() })
	commitStoredDelete(t, guard, "apply-audit", "capture-audit", "before")
	spy := &auditingInspectionFS{storeInspectionFS: osStoreInspectionFS{}}
	storeInspectionFilesystem = spy
	t.Cleanup(func() { storeInspectionFilesystem = osStoreInspectionFS{} })
	if _, err := InspectStore(filepath.Join(stateDir, "config-restore", "v1"), true); err != nil {
		t.Fatal(err)
	}
	if len(spy.operations) == 0 {
		t.Fatal("inspection did not use audited read-only surface")
	}
	if err := os.WriteFile(filepath.Join(stateDir, "config-restore", "v1", "transactions", "unknown"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	operations := len(spy.operations)
	if _, err := InspectStore(filepath.Join(stateDir, "config-restore", "v1"), true); err == nil || len(spy.operations) == operations {
		t.Fatal("failed inspection did not remain on audited read-only surface")
	}
	source, err := os.ReadFile("store_inspect.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"os.WriteFile(", "os.Create(", "os.Mkdir(", "os.Remove(", "os.Rename(", "os.Chtimes(", "os.Chmod("} {
		if strings.Contains(string(source), forbidden) {
			t.Fatalf("inspector source contains mutator %q", forbidden)
		}
	}
}

type inspectionTreeEntry struct {
	mode    os.FileMode
	modTime int64
	bytes   []byte
}

func snapshotInspectionTree(t *testing.T, root string) map[string]inspectionTreeEntry {
	t.Helper()
	var previous map[string]inspectionTreeEntry
	for attempt := 0; attempt < 10; attempt++ {
		current := snapshotInspectionTreeOnce(t, root)
		if previous != nil && reflect.DeepEqual(previous, current) {
			return current
		}
		previous = current
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("inspection tree metadata did not settle: %#v", previous)
	return nil
}

func snapshotInspectionTreeOnce(t *testing.T, root string) map[string]inspectionTreeEntry {
	t.Helper()
	entries := map[string]inspectionTreeEntry{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		record := inspectionTreeEntry{mode: info.Mode(), modTime: info.ModTime().UnixNano()}
		if info.Mode().IsRegular() {
			record.bytes, err = os.ReadFile(path)
			if err != nil {
				return err
			}
		}
		entries[filepath.ToSlash(relative)] = record
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return entries
}
