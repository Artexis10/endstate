// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationharness

import (
	"context"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/validationmatrix"
)

type restoreContractJourneyExecutor interface {
	RebuildRestoreContract(context.Context, *scenarioRuntime) *Failure
	RevertRestoreContract(context.Context, *scenarioRuntime) *Failure
}

func executeRestoreContractJourney(ctx context.Context, runtime *scenarioRuntime, executor restoreContractJourneyExecutor) Result {
	result := Result{
		SchemaVersion: ResultSchemaVersion, Status: ResultStatusFailed,
		ProofLevels: []validationmatrix.ProofLevel{}, AssertionCounts: map[string]int{}, PhaseTimings: map[string]time.Duration{},
	}
	if runtime == nil || runtime.Module == nil || runtime.RestorePlan == nil || runtime.Plan == nil || len(runtime.Plan.Targets) != 1 {
		result.Failure = fail(CodeUnsupportedFixture, "fixture", "operations", "restore contract runtime is incomplete")
		return result
	}
	result.ModuleID = runtime.Module.ID
	result.ModuleRevision = runtime.Module.Revision
	result.ScenarioID = runtime.Scenario.ID
	result.Kind = runtime.Scenario.Mode
	failResult := func(failure *Failure) Result {
		result.Failure = failure
		result.ProofLevels = []validationmatrix.ProofLevel{}
		return result
	}
	timed := func(phase string, operation func() *Failure) *Failure {
		started := time.Now()
		failure := operation()
		result.PhaseTimings[phase] += time.Since(started)
		return failure
	}

	if failure := timed("fixture", runtime.Plan.Mutate); failure != nil {
		return failResult(failure)
	}
	if failure := timed("rebuild", func() *Failure { return executor.RebuildRestoreContract(ctx, runtime) }); failure != nil {
		return failResult(failure)
	}
	if failure := runtime.Plan.CompareCaptured(); failure != nil {
		return failResult(failure)
	}
	result.AssertionCounts[validationmatrix.AssertionRestored] = 1
	result.AssertionCounts[validationmatrix.AssertionContent] = 1
	result.AssertionCounts[validationmatrix.AssertionNestedSummary] = 1
	result.AssertionCounts[validationmatrix.AssertionVerify] = 1

	if failure := timed("revert", func() *Failure { return executor.RevertRestoreContract(ctx, runtime) }); failure != nil {
		return failResult(failure)
	}
	if failure := runtime.Plan.CompareMutated(); failure != nil {
		return failResult(fail(CodeRevertFailure, "revert", failure.Coordinate, "revert did not restore the exact pre-restore fixture state"))
	}
	result.AssertionCounts[validationmatrix.AssertionRevert] = 1

	proof, failure := evaluateAssertions(runtime.Scenario, result.AssertionCounts, OperationCounts{Executed: 1}, []validationmatrix.ProofLevel{validationmatrix.ProofEngineContract})
	if failure != nil {
		return failResult(failure)
	}
	result.Status = ResultStatusPassed
	result.ProofLevels = proof
	return result
}
