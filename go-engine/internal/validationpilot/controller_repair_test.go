// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationpilot

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/catalogplan"
	"github.com/Artexis10/endstate/go-engine/internal/validationharness"
	"github.com/Artexis10/endstate/go-engine/internal/validationmatrix"
)

func TestV1MeasuredToolchainRejectsMismatchedPatch(t *testing.T) {
	toolchain, err := measureV1Toolchain(context.Background(), func(_ context.Context, _ string, command V1ChildCommand) V1ChildResult {
		if command.Name != "go" || len(command.Args) != 2 || command.Args[0] != "env" || command.Args[1] != "GOVERSION" {
			t.Fatalf("probe command = %#v", command)
		}
		return V1ChildResult{Value: "go1.26.2"}
	}, t.TempDir(), nil, "go1.26.1")
	if err == nil || toolchain != "" {
		t.Fatalf("measureV1Toolchain() = %q, %v, want mismatch rejection", toolchain, err)
	}
}

func TestV1RunnerImageComesFromHostedMetadata(t *testing.T) {
	runner, err := v1HostedRunner("windows", "win25", "20260728.1")
	if err != nil || runner.Image != "win25-20260728.1" {
		t.Fatalf("v1HostedRunner() = %#v, %v", runner, err)
	}
	for _, input := range [][3]string{{"windows", "", "20260728.1"}, {"windows", "ubuntu24", "20260728.1"}, {"linux", "win25", "20260728.1"}} {
		if _, err := v1HostedRunner(input[0], input[1], input[2]); err == nil {
			t.Fatalf("v1HostedRunner(%q, %q, %q) accepted spoofed metadata", input[0], input[1], input[2])
		}
	}
}

func TestV1RepositoryDigestBindsCanonicalTreeContent(t *testing.T) {
	first := v1RepositoryDigest([]byte("100644 blob a\tmodules/a\n"))
	second := v1RepositoryDigest([]byte("100644 blob b\tmodules/a\n"))
	if first == second {
		t.Fatal("v1RepositoryDigest() did not change with canonical tree content")
	}
}

func TestV1BaselineProofBindsTypedResultFields(t *testing.T) {
	target := V1Target{ModuleID: "apps.module-d", ScenarioID: "scenario-d"}
	first := v1ModuleBaselineProof(validationharness.Result{Status: validationharness.ResultStatusPassed, ModuleID: target.ModuleID, ScenarioID: target.ScenarioID, ProofLevels: []validationmatrix.ProofLevel{validationmatrix.ProofCatalog}, AssertionCounts: map[string]int{"content": 1}}, target)
	second := v1ModuleBaselineProof(validationharness.Result{Status: validationharness.ResultStatusPassed, ModuleID: target.ModuleID, ScenarioID: target.ScenarioID, ProofLevels: []validationmatrix.ProofLevel{validationmatrix.ProofCatalog}, AssertionCounts: map[string]int{"content": 2}}, target)
	if first == second {
		t.Fatal("v1ModuleBaselineProof() serialized distinct typed proofs identically")
	}
}

func TestV1MeasuredRepositoryIdentityIsRepeatabilityGoverning(t *testing.T) {
	manifest, evidence := validV1Proof()
	evidence.Attempts[3].RepositorySHA256 = "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	aggregate, err := ClassifyV1(manifest, evidence)
	if err != nil || aggregate.Rows[0].Classification != ClassificationFlake {
		t.Fatalf("ClassifyV1(repository drift) = %#v, %v", aggregate, err)
	}
}

func TestV1InfrastructureEvidenceAllowsNoUnmeasuredRepositoryIdentity(t *testing.T) {
	_, evidence := validV1Proof()
	evidence.Attempts[0].Status = V1StatusInfrastructure
	evidence.Attempts[0].RepositorySHA256 = ""
	evidence.Attempts[0].BaselineProof = V1BaselineProofIdentity{}
	if _, _, err := EncodeV1Evidence(evidence); err != nil {
		t.Fatalf("EncodeV1Evidence(infrastructure) = %v", err)
	}
}

func TestV1CatalogFailureUsesOnlyExactTargetRow(t *testing.T) {
	candidate := V1Candidate{Target: V1Target{BundleID: "bundle-b", RowID: "bundle-b"}}
	result := validationharness.CatalogMatrixResult{Rows: []validationharness.CatalogMatrixRow{
		{BundleID: "bundle-a", Status: validationharness.ResultStatusFailed, Failures: []catalogplan.Failure{{Reason: "foreign_failure"}}},
		{BundleID: "bundle-b", Status: validationharness.ResultStatusFailed, Failures: []catalogplan.Failure{{Reason: "target_failure"}}},
	}}
	failure, err := v1CatalogFailure(candidate, result)
	if err != nil || failure.ChildReason != "target_failure" {
		t.Fatalf("v1CatalogFailure() = %#v, %v", failure, err)
	}
	for _, rows := range [][]validationharness.CatalogMatrixRow{
		{{BundleID: "bundle-a"}},
		{{BundleID: "bundle-b"}, {BundleID: "bundle-b"}},
	} {
		if _, err := v1CatalogFailure(candidate, validationharness.CatalogMatrixResult{Rows: rows}); err == nil {
			t.Fatal("v1CatalogFailure() accepted a missing or duplicate target row")
		}
	}
}

