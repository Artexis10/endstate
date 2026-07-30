// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationharness

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

var errHostedLiveTest = errors.New("hosted live test failure")

func TestHostedLiveRunUsesExactLifecycleOrderAndAlwaysCleansUp(t *testing.T) {
	runner := &fakeHostedLiveRunner{}
	result := runHostedLive(context.Background(), runner)
	if result.err != nil {
		t.Fatalf("runHostedLive() error = %v", result.err)
	}
	want := []string{
		"validate", "compile", "initial", "engine-apply", "observe-present", "engine-verify", "hash-bound-seed", "snapshot-seed", "engine-capture", "inspect-capture",
		"winget-exact-uninstall", "declared-target-wipe", "observe-absent", "engine-rebuild", "inspect-rebuild", "engine-revert", "observe-retained", "engine-rebuild", "inspect-recovery", "engine-rebuild", "inspect-convergence",
		"final-uninstall", "final-wipe", "attempt-root-cleanup", "final-boundary",
	}
	if !reflect.DeepEqual(runner.calls, want) {
		t.Fatalf("lifecycle calls = %#v, want %#v", runner.calls, want)
	}
}

func TestHostedLiveRunCleansUpAfterEveryFailure(t *testing.T) {
	for _, phase := range hostedLiveLifecycle {
		t.Run(phase, func(t *testing.T) {
			runner := &fakeHostedLiveRunner{fail: phase}
			result := runHostedLive(context.Background(), runner)
			if result.err == nil {
				t.Fatal("runHostedLive() error = nil, want failure")
			}
			wantSuffix := []string{"final-uninstall", "final-wipe", "attempt-root-cleanup", "final-boundary"}
			if len(runner.calls) < len(wantSuffix) || !reflect.DeepEqual(runner.calls[len(runner.calls)-len(wantSuffix):], wantSuffix) {
				t.Fatalf("calls = %#v, want cleanup suffix %#v", runner.calls, wantSuffix)
			}
		})
	}
}

func TestHostedLiveRunCleanupFailureOverridesSuccess(t *testing.T) {
	runner := &fakeHostedLiveRunner{fail: "final-boundary"}
	result := runHostedLive(context.Background(), runner)
	if result.err == nil || result.phase != "final-boundary" || result.eligible {
		t.Fatalf("result = %#v, want ineligible cleanup failure", result)
	}
}

func TestHostedLiveRunRetainsLifecycleFailureAndRecordsEveryCleanupFailure(t *testing.T) {
	for _, cleanup := range hostedLiveCleanup {
		t.Run(cleanup, func(t *testing.T) {
			runner := &fakeHostedLiveRunner{fail: "engine-apply", cleanupFail: cleanup}
			result := runHostedLive(context.Background(), runner)
			if result.err == nil || result.phase != "engine-apply" || result.eligible {
				t.Fatalf("result = %#v, want retained ineligible lifecycle failure", result)
			}
			if result.cleanupPhase != cleanup || result.cleanupErr == nil {
				t.Fatalf("result = %#v, want sanitized %q cleanup failure", result, cleanup)
			}
		})
	}
}

func TestHostedLiveRunUsesFreshBoundedContextForCleanup(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	runner := &fakeHostedLiveRunner{}
	_ = runHostedLive(ctx, runner)
	if runner.cleanupCanceled {
		t.Fatal("cleanup received the canceled lifecycle context")
	}
}

func TestHostedLiveRunRecordsExactPhaseTruthWithDeterministicDurations(t *testing.T) {
	clock := hostedLiveTestClock(time.Unix(0, 0), 7*time.Millisecond)
	result := runHostedLiveWithClock(context.Background(), &fakeHostedLiveRunner{fail: "engine-apply"}, clock)
	if len(result.phases) != len(hostedLiveLifecycle)+len(hostedLiveCleanup) {
		t.Fatalf("phase records = %d, want %d", len(result.phases), len(hostedLiveLifecycle)+len(hostedLiveCleanup))
	}
	expected := append(append([]string(nil), hostedLiveLifecycle...), hostedLiveCleanup...)
	for index, phase := range result.phases {
		if phase.name != expected[index] {
			t.Fatalf("phase[%d].name = %q, want %q", index, phase.name, expected[index])
		}
		if index < 4 || index >= len(hostedLiveLifecycle) {
			status := "passed"
			if index == 3 {
				status = "failed"
			}
			if phase.status != status || phase.durationMilliseconds != 7 || phase.assertions != 1 {
				t.Fatalf("phase[%d] = %#v, want %s/7ms/1", index, phase, status)
			}
			continue
		}
		if phase.status != "skipped" || phase.durationMilliseconds != 0 || phase.assertions != 0 {
			t.Fatalf("phase[%d] = %#v, want skipped/0ms/0", index, phase)
		}
	}
}

