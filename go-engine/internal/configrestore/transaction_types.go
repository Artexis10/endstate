// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package configrestore

import (
	"context"
	"errors"
	"fmt"
)

// TransactionStatus is the closed single-config-set execution vocabulary.
type TransactionStatus string

const (
	TransactionRestored       TransactionStatus = "restored"
	TransactionFailed         TransactionStatus = "failed"
	TransactionRolledBack     TransactionStatus = "rolled_back"
	TransactionRollbackFailed TransactionStatus = "rollback_failed"
)

// TransactionReason retains the primary execution failure independently of
// rollback outcome.
type TransactionReason string

const (
	ReasonCommitFailed            TransactionReason = "commit_failed"
	ReasonTargetValidationFailed  TransactionReason = "target_validation_failed"
	ReasonJournalCompletionFailed TransactionReason = "journal_completion_failed"
)

// RegistryMutator performs exact named-value mutations. SetValue and
// DeleteValue must complete the OS mutation before returning; the transaction
// engine immediately rereads and verifies the raw type and bytes.
type RegistryMutator interface {
	RegistryReader
	SetValue(context.Context, string, string, uint32, []byte) error
	DeleteValue(context.Context, string, string) error
}

// TransactionRequest executes one already-prepared and durably journaled set.
type TransactionRequest struct {
	Prepared *PreparedSet
	Intent   *JournalIntent
	Registry RegistryMutator
	Observer TransactionObserver
}

// TransactionStage is the command-event-compatible execution stage.
type TransactionStage string

const (
	TransactionStageCommit     TransactionStage = "commit"
	TransactionStageValidation TransactionStage = "validation"
	TransactionStageRollback   TransactionStage = "rollback"
)

// TransactionProgress is the closed observer progress vocabulary.
type TransactionProgress string

const (
	TransactionProgressStarted   TransactionProgress = "started"
	TransactionProgressCompleted TransactionProgress = "completed"
	TransactionProgressFailed    TransactionProgress = "failed"
)

// TransactionObservation is one ordered execution observation. Unused indices
// are -1. Observer failures cannot alter transaction control flow.
type TransactionObservation struct {
	Stage           TransactionStage
	Progress        TransactionProgress
	ActionIndex     int
	ValidationIndex int
	Target          string
	Reason          TransactionReason
	Err             error
}

type TransactionObserver interface {
	Observe(TransactionObservation)
}

type TransactionObserverFunc func(TransactionObservation)

func (f TransactionObserverFunc) Observe(observation TransactionObservation) {
	if f != nil {
		f(observation)
	}
}

// TransactionResult is an immutable closed result. Failed and rolled-back
// transactions return their primary error as well; CanContinue distinguishes
// a safe per-set failure from a run-wide fail-stop.
type TransactionResult struct {
	status        TransactionStatus
	reason        TransactionReason
	primaryErr    error
	rollbackErr   error
	mutationBegan bool
	failStop      bool
	marker        *JournalMarker
}

func (r *TransactionResult) Status() TransactionStatus {
	if r == nil {
		return ""
	}
	return r.status
}

func (r *TransactionResult) Reason() TransactionReason {
	if r == nil {
		return ""
	}
	return r.reason
}

func (r *TransactionResult) PrimaryError() error {
	if r == nil {
		return nil
	}
	return r.primaryErr
}

func (r *TransactionResult) RollbackError() error {
	if r == nil {
		return nil
	}
	return r.rollbackErr
}

func (r *TransactionResult) MutationBegan() bool {
	return r != nil && r.mutationBegan
}

// FailStop is true only when later config-set mutation in the run is unsafe.
func (r *TransactionResult) FailStop() bool {
	return r != nil && r.failStop
}

func (r *TransactionResult) CanContinue() bool {
	return r != nil && !r.failStop
}

func (r *TransactionResult) Marker() *JournalMarker {
	if r == nil {
		return nil
	}
	return cloneJournalMarker(r.marker)
}

func cloneJournalMarker(marker *JournalMarker) *JournalMarker {
	if marker == nil {
		return nil
	}
	return &JournalMarker{
		transactionRoot:  marker.transactionRoot,
		path:             marker.path,
		digest:           marker.digest,
		intentDigest:     marker.intentDigest,
		state:            marker.state,
		validationStatus: marker.validationStatus,
		rollbackOutcome:  marker.rollbackOutcome,
		intent:           cloneJournalIntent(marker.intent),
	}
}

// TransactionError retains the primary failure while making an incomplete
// rollback inspectable without replacing that primary cause.
type TransactionError struct {
	Reason   TransactionReason
	Primary  error
	Rollback error
}

func (e *TransactionError) Error() string {
	if e == nil {
		return ""
	}
	if e.Rollback != nil {
		return fmt.Sprintf("config-set transaction failed (%s): %v; rollback: %v", e.Reason, e.Primary, e.Rollback)
	}
	return fmt.Sprintf("config-set transaction failed (%s): %v", e.Reason, e.Primary)
}

func (e *TransactionError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Primary
}

func transactionError(reason TransactionReason, primary, rollback error) error {
	if primary == nil {
		primary = errors.New("transaction failed without a primary cause")
	}
	return &TransactionError{Reason: reason, Primary: primary, Rollback: rollback}
}

type transactionPhase string

const (
	transactionPhaseBeforeCommitAction     transactionPhase = "before_commit_action"
	transactionPhaseAfterCommitMutation    transactionPhase = "after_commit_mutation"
	transactionPhaseBeforeValidation       transactionPhase = "before_validation"
	transactionPhaseBeforeRollbackAction   transactionPhase = "before_rollback_action"
	transactionPhaseBeforeCommittedMarker  transactionPhase = "before_committed_marker"
	transactionPhaseBeforeRolledBackMarker transactionPhase = "before_rolled_back_marker"
	transactionPhaseBeforeAbortedMarker    transactionPhase = "before_aborted_marker"
)

type transactionCheckpointFunc func(context.Context, transactionPhase, int, string) error

type TransactionExecutor struct {
	checkpoint    transactionCheckpointFunc
	journalWriter *JournalWriter
}

func NewTransactionExecutor() *TransactionExecutor {
	return &TransactionExecutor{journalWriter: NewJournalWriter()}
}
