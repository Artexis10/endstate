// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationharness

import (
	"context"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/validationmatrix"
)

type v2JourneyExecutor interface {
	CaptureV2(context.Context, *scenarioRuntime) (captureEvidence, *Failure)
	TransitionV2(context.Context, *scenarioRuntime, captureEvidence) *Failure
	RebuildV2(context.Context, *scenarioRuntime, captureEvidence) *Failure
	RevertV2(context.Context, *scenarioRuntime) *Failure
	VerifyV2(context.Context, *scenarioRuntime, captureEvidence) *Failure
}

func executeV2Journey(ctx context.Context, runtime *scenarioRuntime, executor v2JourneyExecutor) Result {
	result := Result{
		SchemaVersion: ResultSchemaVersion, ModuleID: runtime.Module.ID, ModuleRevision: runtime.Module.Revision,
		ScenarioID: runtime.Scenario.ID, Kind: runtime.Scenario.Mode, Status: ResultStatusFailed,
		ProofLevels: []validationmatrix.ProofLevel{}, AssertionCounts: map[string]int{}, PhaseTimings: map[string]time.Duration{},
	}
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
	if runtime == nil || runtime.V2Plan == nil {
		return failResult(fail(CodeGenerationContract, "fixture", "plan", "schema-v2 fixture plan is absent"))
	}
	if failure := timed("fixture", runtime.V2Plan.MaterializeCaptured); failure != nil {
		return failResult(failure)
	}
	var evidence captureEvidence
	if failure := timed("capture", func() *Failure {
		var failure *Failure
		evidence, failure = executor.CaptureV2(ctx, runtime)
		return failure
	}); failure != nil {
		return failResult(failure)
	}
	for name, count := range evidence.AssertionCounts {
		result.AssertionCounts[name] += count
	}
	if runtime.Scenario.Mode == validationmatrix.ScenarioConfigMigrationV2 {
		if failure := timed("transition", func() *Failure { return executor.TransitionV2(ctx, runtime, evidence) }); failure != nil {
			return failResult(failure)
		}
	}
	if failure := timed("mutation", runtime.V2Plan.Mutate); failure != nil {
		return failResult(failure)
	}
	for iteration := 0; iteration < 3; iteration++ {
		phase := "rebuild"
		if iteration == 1 {
			phase = "recovery-rebuild"
		}
		if iteration == 2 {
			phase = "repeat-rebuild"
		}
		if failure := timed(phase, func() *Failure { return executor.RebuildV2(ctx, runtime, evidence) }); failure != nil {
			return failResult(failure)
		}
		if failure := runtime.V2Plan.CompareCaptured(); failure != nil {
			return failResult(failure)
		}
		result.AssertionCounts[validationmatrix.AssertionContent] += len(runtime.V2Plan.Targets)
		result.AssertionCounts[validationmatrix.AssertionRebuild]++
		result.AssertionCounts[validationmatrix.AssertionNestedSummary]++
		if iteration == 0 {
			if failure := timed("revert", func() *Failure { return executor.RevertV2(ctx, runtime) }); failure != nil {
				return failResult(failure)
			}
			if failure := runtime.V2Plan.CompareMutated(); failure != nil {
				return failResult(fail(CodeRevertFailure, "revert", failure.Coordinate, "generation revert did not restore the exact physical prior snapshot"))
			}
			result.AssertionCounts[validationmatrix.AssertionRevert]++
		}
	}
	if failure := timed("verify", func() *Failure { return executor.VerifyV2(ctx, runtime, evidence) }); failure != nil {
		return failResult(failure)
	}
	if len(runtime.Module.Verify) > 0 {
		result.AssertionCounts[validationmatrix.AssertionVerify] += len(runtime.Module.Verify)
	}
	proof, failure := evaluateAssertions(runtime.Scenario, result.AssertionCounts, OperationCounts{Executed: len(runtime.V2Plan.Targets)}, []validationmatrix.ProofLevel{
		validationmatrix.ProofCatalog, validationmatrix.ProofEngineContract, validationmatrix.ProofConfigRoundtripV2,
	})
	if failure != nil {
		return failResult(failure)
	}
	result.Status = ResultStatusPassed
	result.ProofLevels = proof
	return result
}
