// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationpilot

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/validationaudit"
	"github.com/Artexis10/endstate/go-engine/internal/validationharness"
	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
)

const (
	v1RepositoryURL = "https://github.com/Artexis10/endstate.git"
	v1CommandLimit  = 256 * 1024
	v1CommandTimeout = 10 * time.Minute
)

type V1LaneRequest struct {
	Root       string
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
	if err := prepareV1ResultRoot(request.ResultRoot); err != nil {
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
	if !validV1Manifest(request.Manifest) || !validV1Runner(request.Runner) || !filepath.IsAbs(request.ResultRoot) || filepath.Clean(request.ResultRoot) != request.ResultRoot {
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
		if request.Role == V1KindBaseline {
			attempt.BaselineProof = V1BaselineProofIdentity{SourceTree: request.Manifest.Authorities.Evaluated.Tree, RepositorySHA256: v1Digest(request.Manifest.Authorities.Evaluated.Tree), Target: candidate.Target, Proof: "detector"}
		}
		if request.Role == V1KindDetector {
			attempt.PatchSHA256, attempt.MutatedTree = candidate.PatchSHA256, candidate.MutatedTree
		}
	}
	directory, err := os.MkdirTemp(request.ResultRoot, "attempt-"+candidate.ID+"-")
	if err != nil {
		return finishV1Infrastructure(attempt, started, "attempt_root")
	}
	defer os.RemoveAll(directory)
	environment, err := v1AttemptEnvironment(directory)
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
		result := RunV1Comparator(func(command V1ChildCommand) V1ChildResult { return run(ctx, filepath.Join(repository, "go-engine"), command) }, environment)
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
	// Detector invocation remains typed: the authoritative validation harness is
	// driven by the controller after the exact engine build, never by shell JSON.
	engine := filepath.Join(directory, "endstate")
	if runtime.GOOS == "windows" {
		engine += ".exe"
	}
	build := run(ctx, filepath.Join(repository, "go-engine"), V1ChildCommand{Name: "go", Args: []string{"build", "-buildvcs=false", "-o", engine, "./cmd/endstate"}, Env: environment})
	if build.Infrastructure != "" || build.Rejected {
		return finishV1Infrastructure(attempt, started, "engine_build")
	}
	status, failure, infrastructure := runV1Detector(ctx, engine, repository, candidate)
	if infrastructure != "" {
		return finishV1Infrastructure(attempt, started, infrastructure)
	}
	attempt.Status, attempt.Failure = status, failure
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

func runV1Detector(ctx context.Context, engine, repository string, candidate V1Candidate) (string, *V1Failure, string) {
	if candidate.Family == "catalog" {
		result, err := validationharness.RunCatalogMatrix(ctx, validationharness.CatalogMatrixRequest{EnginePath: engine, RepoRoot: repository})
		if err != nil {
			return "", nil, "detector_launch"
		}
		if result.Status == "passed" {
			return V1StatusPassed, nil, ""
		}
		failure := catalogFailure(result)
		return V1StatusRejected, v1FailureFromLegacy(failure), ""
	}
	result, err := validationharness.Run(ctx, validationharness.Request{EnginePath: engine, RepoRoot: repository, ModuleID: candidate.Target.ModuleID, ScenarioID: candidate.Target.ScenarioID})
	if err != nil {
		return "", nil, "detector_launch"
	}
	if result.Status == "passed" {
		return V1StatusPassed, nil, ""
	}
	if result.Failure == nil {
		return "", nil, "detector_evidence"
	}
	return V1StatusRejected, v1FailureFromLegacy(&Failure{Code: result.Failure.Code, Phase: result.Failure.Phase, Coordinate: result.Failure.Coordinate}), ""
}

func v1FailureFromLegacy(failure *Failure) *V1Failure {
	if failure == nil {
		return nil
	}
	scope := V1FailureScopeDomain
	for _, value := range []string{failure.Code, failure.Phase, failure.Coordinate} {
		if strings.Contains(value, "schema") || strings.Contains(value, "revision") || strings.Contains(value, "selection") || strings.Contains(value, "admission") || strings.Contains(value, "envelope") || strings.Contains(value, "aggregate") || strings.Contains(value, "parse") || strings.Contains(value, "decode") {
			scope = V1FailureScopeGuard
		}
	}
	return &V1Failure{Class: failure.Code, Phase: failure.Phase, Coordinate: failure.Coordinate, ChildReason: failure.ChildReason, Scope: scope}
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

func prepareV1ResultRoot(root string) error {
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

func v1AttemptEnvironment(root string) ([]string, error) {
	profile := filepath.Join(root, "profile")
	paths := []string{profile, filepath.Join(profile, "appdata"), filepath.Join(profile, "localappdata"), filepath.Join(profile, "temp")}
	for _, path := range paths {
		if err := os.Mkdir(path, 0o700); err != nil {
			return nil, err
		}
	}
	environment := V1ChildEnvironment(os.Environ())
	return append(environment,
		"HOME="+profile,
		"USERPROFILE="+profile,
		"APPDATA="+filepath.Join(profile, "appdata"),
		"LOCALAPPDATA="+filepath.Join(profile, "localappdata"),
		"TEMP="+filepath.Join(profile, "temp"),
		"TMP="+filepath.Join(profile, "temp"),
	), nil
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
			return V1ChildResult{Rejected: true}
		}
		return V1ChildResult{Infrastructure: "launch"}
	}
	return V1ChildResult{Value: strings.TrimSpace(output.String())}
}

type v1LimitedOutput struct { bytes.Buffer; limit int; overflow bool }
func (output *v1LimitedOutput) Write(value []byte) (int, error) {
	if output.Len()+len(value) > output.limit { output.overflow = true; return len(value), errors.New("output limit") }
	return output.Buffer.Write(value)
}
