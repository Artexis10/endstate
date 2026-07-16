// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package configrestore

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	"github.com/Artexis10/endstate/go-engine/internal/configvalidate"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
)

func ExecuteConfigSetTransaction(ctx context.Context, request TransactionRequest) (*TransactionResult, error) {
	return NewTransactionExecutor().Execute(ctx, request)
}

func (e *TransactionExecutor) Execute(ctx context.Context, request TransactionRequest) (*TransactionResult, error) {
	verifiedIntent, preparedActions, err := validateTransactionInputs(ctx, request)
	if err != nil {
		return failedTransactionResult(ReasonCommitFailed, err, true), transactionError(ReasonCommitFailed, err, nil)
	}
	actions := verifiedIntent.Actions()
	if err := verifyAllTransactionStates(ctx, actions, request.Registry, false); err != nil {
		return e.finishFailure(
			ctx, request, verifiedIntent, actions, make([]bool, len(actions)), false,
			ReasonCommitFailed, ValidationNotRun, err,
		)
	}

	touched := make([]bool, len(actions))
	mutationBegan := false
	for index := range actions {
		action := actions[index]
		e.observe(request.Observer, TransactionObservation{
			Stage: TransactionStageCommit, Progress: TransactionProgressStarted,
			ActionIndex: index, ValidationIndex: -1, Target: action.Target,
		})
		if err := e.runCheckpoint(ctx, transactionPhaseBeforeCommitAction, index, action.Target); err != nil {
			e.observeFailure(request.Observer, TransactionStageCommit, index, -1, action.Target, ReasonCommitFailed, err)
			return e.finishFailure(
				ctx, request, verifiedIntent, actions, touched, mutationBegan,
				ReasonCommitFailed, ValidationNotRun, err,
			)
		}
		if err := verifyTransactionActionState(ctx, action, request.Registry, false); err != nil {
			e.observeFailure(request.Observer, TransactionStageCommit, index, -1, action.Target, ReasonCommitFailed, err)
			return e.finishFailure(
				ctx, request, verifiedIntent, actions, touched, mutationBegan,
				ReasonCommitFailed, ValidationNotRun, err,
			)
		}
		touch := func() {
			touched[index] = true
			mutationBegan = true
		}
		if err := executeTransactionAction(ctx, preparedActions[index], action, request.Registry, touch); err != nil {
			e.observeFailure(request.Observer, TransactionStageCommit, index, -1, action.Target, ReasonCommitFailed, err)
			return e.finishFailure(
				ctx, request, verifiedIntent, actions, touched, mutationBegan,
				ReasonCommitFailed, ValidationNotRun, err,
			)
		}
		if err := verifyTransactionActionState(ctx, action, request.Registry, true); err != nil {
			e.observeFailure(request.Observer, TransactionStageCommit, index, -1, action.Target, ReasonCommitFailed, err)
			return e.finishFailure(
				ctx, request, verifiedIntent, actions, touched, mutationBegan,
				ReasonCommitFailed, ValidationNotRun, err,
			)
		}
		e.observe(request.Observer, TransactionObservation{
			Stage: TransactionStageCommit, Progress: TransactionProgressCompleted,
			ActionIndex: index, ValidationIndex: -1, Target: action.Target,
		})
	}

	validations := verifiedIntent.Validations()
	for index, validation := range validations {
		e.observe(request.Observer, TransactionObservation{
			Stage: TransactionStageValidation, Progress: TransactionProgressStarted,
			ActionIndex: -1, ValidationIndex: index, Target: validation.HostPath,
		})
		if err := e.runCheckpoint(ctx, transactionPhaseBeforeValidation, index, validation.HostPath); err != nil {
			e.observeFailure(
				request.Observer, TransactionStageValidation, -1, index, validation.HostPath,
				ReasonTargetValidationFailed, err,
			)
			return e.finishFailure(
				ctx, request, verifiedIntent, actions, touched, mutationBegan,
				ReasonTargetValidationFailed, ValidationFailed, err,
			)
		}
		resolved := configvalidate.ResolvedValidation{
			Definition: modules.ValidationDef{
				Type: validation.Type, Path: validation.Path, JSONPath: validation.JSONPath,
				Section: validation.Section, Key: validation.Key,
			},
			HostPath: validation.HostPath,
		}
		if err := configvalidate.ValidateResolved([]configvalidate.ResolvedValidation{resolved}); err != nil {
			e.observeFailure(
				request.Observer, TransactionStageValidation, -1, index, validation.HostPath,
				ReasonTargetValidationFailed, err,
			)
			return e.finishFailure(
				ctx, request, verifiedIntent, actions, touched, mutationBegan,
				ReasonTargetValidationFailed, ValidationFailed, err,
			)
		}
		e.observe(request.Observer, TransactionObservation{
			Stage: TransactionStageValidation, Progress: TransactionProgressCompleted,
			ActionIndex: -1, ValidationIndex: index, Target: validation.HostPath,
		})
	}

	if err := e.runCheckpoint(ctx, transactionPhaseBeforeCommittedMarker, -1, verifiedIntent.Path()); err != nil {
		return e.finishFailure(
			ctx, request, verifiedIntent, actions, touched, mutationBegan,
			ReasonJournalCompletionFailed, ValidationPassed, err,
		)
	}
	writer := e.writer()
	marker, err := writer.PersistCommitted(ctx, verifiedIntent)
	if err != nil {
		return e.finishFailure(
			ctx, request, verifiedIntent, actions, touched, mutationBegan,
			ReasonJournalCompletionFailed, ValidationPassed, err,
		)
	}
	result := &TransactionResult{
		status: TransactionRestored, mutationBegan: mutationBegan, marker: cloneJournalMarker(marker),
	}
	return result, nil
}

