// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationharness

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/validationmatrix"
)

func TestBrokenExecutorClassificationsNeverCarryPassingProof(t *testing.T) {
	tests := []struct {
		name string
		mode brokenMode
		code string
	}{
		{"malformed output", brokenMalformed, CodeEnvelopeContract},
		{"optional absence capture", brokenOptional, CodeExecutionFailure},
		{"nested failure", brokenNested, CodeEnvelopeContract},
		{"content mismatch", brokenContent, CodeContentMismatch},
		{"failed revert", brokenRevert, CodeRevertFailure},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runtime := fixtureScenarioRuntime(t)
			result := executeJourney(context.Background(), runtime, brokenExecutor{mode: tt.mode})
			if result.Status != ResultStatusFailed || result.Failure == nil || result.Failure.Code != tt.code {
				t.Fatalf("result = %+v, want failure %q", result, tt.code)
			}
			if len(result.ProofLevels) != 0 {
				t.Fatalf("failed result carried proof levels: %+v", result.ProofLevels)
			}
		})
	}
}

func TestSuccessfulDeterministicExecutorMeetsExactV1Ledger(t *testing.T) {
	result := executeJourney(context.Background(), fixtureScenarioRuntime(t), brokenExecutor{})
	if result.Status != ResultStatusPassed || result.Failure != nil {
		t.Fatalf("result = %+v", result)
	}
	want := []validationmatrix.ProofLevel{
		validationmatrix.ProofCatalog,
		validationmatrix.ProofEngineContract,
		validationmatrix.ProofConfigRoundtripV1,
	}
	if len(result.ProofLevels) != len(want) {
		t.Fatalf("proof levels = %q", result.ProofLevels)
	}
	for index := range want {
		if result.ProofLevels[index] != want[index] {
			t.Fatalf("proof levels = %q", result.ProofLevels)
		}
	}
}

type brokenMode string

const (
	brokenMalformed brokenMode = "malformed"
	brokenOptional  brokenMode = "optional"
	brokenNested    brokenMode = "nested"
	brokenContent   brokenMode = "content"
	brokenRevert    brokenMode = "revert"
)

type brokenExecutor struct{ mode brokenMode }

func (executor brokenExecutor) Capture(context.Context, *scenarioRuntime) (captureEvidence, *Failure) {
	if executor.mode == brokenMalformed {
		return captureEvidence{}, fail(CodeEnvelopeContract, "capture", "stdout", "malformed output")
	}
	return captureEvidence{AssertionCounts: map[string]int{
		validationmatrix.AssertionCaptured: 2, validationmatrix.AssertionPayload: 2,
		validationmatrix.AssertionProvenance: 1, validationmatrix.AssertionRewrittenRestore: 2,
	}}, nil
}

func (executor brokenExecutor) CaptureOptionalAbsent(context.Context, *scenarioRuntime) *Failure {
	if executor.mode == brokenOptional {
		return fail(CodeExecutionFailure, "capture", "optional", "optional absence capture was exercised")
	}
	return nil
}

func (executor brokenExecutor) Rebuild(_ context.Context, runtime *scenarioRuntime, _ captureEvidence) *Failure {
	if executor.mode == brokenNested {
		return fail(CodeEnvelopeContract, "rebuild", "data", "nested failure")
	}
	if executor.mode == brokenContent {
		return nil
	}
	return runtime.Plan.MaterializeRestored()
}

func (executor brokenExecutor) Revert(_ context.Context, runtime *scenarioRuntime) *Failure {
	if executor.mode == brokenRevert {
		return nil
	}
	return runtime.Plan.Mutate()
}

func (brokenExecutor) Verify(context.Context, *scenarioRuntime, captureEvidence) (int, *Failure) {
	return 1, nil
}

func fixtureScenarioRuntime(t *testing.T) *scenarioRuntime {
	t.Helper()
	mod, err := modules.ParseModuleJSON([]byte(directoryFixtureModuleJSON))
	if err != nil {
		t.Fatal(err)
	}
	scenario, definitions := directoryFixtureDefinitions(t, mod)
	plan, failure := compileFixturePlan(fixtureValidationContext(t, mod.ID, scenario.ID), mod, scenario, definitions)
	if failure != nil {
		t.Fatal(failure)
	}
	authorityRoot, err := os.MkdirTemp(filepath.Dir(plan.context.Root()), "endstate-validation-task-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(authorityRoot) })
	childWorkingDir := filepath.Join(authorityRoot, "endstate-validation-cwd-test")
	if err := os.Mkdir(childWorkingDir, 0o700); err != nil {
		t.Fatal(err)
	}
	return &scenarioRuntime{
		Module: mod, Scenario: scenario, Plan: plan, Root: plan.context.Root(),
		AuthorityRoot: authorityRoot, ChildWorkingDir: childWorkingDir,
	}
}
