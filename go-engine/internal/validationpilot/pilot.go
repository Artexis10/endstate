// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

// Package validationpilot validates the fixed hosted CI efficacy preflight.
package validationpilot

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/validationharness"
	"github.com/Artexis10/endstate/go-engine/internal/validationmatrix"
)

const (
	LegacyRef                           = "ab8065cd67ab3f4e9e876e07a25facf3100c28c7"
	DetectorRef                         = "437c0ca4167c09bc9f2de515daa6d55d35257d4f"
	ClassificationCorrectNewOnlyKill    = "correct-new-only-kill"
	ClassificationAlreadyCovered        = "already-covered"
	ClassificationWrongKill             = "wrong-kill"
	ClassificationSurvivor              = "survivor"
	ClassificationFlake                 = "flake"
	ClassificationInfrastructureFailure = "infrastructure-failure"
	DecisionMeaningfulSignal            = "meaningful-signal"
	DecisionInsufficientSignal          = "insufficient-signal"
)

var sha256Pattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

var (
	runCatalogMatrix = validationharness.RunCatalogMatrix
	runScenario      = validationharness.Run
)

type Failure struct {
	Code        string `json:"code"`
	Phase       string `json:"phase"`
	Coordinate  string `json:"coordinate"`
	ChildReason string `json:"childReason,omitempty"`
}
type Patch struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}
type Candidate struct {
	ID         string  `json:"id"`
	Family     string  `json:"family"`
	ModuleID   string  `json:"moduleId,omitempty"`
	ScenarioID string  `json:"scenarioId,omitempty"`
	Expected   Failure `json:"expected"`
	Legacy     Patch   `json:"legacy"`
	Detector   Patch   `json:"detector"`
}
type Manifest struct {
	SchemaVersion int         `json:"schemaVersion"`
	LegacyRef     string      `json:"legacyRef"`
	DetectorRef   string      `json:"detectorRef"`
	Candidates    []Candidate `json:"candidates"`
}

func LoadManifest(path string) (Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, err
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	var manifest Manifest
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, fmt.Errorf("decode manifest: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err == nil {
		return Manifest{}, errors.New("manifest has multiple JSON values")
	}
	if manifest.SchemaVersion != 1 || manifest.LegacyRef != LegacyRef || manifest.DetectorRef != DetectorRef {
		return Manifest{}, errors.New("manifest does not bind fixed references")
	}
	if len(manifest.Candidates) != 6 {
		return Manifest{}, errors.New("manifest must contain exactly six candidates")
	}
	ids := []string{"bundle-duplicate", "bundle-missing", "bundle-id-drift", "vlc-backup-off", "alacritty-source-drift", "obs-target-drift"}
	for index, candidate := range manifest.Candidates {
		if candidate.ID != ids[index] {
			return Manifest{}, fmt.Errorf("manifest candidate order %d is not fixed", index)
		}
		for _, patch := range []Patch{candidate.Legacy, candidate.Detector} {
			if !strings.HasPrefix(patch.Path, "patches/"+candidate.ID+"/") || !sha256Pattern.MatchString(patch.SHA256) {
				return Manifest{}, fmt.Errorf("candidate %q has invalid patch identity", candidate.ID)
			}
		}
	}
	return manifest, nil
}

func ValidateCorpus(root string, manifest Manifest) error {
	canonicalRoot, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	root = canonicalRoot
	for _, ref := range []string{manifest.LegacyRef, manifest.DetectorRef} {
		if _, err := gitOutput(root, nil, "rev-parse", "--verify", ref+"^{commit}"); err != nil {
			return fmt.Errorf("fixed authority %s is unavailable: %w", ref, err)
		}
	}
	for _, path := range []string{"bundles/gaming.jsonc", "bundles/remote-access.jsonc", "bundles/communication.jsonc", "modules/apps/vlc/module.jsonc", "modules/apps/alacritty/module.jsonc", "modules/apps/obs-studio/module.jsonc"} {
		legacy, err := gitOutput(root, nil, "rev-parse", manifest.LegacyRef+":"+path)
		if err != nil {
			return fmt.Errorf("read legacy production identity: %w", err)
		}
		detector, err := gitOutput(root, nil, "rev-parse", manifest.DetectorRef+":"+path)
		if err != nil {
			return fmt.Errorf("read detector production identity: %w", err)
		}
		if strings.TrimSpace(legacy) != strings.TrimSpace(detector) {
			return fmt.Errorf("production identity differs for %q", path)
		}
	}
	for _, candidate := range manifest.Candidates {
		for patchIndex, patch := range []Patch{candidate.Legacy, candidate.Detector} {
			data, err := os.ReadFile(filepath.Join(root, "validation", "ci-efficacy", "pilot-v0", filepath.FromSlash(patch.Path)))
			if err != nil {
				return err
			}
			sum := sha256.Sum256(data)
			if hex.EncodeToString(sum[:]) != patch.SHA256 {
				return fmt.Errorf("candidate %q patch digest differs", candidate.ID)
			}
			if err := validatePatch(candidate, patch.Path, string(data)); err != nil {
				return err
			}
			ref := manifest.LegacyRef
			if patchIndex == 1 {
				ref = manifest.DetectorRef
			}
			if err := applyAndValidatePatch(root, ref, filepath.Join(root, "validation", "ci-efficacy", "pilot-v0", filepath.FromSlash(patch.Path)), candidate, patchIndex == 1); err != nil {
				return err
			}
		}
	}
	return nil
}