func validateTransactionInputs(
	ctx context.Context,
	request TransactionRequest,
) (*JournalIntent, []PreparedAction, error) {
	if request.Prepared == nil || request.Intent == nil {
		return nil, nil, fmt.Errorf("prepared set and verified journal intent are required")
	}
	verified, err := verifyIntentForMarker(ctx, request.Intent)
	if err != nil {
		return nil, nil, err
	}
	if err := requirePendingJournalIntent(verified); err != nil {
		return nil, nil, err
	}
	root, lineage, expectedActions, expectedValidations, err := validateJournalIntentRequest(ctx, JournalIntentRequest{
		Prepared: request.Prepared, TransactionRoot: verified.transactionRoot, Lineage: verified.Lineage(),
	})
	if err != nil {
		return nil, nil, err
	}
	if root != verified.transactionRoot || !reflect.DeepEqual(lineage, verified.Lineage()) ||
		!equalJournalActions(expectedActions, verified.Actions()) ||
		!equalJournalValidations(expectedValidations, verified.Validations()) {
		return nil, nil, fmt.Errorf("prepared set differs from the verified journal intent")
	}
	for _, action := range expectedActions {
		if action.Kind == ActionRegistrySet && request.Registry == nil {
			return nil, nil, fmt.Errorf("registry transaction requires a registry mutator")
		}
	}
	return verified, request.Prepared.Actions(), nil
}

func equalJournalActions(left, right []JournalAction) bool {
	return reflect.DeepEqual(cloneJournalActions(left), cloneJournalActions(right))
}

func equalJournalValidations(left, right []JournalValidation) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func (e *TransactionExecutor) finishFailure(
	ctx context.Context,
	request TransactionRequest,
	intent *JournalIntent,
	actions []JournalAction,
	touched []bool,
	mutationBegan bool,
	reason TransactionReason,
	validation ValidationStatus,
	primary error,
) (*TransactionResult, error) {
	if !mutationBegan {
		closeContext := context.WithoutCancel(ctx)
		marker, closeErr := e.persistAborted(closeContext, intent)
		if closeErr != nil {
			result := failedTransactionResult(reason, primary, true)
			result.rollbackErr = closeErr
			return result, transactionError(reason, primary, closeErr)
		}
		result := failedTransactionResult(reason, primary, false)
		result.marker = cloneJournalMarker(marker)
		return result, transactionError(reason, primary, nil)
	}
	if errors.Is(primary, ErrPublicationAmbiguous) {
		result := &TransactionResult{
			status: TransactionRollbackFailed, reason: reason, primaryErr: primary,
			mutationBegan: true, failStop: true,
		}
		return result, transactionError(reason, primary, nil)
	}

	rollbackContext := context.WithoutCancel(ctx)
	rollbackErr := e.rollback(
		rollbackContext, request, intent, actions, touched, validation,
	)
	if rollbackErr != nil {
		result := &TransactionResult{
			status: TransactionRollbackFailed, reason: reason, primaryErr: primary, rollbackErr: rollbackErr,
			mutationBegan: true, failStop: true,
		}
		return result, transactionError(reason, primary, rollbackErr)
	}
	marker, markerErr := e.persistRolledBack(rollbackContext, intent, validation)
	if markerErr != nil {
		result := &TransactionResult{
			status: TransactionRollbackFailed, reason: reason, primaryErr: primary, rollbackErr: markerErr,
			mutationBegan: true, failStop: true,
		}
		return result, transactionError(reason, primary, markerErr)
	}
	result := &TransactionResult{
		status: TransactionRolledBack, reason: reason, primaryErr: primary,
		mutationBegan: true, marker: cloneJournalMarker(marker),
	}
	return result, transactionError(reason, primary, nil)
}

