// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationharness

import (
	"context"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/validationmatrix"
)

type captureContractEvidence struct {
	ArtifactPath    string
	AssertionCounts map[string]int
}

type captureContractJourneyExecutor interface {
	CaptureContract(context.Context, *scenarioRuntime) (captureContractEvidence, *Failure)
	CaptureContractOptionalAbsent(context.Context, *scenarioRuntime) *Failure
}

func executeCaptureContractJourney(ctx context.Context, runtime *scenarioRuntime, executor captureContractJourneyExecutor) Result {
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
	if runtime == nil || runtime.CapturePlan == nil {
		return failResult(fail(CodeUnsupportedFixture, "fixture", "capturePlan", "capture contract plan is absent"))
	}
	if failure := timed("fixture", runtime.CapturePlan.MaterializeCaptured); failure != nil {
		return failResult(failure)
	}
	var evidence captureContractEvidence
	if failure := timed("capture", func() *Failure {
		var failure *Failure
		evidence, failure = executor.CaptureContract(ctx, runtime)
		return failure
	}); failure != nil {
		return failResult(failure)
	}
	for name, count := range evidence.AssertionCounts {
		result.AssertionCounts[name] += count
	}
	if !runtime.CapturePlan.HasOptionalTargets() {
		return failResult(fail(CodeAssertionContract, "assertions", "optional", "capture contract must prove an optional source cannot pass while absent"))
	}
	if failure := timed("optional-capture", func() *Failure {
		if failure := runtime.CapturePlan.MaterializeOptionalAbsent(); failure != nil {
			return failure
		}
		return executor.CaptureContractOptionalAbsent(ctx, runtime)
	}); failure != nil {
		return failResult(failure)
	}
	proof, failure := evaluateAssertions(runtime.Scenario, result.AssertionCounts,
		OperationCounts{Executed: len(runtime.CapturePlan.Targets)},
		[]validationmatrix.ProofLevel{validationmatrix.ProofCatalog, validationmatrix.ProofEngineContract})
	if failure != nil {
		return failResult(failure)
	}
	result.Status = ResultStatusPassed
	result.ProofLevels = proof
	return result
}