func ValidateDetectorAuthority(root string, manifest Manifest) error {
	canonicalRoot, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	root = canonicalRoot
	for _, path := range []string{"go-engine/internal/validationharness", "go-engine/internal/catalogplan", "go-engine/internal/validationmatrix"} {
		if _, err := gitOutput(root, nil, "diff", "--quiet", manifest.DetectorRef, "--", path); err != nil {
			return fmt.Errorf("pilot detector source differs from detector authority for %q", path)
		}
	}
	return nil
}

func applyAndValidatePatch(root, ref, patchPath string, candidate Candidate, detector bool) error {
	index, err := os.CreateTemp("", "endstate-validation-pilot-index-")
	if err != nil {
		return err
	}
	indexPath := index.Name()
	if err := index.Close(); err != nil {
		return err
	}
	if err := os.Remove(indexPath); err != nil {
		return err
	}
	defer os.Remove(indexPath)
	env := []string{"GIT_INDEX_FILE=" + indexPath}
	if _, err := gitOutput(root, env, "read-tree", ref); err != nil {
		return fmt.Errorf("load patch reference: %w", err)
	}
	if _, err := gitOutput(root, env, "apply", "--cached", "--check", patchPath); err != nil {
		return fmt.Errorf("patch does not apply to %s: %w", ref, err)
	}
	if _, err := gitOutput(root, env, "apply", "--cached", patchPath); err != nil {
		return fmt.Errorf("apply patch: %w", err)
	}
	if candidate.ModuleID == "" || !detector {
		return nil
	}
	modulePath := "modules/apps/" + strings.TrimPrefix(candidate.ModuleID, "apps.") + "/module.jsonc"
	moduleData, err := gitOutput(root, env, "show", ":"+modulePath)
	if err != nil {
		return err
	}
	revision, err := modules.ComputeModuleRevision([]byte(moduleData))
	if err != nil {
		return err
	}
	sidecar, err := gitOutput(root, env, "show", ":modules/apps/"+strings.TrimPrefix(candidate.ModuleID, "apps.")+"/validation.jsonc")
	if err != nil {
		return err
	}
	declared := map[string]json.RawMessage{}
	if err := json.Unmarshal([]byte(sidecar), &declared); err != nil {
		return fmt.Errorf("candidate %q revision companion is malformed", candidate.ID)
	}
	var declaredRevision string
	if err := json.Unmarshal(declared["moduleRevision"], &declaredRevision); err != nil || declaredRevision != revision {
		return fmt.Errorf("candidate %q revision companion differs", candidate.ID)
	}
	return nil
}

func gitOutput(root string, extraEnv []string, args ...string) (string, error) {
	command := exec.Command("git", append([]string{"-C", root}, args...)...)
	command.Env = append(os.Environ(), extraEnv...)
	data, err := command.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(string(data)))
	}
	return string(data), nil
}

