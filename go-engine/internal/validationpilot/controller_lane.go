// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationpilot

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/validationaudit"
	"github.com/Artexis10/endstate/go-engine/internal/validationharness"
	"github.com/Artexis10/endstate/go-engine/internal/validationmatrix"
)

const (
	v1RepositoryURL  = "https://github.com/Artexis10/endstate.git"
	v1CommandLimit   = 256 * 1024
	v1CommandTimeout = 10 * time.Minute
)

type V1LaneRequest struct {
	Root       string
	RunnerRoot string
	GoCache    string
	GoModCache string
	Manifest   V1Manifest
	Role       string
	Lane       string
	Runner     V1Runner
	ResultRoot string
	Run        V1ProcessRunner
}

type V1ProcessRunner func(context.Context, string, V1ChildCommand) V1ChildResult

// RunV1Lane owns the complete fixed lane inventory. It never accepts a
// caller-provided command, patch path, ref, or publication leaf.
func RunV1Lane(ctx context.Context, request V1LaneRequest) error {
	if err := validateV1LaneRequest(request); err != nil {
		return err
	}
	if _, err := ValidateV1Repository(request.Root, filepath.Join(request.Root, filepath.FromSlash(V1CorpusRoot), "manifest.json")); err != nil {
		return err
	}
	if err := prepareV1RunnerRoot(request.RunnerRoot); err != nil {
		return err
	}
	if err := prepareV1ResultRoot(request.RunnerRoot, request.ResultRoot); err != nil {
		return err
	}
	run := request.Run
	if run == nil {
		run = runV1Process
	}
	evidence := V1Evidence{SchemaVersion: V1SchemaVersion}
	for _, candidate := range request.Manifest.Candidates {
		for _, repetition := range v1RoleRepetitions(request.Role) {
			attempt := runV1Attempt(ctx, request, run, candidate, repetition)
			evidence.Attempts = append(evidence.Attempts, attempt)
		}
	}
	if _, _, err := EncodeV1Evidence(evidence); err != nil {
		return err
	}
	return writeV1EvidenceNew(filepath.Join(request.ResultRoot, v1EvidenceLeaf(request.Role, request.Lane)), evidence)
}

func validateV1LaneRequest(request V1LaneRequest) error {
	if !validV1Manifest(request.Manifest) || !validV1Runner(request.Runner) || request.GoCache == "" || request.GoModCache == "" || !filepath.IsAbs(request.RunnerRoot) || filepath.Clean(request.RunnerRoot) != request.RunnerRoot || !filepath.IsAbs(request.ResultRoot) || filepath.Clean(request.ResultRoot) != request.ResultRoot {
		return errors.New("invalid v1 lane request")
	}
	switch request.Role {
	case V1KindComparator:
		if request.Lane != V1LaneWindowsGo && request.Lane != V1LaneUbuntuGo && request.Lane != V1LaneMacOSGo || !validV1LaneRunner(request.Lane, request.Runner) {
			return errors.New("invalid v1 comparator lane")
		}
	case V1KindBaseline, V1KindDetector:
		if request.Lane != V1LaneWindowsDetector || !validV1LaneRunner(request.Lane, request.Runner) {
			return errors.New("invalid v1 detector lane")
		}
	default:
		return errors.New("invalid v1 lane role")
	}
	return nil
}

func v1RoleRepetitions(role string) []int {
	if role == V1KindComparator {
		return []int{1}
	}
	return []int{1, 2}
}

func v1EvidenceLeaf(role, lane string) string {
	switch role {
	case V1KindComparator:
		switch lane {
		case V1LaneWindowsGo:
			return "windows-comparator.json"
		case V1LaneUbuntuGo:
			return "ubuntu-comparator.json"
		default:
			return "macos-comparator.json"
		}
	case V1KindBaseline:
		return "windows-baseline.json"
	default:
		return "windows-detector.json"
	}
}

