// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationharness

import (
	"context"
	"path/filepath"
	"strings"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/validationmatrix"
	"github.com/Artexis10/endstate/go-engine/internal/validationmode"
)

type scenarioRuntime struct {
	Module              *modules.Module
	Scenario            validationmatrix.Scenario
	Plan                *FixturePlan
	V2Plan              *V2FixturePlan
	InstallPlan         *InstallContractPlan
	CapturePlan         *CaptureContractPlan
	RestorePlan         *RestoreContractPlan
	V2Transition        *v2VersionTransition
	AuthorityRoot       string
	Root                string
	GuardRoot           string
	ChildWorkingDir     string
	Nonce               string
	Inventory           validationmode.Inventory
	Guards              []guardTarget
	ToolRoot            string
	OriginalEnvironment map[string]string
	repositoryRoot      string
	enginePath          string
	repositoryBoundary  boundaryTree
	guardBoundary       boundaryTree
	workingBoundary     boundaryTree
	engineBoundary      boundaryEntry
}

func (runtime *scenarioRuntime) forbiddenOutputValues() []string {
	if runtime == nil {
		return nil
	}
	seen := map[string]struct{}{}
	result := []string{}
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		if filepath.IsAbs(value) {
			value = filepath.Clean(value)
		} else if len(value) < 16 {
			return
		}
		key := strings.ToLower(value)
		if _, duplicate := seen[key]; duplicate {
			return
		}
		seen[key] = struct{}{}
		result = append(result, value)
	}
	for _, value := range []string{
		runtime.AuthorityRoot, runtime.Root, runtime.GuardRoot, runtime.ChildWorkingDir, runtime.ToolRoot,
		runtime.repositoryRoot, runtime.enginePath, runtime.Nonce,
		filepath.Join(runtime.Root, "manifests", "captured.jsonc"), captureArtifactPath(runtime.Root, "captured"), filepath.Join(runtime.Root, "manifests", "captured.zip"),
		filepath.Join(runtime.Root, "manifests", "captured-verify.jsonc"), filepath.Join(runtime.Root, "manifests", "optional-absent.jsonc"),
		filepath.Join(runtime.Root, "manifests", "optional-absent.zip"), filepath.Join(runtime.Root, "state", "backups"),
		filepath.Join(runtime.Root, "state", "config-restore"), filepath.Join(runtime.Root, "logs"),
	} {
		add(value)
	}
	for _, guard := range runtime.Guards {
		add(guard.Path)
		add(guard.Content)
	}
	if runtime.Plan != nil {
		for _, target := range runtime.Plan.Targets {
			for _, value := range []string{target.Resolved, target.PayloadPath, target.Captured, target.Mutated} {
				add(value)
			}
			all := append(append(append([]FixtureExcluded(nil), target.CaptureExcluded...), target.RestoreExcluded...), target.OverlappingExcluded...)
			for _, excluded := range all {
				for _, value := range []string{excluded.Path, excluded.Captured, excluded.Mutated} {
					add(value)
				}
			}
		}
	}
	if runtime.V2Plan != nil {
		for _, target := range append(append([]V2FixtureTarget(nil), runtime.V2Plan.CaptureTargets...), runtime.V2Plan.Targets...) {
			add(target.Resolved)
			for _, member := range target.Members {
				add(member.Path)
				add(string(member.Captured))
				add(string(member.Mutated))
			}
			for _, excluded := range target.Excluded {
				add(excluded.Path)
				add(string(excluded.Captured))
				add(string(excluded.Mutated))
			}
		}
	}
	if runtime.CapturePlan != nil {
		for _, target := range runtime.CapturePlan.Targets {
			add(target.Resolved)
		}
	}
	if runtime.RestorePlan != nil {
		add(runtime.RestorePlan.PayloadPath)
	}
	return result
}

func (runtime *scenarioRuntime) validationContext() *validationmode.Context {
	if runtime == nil {
		return nil
	}
	if runtime.Plan != nil {
		return runtime.Plan.context
	}
	if runtime.V2Plan != nil {
		return runtime.V2Plan.context
	}
	if runtime.InstallPlan != nil {
		return runtime.InstallPlan.context
	}
	if runtime.CapturePlan != nil {
		return runtime.CapturePlan.context
	}
	if runtime.RestorePlan != nil {
		return runtime.RestorePlan.context
	}
	return nil
}