func validatePatch(candidate Candidate, path, patch string) error {
	if strings.Contains(patch, "fixtures/") || strings.Contains(patch, "scenarios") || strings.Contains(patch, "timeoutSeconds") || strings.Contains(patch, "minimumAssertions") || strings.Contains(patch, "\"live\"") {
		return fmt.Errorf("candidate %q changes forbidden validation policy", candidate.ID)
	}
	targets := map[string]bool{}
	for _, line := range strings.Split(patch, "\n") {
		if strings.HasPrefix(line, "+++ b/") {
			targets[strings.TrimPrefix(line, "+++ b/")] = true
		}
	}
	if len(targets) == 0 {
		return fmt.Errorf("candidate %q patch has no target", candidate.ID)
	}
	if candidate.ModuleID == "" {
		bundleTargets := map[string]string{"bundle-duplicate": "bundles/gaming.jsonc", "bundle-missing": "bundles/remote-access.jsonc", "bundle-id-drift": "bundles/communication.jsonc"}
		if len(targets) != 1 || !targets[bundleTargets[candidate.ID]] {
			return fmt.Errorf("candidate %q does not bind its bundle target", candidate.ID)
		}
		return nil
	}
	modulePath := "modules/apps/" + strings.TrimPrefix(candidate.ModuleID, "apps.") + "/module.jsonc"
	if !targets[modulePath] || len(targets) > 2 || (len(targets) == 2 && !targets["modules/apps/"+strings.TrimPrefix(candidate.ModuleID, "apps.")+"/validation.jsonc"]) {
		return fmt.Errorf("candidate %q does not bind only its module and revision companion", candidate.ID)
	}
	if strings.Contains(path, "detector") && len(targets) != 2 {
		return fmt.Errorf("candidate %q detector patch lacks revision companion", candidate.ID)
	}
	return nil
}

type ProofIdentity struct {
	Commit         string `json:"commit"`
	EngineSHA256   string `json:"engineSha256"`
	RepositoryHash string `json:"repositoryHash"`
	ModuleID       string `json:"moduleId"`
	ScenarioID     string `json:"scenarioId"`
	Proof          string `json:"proof"`
}
type Attempt struct {
	Status          string        `json:"status"`
	Failure         *Failure      `json:"failure,omitempty"`
	Identity        ProofIdentity `json:"identity"`
	CandidateID     string        `json:"candidateId,omitempty"`
	PatchSHA256     string        `json:"patchSha256,omitempty"`
	DurationSeconds float64       `json:"durationSeconds"`
}
type LegacyAttempt struct {
	Contract        string  `json:"contract"`
	Ref             string  `json:"ref"`
	CandidateID     string  `json:"candidateId"`
	PatchSHA256     string  `json:"patchSha256"`
	Status          string  `json:"status"`
	DurationSeconds float64 `json:"durationSeconds"`
}
type CandidateEvidence struct {
	ID       string          `json:"id"`
	Legacy   []LegacyAttempt `json:"legacy"`
	Detector []Attempt       `json:"detector"`
}
type Evidence struct {
	SchemaVersion int                 `json:"schemaVersion"`
	Baseline      []Attempt           `json:"baseline"`
	Candidates    []CandidateEvidence `json:"candidates"`
}
type AggregateRow struct {
	ID             string `json:"id"`
	Classification string `json:"classification"`
}
type Aggregate struct {
	Rows     []AggregateRow `json:"rows"`
	Decision string         `json:"decision"`
}

func ReadEvidence(path string) (Evidence, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Evidence{}, err
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	var evidence Evidence
	if err := decoder.Decode(&evidence); err != nil {
		return Evidence{}, fmt.Errorf("decode evidence: %w", err)
	}
	if evidence.SchemaVersion != 1 {
		return Evidence{}, errors.New("evidence schema version is invalid")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return Evidence{}, errors.New("evidence has multiple JSON values")
	}
	if err := validateEvidence(evidence); err != nil {
		return Evidence{}, err
	}
	return evidence, nil
}

func validateEvidence(evidence Evidence) error {
	if len(evidence.Baseline) != 0 && len(evidence.Baseline) != 8 {
		return errors.New("baseline evidence must contain eight attempts")
	}
	for _, attempt := range evidence.Baseline {
		if err := validateAttempt(attempt, DetectorRef, true); err != nil {
			return fmt.Errorf("baseline evidence: %w", err)
		}
	}
	for _, candidate := range evidence.Candidates {
		if candidate.ID == "" {
			return errors.New("candidate evidence lacks identity")
		}
		for _, attempt := range candidate.Legacy {
			if attempt.Contract == "" || attempt.Ref != LegacyRef || attempt.CandidateID != candidate.ID || !sha256Pattern.MatchString(attempt.PatchSHA256) || !validStatus(attempt.Status) || attempt.DurationSeconds < 0 {
				return fmt.Errorf("candidate %q has malformed legacy evidence", candidate.ID)
			}
		}
		for _, attempt := range candidate.Detector {
			if err := validateAttempt(attempt, DetectorRef, false); err != nil {
				return fmt.Errorf("candidate %q detector evidence: %w", candidate.ID, err)
			}
			if attempt.CandidateID != candidate.ID || !sha256Pattern.MatchString(attempt.PatchSHA256) {
				return fmt.Errorf("candidate %q detector patch identity is malformed", candidate.ID)
			}
		}
	}
	return nil
}

