// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package configrestore

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestExecuteConfigSetTransactionRejectsAnyPriorDriftBeforeFirstWrite(t *testing.T) {
	fixture := prepareFileTransactionFixture(t, true)
	writeTestFile(t, fixture.deleteTarget, "changed after intent")
	result, err := ExecuteConfigSetTransaction(context.Background(), TransactionRequest{
		Prepared: fixture.prepared, Intent: fixture.intent,
	})
	if err == nil || result.Status() != TransactionFailed || result.Reason() != ReasonCommitFailed ||
		result.MutationBegan() || result.FailStop() {
		t.Fatalf("drift result=status %q reason %q mutated %v failStop %v err=%v",
			result.Status(), result.Reason(), result.MutationBegan(), result.FailStop(), err)
	}
	assertTestFile(t, fixture.copyTarget, `{"copied":false}`)
	assertTestFile(t, fixture.writeTarget, `{"written":false}`)
	assertTestFile(t, fixture.deleteTarget, "changed after intent")
	if marker := result.Marker(); marker == nil || marker.State() != JournalRolledBack ||
		marker.RollbackOutcome() != RollbackNotRequired || marker.ValidationStatus() != ValidationNotRun {
		t.Fatalf("no-mutation marker = %#v", marker)
	}
}

func TestExecuteConfigSetTransactionInjectsEveryCommitActionFailure(t *testing.T) {
	for failedIndex := 0; failedIndex < 3; failedIndex++ {
		t.Run(fmt.Sprintf("action-%d", failedIndex), func(t *testing.T) {
			fixture := prepareFileTransactionFixture(t, true)
			cause := fmt.Errorf("commit action %d failed", failedIndex)
			executor := NewTransactionExecutor()
			executor.checkpoint = func(_ context.Context, phase transactionPhase, index int, _ string) error {
				if phase == transactionPhaseBeforeCommitAction && index == failedIndex {
					return cause
				}
				return nil
			}
			result, err := executor.Execute(context.Background(), TransactionRequest{
				Prepared: fixture.prepared, Intent: fixture.intent,
			})
			assertPrimaryCause(t, result, err, cause)
			wantStatus := TransactionFailed
			wantMutation := false
			if failedIndex > 0 {
				wantStatus = TransactionRolledBack
				wantMutation = true
			}
			if result.Status() != wantStatus || result.Reason() != ReasonCommitFailed ||
				result.MutationBegan() != wantMutation || result.FailStop() {
				t.Fatalf("result=status %q reason %q mutated %v failStop %v",
					result.Status(), result.Reason(), result.MutationBegan(), result.FailStop())
			}
			if failedIndex == 0 {
				if marker := result.Marker(); marker == nil || marker.RollbackOutcome() != RollbackNotRequired {
					t.Fatalf("first-action no-mutation marker = %#v", marker)
				}
			}
			assertFileTransactionPrior(t, fixture)
		})
	}
}

func TestExecuteConfigSetTransactionInjectsFailureAfterEachFileActionMutates(t *testing.T) {
	for failedIndex := 0; failedIndex < 3; failedIndex++ {
		t.Run(fmt.Sprintf("action-%d", failedIndex), func(t *testing.T) {
			fixture := prepareFileTransactionFixture(t, true)
			cause := fmt.Errorf("commit action %d failed after mutation", failedIndex)
			executor := NewTransactionExecutor()
			executor.checkpoint = func(_ context.Context, phase transactionPhase, index int, _ string) error {
				if phase == transactionPhaseAfterCommitMutation && index == failedIndex {
					return cause
				}
				return nil
			}
			result, err := executor.Execute(context.Background(), TransactionRequest{
				Prepared: fixture.prepared, Intent: fixture.intent,
			})
			assertPrimaryCause(t, result, err, cause)
			if result.Status() != TransactionRolledBack || result.Reason() != ReasonCommitFailed ||
				!result.MutationBegan() || result.FailStop() {
				t.Fatalf("result=status %q reason %q mutated %v failStop %v",
					result.Status(), result.Reason(), result.MutationBegan(), result.FailStop())
			}
			assertFileTransactionPrior(t, fixture)
		})
	}
}