func runV1Attempt(ctx context.Context, request V1LaneRequest, run V1ProcessRunner, candidate V1Candidate, repetition int) V1Attempt {
	started := time.Now().UTC().Truncate(time.Millisecond)
	attempt := V1Attempt{CandidateID: candidate.ID, DetectorID: candidate.DetectorID, Target: candidate.Target, Kind: request.Role, Lane: request.Lane, Repetition: repetition, Authorities: request.Manifest.Authorities, Toolchain: request.Manifest.Toolchain, Runner: request.Runner, StartedAt: started.Format(time.RFC3339Nano)}
	if request.Role == V1KindComparator {
		attempt.PatchSHA256, attempt.MutatedTree, attempt.ComparatorContractSHA256 = candidate.PatchSHA256, candidate.MutatedTree, request.Manifest.ComparatorContractSHA256
	} else {
		attempt.DetectorContractSHA256 = request.Manifest.DetectorContractSHA256
		attempt.Admission = V1AdmissionAdmitted
		if request.Role == V1KindDetector {
			attempt.PatchSHA256, attempt.MutatedTree = candidate.PatchSHA256, candidate.MutatedTree
		}
	}
	directory, err := os.MkdirTemp(request.ResultRoot, "attempt-"+candidate.ID+"-")
	if err != nil {
		return finishV1Infrastructure(attempt, started, "attempt_root")
	}
	defer removeV1Attempt(request.RunnerRoot, directory)
	environment, err := v1AttemptEnvironment(directory, request.RunnerRoot, request.GoCache, request.GoModCache)
	if err != nil {
		return finishV1Infrastructure(attempt, started, "attempt_environment")
	}
	if request.Role != V1KindBaseline {
		if _, err := validationaudit.LoadV1CandidatePatch(request.Root, v1PatchRequest(candidate)); err != nil {
			return finishV1Infrastructure(attempt, started, "patch")
		}
	}
	if result := acquireV1Reference(ctx, run, directory, request.Manifest.Authorities.Evaluated.Commit, environment); result.Infrastructure != "" {
		return finishV1Infrastructure(attempt, started, result.Infrastructure)
	}
	repository := filepath.Join(directory, "repository")
	tree, repositoryDigest, err := measuredV1Repository(ctx, run, repository, environment, request.Manifest.Authorities.Evaluated)
	if err != nil {
		return finishV1Infrastructure(attempt, started, "source_identity")
	}
	attempt.RepositorySHA256 = repositoryDigest
	if _, err := measureV1Toolchain(ctx, run, filepath.Join(repository, "go-engine"), environment, request.Manifest.Toolchain); err != nil {
		return finishV1Infrastructure(attempt, started, "toolchain")
	}
	if request.Role != V1KindBaseline {
		patch := filepath.Join(request.Root, filepath.FromSlash(V1CorpusRoot), "patches", candidate.ID+".patch")
		if result := run(ctx, repository, V1ChildCommand{Name: "git", Args: []string{"apply", "--check", "--index", patch}, Env: environment}); result.Infrastructure != "" || result.Rejected {
			return finishV1Infrastructure(attempt, started, "patch_apply")
		}
		if result := run(ctx, repository, V1ChildCommand{Name: "git", Args: []string{"apply", "--index", patch}, Env: environment}); result.Infrastructure != "" || result.Rejected {
			return finishV1Infrastructure(attempt, started, "patch_apply")
		}
		if err := ensureV1MutatedModuleRevision(repository, candidate); err != nil {
			return finishV1Infrastructure(attempt, started, "module_revision")
		}
		if tree, err := v1GitValue(ctx, run, repository, environment, "write-tree"); err != nil || tree != candidate.MutatedTree {
			return finishV1Infrastructure(attempt, started, "mutated_tree")
		}
	}
	if request.Role == V1KindComparator {
		result := RunV1Comparator(func(command V1ChildCommand) V1ChildResult {
			return run(ctx, filepath.Join(repository, "go-engine"), command)
		}, environment)
		if result.Infrastructure != "" {
			return finishV1Infrastructure(attempt, started, result.Infrastructure)
		}
		if result.Rejected {
			attempt.Status = V1StatusRejected
			attempt.Failure = &V1Failure{Class: "execution_failure", Phase: "comparator", Coordinate: request.Lane, Scope: V1FailureScopeGuard}
			return finishV1Attempt(attempt, started)
		}
		attempt.Status = V1StatusPassed
		return finishV1Attempt(attempt, started)
	}
	// Both production binaries are rebuilt from the evaluated tree; the controller
	// decodes the validation CLI's bounded result and never invokes it in-process.
	engine := filepath.Join(directory, "endstate")
	validator := filepath.Join(directory, "endstate-validation")
	if runtime.GOOS == "windows" {
		engine += ".exe"
		validator += ".exe"
	}
	build := run(ctx, filepath.Join(repository, "go-engine"), V1ChildCommand{Name: "go", Args: []string{"build", "-buildvcs=false", "-o", engine, "./cmd/endstate"}, Env: environment})
	if build.Infrastructure != "" || build.Rejected {
		return finishV1Infrastructure(attempt, started, "engine_build")
	}
	build = run(ctx, filepath.Join(repository, "go-engine"), V1ChildCommand{Name: "go", Args: []string{"build", "-buildvcs=false", "-o", validator, "./cmd/endstate-validation"}, Env: environment})
	if build.Infrastructure != "" || build.Rejected {
		return finishV1Infrastructure(attempt, started, "validation_build")
	}
	if engineBytes, err := os.ReadFile(engine); err != nil {
		return finishV1Infrastructure(attempt, started, "engine_hash")
	} else {
		attempt.DiagnosticEngineSHA256 = v1RepositoryDigest(engineBytes)
	}
	if validationBytes, err := os.ReadFile(validator); err != nil {
		return finishV1Infrastructure(attempt, started, "validation_hash")
	} else {
		attempt.DiagnosticValidationSHA256 = v1RepositoryDigest(validationBytes)
	}
	mode, err := v1LoadedSidecarMode(repository, candidate.Target)
	if err != nil || mode != lifecycleV1Mode(candidate.Lifecycle) {
		return finishV1Infrastructure(attempt, started, "sidecar_mode")
	}
	attempt.VerifiedMode = mode
	admission, status, failure, proof, infrastructure := runV1ExternalDetector(ctx, run, directory, filepath.Join(repository, "go-engine"), engine, validator, repository, candidate, environment)
	if infrastructure != "" {
		return finishV1Infrastructure(attempt, started, infrastructure)
	}
	attempt.Admission, attempt.Status, attempt.Failure = admission, status, failure
	if request.Role == V1KindBaseline {
		attempt.BaselineProof = V1BaselineProofIdentity{SourceTree: tree, RepositorySHA256: repositoryDigest, Target: candidate.Target, Proof: proof}
	}
	return finishV1Attempt(attempt, started)
}