func validStatus(status string) bool {
	return status == "passed" || status == "failed" || status == "infrastructure-failure"
}
func validateAttempt(attempt Attempt, ref string, requireProof bool) error {
	if !validStatus(attempt.Status) || attempt.DurationSeconds < 0 || attempt.Identity.Commit != ref || !sha256Pattern.MatchString(attempt.Identity.EngineSHA256) || !sha256Pattern.MatchString(attempt.Identity.RepositoryHash) {
		return errors.New("malformed attempt identity")
	}
	if requireProof && (attempt.Identity.Proof == "" || (attempt.Identity.ModuleID != "" && attempt.Identity.ScenarioID == "")) {
		return errors.New("missing proof identity")
	}
	if attempt.Status == "passed" && attempt.Failure != nil {
		return errors.New("passed attempt has failure")
	}
	if attempt.Status != "passed" && attempt.Failure == nil {
		return errors.New("failed attempt lacks structured failure")
	}
	return nil
}

// AggregateArtifacts accepts only the fixed artifact inventory produced by the
// manual preflight. Each candidate lane contributes one bounded evidence.json.
func AggregateArtifacts(manifest Manifest, root string) (Aggregate, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return Aggregate{}, err
	}
	artifacts := map[string]string{}
	for _, entry := range entries {
		if !entry.IsDir() {
			return Aggregate{}, errors.New("evidence inventory contains a file")
		}
		name := entry.Name()
		if name == "efficacy-baseline" || strings.HasPrefix(name, "efficacy-") {
			if _, duplicate := artifacts[name]; duplicate {
				return Aggregate{}, errors.New("evidence inventory contains duplicate")
			}
			artifacts[name] = filepath.Join(root, name, "evidence.json")
		} else {
			return Aggregate{}, errors.New("evidence inventory contains unknown artifact")
		}
	}
	if len(artifacts) != 19 || artifacts["efficacy-baseline"] == "" {
		return Aggregate{}, errors.New("evidence inventory is incomplete")
	}
	evidence := Evidence{SchemaVersion: 1}
	baseline, err := ReadEvidence(artifacts["efficacy-baseline"])
	if err != nil {
		return Aggregate{}, err
	}
	evidence.Baseline = baseline.Baseline
	for _, candidate := range manifest.Candidates {
		row := CandidateEvidence{ID: candidate.ID}
		for _, osName := range []string{"windows-latest", "ubuntu-latest", "macos-latest"} {
			lane, err := ReadEvidence(artifacts["efficacy-"+candidate.ID+"-"+osName])
			if err != nil || len(lane.Candidates) != 1 || lane.Candidates[0].ID != candidate.ID {
				return Aggregate{}, errors.New("evidence inventory has invalid candidate lane")
			}
			laneCandidate := lane.Candidates[0]
			if len(lane.Baseline) != 0 || !validLaneInventory(osName, laneCandidate) {
				return Aggregate{}, errors.New("evidence inventory has invalid candidate lane attempts")
			}
			row.Legacy = append(row.Legacy, laneCandidate.Legacy...)
			row.Detector = append(row.Detector, laneCandidate.Detector...)
		}
		evidence.Candidates = append(evidence.Candidates, row)
	}
	return Classify(manifest, evidence)
}

