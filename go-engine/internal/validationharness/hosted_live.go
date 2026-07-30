// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationharness

import (
	"context"
	"fmt"
	"time"
)

const hostedLiveCleanupTimeout = 2 * time.Minute

// hostedLiveRunner is deliberately package-private: only the hosted validator
// can bind its operations to the authority session and sealed receipts.
type hostedLiveRunner interface {
	validate(context.Context) error
	compile(context.Context) error
	initial(context.Context) error
	engineApply(context.Context) error
	observePresent(context.Context) error
	engineVerify(context.Context) error
	seed(context.Context) error
	snapshotSeed(context.Context) error
	engineCapture(context.Context) error
	inspectCapture(context.Context) error
	uninstall(context.Context) error
	wipe(context.Context) error
	observeAbsent(context.Context) error
	rebuild(context.Context) error
	inspectRebuild(context.Context) error
	revert(context.Context) error
	observeRetained(context.Context) error
	recovery(context.Context) error
	inspectRecovery(context.Context) error
	convergence(context.Context) error
	inspectConvergence(context.Context) error
	finalUninstall(context.Context) error
	finalWipe(context.Context) error
	attemptRootCleanup(context.Context) error
	finalBoundary(context.Context) error
}

type hostedLiveRunResult struct {
	phase        string
	err          error
	cleanupPhase string
	cleanupErr   error
	evidenceErr  error
	eligible     bool
	phases       []hostedLivePhaseRecord
}

// hostedLivePhaseRecord is the internal, typed account of the lifecycle. It
// deliberately records only closed phase names and completion facts; errors
// remain local to the runner and are never evidence input.
type hostedLivePhaseRecord struct {
	name                 string
	status               string
	durationMilliseconds int64
	assertions           int
}

var hostedLiveLifecycle = []string{
	"validate", "compile", "initial", "engine-apply", "observe-present", "engine-verify", "hash-bound-seed", "snapshot-seed", "engine-capture", "inspect-capture",
	"winget-exact-uninstall", "declared-target-wipe", "observe-absent", "engine-rebuild", "inspect-rebuild", "engine-revert", "observe-retained", "engine-rebuild", "inspect-recovery", "engine-rebuild", "inspect-convergence",
}

var hostedLiveCleanup = []string{"final-uninstall", "final-wipe", "attempt-root-cleanup", "final-boundary"}

func runHostedLive(ctx context.Context, runner hostedLiveRunner) hostedLiveRunResult {
	return runHostedLiveWithClock(ctx, runner, time.Now)
}

func runHostedLiveWithClock(ctx context.Context, runner hostedLiveRunner, now func() time.Time) hostedLiveRunResult {
	if runner == nil {
		return hostedLiveRunResult{phase: "validate", err: fmt.Errorf("hosted live runner is unavailable")}
	}
	if now == nil {
		return hostedLiveRunResult{phase: "validate", err: fmt.Errorf("hosted live clock is unavailable")}
	}
	var result hostedLiveRunResult
	run := func(phase string, action func(context.Context) error) {
		if result.err != nil {
			result.phases = append(result.phases, hostedLivePhaseRecord{name: phase, status: "skipped"})
			return
		}
		started := now()
		err := action(ctx)
		elapsed := now().Sub(started).Milliseconds()
		if elapsed < 0 {
			elapsed = 0
		}
		record := hostedLivePhaseRecord{name: phase, status: "passed", durationMilliseconds: elapsed, assertions: 1}
		if err != nil {
			record.status = "failed"
			result.phase, result.err = phase, fmt.Errorf("hosted live %s failed", phase)
		}
		result.phases = append(result.phases, record)
	}
	run("validate", runner.validate)
	run("compile", runner.compile)
	run("initial", runner.initial)
	run("engine-apply", runner.engineApply)
	run("observe-present", runner.observePresent)
	run("engine-verify", runner.engineVerify)
	run("hash-bound-seed", runner.seed)
	run("snapshot-seed", runner.snapshotSeed)
	run("engine-capture", runner.engineCapture)
	run("inspect-capture", runner.inspectCapture)
	run("winget-exact-uninstall", runner.uninstall)
	run("declared-target-wipe", runner.wipe)
	run("observe-absent", runner.observeAbsent)
	run("engine-rebuild", runner.rebuild)
	run("inspect-rebuild", runner.inspectRebuild)
	run("engine-revert", runner.revert)
	run("observe-retained", runner.observeRetained)
	run("engine-rebuild", runner.recovery)
	run("inspect-recovery", runner.inspectRecovery)
	run("engine-rebuild", runner.convergence)
	run("inspect-convergence", runner.inspectConvergence)
	cleanupContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), hostedLiveCleanupTimeout)
	defer cancel()
	cleanup := func(phase string, action func(context.Context) error) {
		started := now()
		err := action(cleanupContext)
		elapsed := now().Sub(started).Milliseconds()
		if elapsed < 0 {
			elapsed = 0
		}
		record := hostedLivePhaseRecord{name: phase, status: "passed", durationMilliseconds: elapsed, assertions: 1}
		if err != nil {
			record.status = "failed"
			cleanupErr := fmt.Errorf("hosted live cleanup %s failed", phase)
			if result.cleanupErr == nil {
				result.cleanupPhase, result.cleanupErr = phase, cleanupErr
			}
			if result.err == nil {
				result.phase, result.err = phase, cleanupErr
			}
		}
		result.phases = append(result.phases, record)
	}
	cleanup("final-uninstall", runner.finalUninstall)
	cleanup("final-wipe", runner.finalWipe)
	cleanup("attempt-root-cleanup", runner.attemptRootCleanup)
	cleanup("final-boundary", runner.finalBoundary)
	if result.err != nil {
		return result
	}
	result.eligible = true
	return result
}
