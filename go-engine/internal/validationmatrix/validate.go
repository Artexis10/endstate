// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationmatrix

import (
	"strings"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/modules"
)

func validateRecord(record *ValidationRecord, mod *modules.Module, now time.Time) error {
	if record.SchemaVersion != 1 {
		return validationError(CodeInvalidSidecar, record.ModuleID, record.FilePath, "schemaVersion must be 1")
	}
	if strings.TrimSpace(record.ModuleID) == "" {
		return validationError(CodeInvalidSidecar, record.ModuleID, record.FilePath, "moduleId is required")
	}
	if !lowerSHA256Pattern.MatchString(record.ModuleRevision) {
		return validationError(CodeInvalidSidecar, record.ModuleID, record.FilePath, "moduleRevision must be lowercase SHA-256")
	}
	if record.ModuleRevision != mod.Revision {
		return validationError(CodeStaleSidecar, record.ModuleID, record.FilePath, "moduleRevision %q does not match current revision %q", record.ModuleRevision, mod.Revision)
	}
	if len(record.Synthetic.Scenarios) == 0 {
		return validationError(CodeInvalidSidecar, record.ModuleID, record.FilePath, "synthetic.scenarios must not be empty")
	}

	seenScenarioIDs := make(map[string]struct{}, len(record.Synthetic.Scenarios))
	for index := range record.Synthetic.Scenarios {
		scenario := &record.Synthetic.Scenarios[index]
		if strings.TrimSpace(scenario.ID) == "" {
			return validationError(CodeInvalidSidecar, record.ModuleID, record.FilePath, "scenario[%d].id must not be blank", index)
		}
		if _, exists := seenScenarioIDs[scenario.ID]; exists {
			return validationError(CodeDuplicateScenario, record.ModuleID, record.FilePath, "duplicate scenario id %q", scenario.ID)
		}
		seenScenarioIDs[scenario.ID] = struct{}{}
		if !knownScenarioKind(scenario.Mode) {
			return validationError(CodeUnknownScenarioKind, record.ModuleID, record.FilePath, "scenario %q uses unknown mode %q", scenario.ID, scenario.Mode)
		}
		if err := validateOneWayReview(record, scenario, now); err != nil {
			return err
		}
		if err := validateFixture(record, scenario); err != nil {
			return err
		}
		if scenario.TimeoutSeconds <= 0 || scenario.TimeoutSeconds > 15*60 {
			return validationError(CodeInvalidSidecar, record.ModuleID, record.FilePath, "scenario %q timeoutSeconds must be between 1 and 900", scenario.ID)
		}
		if err := validateAssertions(record, mod, scenario); err != nil {
			return err
		}
		if err := validateExpected(record, mod, scenario); err != nil {
			return err
		}
	}

	if err := validateClassification(record, mod); err != nil {
		return err
	}
	if err := validateLivePolicy(record); err != nil {
		return err
	}
	for index := range record.Quarantines {
		if err := validateQuarantine(record, index, &record.Quarantines[index], now); err != nil {
			return err
		}
	}
	return nil
}

func knownScenarioKind(kind ScenarioKind) bool {
	switch kind {
	case ScenarioConfigRoundtripV1, ScenarioConfigGenerationV2, ScenarioConfigMigrationV2,
		ScenarioCaptureContract, ScenarioRestoreContract, ScenarioInstallContract:
		return true
	default:
		return false
	}
}