func Classify(manifest Manifest, evidence Evidence) (Aggregate, error) {
	if err := validateEvidence(evidence); err != nil {
		return Aggregate{}, err
	}
	if len(evidence.Baseline) != 8 {
		return Aggregate{}, errors.New("baseline evidence is incomplete")
	}
	baselineGreen := true
	baseline := map[string][]Attempt{}
	for _, attempt := range evidence.Baseline {
		if attempt.Status != "passed" {
			baselineGreen = false
		}
		baseline[attempt.Identity.ModuleID+"\x00"+attempt.Identity.ScenarioID] = append(baseline[attempt.Identity.ModuleID+"\x00"+attempt.Identity.ScenarioID], attempt)
	}
	for _, key := range []string{"\x00", "apps.vlc\x00default-v1", "apps.alacritty\x00default-v1", "apps.obs-studio\x00default-v1"} {
		attempts := baseline[key]
		if len(attempts) != 2 || attempts[0].Identity != attempts[1].Identity {
			return Aggregate{}, errors.New("baseline repetition identity differs")
		}
	}
	if len(evidence.Candidates) != len(manifest.Candidates) {
		return Aggregate{}, errors.New("candidate evidence inventory is incomplete")
	}
	aggregate := Aggregate{Rows: make([]AggregateRow, 0, len(manifest.Candidates)), Decision: DecisionInsufficientSignal}
	correct, modules := 0, 0
	families := map[string]bool{}
	invalid := false
	for index, candidate := range manifest.Candidates {
		evidenceCandidate := evidence.Candidates[index]
		if evidenceCandidate.ID != candidate.ID {
			return Aggregate{}, errors.New("candidate evidence order differs from manifest")
		}
		if len(evidenceCandidate.Legacy) != 4 || len(evidenceCandidate.Detector) != 2 {
			return Aggregate{}, fmt.Errorf("candidate %q has incomplete attempt inventory", candidate.ID)
		}
		legacy := map[string]LegacyAttempt{}
		for _, attempt := range evidenceCandidate.Legacy {
			legacy[attempt.Contract] = attempt
		}
		for _, contract := range []string{"windows-go", "windows-integration", "ubuntu-go", "macos-go"} {
			attempt, found := legacy[contract]
			if !found || attempt.PatchSHA256 != candidate.Legacy.SHA256 {
				return Aggregate{}, fmt.Errorf("candidate %q legacy contract identity differs", candidate.ID)
			}
		}
		for _, attempt := range evidenceCandidate.Detector {
			if attempt.CandidateID != candidate.ID || attempt.PatchSHA256 != candidate.Detector.SHA256 {
				return Aggregate{}, fmt.Errorf("candidate %q detector patch identity differs", candidate.ID)
			}
		}
		classification := ClassificationInfrastructureFailure
		if baselineGreen {
			classification = classifyCandidate(candidate, evidenceCandidate)
		}
		aggregate.Rows = append(aggregate.Rows, AggregateRow{ID: candidate.ID, Classification: classification})
		if classification == ClassificationCorrectNewOnlyKill {
			correct++
			families[candidate.Family] = true
			if candidate.ModuleID != "" {
				modules++
			}
		}
		if classification == ClassificationWrongKill || classification == ClassificationFlake || classification == ClassificationInfrastructureFailure {
			invalid = true
		}
	}
	if !invalid && correct >= 5 && modules == 3 && len(families) >= 2 {
		aggregate.Decision = DecisionMeaningfulSignal
	}
	return aggregate, nil
}

func classifyCandidate(candidate Candidate, evidence CandidateEvidence) string {
	for _, attempt := range evidence.Legacy {
		if attempt.Status == "infrastructure-failure" {
			return ClassificationInfrastructureFailure
		}
	}
	for _, attempt := range evidence.Detector {
		if attempt.Status == "infrastructure-failure" {
			return ClassificationInfrastructureFailure
		}
	}
	if evidence.Detector[0].Status != evidence.Detector[1].Status || evidence.Detector[0].Identity != evidence.Detector[1].Identity || !sameFailure(evidence.Detector[0].Failure, evidence.Detector[1].Failure) {
		return ClassificationFlake
	}
	for _, attempt := range evidence.Legacy {
		if attempt.Status != "passed" {
			return ClassificationAlreadyCovered
		}
	}
	if evidence.Detector[0].Status == "passed" {
		return ClassificationSurvivor
	}
	if evidence.Detector[0].CandidateID != candidate.ID || evidence.Detector[0].PatchSHA256 != candidate.Detector.SHA256 || *evidence.Detector[0].Failure != candidate.Expected {
		return ClassificationWrongKill
	}
	return ClassificationCorrectNewOnlyKill
}

func validLaneInventory(osName string, evidence CandidateEvidence) bool {
	contracts := map[string]bool{}
	for _, attempt := range evidence.Legacy {
		contracts[attempt.Contract] = true
	}
	switch osName {
	case "windows-latest":
		return len(evidence.Legacy) == 2 && contracts["windows-go"] && contracts["windows-integration"] && len(evidence.Detector) == 2
	case "ubuntu-latest":
		return len(evidence.Legacy) == 1 && contracts["ubuntu-go"] && len(evidence.Detector) == 0
	case "macos-latest":
		return len(evidence.Legacy) == 1 && contracts["macos-go"] && len(evidence.Detector) == 0
	}
	return false
}
func sameFailure(left, right *Failure) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