func ensureV1MutatedModuleRevision(repository string, candidate V1Candidate) error {
	if candidate.Family != "module" {
		return nil
	}
	slug := strings.TrimPrefix(candidate.Target.ModuleID, "apps.")
	moduleBytes, err := os.ReadFile(filepath.Join(repository, "modules", "apps", slug, "module.jsonc"))
	if err != nil {
		return err
	}
	revision, err := modules.ComputeModuleRevision(moduleBytes)
	if err != nil {
		return err
	}
	sidecarBytes, err := os.ReadFile(filepath.Join(repository, "modules", "apps", slug, "validation.jsonc"))
	if err != nil {
		return err
	}
	var sidecar map[string]any
	if err := json.Unmarshal(manifest.StripJsoncComments(sidecarBytes), &sidecar); err != nil {
		return err
	}
	declared, _ := sidecar["moduleRevision"].(string)
	if declared != revision {
		return errors.New("module revision differs")
	}
	return nil
}

func v1LoadedSidecarMode(repository string, target V1Target) (string, error) {
	slug := strings.TrimPrefix(target.ModuleID, "apps.")
	raw, err := os.ReadFile(filepath.Join(repository, "modules", "apps", slug, "validation.jsonc"))
	if err != nil {
		return "", err
	}
	var sidecar struct {
		Synthetic struct {
			Scenarios []struct {
				ID   string `json:"id"`
				Mode string `json:"mode"`
			} `json:"scenarios"`
		} `json:"synthetic"`
	}
	if json.Unmarshal(manifest.StripJsoncComments(raw), &sidecar) != nil {
		return "", errors.New("sidecar decode failed")
	}
	for _, scenario := range sidecar.Synthetic.Scenarios {
		if scenario.ID == target.ScenarioID && validV1Mode(scenario.Mode) {
			return scenario.Mode, nil
		}
	}
	return "", errors.New("sidecar scenario differs")
}