func (e *TransactionExecutor) persistAborted(
	ctx context.Context,
	intent *JournalIntent,
) (*JournalMarker, error) {
	if err := e.runCheckpoint(ctx, transactionPhaseBeforeAbortedMarker, -1, intent.Path()); err != nil {
		return nil, err
	}
	return e.writer().PersistAborted(ctx, intent)
}

func (e *TransactionExecutor) rollback(
	ctx context.Context,
	request TransactionRequest,
	intent *JournalIntent,
	actions []JournalAction,
	touched []bool,
	validation ValidationStatus,
) error {
	var rollbackErrors []error
	for index := len(actions) - 1; index >= 0; index-- {
		if !touched[index] {
			continue
		}
		action := actions[index]
		e.observe(request.Observer, TransactionObservation{
			Stage: TransactionStageRollback, Progress: TransactionProgressStarted,
			ActionIndex: index, ValidationIndex: -1, Target: action.Target,
		})
		if err := e.runCheckpoint(ctx, transactionPhaseBeforeRollbackAction, index, action.Target); err != nil {
			rollbackErrors = append(rollbackErrors, fmt.Errorf("rollback action[%d]: %w", index, err))
			e.observeFailure(request.Observer, TransactionStageRollback, index, -1, action.Target, "", err)
			continue
		}
		if err := rollbackTransactionAction(ctx, action, request.Registry); err != nil {
			rollbackErrors = append(rollbackErrors, fmt.Errorf("rollback action[%d]: %w", index, err))
			e.observeFailure(request.Observer, TransactionStageRollback, index, -1, action.Target, "", err)
			continue
		}
		if err := verifyTransactionActionState(ctx, action, request.Registry, false); err != nil {
			rollbackErrors = append(rollbackErrors, fmt.Errorf("verify rollback action[%d]: %w", index, err))
			e.observeFailure(request.Observer, TransactionStageRollback, index, -1, action.Target, "", err)
			continue
		}
		e.observe(request.Observer, TransactionObservation{
			Stage: TransactionStageRollback, Progress: TransactionProgressCompleted,
			ActionIndex: index, ValidationIndex: -1, Target: action.Target,
		})
	}
	if err := verifyAllTransactionStates(ctx, actions, request.Registry, false); err != nil {
		rollbackErrors = append(rollbackErrors, fmt.Errorf("verify complete rollback: %w", err))
	}
	return errors.Join(rollbackErrors...)
}

func (e *TransactionExecutor) persistRolledBack(
	ctx context.Context,
	intent *JournalIntent,
	validation ValidationStatus,
) (*JournalMarker, error) {
	if err := e.runCheckpoint(ctx, transactionPhaseBeforeRolledBackMarker, -1, intent.Path()); err != nil {
		return nil, err
	}
	return e.writer().PersistRolledBack(ctx, intent, validation)
}

func failedTransactionResult(reason TransactionReason, primary error, failStop bool) *TransactionResult {
	return &TransactionResult{
		status: TransactionFailed, reason: reason, primaryErr: primary, failStop: failStop,
	}
}

func (e *TransactionExecutor) writer() *JournalWriter {
	if e != nil && e.journalWriter != nil {
		return e.journalWriter
	}
	return NewJournalWriter()
}

func (e *TransactionExecutor) runCheckpoint(
	ctx context.Context,
	phase transactionPhase,
	index int,
	target string,
) error {
	if err := checkSnapshotContext(ctx); err != nil {
		return err
	}
	if e != nil && e.checkpoint != nil {
		if err := e.checkpoint(ctx, phase, index, target); err != nil {
			return err
		}
	}
	return checkSnapshotContext(ctx)
}

func (e *TransactionExecutor) observe(observer TransactionObserver, observation TransactionObservation) {
	if observer != nil {
		observer.Observe(observation)
	}
}

func (e *TransactionExecutor) observeFailure(
	observer TransactionObserver,
	stage TransactionStage,
	actionIndex int,
	validationIndex int,
	target string,
	reason TransactionReason,
	err error,
) {
	e.observe(observer, TransactionObservation{
		Stage: stage, Progress: TransactionProgressFailed,
		ActionIndex: actionIndex, ValidationIndex: validationIndex, Target: target, Reason: reason, Err: err,
	})
}
