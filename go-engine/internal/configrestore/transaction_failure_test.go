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

func assertNoCommittedMarker(t *testing.T, transactionRoot, digest string) {
	t.Helper()
	path := filepath.Join(transactionRoot, "journal", "committed-"+digest+".json")
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("committed marker exists: %v", err)
	}
}
