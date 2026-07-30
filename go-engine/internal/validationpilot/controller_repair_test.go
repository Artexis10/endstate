// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationpilot

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/validationharness"
	"github.com/Artexis10/endstate/go-engine/internal/validationmatrix"
)

func TestRunV1ProcessPreservesRawOutputForExternalEvidence(t *testing.T) {
	result := runV1Process(context.Background(), t.TempDir(), V1ChildCommand{Name: "cmd", Args: []string{"/c", "<nul set /p ={}& echo."}})
	if result.Infrastructure != "" || result.Rejected || result.Value != "{}" || result.RawValue != "{}\r\n" {
		t.Fatalf("runV1Process() = %#v, want separately preserved canonical bytes", result)
	}
}

func TestRunV1ExternalDetectorRequiresExactPersistedCanonicalBytes(t *testing.T) {
	repository := t.TempDir()
	module := filepath.Join(repository, "modules", "apps", "aida64")
	if err := os.MkdirAll(module, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, leaf := range []string{"module.jsonc", "validation.jsonc"} {
		raw, err := os.ReadFile(filepath.Join("..", "..", "..", "modules", "apps", "aida64", leaf))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(module, leaf), raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	attemptRoot := t.TempDir()
	tempRoot := filepath.Join(attemptRoot, "profile", "temp")
	if err := os.MkdirAll(tempRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	candidate := validV1CandidateForRepair(t)
	candidate.Target = V1Target{ModuleID: "apps.aida64", ScenarioID: "reviewed-capture-v1"}
	revision, err := v1LoadedModuleRevision(repository, candidate.Target)
	if err != nil {
		t.Fatal(err)
	}
	result := validationharness.Result{SchemaVersion: validationharness.ResultSchemaVersion, ModuleID: candidate.Target.ModuleID, ModuleRevision: revision, ScenarioID: candidate.Target.ScenarioID, Kind: validationmatrix.ScenarioCaptureContract, Status: validationharness.ResultStatusPassed, ProofLevels: []validationmatrix.ProofLevel{validationmatrix.ProofCatalog}, AssertionCounts: map[string]int{"capture": 1}, PhaseTimings: map[string]time.Duration{"capture": time.Millisecond}}
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw, '\n')
	var seen V1ChildCommand
	run := func(_ context.Context, directory string, command V1ChildCommand) V1ChildResult {
		seen = command
		if directory != filepath.Join(repository, "go-engine") {
			t.Fatalf("directory = %q", directory)
		}
		if err := os.WriteFile(command.Args[9], raw, 0o600); err != nil {
			t.Fatal(err)
		}
		return V1ChildResult{Value: string(raw[:len(raw)-1]), RawValue: string(raw)}
	}
	admission, status, _, _, infrastructure := runV1ExternalDetector(context.Background(), run, attemptRoot, filepath.Join(repository, "go-engine"), filepath.Join(attemptRoot, "endstate"), filepath.Join(attemptRoot, "endstate-validation"), repository, candidate, []string{"TEMP=" + tempRoot})
	if infrastructure != "" || admission != V1AdmissionAdmitted || status != V1StatusPassed {
		t.Fatalf("runV1ExternalDetector() = %q, %q, %q", admission, status, infrastructure)
	}
	if len(seen.Args) != 10 || seen.Args[8] != "--result" || !filepath.IsAbs(seen.Args[9]) || !v1StrictDescendant(tempRoot, seen.Args[9]) || filepath.Dir(seen.Args[9]) != filepath.Join(tempRoot, "validation-result") {
		t.Fatalf("detector command = %#v", seen)
	}
}

func TestRunV1ExternalDetectorTreatsMissingOrDisagreeingResultFileAsInfrastructure(t *testing.T) {
	for _, write := range []func(string, []byte) error{
		func(_ string, _ []byte) error { return nil },
		func(path string, raw []byte) error { return os.WriteFile(path, append(raw, ' '), 0o600) },
	} {
		repository, attemptRoot, candidate, raw := v1ExternalDetectorFixture(t)
		run := func(_ context.Context, _ string, command V1ChildCommand) V1ChildResult {
			if err := write(command.Args[9], raw); err != nil {
				t.Fatal(err)
			}
			return V1ChildResult{Value: strings.TrimSpace(string(raw)), RawValue: string(raw)}
		}
		_, _, _, _, infrastructure := runV1ExternalDetector(context.Background(), run, attemptRoot, filepath.Join(repository, "go-engine"), filepath.Join(attemptRoot, "endstate"), filepath.Join(attemptRoot, "endstate-validation"), repository, candidate, []string{"TEMP=" + filepath.Join(attemptRoot, "profile", "temp")})
		if infrastructure != "detector_evidence" {
			t.Fatalf("runV1ExternalDetector() infrastructure = %q, want detector_evidence", infrastructure)
		}
	}
}

func v1ExternalDetectorFixture(t *testing.T) (string, string, V1Candidate, []byte) {
	t.Helper()
	repository := t.TempDir()
	module := filepath.Join(repository, "modules", "apps", "aida64")
	if err := os.MkdirAll(module, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, leaf := range []string{"module.jsonc", "validation.jsonc"} {
		value, err := os.ReadFile(filepath.Join("..", "..", "..", "modules", "apps", "aida64", leaf))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(module, leaf), value, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	attemptRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(attemptRoot, "profile", "temp"), 0o700); err != nil {
		t.Fatal(err)
	}
	candidate := validV1CandidateForRepair(t)
	candidate.Target = V1Target{ModuleID: "apps.aida64", ScenarioID: "reviewed-capture-v1"}
	revision, err := v1LoadedModuleRevision(repository, candidate.Target)
	if err != nil {
		t.Fatal(err)
	}
	result := validationharness.Result{SchemaVersion: validationharness.ResultSchemaVersion, ModuleID: candidate.Target.ModuleID, ModuleRevision: revision, ScenarioID: candidate.Target.ScenarioID, Kind: validationmatrix.ScenarioCaptureContract, Status: validationharness.ResultStatusPassed, ProofLevels: []validationmatrix.ProofLevel{validationmatrix.ProofCatalog}, AssertionCounts: map[string]int{"capture": 1}, PhaseTimings: map[string]time.Duration{"capture": time.Millisecond}}
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	return repository, attemptRoot, candidate, append(raw, '\n')
}

func TestValidateV1ReviewRecordRejectsMissingTamperedAndForeignRecords(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	candidate := validV1CandidateForRepair(t)
	if err := validateV1ReviewRecord(root, candidate); err == nil {
		t.Fatal("validateV1ReviewRecord() accepted a missing record")
	}
	record := v1ReviewRecord{CandidateID: candidate.ID, PatchSHA256: candidate.PatchSHA256, OperatorFingerprint: candidate.OperatorFingerprint, InvariantFingerprint: candidate.InvariantFingerprint, Target: candidate.Target, ProductionFile: candidate.ProductionFile, Lifecycle: candidate.Lifecycle, Expected: candidate.Expected, Realistic: true, NonEquivalent: true, Disjoint: true, PatchScope: true, FailureIdentity: true, ProductionReachability: true, Ordering: true}
	writeV1ReviewRecord(t, root, &candidate, record)
	if err := validateV1ReviewRecord(root, candidate); err != nil {
		t.Fatalf("validateV1ReviewRecord(valid) = %v", err)
	}
	path := filepath.Join(root, filepath.FromSlash(V1CorpusRoot), "reviews", candidate.ID+".json")
	if err := os.WriteFile(path, []byte(`{"candidateId":"tampered"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := validateV1ReviewRecord(root, candidate); err == nil {
		t.Fatal("validateV1ReviewRecord() accepted tampered bytes")
	}
	record.CandidateID = "foreign-candidate"
	writeV1ReviewRecord(t, root, &candidate, record)
	if err := validateV1ReviewRecord(root, candidate); err == nil {
		t.Fatal("validateV1ReviewRecord() accepted a foreign identity")
	}
}

func TestValidateV1ReviewRecordRejectsFingerprintNegativeAndMalformedRecords(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	candidate := validV1CandidateForRepair(t)
	base := v1ReviewRecord{CandidateID: candidate.ID, PatchSHA256: candidate.PatchSHA256, OperatorFingerprint: candidate.OperatorFingerprint, InvariantFingerprint: candidate.InvariantFingerprint, Target: candidate.Target, ProductionFile: candidate.ProductionFile, Lifecycle: candidate.Lifecycle, Expected: candidate.Expected, Realistic: true, NonEquivalent: true, Disjoint: true, PatchScope: true, FailureIdentity: true, ProductionReachability: true, Ordering: true}
	for _, mutate := range []func(*v1ReviewRecord){
		func(record *v1ReviewRecord) { record.OperatorFingerprint = "foreign-operator" },
		func(record *v1ReviewRecord) { record.InvariantFingerprint = "foreign-invariant" },
		func(record *v1ReviewRecord) { record.Realistic = false },
		func(record *v1ReviewRecord) { record.NonEquivalent = false },
		func(record *v1ReviewRecord) { record.Disjoint = false },
		func(record *v1ReviewRecord) { record.PatchScope = false },
		func(record *v1ReviewRecord) { record.FailureIdentity = false },
		func(record *v1ReviewRecord) { record.ProductionReachability = false },
		func(record *v1ReviewRecord) { record.Ordering = false },
	} {
		record := base
		mutate(&record)
		writeV1ReviewRecord(t, root, &candidate, record)
		if err := validateV1ReviewRecord(root, candidate); err == nil {
			t.Fatal("validateV1ReviewRecord() accepted an invalid reviewed conclusion")
		}
	}
	for _, raw := range [][]byte{
		[]byte(`{"candidateId":"x","candidateId":"x"}`),
		[]byte(`{"candidateId":"x","unknown":true}`),
	} {
		path := filepath.Join(root, filepath.FromSlash(V1CorpusRoot), "reviews", candidate.ID+".json")
		if err := os.WriteFile(path, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		candidate.ReviewRecordSHA256 = v1RepositoryDigest(raw)
		if err := validateV1ReviewRecord(root, candidate); err == nil {
			t.Fatal("validateV1ReviewRecord() accepted malformed strict JSON")
		}
	}
}

func TestValidateV1ReviewRecordRejectsReviewDirectoryLink(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	reviews := filepath.Join(root, filepath.FromSlash(V1CorpusRoot), "reviews")
	if err := os.MkdirAll(filepath.Dir(reviews), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), reviews); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if err := validateV1ReviewRecord(root, validV1CandidateForRepair(t)); err == nil {
		t.Fatal("validateV1ReviewRecord() accepted a linked review directory")
	}
}

func writeV1ReviewRecord(t *testing.T, root string, candidate *V1Candidate, record v1ReviewRecord) {
	t.Helper()
	raw, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw, '\n')
	candidate.ReviewRecordSHA256 = v1RepositoryDigest(raw)
	path := filepath.Join(root, filepath.FromSlash(V1CorpusRoot), "reviews", candidate.ID+".json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

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

func TestV1AttemptEnvironmentReplacesTMPDIRAndPreservesItForNestedChild(t *testing.T) {
	runnerRoot := t.TempDir()
	attemptRoot := filepath.Join(runnerRoot, "attempt")
	goCache := filepath.Join(runnerRoot, "go-build")
	goModCache := filepath.Join(runnerRoot, "go-mod")
	for _, path := range []string{attemptRoot, goCache, goModCache} {
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	inherited := filepath.Join(t.TempDir(), "inherited-tmpdir")
	t.Setenv("TMPDIR", inherited)
	environment, err := v1AttemptEnvironment(attemptRoot, runnerRoot, goCache, goModCache)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(attemptRoot, "profile", "temp")
	if got := v1EnvironmentValue(environment, "TMPDIR"); got != want {
		t.Fatalf("TMPDIR = %q, want %q", got, want)
	}
	if got := v1EnvironmentValue(V1ChildEnvironment(environment), "TMPDIR"); got != want {
		t.Fatalf("nested TMPDIR = %q, want %q", got, want)
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
