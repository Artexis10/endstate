// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"github.com/Artexis10/endstate/go-engine/internal/configrestore"
	"github.com/Artexis10/endstate/go-engine/internal/events"
	"github.com/Artexis10/endstate/go-engine/internal/planner"
)

type configRestoreTransactionObserver struct {
	emitter     *events.Emitter
	captureID   string
	configSetID string
}

func newConfigRestoreTransactionObserver(
	emitter *events.Emitter,
	captureID string,
	configSetID string,
) configrestore.TransactionObserver {
	return &configRestoreTransactionObserver{
		emitter: emitter, captureID: captureID, configSetID: configSetID,
	}
}

func (observer *configRestoreTransactionObserver) Observe(observation configrestore.TransactionObservation) {
	if observer == nil || observer.emitter == nil {
		return
	}
	stage, ok := publicConfigRestoreTransactionStage(observation.Stage)
	if !ok {
		return
	}
	status, ok := publicConfigRestoreTransactionProgress(observation.Progress)
	if !ok {
		return
	}
	progress := events.ConfigMigrationProgress{
		CaptureID: observer.captureID, ConfigSetID: observer.configSetID,
		Stage: stage, Status: status, Message: configRestoreTransactionMessage(stage, status),
	}
	if status == events.ConfigProgressFailed && observation.Reason != "" {
		reason := planner.ResolutionReason(observation.Reason)
		projected := planner.ProjectConfigResolution(planner.PlanSet{Resolution: planner.ConfigResolution{
			Resolution: planner.ResolutionUnknown, Reason: &reason,
		}})
		reasonText := reason.String()
		progress.Reason = &reasonText
		progress.Message = projected.Message
		progress.Remediation = projected.Remediation
	}
	observer.emitter.EmitConfigMigration(progress)
}

func publicConfigRestoreTransactionStage(stage configrestore.TransactionStage) (events.ConfigMigrationStage, bool) {
	switch stage {
	case configrestore.TransactionStageCommit:
		return events.ConfigMigrationCommit, true
	case configrestore.TransactionStageValidation:
		return events.ConfigMigrationValidation, true
	case configrestore.TransactionStageRollback:
		return events.ConfigMigrationRollback, true
	default:
		return "", false
	}
}

func publicConfigRestoreTransactionProgress(progress configrestore.TransactionProgress) (events.ConfigProgressStatus, bool) {
	switch progress {
	case configrestore.TransactionProgressStarted:
		return events.ConfigProgressStarted, true
	case configrestore.TransactionProgressCompleted:
		return events.ConfigProgressCompleted, true
	case configrestore.TransactionProgressFailed:
		return events.ConfigProgressFailed, true
	default:
		return "", false
	}
}

func configRestoreTransactionMessage(stage events.ConfigMigrationStage, status events.ConfigProgressStatus) string {
	switch stage {
	case events.ConfigMigrationCommit:
		if status == events.ConfigProgressStarted {
			return "committing settings"
		}
		if status == events.ConfigProgressFailed {
			return "settings commit failed"
		}
		return "settings committed"
	case events.ConfigMigrationValidation:
		if status == events.ConfigProgressStarted {
			return "validating restored settings"
		}
		if status == events.ConfigProgressFailed {
			return "restored settings validation failed"
		}
		return "restored settings validated"
	case events.ConfigMigrationRollback:
		if status == events.ConfigProgressStarted {
			return "rolling back settings"
		}
		if status == events.ConfigProgressFailed {
			return "settings rollback failed"
		}
		return "settings rollback completed"
	default:
		return ""
	}
}