func TestHostedLiveRunRecordsCleanupFailureAfterLifecycleFailure(t *testing.T) {
	result := runHostedLiveWithClock(context.Background(), &fakeHostedLiveRunner{fail: "engine-apply", cleanupFail: "final-wipe"}, hostedLiveTestClock(time.Unix(0, 0), time.Millisecond))
	if result.phase != "engine-apply" || result.cleanupPhase != "final-wipe" || result.eligible {
		t.Fatalf("result = %#v, want retained lifecycle and cleanup failure", result)
	}
	if got := result.phases[len(hostedLiveLifecycle)+1]; got.status != "failed" || got.assertions != 1 {
		t.Fatalf("final-wipe record = %#v, want failed executed cleanup", got)
	}
}

func TestNewHostedLiveRunnerRejectsMissingAuthority(t *testing.T) {
	if _, err := newHostedLiveRunner(nil, LiveDefinition{}, "", ""); err == nil {
		t.Fatal("newHostedLiveRunner() accepted missing authority and roots")
	}
}

type fakeHostedLiveRunner struct {
	calls           []string
	fail            string
	cleanupFail     string
	phaseErr        error
	cleanupCanceled bool
}

func (runner *fakeHostedLiveRunner) cleanup(ctx context.Context, phase string) error {
	if ctx.Err() != nil {
		runner.cleanupCanceled = true
	}
	err := runner.phase(phase)
	if runner.cleanupFail == phase {
		if runner.phaseErr != nil {
			return runner.phaseErr
		}
		return errHostedLiveTest
	}
	return err
}

func (runner *fakeHostedLiveRunner) phase(phase string) error {
	runner.calls = append(runner.calls, phase)
	if runner.fail == phase {
		if runner.phaseErr != nil {
			return runner.phaseErr
		}
		return errHostedLiveTest
	}
	return nil
}

func (runner *fakeHostedLiveRunner) validate(context.Context) error { return runner.phase("validate") }
func (runner *fakeHostedLiveRunner) compile(context.Context) error  { return runner.phase("compile") }
func (runner *fakeHostedLiveRunner) initial(context.Context) error  { return runner.phase("initial") }
func (runner *fakeHostedLiveRunner) engineApply(context.Context) error {
	return runner.phase("engine-apply")
}
func (runner *fakeHostedLiveRunner) observePresent(context.Context) error {
	return runner.phase("observe-present")
}
func (runner *fakeHostedLiveRunner) engineVerify(context.Context) error {
	return runner.phase("engine-verify")
}
func (runner *fakeHostedLiveRunner) seed(context.Context) error {
	return runner.phase("hash-bound-seed")
}
func (runner *fakeHostedLiveRunner) snapshotSeed(context.Context) error {
	return runner.phase("snapshot-seed")
}
func (runner *fakeHostedLiveRunner) engineCapture(context.Context) error {
	return runner.phase("engine-capture")
}
func (runner *fakeHostedLiveRunner) inspectCapture(context.Context) error {
	return runner.phase("inspect-capture")
}
func (runner *fakeHostedLiveRunner) uninstall(context.Context) error {
	return runner.phase("winget-exact-uninstall")
}
func (runner *fakeHostedLiveRunner) wipe(context.Context) error {
	return runner.phase("declared-target-wipe")
}
func (runner *fakeHostedLiveRunner) observeAbsent(context.Context) error {
	return runner.phase("observe-absent")
}
func (runner *fakeHostedLiveRunner) rebuild(context.Context) error {
	return runner.phase("engine-rebuild")
}
func (runner *fakeHostedLiveRunner) inspectRebuild(context.Context) error {
	return runner.phase("inspect-rebuild")
}
func (runner *fakeHostedLiveRunner) revert(context.Context) error {
	return runner.phase("engine-revert")
}
func (runner *fakeHostedLiveRunner) observeRetained(context.Context) error {
	return runner.phase("observe-retained")
}
func (runner *fakeHostedLiveRunner) recovery(context.Context) error {
	return runner.phase("engine-rebuild")
}
func (runner *fakeHostedLiveRunner) inspectRecovery(context.Context) error {
	return runner.phase("inspect-recovery")
}
func (runner *fakeHostedLiveRunner) convergence(context.Context) error {
	return runner.phase("engine-rebuild")
}
func (runner *fakeHostedLiveRunner) inspectConvergence(context.Context) error {
	return runner.phase("inspect-convergence")
}
func (runner *fakeHostedLiveRunner) finalUninstall(ctx context.Context) error {
	return runner.cleanup(ctx, "final-uninstall")
}
func (runner *fakeHostedLiveRunner) finalWipe(ctx context.Context) error {
	return runner.cleanup(ctx, "final-wipe")
}
func (runner *fakeHostedLiveRunner) attemptRootCleanup(ctx context.Context) error {
	return runner.cleanup(ctx, "attempt-root-cleanup")
}
func (runner *fakeHostedLiveRunner) finalBoundary(ctx context.Context) error {
	return runner.cleanup(ctx, "final-boundary")
}

func hostedLiveTestClock(start time.Time, step time.Duration) func() time.Time {
	now := start
	return func() time.Time {
		value := now
		now = now.Add(step)
		return value
	}
}