func TestV1ModuleAdmissionAndGuardFailuresCannotClaimDomainCredit(t *testing.T) {
	candidate := V1Candidate{Target: V1Target{ModuleID: "apps.module-d", ScenarioID: "scenario-d"}}
	foreign := validationharness.Result{Status: validationharness.ResultStatusFailed, ModuleID: "apps.foreign", ScenarioID: candidate.Target.ScenarioID}
	if _, _, err := v1ModuleDetectorResult(candidate, foreign); err == nil {
		t.Fatal("v1ModuleDetectorResult() accepted a foreign module result")
	}
	guard := validationharness.Result{Status: validationharness.ResultStatusFailed, ModuleID: candidate.Target.ModuleID, ScenarioID: candidate.Target.ScenarioID, Failure: &validationharness.Failure{Code: validationharness.CodeIsolationFailure, Phase: "isolation", Coordinate: "root"}}
	admission, failure, err := v1ModuleDetectorResult(candidate, guard)
	if err != nil || admission != V1AdmissionRejected || failure.Scope != V1FailureScopeGuard {
		t.Fatalf("v1ModuleDetectorResult(guard) = %q, %#v, %v", admission, failure, err)
	}
}

func TestV1RunnerRootRejectsEqualTraversalAndSymlinkResultRoots(t *testing.T) {
	runnerRoot := t.TempDir()
	if err := prepareV1ResultRoot(runnerRoot, runnerRoot); err == nil {
		t.Fatal("prepareV1ResultRoot() accepted the runner root itself")
	}
	if err := prepareV1ResultRoot(runnerRoot, filepath.Join(runnerRoot, "..", "escape")); err == nil {
		t.Fatal("prepareV1ResultRoot() accepted a noncanonical traversal path")
	}
	link := filepath.Join(runnerRoot, "link")
	if err := os.Symlink(t.TempDir(), link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if err := prepareV1ResultRoot(runnerRoot, filepath.Join(link, "results")); err == nil {
		t.Fatal("prepareV1ResultRoot() accepted a symlink escape")
	}
}

func TestPrepareV1AuthorityGraphKeepsFullDispatchAncestry(t *testing.T) {
	repository := t.TempDir()
	runV1TestGit(t, repository, "init")
	runV1TestGit(t, repository, "-c", "user.name=test", "-c", "user.email=test@example.invalid", "commit", "--allow-empty", "-m", "freeze")
	freeze := strings.TrimSpace(runV1TestGit(t, repository, "rev-parse", "HEAD"))
	runV1TestGit(t, repository, "-c", "user.name=test", "-c", "user.email=test@example.invalid", "commit", "--allow-empty", "-m", "dispatch")
	dispatch := strings.TrimSpace(runV1TestGit(t, repository, "rev-parse", "HEAD"))
	if err := prepareV1AuthorityGraph(repository); err != nil {
		t.Fatal(err)
	}
	if err := exec.Command("git", "-C", repository, "merge-base", "--is-ancestor", freeze, dispatch).Run(); err != nil {
		t.Fatalf("freeze is not recognized as dispatch ancestor: %v", err)
	}
}

func TestPrepareV1AuthorityGraphRejectsShallowDispatch(t *testing.T) {
	repository := t.TempDir()
	runV1TestGit(t, repository, "init")
	runV1TestGit(t, repository, "-c", "user.name=test", "-c", "user.email=test@example.invalid", "commit", "--allow-empty", "-m", "dispatch")
	dispatch := strings.TrimSpace(runV1TestGit(t, repository, "rev-parse", "HEAD"))
	if err := os.WriteFile(filepath.Join(repository, ".git", "shallow"), []byte(dispatch+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := prepareV1AuthorityGraph(repository); err == nil {
		t.Fatal("prepareV1AuthorityGraph() accepted a shallow dispatch")
	}
}

func TestV1AttemptEnvironmentPreservesSafeSharedJobCaches(t *testing.T) {
	runnerRoot := t.TempDir()
	attemptRoot := filepath.Join(runnerRoot, "attempt")
	goCache := filepath.Join(runnerRoot, "go-build")
	goModCache := filepath.Join(runnerRoot, "go-mod")
	for _, path := range []string{attemptRoot, goCache, goModCache} {
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	environment, err := v1AttemptEnvironment(attemptRoot, runnerRoot, goCache, goModCache)
	if err != nil {
		t.Fatal(err)
	}
	if got := v1EnvironmentValue(environment, "GOCACHE"); got != goCache {
		t.Fatalf("GOCACHE = %q, want %q", got, goCache)
	}
	if got := v1EnvironmentValue(environment, "GOMODCACHE"); got != goModCache {
		t.Fatalf("GOMODCACHE = %q, want %q", got, goModCache)
	}
	if _, err := v1AttemptEnvironment(attemptRoot, runnerRoot, filepath.Join(runnerRoot, "missing"), goModCache); err == nil {
		t.Fatal("v1AttemptEnvironment() accepted a missing shared cache")
	}
	if _, err := v1AttemptEnvironment(attemptRoot, runnerRoot, t.TempDir(), goModCache); err == nil {
		t.Fatal("v1AttemptEnvironment() accepted a cache outside the runner root")
	}
}

func runV1TestGit(t *testing.T, directory string, arguments ...string) string {
	t.Helper()
	output, err := exec.Command("git", append([]string{"-C", directory}, arguments...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", arguments, err, output)
	}
	return string(output)
}
