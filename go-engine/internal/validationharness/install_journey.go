// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationharness

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/safepath"
	"github.com/Artexis10/endstate/go-engine/internal/validationmatrix"
	"github.com/Artexis10/endstate/go-engine/internal/validationmode"
)

type installJourneyExecutor interface {
	ApplyDryRun(context.Context, *scenarioRuntime) *Failure
	VerifyAbsent(context.Context, *scenarioRuntime) *Failure
	VerifyPresent(context.Context, *scenarioRuntime) *Failure
}

func executeInstallJourney(ctx context.Context, runtime *scenarioRuntime, executor installJourneyExecutor) Result {
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

	if failure := assertInstallVerifier(runtime, false); failure != nil {
		return failResult(failure)
	}
	if failure := timed("apply-dry-run", func() *Failure { return executor.ApplyDryRun(ctx, runtime) }); failure != nil {
		return failResult(failure)
	}
	result.AssertionCounts[validationmatrix.AssertionAppReferences] = 1
	if failure := assertInstallVerifier(runtime, false); failure != nil {
		return failResult(failure)
	}
	if failure := timed("verify-absent", func() *Failure { return executor.VerifyAbsent(ctx, runtime) }); failure != nil {
		return failResult(failure)
	}
	if failure := materializeInstallVerifier(runtime); failure != nil {
		return failResult(failure)
	}
	if failure := timed("verify-present", func() *Failure { return executor.VerifyPresent(ctx, runtime) }); failure != nil {
		return failResult(failure)
	}
	if failure := assertInstallVerifier(runtime, true); failure != nil {
		return failResult(failure)
	}
	result.AssertionCounts[validationmatrix.AssertionVerify] = len(runtime.InstallPlan.Verifiers)
	proof, failure := evaluateAssertions(runtime.Scenario, result.AssertionCounts, OperationCounts{Executed: 1}, []validationmatrix.ProofLevel{
		validationmatrix.ProofCatalog, validationmatrix.ProofEngineContract,
	})
	if failure != nil {
		return failResult(failure)
	}
	result.Status = ResultStatusPassed
	result.ProofLevels = proof
	return result
}

func assertInstallVerifier(runtime *scenarioRuntime, present bool) *Failure {
	if runtime == nil || runtime.InstallPlan == nil || len(runtime.InstallPlan.Verifiers) != 1 {
		return fail(CodeIsolationFailure, "install", "verify", "install verifier authority is absent")
	}
	verifier := runtime.InstallPlan.Verifiers[0]
	switch verifier.Type {
	case "command-exists":
		return assertInstallToolRoot(runtime, present)
	case "file-exists":
		path, failure := installFileVerifierPath(runtime)
		if failure != nil {
			return failure
		}
		info, err := os.Lstat(path)
		if !present {
			if os.IsNotExist(err) {
				return nil
			}
			return fail(CodeIsolationFailure, "install", "verify.path", "install file verifier was materialized before the negative verification")
		}
		if err != nil || safepath.IsLinkOrReparse(info) || !info.Mode().IsRegular() {
			return fail(CodeIsolationFailure, "install", "verify.path", "install file verifier is not a contained regular fixture")
		}
		return nil
	case "registry-key-exists":
		if runtime.RegistryFixture == nil {
			return fail(CodeIsolationFailure, "install", "verify.path", "install registry verifier fixture is absent")
		}
		if !present {
			if err := runtime.RegistryFixture.ProveAbsent(verifier.Path); err != nil {
				return fail(CodeIsolationFailure, "install", "verify.path", "install registry verifier was not absent before the negative verification")
			}
			return nil
		}
		if _, err := runtime.RegistryFixture.Snapshot(verifier.Path); err != nil {
			return fail(CodeIsolationFailure, "install", "verify.path", "install registry verifier was not materialized")
		}
		return nil
	default:
		return fail(CodeIsolationFailure, "install", "verify.type", "install verifier type is unsupported")
	}
}

