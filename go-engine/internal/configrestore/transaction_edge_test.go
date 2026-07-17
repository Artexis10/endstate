// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package configrestore

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEqualJournalActionsTreatsNilAndEmptyMissingParentsEqually(t *testing.T) {
	fixture := prepareFileTransactionFixture(t, true)
	left := fixture.intent.Actions()
	right := fixture.intent.Actions()
	left[0].MissingParents = nil
	right[0].MissingParents = []string{}
	if !equalJournalActions(left, right) {
		t.Fatal("nil and empty missing-parent lists changed prepared/intent identity")
	}
}

func TestValidateJournalActionsRejectsUnsafeOrDuplicateMissingParentOwnership(t *testing.T) {
	t.Run("control characters", func(t *testing.T) {
		transactionRoot := t.TempDir()
		parent := filepath.Join(t.TempDir(), "bad\nparent")
		action := JournalAction{Target: filepath.Join(parent, "settings.json"), MissingParents: []string{parent}}
		if err := validateJournalMissingParents(transactionRoot, action); err == nil ||
			!strings.Contains(err.Error(), "control") {
			t.Fatalf("validateJournalMissingParents() error = %v", err)
		}
	})

	t.Run("cross-action duplicate", func(t *testing.T) {
		fixture := prepareFileTransactionFixture(t, true)
		actions := fixture.intent.Actions()
		shared := filepath.Dir(actions[0].Target)
		actions[0].MissingParents = []string{shared}
		actions[1].MissingParents = []string{shared}
		if err := validateJournalActions(fixture.transactionRoot, actions); err == nil ||
			!strings.Contains(err.Error(), "duplicates missing parent") {
			t.Fatalf("validateJournalActions() error = %v", err)
		}
	})
}

func TestExecuteConfigSetTransactionRollsBackOwnedMissingParents(t *testing.T) {
	transactionRoot := t.TempDir()
	hostRoot := t.TempDir()
	ownedRoot := filepath.Join(hostRoot, "created-by-transaction")
	nested := filepath.Join(ownedRoot, "nested")
	firstTarget := filepath.Join(ownedRoot, "first.json")
	secondTarget := filepath.Join(nested, "second.json")
	prepared, err := PrepareSnapshots(context.Background(), SnapshotRequest{
		Set: &MaterializedSet{Actions: []Action{
			{
				Kind: ActionWriteFile, Strategy: "merge-json", Target: firstTarget,
				DesiredContent: []byte(`{"first":true}`), SnapshotRequired: true,
			},
			{
				Kind: ActionWriteFile, Strategy: "merge-json", Target: secondTarget,
				DesiredContent: []byte(`{"second":true}`), SnapshotRequired: true,
			},
		}},
		TransactionRoot: transactionRoot,
	})
	if err != nil {
		t.Fatal(err)
	}
	actions := prepared.Actions()
	if got := actions[0].MissingParents(); len(got) != 1 || got[0] != ownedRoot {
		t.Fatalf("first action owns %#v, want [%q]", got, ownedRoot)
	}
	if got := actions[1].MissingParents(); len(got) != 1 || got[0] != nested {
		t.Fatalf("second action owns %#v, want [%q]", got, nested)
	}
	intent, err := PersistJournalIntent(context.Background(), JournalIntentRequest{
		Prepared: prepared, TransactionRoot: transactionRoot, Lineage: testJournalLineage(),
	})
	if err != nil {
		t.Fatal(err)
	}
	primary := errors.New("fail after all target mutations")
	executor := NewTransactionExecutor()
	executor.checkpoint = func(_ context.Context, phase transactionPhase, _ int, _ string) error {
		if phase == transactionPhaseBeforeCommittedMarker {
			return primary
		}
		return nil
	}
	result, err := executor.Execute(context.Background(), TransactionRequest{Prepared: prepared, Intent: intent})
	assertPrimaryCause(t, result, err, primary)
	if result.Status() != TransactionRolledBack || result.FailStop() {
		t.Fatalf("result status=%q failStop=%v rollback=%v", result.Status(), result.FailStop(), result.RollbackError())
	}
	if _, err := os.Lstat(ownedRoot); !os.IsNotExist(err) {
		t.Fatalf("owned parent survived rollback: %v", err)
	}
}