func v1LoadedModuleRevision(repository string, target V1Target) (string, error) {
	slug := strings.TrimPrefix(target.ModuleID, "apps.")
	raw, err := os.ReadFile(filepath.Join(repository, "modules", "apps", slug, "module.jsonc"))
	if err != nil {
		return "", err
	}
	return modules.ComputeModuleRevision(raw)
}

func runV1ExternalDetector(ctx context.Context, run V1ProcessRunner, attemptRoot, directory, engine, validator, repository string, candidate V1Candidate, environment []string) (string, string, *V1Failure, string, string) {
	tempRoot := v1EnvironmentValue(environment, "TEMP")
	if tempRoot != filepath.Join(attemptRoot, "profile", "temp") || !v1StrictDescendant(attemptRoot, tempRoot) || !v1SafeExistingAncestors(tempRoot) {
		return "", "", nil, "", "detector_result_root"
	}
	resultPath := filepath.Join(tempRoot, "validation-result", "result.json")
	if err := os.Mkdir(filepath.Dir(resultPath), 0o700); err != nil {
		return "", "", nil, "", "detector_result_root"
	}
	result := run(ctx, directory, V1ChildCommand{Name: validator, Args: []string{"--engine", engine, "--repo", repository, "--module", candidate.Target.ModuleID, "--scenario", candidate.Target.ScenarioID, "--result", resultPath}, Env: environment})
	if result.Infrastructure != "" {
		return "", "", nil, "", "detector_launch"
	}
	mode, err := v1LoadedSidecarMode(repository, candidate.Target)
	if err != nil {
		return "", "", nil, "", "detector_evidence"
	}
	revision, err := v1LoadedModuleRevision(repository, candidate.Target)
	if err != nil {
		return "", "", nil, "", "detector_evidence"
	}
	typed, err := DecodeV1ExternalResult([]byte(result.Value), candidate, revision, mode)
	if err != nil {
		return "", "", nil, "", "detector_evidence"
	}
	persisted, err := os.ReadFile(resultPath)
	if err != nil || len(persisted) > V1MaxDocumentSize {
		return "", "", nil, "", "detector_evidence"
	}
	persistedTyped, err := DecodeV1ExternalResult(persisted, candidate, revision, mode)
	if err != nil || !bytes.Equal([]byte(result.Value), persisted) || !reflect.DeepEqual(typed, persistedTyped) {
		return "", "", nil, "", "detector_evidence"
	}
	admission, failure, err := v1ModuleDetectorResult(candidate, typed)
	if err != nil {
		return "", "", nil, "", "detector_evidence"
	}
	if typed.Status == validationharness.ResultStatusPassed {
		if result.Rejected {
			return "", "", nil, "", "detector_evidence"
		}
		return admission, V1StatusPassed, nil, v1ModuleBaselineProof(typed, candidate.Target), ""
	}
	if !result.Rejected {
		return "", "", nil, "", "detector_evidence"
	}
	return admission, V1StatusRejected, failure, v1ModuleBaselineProof(typed, candidate.Target), ""
}

