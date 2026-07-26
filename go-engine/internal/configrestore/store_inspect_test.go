// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package configrestore

import (
	"context"
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