func TestExecuteConfigSetTransactionRollsBackRootAndNestedFileDirectoryTransitions(t *testing.T) {
	tests := []struct {
		name              string
		sourceIsDirectory bool
		setup             func(*testing.T, string, string)
	}{
		{
			name: "root file to directory", sourceIsDirectory: true,
			setup: func(t *testing.T, source, target string) {
				writeTestFile(t, target, "prior root file")
				writeTestFile(t, filepath.Join(source, "child.txt"), "desired child")
			},
		},
		{
			name: "root directory to file",
			setup: func(t *testing.T, source, target string) {
				writeTestFile(t, filepath.Join(target, "old.txt"), "prior child")
				writeTestFile(t, source, "desired root file")
			},
		},
		{
			name: "nested file to directory", sourceIsDirectory: true,
			setup: func(t *testing.T, source, target string) {
				writeTestFile(t, filepath.Join(target, "node"), "prior nested file")
				writeTestFile(t, filepath.Join(target, "untouched.txt"), "keep")
				writeTestFile(t, filepath.Join(source, "node", "child.txt"), "desired nested child")
			},
		},
		{
			name: "nested directory to file", sourceIsDirectory: true,
			setup: func(t *testing.T, source, target string) {
				writeTestFile(t, filepath.Join(target, "node", "old.txt"), "prior nested child")
				writeTestFile(t, filepath.Join(target, "untouched.txt"), "keep")
				writeTestFile(t, filepath.Join(source, "node"), "desired nested file")
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transactionRoot := t.TempDir()
			stageRoot := t.TempDir()
			hostRoot := t.TempDir()
			source := filepath.Join(stageRoot, "source")
			target := filepath.Join(hostRoot, "target")
			tt.setup(t, source, target)
			prior, err := scanFilesystemState(context.Background(), target)
			if err != nil {
				t.Fatal(err)
			}
			prepared, err := PrepareSnapshots(context.Background(), SnapshotRequest{
				Set: &MaterializedSet{Actions: []Action{{
					Kind: ActionCopy, Strategy: "copy", Source: source, Target: target,
					SourceIsDirectory: tt.sourceIsDirectory, SnapshotRequired: true,
				}}},
				TransactionRoot: transactionRoot,
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
			primary := errors.New("force rollback after transition")
			executor := NewTransactionExecutor()
			executor.checkpoint = func(_ context.Context, phase transactionPhase, _ int, _ string) error {
				if phase == transactionPhaseBeforeCommittedMarker {
					return primary
				}
				return nil
			}
			result, err := executor.Execute(context.Background(), TransactionRequest{Prepared: prepared, Intent: intent})
			assertPrimaryCause(t, result, err, primary)
			if result.Status() != TransactionRolledBack || result.FailStop() {
				t.Fatalf("result status=%q failStop=%v rollback=%v", result.Status(), result.FailStop(), result.RollbackError())
			}
			after, err := scanFilesystemState(context.Background(), target)
			if err != nil {
				t.Fatal(err)
			}
			if !statesEqual(after, prior) {
				t.Fatalf("rolled-back state digest=%s kind=%q, want digest=%s kind=%q", after.Digest, after.Kind, prior.Digest, prior.Kind)
			}
		})
	}
}

func TestRollbackAcceptsOnlyDeterministicDirectoryCommitPrefixes(t *testing.T) {
	t.Run("valid prefix restores", func(t *testing.T) {
		transactionRoot := t.TempDir()
		source := filepath.Join(t.TempDir(), "source")
		target := filepath.Join(t.TempDir(), "target")
		writeTestFile(t, filepath.Join(target, "a-change.txt"), "prior")
		writeTestFile(t, filepath.Join(target, "untouched.txt"), "keep")
		writeTestFile(t, filepath.Join(source, "a-change.txt"), "desired")
		writeTestFile(t, filepath.Join(source, "z-added.txt"), "added")
		prepared, err := PrepareSnapshots(context.Background(), SnapshotRequest{
			Set: &MaterializedSet{Actions: []Action{{
				Kind: ActionCopy, Strategy: "copy", Source: source, Target: target,
				SourceIsDirectory: true, SnapshotRequired: true,
			}}},
			TransactionRoot: transactionRoot,
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
		writeTestFile(t, filepath.Join(target, "a-change.txt"), "desired")
		if err := rollbackTransactionAction(context.Background(), intent.Actions()[0], nil); err != nil {
			t.Fatalf("rollback valid commit prefix: %v", err)
		}
		assertTestFile(t, filepath.Join(target, "a-change.txt"), "prior")
		assertTestFile(t, filepath.Join(target, "untouched.txt"), "keep")
		if _, err := os.Lstat(filepath.Join(target, "z-added.txt")); !os.IsNotExist(err) {
			t.Fatalf("uncommitted later path exists: %v", err)
		}
	})

	t.Run("unrelated missing prior file fails stop without overwrite", func(t *testing.T) {
		transactionRoot := t.TempDir()
		source := filepath.Join(t.TempDir(), "source")
		target := filepath.Join(t.TempDir(), "target")
		changed := filepath.Join(target, "changed.txt")
		untouched := filepath.Join(target, "untouched.txt")
		writeTestFile(t, changed, "prior")
		writeTestFile(t, untouched, "keep")
		writeTestFile(t, filepath.Join(source, "changed.txt"), "desired")
		prepared, err := PrepareSnapshots(context.Background(), SnapshotRequest{
			Set: &MaterializedSet{Actions: []Action{{
				Kind: ActionCopy, Strategy: "copy", Source: source, Target: target,
				SourceIsDirectory: true, SnapshotRequired: true,
			}}},
			TransactionRoot: transactionRoot,
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
		primary := errors.New("force rollback")
		executor := NewTransactionExecutor()
		executor.checkpoint = func(_ context.Context, phase transactionPhase, _ int, _ string) error {
			switch phase {
			case transactionPhaseBeforeCommittedMarker:
				return primary
			case transactionPhaseBeforeRollbackAction:
				return os.Remove(untouched)
			default:
				return nil
			}
		}
		result, err := executor.Execute(context.Background(), TransactionRequest{Prepared: prepared, Intent: intent})
		assertPrimaryCause(t, result, err, primary)
		if result.Status() != TransactionRollbackFailed || !result.FailStop() || result.RollbackError() == nil {
			t.Fatalf("result status=%q failStop=%v rollback=%v", result.Status(), result.FailStop(), result.RollbackError())
		}
		assertTestFile(t, changed, "desired")
		if _, err := os.Lstat(untouched); !os.IsNotExist(err) {
			t.Fatalf("unrelated deletion was overwritten: %v", err)
		}
	})
}

func TestRollbackPrefixClassifierDoesNotInventExistingDirectory0700Transition(t *testing.T) {
	state := func(mode os.FileMode) filesystemState {
		return filesystemStateFromTransactionEntries(map[string]filesystemEntry{
			".": {Path: ".", Kind: StateDirectory, Mode: mode},
		})
	}
	if isJournalProvableFilesystemPartial(state(0o700), state(0o755), state(0o750)) {
		t.Fatal("classifier authorized a 0700 state not produced by direct chmod of an existing directory")
	}
}