type captureEvidence struct {
	ArtifactPath    string
	VerifyManifest  string
	AssertionCounts map[string]int
	V2              *v2CaptureEvidence
}

type journeyExecutor interface {
	Capture(context.Context, *scenarioRuntime) (captureEvidence, *Failure)
	CaptureOptionalAbsent(context.Context, *scenarioRuntime) *Failure
	Rebuild(context.Context, *scenarioRuntime, captureEvidence) *Failure
	Revert(context.Context, *scenarioRuntime) *Failure
	Verify(context.Context, *scenarioRuntime, captureEvidence) (int, *Failure)
}

func executeJourney(ctx context.Context, runtime *scenarioRuntime, executor journeyExecutor) Result {
	result := Result{
		SchemaVersion: ResultSchemaVersion, ModuleID: runtime.Module.ID, ModuleRevision: runtime.Module.Revision,
		ScenarioID: runtime.Scenario.ID, Kind: runtime.Scenario.Mode, Status: ResultStatusFailed,
		ProofLevels: []validationmatrix.ProofLevel{}, AssertionCounts: map[string]int{},
		PhaseTimings: map[string]time.Duration{},
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

	if failure := timed("fixture", runtime.Plan.MaterializeCaptured); failure != nil {
		return failResult(failure)
	}
	var evidence captureEvidence
	if failure := timed("capture", func() *Failure {
		var failure *Failure
		evidence, failure = executor.Capture(ctx, runtime)
		return failure
	}); failure != nil {
		return failResult(failure)
	}
	for name, count := range evidence.AssertionCounts {
		result.AssertionCounts[name] += count
	}
	if runtime.Plan.HasOptionalTargets() {
		if failure := timed("optional-capture", func() *Failure {
			if failure := runtime.Plan.MaterializeOptionalAbsent(); failure != nil {
				return failure
			}
			return executor.CaptureOptionalAbsent(ctx, runtime)
		}); failure != nil {
			return failResult(failure)
		}
	}
	if failure := timed("mutation", runtime.Plan.Mutate); failure != nil {
		return failResult(failure)
	}

	for iteration := 0; iteration < 3; iteration++ {
		phase := "rebuild"
		if iteration == 1 {
			phase = "recovery-rebuild"
		} else if iteration == 2 {
			phase = "repeat-rebuild"
		}
		if failure := timed(phase, func() *Failure { return executor.Rebuild(ctx, runtime, evidence) }); failure != nil {
			return failResult(failure)
		}
		if failure := runtime.Plan.CompareCaptured(); failure != nil {
			return failResult(failure)
		}
		result.AssertionCounts[validationmatrix.AssertionContent] += len(runtime.Plan.Targets)
		result.AssertionCounts[validationmatrix.AssertionRebuild]++
		result.AssertionCounts[validationmatrix.AssertionNestedSummary]++
		if iteration == 0 {
			if failure := timed("revert", func() *Failure { return executor.Revert(ctx, runtime) }); failure != nil {
				return failResult(failure)
			}
			if failure := runtime.Plan.CompareMutated(); failure != nil {
				return failResult(fail(CodeRevertFailure, "revert", failure.Coordinate, "revert did not restore the exact pre-rebuild fixture state"))
			}
			result.AssertionCounts[validationmatrix.AssertionRevert]++
		}
	}

	var verifyCount int
	if failure := timed("verify", func() *Failure {
		var failure *Failure
		verifyCount, failure = executor.Verify(ctx, runtime, evidence)
		return failure
	}); failure != nil {
		return failResult(failure)
	}
	if verifyCount > 0 {
		result.AssertionCounts[validationmatrix.AssertionVerify] += verifyCount
	}
	proof, failure := evaluateAssertions(runtime.Scenario, result.AssertionCounts,
		OperationCounts{Executed: len(runtime.Plan.Targets)},
		[]validationmatrix.ProofLevel{validationmatrix.ProofCatalog, validationmatrix.ProofEngineContract, validationmatrix.ProofConfigRoundtripV1})
	if failure != nil {
		return failResult(failure)
	}
	result.Status = ResultStatusPassed
	result.ProofLevels = proof
	return result
}
