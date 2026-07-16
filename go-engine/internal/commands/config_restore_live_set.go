// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"context"

	"github.com/Artexis10/endstate/go-engine/internal/configrestore"
	"github.com/Artexis10/endstate/go-engine/internal/planner"
)

var prepareConfigRestoreSnapshotsFn = configrestore.PrepareSnapshots
var persistConfigRestoreJournalIntentFn = configrestore.PersistJournalIntent
var executeConfigRestoreTransactionFn = configrestore.ExecuteConfigSetTransaction

type configRestoreLiveSetRequest struct {
	Materialized    *configrestore.MaterializedSet
	TransactionRoot string
	Lineage         configrestore.JournalLineage
	Registry        configrestore.RegistryMutator
	Observer        configrestore.TransactionObserver
	Ready           func(*configrestore.PreparedSet)
}

type configRestoreSetOutcome struct {
	Status      planner.TerminalStatus
	Reason      *planner.ResolutionReason
	Err         error
	CanContinue bool
	Prepared    *configrestore.PreparedSet
}

func executeLiveConfigRestoreSet(ctx context.Context, request configRestoreLiveSetRequest) configRestoreSetOutcome {
	prepared, err := prepareConfigRestoreSnapshotsFn(ctx, configrestore.SnapshotRequest{
		Set: request.Materialized, TransactionRoot: request.TransactionRoot, RegistryReader: request.Registry,
	})
	if err != nil {
		return failedConfigRestoreSet(planner.ReasonBackupFailed, err, true, nil)
	}
	if preparedConfigRestoreAlreadyCurrent(prepared) {
		reason := planner.ReasonAlreadyUpToDate
		return configRestoreSetOutcome{
			Status: planner.StatusSkipped, Reason: &reason, CanContinue: true, Prepared: prepared,
		}
	}

	intent, err := persistConfigRestoreJournalIntentFn(ctx, configrestore.JournalIntentRequest{
		Prepared: prepared, TransactionRoot: request.TransactionRoot, Lineage: request.Lineage,
	})
	if err != nil {
		return failedConfigRestoreSet(planner.ReasonJournalIntentFailed, err, false, prepared)
	}
	if request.Ready != nil {
		request.Ready(prepared)
	}
	transaction, transactionErr := executeConfigRestoreTransactionFn(ctx, configrestore.TransactionRequest{
		Prepared: prepared, Intent: intent, Registry: request.Registry, Observer: request.Observer,
	})
	if transaction == nil {
		return failedConfigRestoreSet(planner.ReasonCommitFailed, transactionErr, false, prepared)
	}
	status := publicConfigRestoreTransactionStatus(transaction.Status())
	var reason *planner.ResolutionReason
	if transaction.Reason() != "" {
		value := planner.ResolutionReason(transaction.Reason())
		reason = &value
	}
	return configRestoreSetOutcome{
		Status: status, Reason: reason, Err: transactionErr,
		CanContinue: transaction.CanContinue(), Prepared: prepared,
	}
}

func failedConfigRestoreSet(
	reason planner.ResolutionReason,
	err error,
	canContinue bool,
	prepared *configrestore.PreparedSet,
) configRestoreSetOutcome {
	return configRestoreSetOutcome{
		Status: planner.StatusFailed, Reason: &reason, Err: err,
		CanContinue: canContinue, Prepared: prepared,
	}
}

func preparedConfigRestoreAlreadyCurrent(prepared *configrestore.PreparedSet) bool {
	actions := prepared.Actions()
	if len(actions) == 0 {
		return true
	}
	for _, action := range actions {
		prior := action.Prior()
		desired := action.Desired()
		if prior.Kind != desired.Kind || prior.Digest != desired.Digest {
			return false
		}
	}
	return true
}

func publicConfigRestoreTransactionStatus(status configrestore.TransactionStatus) planner.TerminalStatus {
	switch status {
	case configrestore.TransactionRestored:
		return planner.StatusRestored
	case configrestore.TransactionRolledBack:
		return planner.StatusRolledBack
	case configrestore.TransactionRollbackFailed:
		return planner.StatusRollbackFailed
	default:
		return planner.StatusFailed
	}
}