// DecodeV1ExternalResult accepts only the single, strict result contract
// emitted by the co-built validation CLI.
func DecodeV1ExternalResult(raw []byte, candidate V1Candidate, revision, mode string) (validationharness.Result, error) {
	var result validationharness.Result
	if err := decodeV1(raw, &result); err != nil || !validV1ExternalResult(result, candidate, revision, mode) {
		return validationharness.Result{}, errors.New("invalid v1 external result")
	}
	return result, nil
}

func validV1ExternalResult(result validationharness.Result, candidate V1Candidate, revision, mode string) bool {
	if result.SchemaVersion != validationharness.ResultSchemaVersion || result.ModuleID != candidate.Target.ModuleID || result.ModuleRevision != revision || result.ScenarioID != candidate.Target.ScenarioID || string(result.Kind) != mode || result.ProofLevels == nil || result.AssertionCounts == nil || result.PhaseTimings == nil || len(result.ProofLevels) > 16 || len(result.AssertionCounts) > 32 || len(result.PhaseTimings) > 32 {
		return false
	}
	for _, level := range result.ProofLevels {
		if !validV1Value(string(level)) {
			return false
		}
	}
	for key, value := range result.AssertionCounts {
		if !validV1Value(key) || value < 0 || value > 1000000 {
			return false
		}
	}
	for key, value := range result.PhaseTimings {
		if !validV1Value(key) || value < 0 || value > 10*time.Minute {
			return false
		}
	}
	if result.Status == validationharness.ResultStatusPassed {
		return result.Failure == nil && len(result.ProofLevels) > 0 && len(result.AssertionCounts) > 0 && len(result.PhaseTimings) > 0
	}
	if result.Status != validationharness.ResultStatusFailed || result.Failure == nil || !validV1FailureDetail(result.Failure.Detail) || len(result.Failure.ProofLevels) != 0 || len(result.AssertionCounts) == 0 || len(result.PhaseTimings) == 0 {
		return false
	}
	failure := v1FailureFromLegacy(&Failure{Code: result.Failure.Code, Phase: result.Failure.Phase, Coordinate: result.Failure.Coordinate})
	return failure != nil && validV1Failure(*failure)
}

func validV1FailureDetail(detail string) bool {
	return len(detail) <= 512 && !strings.ContainsAny(detail, "\r\n\\")
}

func v1ModuleDetectorResult(candidate V1Candidate, result validationharness.Result) (string, *V1Failure, error) {
	if result.ModuleID != candidate.Target.ModuleID || result.ScenarioID != candidate.Target.ScenarioID {
		return "", nil, errors.New("module target differs")
	}
	if result.Status == validationharness.ResultStatusPassed {
		return V1AdmissionAdmitted, nil, nil
	}
	if result.Status != validationharness.ResultStatusFailed || result.Failure == nil {
		return "", nil, errors.New("module result is malformed")
	}
	failure := v1FailureFromLegacy(&Failure{Code: result.Failure.Code, Phase: result.Failure.Phase, Coordinate: result.Failure.Coordinate})
	if shallowV1Failure(failure) {
		return V1AdmissionRejected, failure, nil
	}
	return V1AdmissionAdmitted, failure, nil
}

func v1FailureFromLegacy(failure *Failure) *V1Failure {
	if failure == nil {
		return nil
	}
	scope := V1FailureScopeDomain
	for _, value := range []string{failure.Code, failure.Phase, failure.Coordinate, failure.ChildReason} {
		if v1GuardValue(value) {
			scope = V1FailureScopeGuard
		}
	}
	return &V1Failure{Class: failure.Code, Phase: failure.Phase, Coordinate: failure.Coordinate, ChildReason: failure.ChildReason, Scope: scope}
}

