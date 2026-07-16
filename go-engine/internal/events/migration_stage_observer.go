// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package events

import (
	"github.com/Artexis10/endstate/go-engine/internal/migration"
	"github.com/Artexis10/endstate/go-engine/internal/planner"
)

type migrationStageObserver struct {
	emitter     *Emitter
	configSetID string
}

// NewMigrationStageObserver adapts migration's synchronous staging progress to
// the public config-migration event vocabulary for one config set.
func NewMigrationStageObserver(emitter *Emitter, configSetID string) migration.StageObserver {
	return &migrationStageObserver{emitter: emitter, configSetID: configSetID}
}

func (observer *migrationStageObserver) ObserveStageProgress(progress migration.StageProgress) {
	if observer == nil || observer.emitter == nil {
		return
	}
	stage, ok := publicMigrationStage(progress.Stage)
	if !ok {
		return
	}
	status, ok := publicMigrationStatus(progress.Status)
	if !ok {
		return
	}

	event := ConfigMigrationProgress{
		CaptureID:   progress.CaptureID,
		ConfigSetID: observer.configSetID,
		Stage:       stage,
		Status:      status,
		Message:     migrationProgressMessage(stage, status),
	}
	if stage == ConfigMigrationEdge {
		event.FromGeneration = progress.FromGeneration
		event.ToGeneration = progress.ToGeneration
	}
	if status == ConfigProgressFailed {
		reason := stagingFailureReason(progress.Code)
		projected := planner.ProjectConfigResolution(planner.PlanSet{Resolution: planner.ConfigResolution{
			Resolution: planner.ResolutionUnknown,
			Reason:     &reason,
		}})
		reasonText := reason.String()
		event.Reason = &reasonText
		event.Message = projected.Message
		event.Remediation = projected.Remediation
	}
	observer.emitter.EmitConfigMigration(event)
}

func publicMigrationStage(stage migration.ProgressStage) (ConfigMigrationStage, bool) {
	switch stage {
	case migration.ProgressStaging:
		return ConfigMigrationStaging, true
	case migration.ProgressEdge:
		return ConfigMigrationEdge, true
	case migration.ProgressValidation:
		return ConfigMigrationValidation, true
	default:
		return "", false
	}
}

func publicMigrationStatus(status migration.ProgressStatus) (ConfigProgressStatus, bool) {
	switch status {
	case migration.ProgressStarted:
		return ConfigProgressStarted, true
	case migration.ProgressCompleted:
		return ConfigProgressCompleted, true
	case migration.ProgressFailed:
		return ConfigProgressFailed, true
	default:
		return "", false
	}
}

func migrationProgressMessage(stage ConfigMigrationStage, status ConfigProgressStatus) string {
	switch stage {
	case ConfigMigrationStaging:
		if status == ConfigProgressStarted {
			return "staging settings payload"
		}
		return "settings payload staged"
	case ConfigMigrationEdge:
		if status == ConfigProgressStarted {
			return "applying migration edge"
		}
		return "migration edge validated"
	case ConfigMigrationValidation:
		if status == ConfigProgressStarted {
			return "validating staged settings"
		}
		return "staged settings validated"
	default:
		return ""
	}
}

func stagingFailureReason(code migration.ErrorCode) planner.ResolutionReason {
	if code == migration.CodePayloadIntegrityFailed {
		return planner.ReasonPayloadIntegrityFailed
	}
	return planner.ReasonStagingValidationFailed
}
