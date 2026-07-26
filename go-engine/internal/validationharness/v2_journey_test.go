// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationharness

import (
	"context"
	"reflect"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/validationmatrix"
)

func TestV2MigrationTransitionFailureLaunchesNoFurtherChild(t *testing.T) {
	executor := &v2JourneyExecutorFixture{
		transitionFailure: fail(CodeMigrationContract, "transition", "packageState", "injected mismatch"),
	}
	runtime := &scenarioRuntime{
		Module: &modules.Module{ID: "apps.owncloud"},
		Scenario: validationmatrix.Scenario{
			ID: "migration-preferences-g1-to-g2", Mode: validationmatrix.ScenarioConfigMigrationV2,
		},
		V2Plan: &V2FixturePlan{},
	}

	result := executeV2Journey(context.Background(), runtime, executor)
	if result.Failure == nil || result.Failure.Code != CodeMigrationContract ||
		!reflect.DeepEqual(executor.calls, []string{"capture", "transition"}) {
		t.Fatalf("transition cutoff = result:%+v calls:%v", result, executor.calls)
	}
}

type v2JourneyExecutorFixture struct {
	calls             []string
	transitionFailure *Failure
	captureEvidence   captureEvidence
	transition        func(captureEvidence) *Failure
}

func (executor *v2JourneyExecutorFixture) CaptureV2(context.Context, *scenarioRuntime) (captureEvidence, *Failure) {
	executor.calls = append(executor.calls, "capture")
	return executor.captureEvidence, nil
}

func (executor *v2JourneyExecutorFixture) TransitionV2(_ context.Context, _ *scenarioRuntime, evidence captureEvidence) *Failure {
	executor.calls = append(executor.calls, "transition")
	if executor.transition != nil {
		return executor.transition(evidence)
	}
	return executor.transitionFailure
}

func (executor *v2JourneyExecutorFixture) RebuildV2(context.Context, *scenarioRuntime, captureEvidence) *Failure {
	executor.calls = append(executor.calls, "rebuild")
	return nil
}

func (executor *v2JourneyExecutorFixture) RevertV2(context.Context, *scenarioRuntime) *Failure {
	executor.calls = append(executor.calls, "revert")
	return nil
}

func (executor *v2JourneyExecutorFixture) VerifyV2(context.Context, *scenarioRuntime, captureEvidence) *Failure {
	executor.calls = append(executor.calls, "verify")
	return nil
}