func v1ModuleBaselineProof(result validationharness.Result, target V1Target) string {
	return v1TypedProof(target, result.Status, result.ProofLevels, result.AssertionCounts, v1FailureFromLegacy(&Failure{Code: failureCode(result.Failure), Phase: failurePhase(result.Failure), Coordinate: failureCoordinate(result.Failure)}))
}

func failureCode(failure *validationharness.Failure) string {
	if failure == nil {
		return ""
	}
	return failure.Code
}
func failurePhase(failure *validationharness.Failure) string {
	if failure == nil {
		return ""
	}
	return failure.Phase
}
func failureCoordinate(failure *validationharness.Failure) string {
	if failure == nil {
		return ""
	}
	return failure.Coordinate
}

func v1TypedProof(target V1Target, status string, levels []validationmatrix.ProofLevel, counts map[string]int, failure *V1Failure) string {
	proofLevels := make([]string, len(levels))
	for index, level := range levels {
		proofLevels[index] = string(level)
	}
	sort.Strings(proofLevels)
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	type count struct {
		Name  string `json:"name"`
		Value int    `json:"value"`
	}
	typedCounts := make([]count, 0, len(keys))
	for _, key := range keys {
		typedCounts = append(typedCounts, count{Name: key, Value: counts[key]})
	}
	raw, _ := json.Marshal(struct {
		Target          V1Target   `json:"target"`
		Status          string     `json:"status"`
		ProofLevels     []string   `json:"proofLevels"`
		AssertionCounts []count    `json:"assertionCounts"`
		Failure         *V1Failure `json:"failure,omitempty"`
	}{target, status, proofLevels, typedCounts, failure})
	return v1RepositoryDigest(raw)
}

func acquireV1Reference(ctx context.Context, run V1ProcessRunner, directory, commit string, env []string) V1ChildResult {
	for _, command := range []V1ChildCommand{
		{Name: "git", Args: []string{"init", "repository"}, Env: env},
		{Name: "git", Args: []string{"-C", "repository", "-c", "credential.helper=", "-c", "http.extraheader=", "fetch", "--depth=1", v1RepositoryURL, commit}, Env: env},
		{Name: "git", Args: []string{"-C", "repository", "checkout", "--detach", "FETCH_HEAD"}, Env: env},
	} {
		result := run(ctx, directory, command)
		if result.Infrastructure != "" || result.Rejected {
			return V1ChildResult{Infrastructure: "acquisition"}
		}
	}
	return V1ChildResult{}
}

func v1GitValue(ctx context.Context, run V1ProcessRunner, directory string, env []string, args ...string) (string, error) {
	result := run(ctx, directory, V1ChildCommand{Name: "git", Args: args, Env: env})
	if result.Infrastructure != "" || result.Rejected {
		return "", errors.New("git authority failed")
	}
	return strings.TrimSpace(result.Value), nil
}

func measureV1Toolchain(ctx context.Context, run V1ProcessRunner, directory string, env []string, expected string) (string, error) {
	result := run(ctx, directory, V1ChildCommand{Name: "go", Args: []string{"env", "GOVERSION"}, Env: env})
	value := strings.TrimSpace(result.Value)
	if result.Infrastructure != "" || result.Rejected || !v1ToolchainPattern.MatchString(value) || value != expected {
		return "", errors.New("toolchain differs")
	}
	return value, nil
}

func measuredV1Repository(ctx context.Context, run V1ProcessRunner, repository string, env []string, expected V1Reference) (string, string, error) {
	commit, err := v1GitValue(ctx, run, repository, env, "rev-parse", "HEAD")
	if err != nil || commit != expected.Commit {
		return "", "", errors.New("repository commit differs")
	}
	tree, err := v1GitValue(ctx, run, repository, env, "rev-parse", "HEAD^{tree}")
	if err != nil || tree != expected.Tree {
		return "", "", errors.New("repository tree differs")
	}
	result := run(ctx, repository, V1ChildCommand{Name: "git", Args: []string{"ls-tree", "-r", "--full-tree", "HEAD"}, Env: env})
	if result.Infrastructure != "" || result.Rejected {
		return "", "", errors.New("repository tree listing failed")
	}
	return tree, v1RepositoryDigest([]byte(result.Value)), nil
}