func TestExecuteConfigSetTransactionRollsBackInjectedPartialDirectoryCommit(t *testing.T) {
	transactionRoot := t.TempDir()
	source := filepath.Join(t.TempDir(), "source")
	target := filepath.Join(t.TempDir(), "target")
	writeTestFile(t, filepath.Join(source, "a-change.txt"), "desired")
	writeTestFile(t, filepath.Join(source, "z-added.txt"), "added")
	writeTestFile(t, filepath.Join(target, "a-change.txt"), "prior")
	writeTestFile(t, filepath.Join(target, "untouched.txt"), "keep")
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
	cause := errors.New("fail after first directory entry mutation")
	executor := NewTransactionExecutor()
	executor.checkpoint = func(_ context.Context, phase transactionPhase, _ int, _ string) error {
		if phase == transactionPhaseAfterCommitMutation {
			return cause
		}
		return nil
	}
	result, err := executor.Execute(context.Background(), TransactionRequest{Prepared: prepared, Intent: intent})
	assertPrimaryCause(t, result, err, cause)
	if result.Status() != TransactionRolledBack || result.FailStop() {
		t.Fatalf("result status=%q failStop=%v rollback=%v", result.Status(), result.FailStop(), result.RollbackError())
	}
	assertTestFile(t, filepath.Join(target, "a-change.txt"), "prior")
	assertTestFile(t, filepath.Join(target, "untouched.txt"), "keep")
	if _, err := os.Lstat(filepath.Join(target, "z-added.txt")); !os.IsNotExist(err) {
		t.Fatalf("later directory entry was committed: %v", err)
	}
}

func TestExecuteConfigSetTransactionRollsBackInjectedTypeTransitionWindow(t *testing.T) {
	transactionRoot := t.TempDir()
	source := filepath.Join(t.TempDir(), "source")
	target := filepath.Join(t.TempDir(), "target")
	writeTestFile(t, target, "prior file")
	writeTestFile(t, filepath.Join(source, "child.txt"), "desired child")
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
	cause := errors.New("fail after removing transition source kind")
	executor := NewTransactionExecutor()
	executor.checkpoint = func(_ context.Context, phase transactionPhase, _ int, _ string) error {
		if phase == transactionPhaseAfterCommitMutation {
			return cause
		}
		return nil
	}
	result, err := executor.Execute(context.Background(), TransactionRequest{Prepared: prepared, Intent: intent})
	assertPrimaryCause(t, result, err, cause)
	if result.Status() != TransactionRolledBack || result.FailStop() {
		t.Fatalf("result status=%q failStop=%v rollback=%v", result.Status(), result.FailStop(), result.RollbackError())
	}
	assertTestFile(t, target, "prior file")
}