// InfrastructureAttempt preserves a bounded detector setup failure for
// aggregation when a fresh authority cannot be acquired or built.
func InfrastructureAttempt(moduleID, scenarioID, detail string) Attempt {
	return Attempt{
		Status:   "infrastructure-failure",
		Failure:  &Failure{Code: "isolation_failure", Phase: "setup", Coordinate: "detector", ChildReason: detail},
		Identity: ProofIdentity{Commit: DetectorRef, EngineSHA256: strings.Repeat("0", 64), RepositoryHash: strings.Repeat("0", 64), ModuleID: moduleID, ScenarioID: scenarioID},
	}
}

type DetectorRequest struct {
	EnginePath string
	RepoRoot   string
	Commit     string
	ModuleID   string
	ScenarioID string
	ResultPath string
	Catalog    bool
}

func RunDetector(ctx context.Context, request DetectorRequest) (Attempt, error) {
	identity, err := detectorIdentity(request)
	if err != nil {
		return Attempt{}, err
	}
	attempt := Attempt{Status: "failed", Identity: identity}
	if request.Catalog {
		result, err := runCatalogMatrix(ctx, validationharness.CatalogMatrixRequest{EnginePath: request.EnginePath, RepoRoot: request.RepoRoot, ResultPath: request.ResultPath})
		if err != nil {
			return Attempt{}, err
		}
		attempt.Status = result.Status
		attempt.Identity.Proof = strings.Join(proofStrings(result.ProofLevels), ",")
		if result.Status != "passed" {
			attempt.Failure = catalogFailure(result)
		}
		return attempt, nil
	}
	result, err := runScenario(ctx, validationharness.Request{EnginePath: request.EnginePath, RepoRoot: request.RepoRoot, ModuleID: request.ModuleID, ScenarioID: request.ScenarioID, ResultPath: request.ResultPath})
	if err != nil {
		return Attempt{}, err
	}
	attempt.Status, attempt.Identity.ModuleID, attempt.Identity.ScenarioID, attempt.Identity.Proof = result.Status, result.ModuleID, result.ScenarioID, strings.Join(proofStrings(result.ProofLevels), ",")
	if result.Failure != nil {
		attempt.Failure = &Failure{Code: result.Failure.Code, Phase: result.Failure.Phase, Coordinate: result.Failure.Coordinate}
	}
	return attempt, nil
}
func catalogFailure(result validationharness.CatalogMatrixResult) *Failure {
	for _, row := range result.Rows {
		if len(row.Failures) > 0 {
			return &Failure{Code: "execution_failure", Phase: "catalog-plan", Coordinate: "success", ChildReason: row.Failures[0].Reason}
		}
		if row.Failure != nil {
			child := ""
			return &Failure{Code: row.Failure.Code, Phase: row.Failure.Phase, Coordinate: row.Failure.Coordinate, ChildReason: child}
		}
	}
	if result.Failure != nil {
		return &Failure{Code: result.Failure.Code, Phase: result.Failure.Phase, Coordinate: result.Failure.Coordinate}
	}
	return &Failure{Code: "execution_failure", Phase: "catalog-plan", Coordinate: "unknown"}
}
func proofStrings(values []validationmatrix.ProofLevel) []string {
	result := make([]string, len(values))
	for index, value := range values {
		result[index] = string(value)
	}
	return result
}

func detectorIdentity(request DetectorRequest) (ProofIdentity, error) {
	if request.Commit != DetectorRef {
		return ProofIdentity{}, errors.New("detector commit is not fixed")
	}
	engine, err := digestFile(request.EnginePath)
	if err != nil {
		return ProofIdentity{}, err
	}
	repository, err := digestRepository(request.RepoRoot)
	if err != nil {
		return ProofIdentity{}, err
	}
	return ProofIdentity{Commit: request.Commit, EngineSHA256: engine, RepositoryHash: repository, ModuleID: request.ModuleID, ScenarioID: request.ScenarioID}, nil
}

func digestFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func digestRepository(root string) (string, error) {
	var paths []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if relative == "." {
			return nil
		}
		if relative == ".git" {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type().IsRegular() {
			paths = append(paths, filepath.ToSlash(relative))
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(paths)
	hash := sha256.New()
	for _, path := range paths {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
		if err != nil {
			return "", err
		}
		_, _ = io.WriteString(hash, path+"\n")
		_, _ = hash.Write(data)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func ComputePatchedModuleRevision(data []byte) (string, error) {
	return modules.ComputeModuleRevision(data)
}
