// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationharness

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/validationmatrix"
	"github.com/Artexis10/endstate/go-engine/internal/validationmode"
)

func TestRegistryFixtureAccessDeniedFailuresAreStructuredAndCleaned(t *testing.T) {
	tests := []struct {
		name     string
		selected func(*testing.T) *selection
		fixture  func() *runtimeRegistryFixture
		execute  scenarioExecution
		phase    string
	}{
		{
			name:     "ordinary materialize",
			selected: registryVerifierSelection,
			fixture: func() *runtimeRegistryFixture {
				return &runtimeRegistryFixture{materializeErr: errors.New("ERROR_ACCESS_DENIED nonce-sensitive")}
			},
			execute: func(context.Context, *selection, *scenarioRuntime) Result {
				t.Fatal("ordinary verifier materialization must fail during preparation")
				return Result{}
			},
			phase: "setup",
		},
		{
			name:     "install negative absence",
			selected: registryInstallSelection,
			fixture: func() *runtimeRegistryFixture {
				return &runtimeRegistryFixture{proveAbsentErr: errors.New("ERROR_ACCESS_DENIED nonce-sensitive")}
			},
			execute: func(ctx context.Context, _ *selection, runtime *scenarioRuntime) Result {
				return executeInstallJourney(ctx, runtime, registryInstallExecutor{})
			},
			phase: "install",
		},
		{
			name:     "install positive snapshot",
			selected: registryInstallSelection,
			fixture: func() *runtimeRegistryFixture {
				return &runtimeRegistryFixture{snapshotErr: errors.New("ERROR_ACCESS_DENIED nonce-sensitive")}
			},
			execute: func(ctx context.Context, _ *selection, runtime *scenarioRuntime) Result {
				return executeInstallJourney(ctx, runtime, registryInstallExecutor{})
			},
			phase: "install",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := test.fixture()
			result, err := runSelectedScenarioWithRegistryFixtureFactory(context.Background(), test.selected(t), func(*validationmode.Context) (scenarioRegistryFixture, error) {
				return fixture, nil
			}, test.execute)
			if err != nil || result.Status != ResultStatusFailed || result.Failure == nil || result.Failure.Code != CodeIsolationFailure || result.Failure.Phase != test.phase || len(result.ProofLevels) != 0 {
				t.Fatalf("structured failure = %+v, %v", result, err)
			}
			if strings.Contains(strings.ToLower(result.Failure.Detail), "access") || strings.Contains(result.Failure.Detail, "nonce-sensitive") {
				t.Fatalf("raw fixture error leaked into result: %+v", result.Failure)
			}
			if got := registryFixtureCallCount(fixture, "cleanup"); got != 2 {
				t.Fatalf("cleanup calls = %d, want initial and final cleanup once each (%v)", got, fixture.calls)
			}
		})
	}
}

func TestRunSelectedScenarioCleansRegistryFixtureForEveryTerminalResult(t *testing.T) {
	terminals := []struct {
		name   string
		result Result
	}{
		{"capture failure", registryTerminalResult(CodeExecutionFailure, "capture")},
		{"rebuild failure", registryTerminalResult(CodeExecutionFailure, "rebuild")},
		{"revert failure", registryTerminalResult(CodeRevertFailure, "revert")},
		{"verify failure", registryTerminalResult(CodeExecutionFailure, "verify")},
		{"cancellation", registryTerminalResult(CodeExecutionFailure, "timeout")},
		{"success", Result{Status: ResultStatusPassed, ProofLevels: []validationmatrix.ProofLevel{validationmatrix.ProofConfigRoundtripV1}}},
	}
	for _, terminal := range terminals {
		t.Run(terminal.name, func(t *testing.T) {
			fixture := &runtimeRegistryFixture{}
			result, err := runSelectedScenarioWithRegistryFixtureFactory(context.Background(), registryVerifierSelection(t), func(*validationmode.Context) (scenarioRegistryFixture, error) {
				return fixture, nil
			}, func(context.Context, *selection, *scenarioRuntime) Result {
				return terminal.result
			})
			if err != nil || result.Status != terminal.result.Status || result.Failure != terminal.result.Failure {
				t.Fatalf("terminal result = %+v, %v", result, err)
			}
			if got := registryFixtureCallCount(fixture, "cleanup"); got != 2 {
				t.Fatalf("cleanup calls = %d, want initial and final cleanup once each (%v)", got, fixture.calls)
			}
		})
	}
}

func TestRunSelectedScenarioCleanupFailureOverridesEveryTerminalResult(t *testing.T) {
	terminals := []struct {
		name   string
		result Result
	}{
		{"capture failure", registryTerminalResult(CodeExecutionFailure, "capture")},
		{"rebuild failure", registryTerminalResult(CodeExecutionFailure, "rebuild")},
		{"revert failure", registryTerminalResult(CodeRevertFailure, "revert")},
		{"verify failure", registryTerminalResult(CodeExecutionFailure, "verify")},
		{"cancellation", registryTerminalResult(CodeExecutionFailure, "timeout")},
		{"success", Result{Status: ResultStatusPassed, ProofLevels: []validationmatrix.ProofLevel{validationmatrix.ProofConfigRoundtripV1}}},
	}
	for _, terminal := range terminals {
		t.Run(terminal.name, func(t *testing.T) {
			fixture := &runtimeRegistryFixture{cleanupErrors: map[int]error{2: errors.New("cleanup denied")}}
			result, err := runSelectedScenarioWithRegistryFixtureFactory(context.Background(), registryVerifierSelection(t), func(*validationmode.Context) (scenarioRegistryFixture, error) {
				return fixture, nil
			}, func(context.Context, *selection, *scenarioRuntime) Result {
				return terminal.result
			})
			if err != nil || result.Status != ResultStatusFailed || result.Failure == nil || result.Failure.Code != CodeIsolationFailure || result.Failure.Phase != "cleanup" || result.Failure.Coordinate != "runtime" || len(result.ProofLevels) != 0 {
				t.Fatalf("cleanup override = %+v, %v", result, err)
			}
			if got := registryFixtureCallCount(fixture, "cleanup"); got != 2 {
				t.Fatalf("cleanup calls = %d, want initial and final cleanup once each (%v)", got, fixture.calls)
			}
		})
	}
}

func registryTerminalResult(code, phase string) Result {
	return Result{Status: ResultStatusFailed, ProofLevels: []validationmatrix.ProofLevel{}, Failure: fail(code, phase, "fixture", "terminal fixture failure")}
}

func registryFixtureCallCount(fixture *runtimeRegistryFixture, call string) int {
	count := 0
	for _, candidate := range fixture.calls {
		if candidate == call {
			count++
		}
	}
	return count
}