func assertInstallToolRoot(runtime *scenarioRuntime, present bool) *Failure {
	if runtime == nil || runtime.InstallPlan == nil || runtime.ToolRoot == "" || !filepath.IsAbs(runtime.ToolRoot) {
		return fail(CodeIsolationFailure, "install", "toolRoot", "install verifier authority is absent")
	}
	if err := safepath.ValidateRoot(runtime.ToolRoot); err != nil {
		return fail(CodeIsolationFailure, "install", "toolRoot", "install verifier authority is unsafe")
	}
	entries, err := os.ReadDir(runtime.ToolRoot)
	if err != nil {
		return fail(CodeIsolationFailure, "install", "toolRoot", "install verifier authority cannot be inspected")
	}
	want := 0
	if present {
		want = 1
	}
	if len(entries) != want {
		return fail(CodeIsolationFailure, "install", "toolRoot", "install verifier authority has an unexpected member set")
	}
	if !present {
		return nil
	}
	if entries[0].Name() != runtime.InstallPlan.CommandExecutable {
		return fail(CodeIsolationFailure, "install", "toolRoot", "install verifier authority has a foreign executable")
	}
	info, err := os.Lstat(filepath.Join(runtime.ToolRoot, entries[0].Name()))
	if err != nil || safepath.IsLinkOrReparse(info) || !info.Mode().IsRegular() {
		return fail(CodeIsolationFailure, "install", "toolRoot", "install verifier executable is not a contained regular file")
	}
	return nil
}

func materializeInstallVerifier(runtime *scenarioRuntime) *Failure {
	if failure := assertInstallVerifier(runtime, false); failure != nil {
		return failure
	}
	verifier := runtime.InstallPlan.Verifiers[0]
	switch verifier.Type {
	case "command-exists":
		name := runtime.InstallPlan.CommandExecutable
		if filepath.Base(name) != name || filepath.Ext(name) == "" {
			return fail(CodeIsolationFailure, "install", "verify.command", "install verifier executable is not a contained name")
		}
		path := filepath.Join(runtime.ToolRoot, name)
		if !fixtureContained(runtime.ToolRoot, path) {
			return fail(CodeIsolationFailure, "install", "verify.command", "install verifier executable escaped ToolRoot")
		}
		if err := safepath.AtomicWriteFile(path, []byte("endstate validation command sentinel"), 0o700); err != nil {
			return fail(CodeIsolationFailure, "install", "verify.command", "install verifier executable could not be materialized")
		}
	case "file-exists":
		path, failure := installFileVerifierPath(runtime)
		if failure != nil {
			return failure
		}
		if failure := prepareFixtureFile(runtime.validationContext(), path, "verify[0].path"); failure != nil {
			return failure
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return fail(CodeIsolationFailure, "install", "verify[0].path", "install file verifier parent could not be materialized")
		}
		if failure := prepareFixtureFile(runtime.validationContext(), path, "verify[0].path"); failure != nil {
			return failure
		}
		if err := safepath.AtomicWriteFile(path, []byte("endstate validation file sentinel"), 0o600); err != nil {
			return fail(CodeIsolationFailure, "install", "verify[0].path", "install file verifier could not be materialized")
		}
	case "registry-key-exists":
		if runtime.RegistryFixture == nil {
			return fail(CodeIsolationFailure, "install", "verify[0].path", "install registry verifier fixture is absent")
		}
		if err := runtime.RegistryFixture.Materialize(verifier.Path); err != nil {
			return fail(CodeIsolationFailure, "install", "verify[0].path", "install registry verifier could not be materialized")
		}
	default:
		return fail(CodeIsolationFailure, "install", "verify.type", "install verifier type is unsupported")
	}
	return assertInstallVerifier(runtime, true)
}

func installFileVerifierPath(runtime *scenarioRuntime) (string, *Failure) {
	if runtime == nil || runtime.InstallPlan == nil || len(runtime.InstallPlan.Verifiers) != 1 || runtime.validationContext() == nil {
		return "", fail(CodeIsolationFailure, "install", "verify.path", "install file verifier validation authority is absent")
	}
	path, err := runtime.validationContext().ResolveHostPath(runtime.InstallPlan.Verifiers[0].Path, validationmode.HostPathPolicy{})
	if err != nil || runtime.validationContext().ValidateSandboxPath(path) != nil || !fixtureContained(runtime.Root, path) {
		return "", fail(CodeIsolationFailure, "install", "verify.path", "install file verifier escaped validation authority")
	}
	return path, nil
}
