// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationharness

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/manifest"
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

func TestForbiddenOutputValuesIncludeCanonicalCaptureArtifact(t *testing.T) {
	runtime := fixtureScenarioRuntime(t)
	want := filepath.Join(runtime.Root, "manifests", "captured"+manifest.BundleExt)
	for _, value := range runtime.forbiddenOutputValues() {
		if strings.EqualFold(value, want) {
			return
		}
	}
	t.Fatalf("forbidden output values omit %q", want)
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

func TestJourneyRejectsCaptureLiveStateCorruptionBeforeMutation(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*scenarioRuntime)
	}{
		{"file", func(runtime *scenarioRuntime) {
			target := runtime.Plan.Targets[0]
			if err := os.WriteFile(target.PayloadPath, []byte("capture-corrupted"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{"registry", func(runtime *scenarioRuntime) {
			target := runtime.Plan.RegistryTargets[0]
			fixture := runtime.Plan.registryFixture.(*recordingRegistryFixture)
			fixture.states[target.Authored] = target.Mutated
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			runtime := mixedRegistryJourneyRuntime(t, false)
			result := executeJourney(context.Background(), runtime, hostileJourneyExecutor{capture: test.mutate})
			if result.Failure == nil || result.Failure.Code != CodeContentMismatch {
				t.Fatalf("capture corruption result = %+v", result)
			}
		})
	}
}

func TestJourneyRejectsOptionalAbsenceCorruptionBeforeMutation(t *testing.T) {
	for _, test := range []struct {
		name   string
		code   string
		mutate func(*scenarioRuntime)
	}{
		{"optional file recreated", CodeContentMismatch, func(runtime *scenarioRuntime) {
			target := runtime.Plan.Targets[0]
			if err := os.MkdirAll(filepath.Dir(target.PayloadPath), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(target.PayloadPath, []byte("recreated"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{"optional registry recreated", CodeIsolationFailure, func(runtime *scenarioRuntime) {
			target := runtime.Plan.RegistryTargets[0]
			fixture := runtime.Plan.registryFixture.(*recordingRegistryFixture)
			fixture.states[target.Authored] = target.Captured
		}},
		{"required file corrupted", CodeContentMismatch, func(runtime *scenarioRuntime) {
			target := runtime.Plan.Targets[1]
			if err := os.WriteFile(target.PayloadPath, []byte("required-corrupted"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			runtime := mixedRegistryJourneyRuntime(t, true)
			result := executeJourney(context.Background(), runtime, hostileJourneyExecutor{optional: test.mutate})
			if result.Failure == nil || result.Failure.Code != test.code {
				t.Fatalf("optional corruption result = %+v", result)
			}
		})
	}
}

type hostileJourneyExecutor struct {
	capture  func(*scenarioRuntime)
	optional func(*scenarioRuntime)
}

func (executor hostileJourneyExecutor) Capture(_ context.Context, runtime *scenarioRuntime) (captureEvidence, *Failure) {
	if executor.capture != nil {
		executor.capture(runtime)
	}
	return captureEvidence{AssertionCounts: map[string]int{
		validationmatrix.AssertionCaptured: runtime.Plan.OperationCount(), validationmatrix.AssertionPayload: runtime.Plan.OperationCount(),
		validationmatrix.AssertionProvenance: 1, validationmatrix.AssertionRewrittenRestore: runtime.Plan.OperationCount(),
	}}, nil
}

func (executor hostileJourneyExecutor) CaptureOptionalAbsent(_ context.Context, runtime *scenarioRuntime) *Failure {
	if executor.optional != nil {
		executor.optional(runtime)
	}
	return nil
}

func (hostileJourneyExecutor) Rebuild(_ context.Context, runtime *scenarioRuntime, _ captureEvidence) *Failure {
	return runtime.Plan.MaterializeRestored()
}

func (hostileJourneyExecutor) Revert(_ context.Context, runtime *scenarioRuntime) *Failure {
	return runtime.Plan.Mutate()
}

func (hostileJourneyExecutor) Verify(context.Context, *scenarioRuntime, captureEvidence) (int, *Failure) {
	return 1, nil
}

func mixedRegistryJourneyRuntime(t *testing.T, requiredFile bool) *scenarioRuntime {
	t.Helper()
	mod := mixedRegistryFixtureModule()
	if requiredFile {
		mod.Capture.Files = append(mod.Capture.Files, modules.CaptureFile{Source: `%APPDATA%\Fixture\required.json`, Dest: "apps/fixture/required.json"})
		mod.Restore = append(mod.Restore, modules.RestoreDef{Type: "copy", Source: "./payload/apps/fixture/required.json", Target: `%APPDATA%\Fixture\required.json`, Backup: true})
	}
	scenario := fixtureScenario()
	fixture := &recordingRegistryFixture{}
	plan, failure := compileCompositeFixturePlanAt("", fixtureValidationContext(t, mod.ID, scenario.ID), mod, scenario, fixture)
	if failure != nil {
		t.Fatal(failure)
	}
	return &scenarioRuntime{Module: mod, Scenario: scenario, Plan: plan, Root: plan.context.Root()}
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