func v1RepositoryDigest(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func v1HostedRunner(family, imageOS, imageVersion string) (V1Runner, error) {
	imageOS = strings.ToLower(imageOS)
	if !validV1Value(imageOS) || !validV1Value(imageVersion) {
		return V1Runner{}, errors.New("runner image metadata is invalid")
	}
	validFamily := (family == "windows" && strings.HasPrefix(imageOS, "win")) || (family == "linux" && strings.HasPrefix(imageOS, "ubuntu")) || (family == "darwin" && strings.HasPrefix(imageOS, "macos"))
	if !validFamily {
		return V1Runner{}, errors.New("runner image metadata has the wrong family")
	}
	return V1Runner{Family: family, Image: imageOS + "-" + imageVersion, ImageOS: imageOS, ImageVersion: imageVersion}, nil
}

// HostedV1Runner derives the recorded runner image from GitHub-hosted image
// metadata instead of accepting a caller-composed image identity.
func HostedV1Runner(family, imageOS, imageVersion string) (V1Runner, error) {
	return v1HostedRunner(family, imageOS, imageVersion)
}

func finishV1Infrastructure(attempt V1Attempt, started time.Time, coordinate string) V1Attempt {
	attempt.Status = V1StatusInfrastructure
	return finishV1Attempt(attempt, started)
}

func finishV1Attempt(attempt V1Attempt, started time.Time) V1Attempt {
	ended := time.Now().UTC().Truncate(time.Millisecond)
	attempt.EndedAt = ended.Format(time.RFC3339Nano)
	attempt.DurationMillis = ended.Sub(started).Milliseconds()
	return attempt
}

func prepareV1RunnerRoot(root string) error {
	if !filepath.IsAbs(root) || filepath.Clean(root) != root || !v1SafeExistingAncestors(root) {
		return errors.New("unsafe v1 runner root")
	}
	if _, err := os.Lstat(root); os.IsNotExist(err) {
		if err := os.Mkdir(root, 0o700); err != nil {
			return err
		}
	}
	if !v1SafeExistingAncestors(root) {
		return errors.New("unsafe v1 runner root")
	}
	return nil
}

func prepareV1ResultRoot(runnerRoot, root string) error {
	if !v1StrictDescendant(runnerRoot, root) || !v1SafeExistingAncestors(root) {
		return errors.New("unsafe v1 result root")
	}
	if info, err := os.Lstat(root); err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("unsafe v1 result root")
		}
		return nil
	}
	if err := os.Mkdir(root, 0o700); err != nil {
		return err
	}
	return nil
}

func v1SafeExistingAncestors(path string) bool {
	for current := path; ; current = filepath.Dir(current) {
		if info, err := os.Lstat(current); err == nil {
			if !info.IsDir() || validationaudit.IsUnsafePath(current) {
				return false
			}
		} else if !os.IsNotExist(err) {
			return false
		}
		parent := filepath.Dir(current)
		if parent == current {
			return true
		}
	}
}

func v1StrictDescendant(root, path string) bool {
	if !filepath.IsAbs(root) || filepath.Clean(root) != root || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return false
	}
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != "." && relative != "" && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}

func removeV1Attempt(runnerRoot, directory string) {
	if v1StrictDescendant(runnerRoot, directory) && v1SafeExistingAncestors(directory) && strings.HasPrefix(filepath.Base(directory), "attempt-") {
		_ = os.RemoveAll(directory)
	}
}