func TestExecuteConfigSetTransactionRollsBackInjectedRegistryMutation(t *testing.T) {
	key := `HKCU\Software\Vendor\InjectedTransaction`
	valueName := "Theme"
	identity := key + "\x00" + valueName
	prior := RegistryReadResult{Exists: true, ValueType: RegistryTypeSZ, Data: []byte{'o', 0, 'l', 0, 'd', 0, 0, 0}}
	registry := &memoryRegistryMutator{values: map[string]RegistryReadResult{identity: prior}}
	transactionRoot := t.TempDir()
	prepared, err := PrepareSnapshots(context.Background(), SnapshotRequest{
		Set: &MaterializedSet{Actions: []Action{{
			Kind: ActionRegistrySet, Strategy: "registry-set", Target: key + `\` + valueName,
			RegistryValue:    &RegistryValue{Key: key, ValueName: valueName, ValueType: "REG_SZ", Data: "new"},
			SnapshotRequired: true,
		}}},
		TransactionRoot: transactionRoot,
		RegistryReader:  registry,
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
	cause := errors.New("fail after registry mutation")
	executor := NewTransactionExecutor()
	executor.checkpoint = func(_ context.Context, phase transactionPhase, _ int, _ string) error {
		if phase == transactionPhaseAfterCommitMutation {
			return cause
		}
		return nil
	}
	result, err := executor.Execute(context.Background(), TransactionRequest{
		Prepared: prepared, Intent: intent, Registry: registry,
	})
	assertPrimaryCause(t, result, err, cause)
	if result.Status() != TransactionRolledBack || result.FailStop() {
		t.Fatalf("result status=%q failStop=%v rollback=%v", result.Status(), result.FailStop(), result.RollbackError())
	}
	got := registry.values[identity]
	if !got.Exists || got.ValueType != prior.ValueType || string(got.Data) != string(prior.Data) {
		t.Fatalf("registry prior not restored: got %#v want %#v", got, prior)
	}
}

func TestExecuteConfigSetTransactionNoMutationMarkerFailureIsFailStop(t *testing.T) {
	fixture := prepareFileTransactionFixture(t, true)
	primary := errors.New("first action failed before mutation")
	closeFailure := errors.New("aborted marker failed")
	executor := NewTransactionExecutor()
	executor.checkpoint = func(_ context.Context, phase transactionPhase, index int, _ string) error {
		switch {
		case phase == transactionPhaseBeforeCommitAction && index == 0:
			return primary
		case phase == transactionPhaseBeforeAbortedMarker:
			return closeFailure
		default:
			return nil
		}
	}
	result, err := executor.Execute(context.Background(), TransactionRequest{
		Prepared: fixture.prepared, Intent: fixture.intent,
	})
	assertPrimaryCause(t, result, err, primary)
	if result.Status() != TransactionFailed || result.MutationBegan() || !result.FailStop() ||
		result.CanContinue() || !errors.Is(result.RollbackError(), closeFailure) {
		t.Fatalf("result=status %q mutated %v failStop %v continue %v close=%v",
			result.Status(), result.MutationBegan(), result.FailStop(), result.CanContinue(), result.RollbackError())
	}
	assertNoTerminalMarkersOrTemps(t, fixture.transactionRoot, fixture.intent.Digest())
	assertFileTransactionPrior(t, fixture)
}

func TestExecuteConfigSetTransactionInjectsEveryFinalValidationFailure(t *testing.T) {
	for failedIndex := 0; failedIndex < 2; failedIndex++ {
		t.Run(fmt.Sprintf("validation-%d", failedIndex), func(t *testing.T) {
			fixture := prepareFileTransactionFixture(t, true)
			cause := fmt.Errorf("validation %d failed", failedIndex)
			executor := NewTransactionExecutor()
			executor.checkpoint = func(_ context.Context, phase transactionPhase, index int, _ string) error {
				if phase == transactionPhaseBeforeValidation && index == failedIndex {
					return cause
				}
				return nil
			}
			result, err := executor.Execute(context.Background(), TransactionRequest{
				Prepared: fixture.prepared, Intent: fixture.intent,
			})
			assertPrimaryCause(t, result, err, cause)
			if result.Status() != TransactionRolledBack || result.Reason() != ReasonTargetValidationFailed ||
				!result.MutationBegan() || result.FailStop() {
				t.Fatalf("result=status %q reason %q mutated %v failStop %v",
					result.Status(), result.Reason(), result.MutationBegan(), result.FailStop())
			}
			assertFileTransactionPrior(t, fixture)
		})
	}
}

func TestExecuteConfigSetTransactionRollsBackActualFinalValidationFailure(t *testing.T) {
	fixture := prepareFileTransactionFixture(t, false)
	result, err := ExecuteConfigSetTransaction(context.Background(), TransactionRequest{
		Prepared: fixture.prepared, Intent: fixture.intent,
	})
	if err == nil || result.Status() != TransactionRolledBack ||
		result.Reason() != ReasonTargetValidationFailed || !result.MutationBegan() {
		t.Fatalf("validation result=status %q reason %q mutated %v err=%v",
			result.Status(), result.Reason(), result.MutationBegan(), err)
	}
	assertFileTransactionPrior(t, fixture)
}

func TestExecuteConfigSetTransactionCompletionFailureRollsBackOnlyWhenProvenAbsent(t *testing.T) {
	t.Run("proven absent", func(t *testing.T) {
		fixture := prepareFileTransactionFixture(t, true)
		cause := errors.New("committed marker write failed")
		executor := NewTransactionExecutor()
		executor.checkpoint = func(_ context.Context, phase transactionPhase, _ int, _ string) error {
			if phase == transactionPhaseBeforeCommittedMarker {
				return cause
			}
			return nil
		}
		result, err := executor.Execute(context.Background(), TransactionRequest{
			Prepared: fixture.prepared, Intent: fixture.intent,
		})
		assertPrimaryCause(t, result, err, cause)
		if result.Status() != TransactionRolledBack || result.Reason() != ReasonJournalCompletionFailed ||
			result.FailStop() {
			t.Fatalf("result=status %q reason %q failStop %v", result.Status(), result.Reason(), result.FailStop())
		}
		assertFileTransactionPrior(t, fixture)
	})

	t.Run("ambiguous", func(t *testing.T) {
		fixture := prepareFileTransactionFixture(t, true)
		cause := fmt.Errorf("publish uncertain: %w", ErrPublicationAmbiguous)
		executor := NewTransactionExecutor()
		executor.checkpoint = func(_ context.Context, phase transactionPhase, _ int, _ string) error {
			if phase == transactionPhaseBeforeCommittedMarker {
				return cause
			}
			return nil
		}
		result, err := executor.Execute(context.Background(), TransactionRequest{
			Prepared: fixture.prepared, Intent: fixture.intent,
		})
		assertPrimaryCause(t, result, err, cause)
		if result.Status() != TransactionRollbackFailed || result.Reason() != ReasonJournalCompletionFailed ||
			!result.FailStop() || result.RollbackError() != nil {
			t.Fatalf("result=status %q reason %q failStop %v rollback=%v",
				result.Status(), result.Reason(), result.FailStop(), result.RollbackError())
		}
		assertFileTransactionDesired(t, fixture)
		assertNoTerminalMarkersOrTemps(t, fixture.transactionRoot, fixture.intent.Digest())
	})
}

func TestExecuteConfigSetTransactionInjectsEveryRollbackActionFailureAndContinues(t *testing.T) {
	for failedIndex := 0; failedIndex < 3; failedIndex++ {
		t.Run(fmt.Sprintf("action-%d", failedIndex), func(t *testing.T) {
			fixture := prepareFileTransactionFixture(t, true)
			primary := errors.New("completion failed")
			rollbackCause := fmt.Errorf("rollback action %d failed", failedIndex)
			executor := NewTransactionExecutor()
			executor.checkpoint = func(_ context.Context, phase transactionPhase, index int, _ string) error {
				switch {
				case phase == transactionPhaseBeforeCommittedMarker:
					return primary
				case phase == transactionPhaseBeforeRollbackAction && index == failedIndex:
					return rollbackCause
				default:
					return nil
				}
			}
			result, err := executor.Execute(context.Background(), TransactionRequest{
				Prepared: fixture.prepared, Intent: fixture.intent,
			})
			assertPrimaryCause(t, result, err, primary)
			if result.Status() != TransactionRollbackFailed || result.Reason() != ReasonJournalCompletionFailed ||
				!result.FailStop() || !errors.Is(result.RollbackError(), rollbackCause) {
				t.Fatalf("result=status %q reason %q failStop %v rollback=%v",
					result.Status(), result.Reason(), result.FailStop(), result.RollbackError())
			}
			// Rollback continues after one action failure, so at least every other
			// target must have returned to its exact prior state.
			if failedIndex != 0 {
				assertTestFile(t, fixture.copyTarget, `{"copied":false}`)
			}
			if failedIndex != 1 {
				assertTestFile(t, fixture.writeTarget, `{"written":false}`)
			}
			if failedIndex != 2 {
				assertTestFile(t, fixture.deleteTarget, "delete me")
			}
		})
	}
}

func TestExecuteConfigSetTransactionRollbackMarkerFailureIsFailStop(t *testing.T) {
	fixture := prepareFileTransactionFixture(t, true)
	primary := errors.New("validation failed")
	markerFailure := errors.New("rolled-back marker failed")
	executor := NewTransactionExecutor()
	executor.checkpoint = func(_ context.Context, phase transactionPhase, _ int, _ string) error {
		switch phase {
		case transactionPhaseBeforeValidation:
			return primary
		case transactionPhaseBeforeRolledBackMarker:
			return markerFailure
		default:
			return nil
		}
	}
	result, err := executor.Execute(context.Background(), TransactionRequest{
		Prepared: fixture.prepared, Intent: fixture.intent,
	})
	assertPrimaryCause(t, result, err, primary)
	if result.Status() != TransactionRollbackFailed || result.Reason() != ReasonTargetValidationFailed ||
		!result.FailStop() || !errors.Is(result.RollbackError(), markerFailure) {
		t.Fatalf("result=status %q reason %q failStop %v rollback=%v",
			result.Status(), result.Reason(), result.FailStop(), result.RollbackError())
	}
	assertFileTransactionPrior(t, fixture)
}

func TestExecuteConfigSetTransactionRejectsPreparedIntentMismatch(t *testing.T) {
	first := prepareFileTransactionFixture(t, true)
	second := prepareFileTransactionFixture(t, true)
	result, err := ExecuteConfigSetTransaction(context.Background(), TransactionRequest{
		Prepared: first.prepared,
		Intent:   second.intent,
	})
	if err == nil || result.Status() != TransactionFailed || result.MutationBegan() {
		t.Fatalf("mismatched inputs result=%#v err=%v", result, err)
	}
	assertFileTransactionPrior(t, first)
	assertFileTransactionPrior(t, second)
}

func TestExecuteConfigSetTransactionRejectsClosedIntentBeforeMutation(t *testing.T) {
	tests := []struct {
		name  string
		close func(context.Context, *JournalIntent) (*JournalMarker, error)
	}{
		{name: "committed", close: PersistCommittedMarker},
		{name: "rolled back", close: PersistAbortedMarker},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := prepareFileTransactionFixture(t, true)
			marker, err := tt.close(context.Background(), fixture.intent)
			if err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(marker.Path())
			if err != nil {
				t.Fatal(err)
			}
			var observations []TransactionObservation
			result, executeErr := ExecuteConfigSetTransaction(context.Background(), TransactionRequest{
				Prepared: fixture.prepared,
				Intent:   fixture.intent,
				Observer: TransactionObserverFunc(func(observation TransactionObservation) {
					observations = append(observations, observation)
				}),
			})
			if executeErr == nil || result.Status() != TransactionFailed || result.MutationBegan() ||
				!result.FailStop() || len(observations) != 0 {
				t.Fatalf("closed replay result=%#v err=%v observations=%#v", result, executeErr, observations)
			}
			after, err := os.ReadFile(marker.Path())
			if err != nil {
				t.Fatal(err)
			}
			if string(after) != string(before) {
				t.Fatal("closed marker changed during rejected replay")
			}
			assertFileTransactionPrior(t, fixture)
		})
	}
}

func assertNoCommittedMarker(t *testing.T, transactionRoot, digest string) {
	t.Helper()
	path := filepath.Join(transactionRoot, "journal", "terminal-"+digest+".json")
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("committed marker exists: %v", err)
	}
}
