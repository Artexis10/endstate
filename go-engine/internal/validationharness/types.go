// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

// Package validationharness executes repository-owned validation scenarios
// through a separately built Endstate engine.
package validationharness

import (
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/validationmatrix"
)

const (
	CodeInvalidEngine      = "invalid_engine"
	CodeInvalidResultPath  = "invalid_result_path"
	CodeScenarioSelection  = "scenario_selection"
	CodeUnsupportedFixture = "unsupported_fixture"
	CodeAssertionContract  = "assertion_contract"
	CodeEnvelopeContract   = "envelope_contract"
	CodeEventContract      = "event_contract"
	CodeExecutionFailure   = "execution_failure"
	CodeArtifactContract   = "artifact_contract"
	CodeContentMismatch    = "content_mismatch"
	CodeRevertFailure      = "revert_failure"
	CodeIsolationFailure   = "isolation_failure"
	CodeGenerationContract = "generation_contract"
	CodeMigrationContract  = "migration_contract"
	ResultSchemaVersion    = 1
	ResultStatusPassed     = "passed"
	ResultStatusFailed     = "failed"
)

type Request struct {
	EnginePath string
	RepoRoot   string
	ModuleID   string
	ScenarioID string
	ResultPath string
}

type Result struct {
	SchemaVersion   int                           `json:"schemaVersion"`
	ModuleID        string                        `json:"moduleId"`
	ModuleRevision  string                        `json:"moduleRevision,omitempty"`
	ScenarioID      string                        `json:"scenarioId"`
	Kind            validationmatrix.ScenarioKind `json:"kind,omitempty"`
	Status          string                        `json:"status"`
	ProofLevels     []validationmatrix.ProofLevel `json:"proofLevels"`
	AssertionCounts map[string]int                `json:"assertionCounts"`
	Failure         *Failure                      `json:"failure,omitempty"`
	PhaseTimings    map[string]time.Duration      `json:"phaseTimings"`
}

type Failure struct {
	Code        string                        `json:"code"`
	Phase       string                        `json:"phase"`
	Coordinate  string                        `json:"coordinate,omitempty"`
	Detail      string                        `json:"detail,omitempty"`
	ProofLevels []validationmatrix.ProofLevel `json:"-"`
}

type OperationCounts struct {
	Executed int
	Skipped  int
}

func fail(code, phase, coordinate, detail string) *Failure {
	return &Failure{Code: code, Phase: phase, Coordinate: coordinate, Detail: detail}
}