func v1AttemptEnvironment(root, runnerRoot, goCache, goModCache string) ([]string, error) {
	if !v1SafeSharedCache(runnerRoot, goCache) || !v1SafeSharedCache(runnerRoot, goModCache) {
		return nil, errors.New("unsafe shared Go cache")
	}
	profile := filepath.Join(root, "profile")
	paths := []string{profile, filepath.Join(profile, "appdata"), filepath.Join(profile, "localappdata"), filepath.Join(profile, "temp")}
	for _, path := range paths {
		if err := os.Mkdir(path, 0o700); err != nil {
			return nil, err
		}
	}
	environment := v1WithoutGoCaches(V1ChildEnvironment(os.Environ()))
	return append(environment,
		"HOME="+profile,
		"USERPROFILE="+profile,
		"APPDATA="+filepath.Join(profile, "appdata"),
		"LOCALAPPDATA="+filepath.Join(profile, "localappdata"),
		"TEMP="+filepath.Join(profile, "temp"),
		"TMP="+filepath.Join(profile, "temp"),
		"GOCACHE="+goCache,
		"GOMODCACHE="+goModCache,
	), nil
}

func v1SafeSharedCache(runnerRoot, cache string) bool {
	if !v1StrictDescendant(runnerRoot, cache) || !v1SafeExistingAncestors(cache) {
		return false
	}
	info, err := os.Lstat(cache)
	return err == nil && info.IsDir() && !validationaudit.IsUnsafePath(cache)
}

func v1WithoutGoCaches(environment []string) []string {
	filtered := make([]string, 0, len(environment))
	for _, value := range environment {
		name, _, found := strings.Cut(value, "=")
		if found && (name == "GOCACHE" || name == "GOMODCACHE") {
			continue
		}
		filtered = append(filtered, value)
	}
	return filtered
}

func v1EnvironmentValue(environment []string, name string) string {
	for _, value := range environment {
		key, value, found := strings.Cut(value, "=")
		if found && key == name {
			return value
		}
	}
	return ""
}

func writeV1EvidenceNew(path string, evidence V1Evidence) error {
	raw, _, err := EncodeV1Evidence(evidence)
	if err != nil {
		return err
	}
	parent := filepath.Dir(path)
	temporary, err := os.CreateTemp(parent, ".evidence-*")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(raw); err != nil || temporary.Sync() != nil || temporary.Close() != nil {
		return errors.New("v1 evidence publication failed")
	}
	if err := os.Link(temporaryName, path); err != nil {
		return err
	}
	stored, err := os.ReadFile(path)
	if err != nil || string(stored) != string(raw) {
		return errors.New("v1 evidence publication differs")
	}
	return nil
}

func runV1Process(ctx context.Context, directory string, command V1ChildCommand) V1ChildResult {
	processContext, cancel := context.WithTimeout(ctx, v1CommandTimeout)
	defer cancel()
	process := exec.CommandContext(processContext, command.Name, command.Args...)
	process.Dir, process.Env = directory, command.Env
	output := &v1LimitedOutput{limit: v1CommandLimit}
	process.Stdout, process.Stderr = output, output
	err := process.Run()
	if output.overflow || errors.Is(err, context.DeadlineExceeded) || processContext.Err() != nil {
		return V1ChildResult{Infrastructure: "process"}
	}
	if err != nil {
		if _, ok := err.(*exec.ExitError); ok {
			return V1ChildResult{Rejected: true, Value: strings.TrimSpace(output.String())}
		}
		return V1ChildResult{Infrastructure: "launch"}
	}
	return V1ChildResult{Value: strings.TrimSpace(output.String())}
}

type v1LimitedOutput struct {
	bytes.Buffer
	limit    int
	overflow bool
}

func (output *v1LimitedOutput) Write(value []byte) (int, error) {
	if output.Len()+len(value) > output.limit {
		output.overflow = true
		return len(value), errors.New("output limit")
	}
	return output.Buffer.Write(value)
}
