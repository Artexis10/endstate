// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

// Package validationpilot validates the fixed hosted CI efficacy preflight.
package validationpilot

import (
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
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/modules"
)

const (
	LegacyRef   = "ab8065cd67ab3f4e9e876e07a25facf3100c28c7"
	DetectorRef = "437c0ca4167c09bc9f2de515daa6d55d35257d4f"
	ClassificationCorrectNewOnlyKill = "correct-new-only-kill"
	ClassificationAlreadyCovered = "already-covered"
	ClassificationWrongKill = "wrong-kill"
	ClassificationSurvivor = "survivor"
	ClassificationFlake = "flake"
	ClassificationInfrastructureFailure = "infrastructure-failure"
	DecisionMeaningfulSignal = "meaningful-signal"
	DecisionInsufficientSignal = "insufficient-signal"
)

var sha256Pattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

type Failure struct { Code string `json:"code"`; Phase string `json:"phase"`; Coordinate string `json:"coordinate"`; ChildReason string `json:"childReason,omitempty"` }
type Patch struct { Path string `json:"path"`; SHA256 string `json:"sha256"` }
type Candidate struct { ID string `json:"id"`; Family string `json:"family"`; ModuleID string `json:"moduleId,omitempty"`; ScenarioID string `json:"scenarioId,omitempty"`; Expected Failure `json:"expected"`; Legacy Patch `json:"legacy"`; Detector Patch `json:"detector"` }
type Manifest struct { SchemaVersion int `json:"schemaVersion"`; LegacyRef string `json:"legacyRef"`; DetectorRef string `json:"detectorRef"`; Candidates []Candidate `json:"candidates"` }

func LoadManifest(path string) (Manifest, error) {
	data, err := os.ReadFile(path); if err != nil { return Manifest{}, err }
	decoder := json.NewDecoder(strings.NewReader(string(data))); decoder.DisallowUnknownFields()
	var manifest Manifest
	if err := decoder.Decode(&manifest); err != nil { return Manifest{}, fmt.Errorf("decode manifest: %w", err) }
	var extra any; if err := decoder.Decode(&extra); err == nil { return Manifest{}, errors.New("manifest has multiple JSON values") }
	if manifest.SchemaVersion != 1 || manifest.LegacyRef != LegacyRef || manifest.DetectorRef != DetectorRef { return Manifest{}, errors.New("manifest does not bind fixed references") }
	if len(manifest.Candidates) != 6 { return Manifest{}, errors.New("manifest must contain exactly six candidates") }
	ids := []string{"bundle-duplicate", "bundle-missing", "bundle-id-drift", "vlc-backup-off", "alacritty-source-drift", "obs-target-drift"}
	for index, candidate := range manifest.Candidates { if candidate.ID != ids[index] { return Manifest{}, fmt.Errorf("manifest candidate order %d is not fixed", index) }; for _, patch := range []Patch{candidate.Legacy, candidate.Detector} { if !strings.HasPrefix(patch.Path, "patches/"+candidate.ID+"/") || !sha256Pattern.MatchString(patch.SHA256) { return Manifest{}, fmt.Errorf("candidate %q has invalid patch identity", candidate.ID) } } }
	return manifest, nil
}

func ValidateCorpus(root string, manifest Manifest) error {
	canonicalRoot, err := filepath.Abs(root)
	if err != nil { return err }
	root = canonicalRoot
	for _, path := range []string{"bundles/gaming.jsonc", "bundles/remote-access.jsonc", "bundles/communication.jsonc", "modules/apps/vlc/module.jsonc", "modules/apps/alacritty/module.jsonc", "modules/apps/obs-studio/module.jsonc"} {
		legacy, err := gitOutput(root, nil, "rev-parse", manifest.LegacyRef+":"+path); if err != nil { return fmt.Errorf("read legacy production identity: %w", err) }
		detector, err := gitOutput(root, nil, "rev-parse", manifest.DetectorRef+":"+path); if err != nil { return fmt.Errorf("read detector production identity: %w", err) }
		if strings.TrimSpace(legacy) != strings.TrimSpace(detector) { return fmt.Errorf("production identity differs for %q", path) }
	}
	for _, candidate := range manifest.Candidates {
		for patchIndex, patch := range []Patch{candidate.Legacy, candidate.Detector} {
			data, err := os.ReadFile(filepath.Join(root, "validation", "ci-efficacy", "pilot-v0", filepath.FromSlash(patch.Path))); if err != nil { return err }
			sum := sha256.Sum256(data); if hex.EncodeToString(sum[:]) != patch.SHA256 { return fmt.Errorf("candidate %q patch digest differs", candidate.ID) }
			if err := validatePatch(candidate, patch.Path, string(data)); err != nil { return err }
			ref := manifest.LegacyRef; if patchIndex == 1 { ref = manifest.DetectorRef }
			if err := applyAndValidatePatch(root, ref, filepath.Join(root, "validation", "ci-efficacy", "pilot-v0", filepath.FromSlash(patch.Path)), candidate, patchIndex == 1); err != nil { return err }
		}
	}
	return nil
}

func applyAndValidatePatch(root, ref, patchPath string, candidate Candidate, detector bool) error {
	index, err := os.CreateTemp("", "endstate-validation-pilot-index-"); if err != nil { return err }
	indexPath := index.Name(); if err := index.Close(); err != nil { return err }; if err := os.Remove(indexPath); err != nil { return err }; defer os.Remove(indexPath)
	env := []string{"GIT_INDEX_FILE=" + indexPath}
	if _, err := gitOutput(root, env, "read-tree", ref); err != nil { return fmt.Errorf("load patch reference: %w", err) }
	if _, err := gitOutput(root, env, "apply", "--cached", "--check", patchPath); err != nil { return fmt.Errorf("patch does not apply to %s: %w", ref, err) }
	if _, err := gitOutput(root, env, "apply", "--cached", patchPath); err != nil { return fmt.Errorf("apply patch: %w", err) }
	if candidate.ModuleID == "" || !detector { return nil }
	modulePath := "modules/apps/" + strings.TrimPrefix(candidate.ModuleID, "apps.") + "/module.jsonc"
	moduleData, err := gitOutput(root, env, "show", ":"+modulePath); if err != nil { return err }
	revision, err := modules.ComputeModuleRevision([]byte(moduleData)); if err != nil { return err }
	sidecar, err := gitOutput(root, env, "show", ":modules/apps/"+strings.TrimPrefix(candidate.ModuleID, "apps.")+"/validation.jsonc"); if err != nil { return err }
	declared := map[string]json.RawMessage{}
	if err := json.Unmarshal([]byte(sidecar), &declared); err != nil { return fmt.Errorf("candidate %q revision companion is malformed", candidate.ID) }
	var declaredRevision string
	if err := json.Unmarshal(declared["moduleRevision"], &declaredRevision); err != nil || declaredRevision != revision { return fmt.Errorf("candidate %q revision companion differs", candidate.ID) }
	return nil
}

func gitOutput(root string, extraEnv []string, args ...string) (string, error) {
	command := exec.Command("git", append([]string{"-C", root}, args...)...)
	command.Env = append(os.Environ(), extraEnv...)
	data, err := command.CombinedOutput()
	if err != nil { return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(string(data))) }
	return string(data), nil
}

func validatePatch(candidate Candidate, path, patch string) error {
	if strings.Contains(patch, "fixtures/") || strings.Contains(patch, "scenarios") || strings.Contains(patch, "timeoutSeconds") || strings.Contains(patch, "minimumAssertions") || strings.Contains(patch, "\"live\"") { return fmt.Errorf("candidate %q changes forbidden validation policy", candidate.ID) }
	targets := map[string]bool{}
	for _, line := range strings.Split(patch, "\n") { if strings.HasPrefix(line, "+++ b/") { targets[strings.TrimPrefix(line, "+++ b/")] = true } }
	if len(targets) == 0 { return fmt.Errorf("candidate %q patch has no target", candidate.ID) }
	if candidate.ModuleID == "" {
		bundleTargets := map[string]string{"bundle-duplicate": "bundles/gaming.jsonc", "bundle-missing": "bundles/remote-access.jsonc", "bundle-id-drift": "bundles/communication.jsonc"}
		if len(targets) != 1 || !targets[bundleTargets[candidate.ID]] { return fmt.Errorf("candidate %q does not bind its bundle target", candidate.ID) }
		return nil
	}
	modulePath := "modules/apps/"+strings.TrimPrefix(candidate.ModuleID, "apps.")+"/module.jsonc"
	if !targets[modulePath] || len(targets) > 2 || (len(targets) == 2 && !targets["modules/apps/"+strings.TrimPrefix(candidate.ModuleID, "apps.")+"/validation.jsonc"]) { return fmt.Errorf("candidate %q does not bind only its module and revision companion", candidate.ID) }
	if strings.Contains(path, "detector") && len(targets) != 2 { return fmt.Errorf("candidate %q detector patch lacks revision companion", candidate.ID) }
	return nil
}

type Attempt struct { Status string `json:"status"`; Failure Failure `json:"failure"`; DurationSeconds float64 `json:"durationSeconds"` }
type CandidateEvidence struct { ID string `json:"id"`; Legacy []Attempt `json:"legacy"`; Detector []Attempt `json:"detector"` }
type Evidence struct { SchemaVersion int `json:"schemaVersion"`; Baseline []Attempt `json:"baseline"`; Candidates []CandidateEvidence `json:"candidates"` }
type AggregateRow struct { ID string `json:"id"`; Classification string `json:"classification"` }
type Aggregate struct { Rows []AggregateRow `json:"rows"`; Decision string `json:"decision"` }

func ReadEvidence(path string) (Evidence, error) {
	data, err := os.ReadFile(path)
	if err != nil { return Evidence{}, err }
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	var evidence Evidence
	if err := decoder.Decode(&evidence); err != nil { return Evidence{}, fmt.Errorf("decode evidence: %w", err) }
	if evidence.SchemaVersion != 1 { return Evidence{}, errors.New("evidence schema version is invalid") }
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF { return Evidence{}, errors.New("evidence has multiple JSON values") }
	for _, attempt := range evidence.Baseline {
		if attempt.DurationSeconds < 0 { return Evidence{}, errors.New("evidence duration is negative") }
	}
	return evidence, nil
}

// AggregateArtifacts accepts only the fixed artifact inventory produced by the
// manual preflight. Each candidate lane contributes one bounded evidence.json.
func AggregateArtifacts(manifest Manifest, root string) (Aggregate, error) {
	entries, err := os.ReadDir(root)
	if err != nil { return Aggregate{}, err }
	artifacts := map[string]string{}
	for _, entry := range entries {
		if !entry.IsDir() { return Aggregate{}, errors.New("evidence inventory contains a file") }
		name := entry.Name()
		if name == "efficacy-baseline" || strings.HasPrefix(name, "efficacy-") {
			if _, duplicate := artifacts[name]; duplicate { return Aggregate{}, errors.New("evidence inventory contains duplicate") }
			artifacts[name] = filepath.Join(root, name, "evidence.json")
		} else { return Aggregate{}, errors.New("evidence inventory contains unknown artifact") }
	}
	if len(artifacts) != 19 || artifacts["efficacy-baseline"] == "" { return Aggregate{}, errors.New("evidence inventory is incomplete") }
	evidence := Evidence{SchemaVersion: 1}
	baseline, err := ReadEvidence(artifacts["efficacy-baseline"])
	if err != nil { return Aggregate{}, err }
	evidence.Baseline = baseline.Baseline
	for _, candidate := range manifest.Candidates {
		row := CandidateEvidence{ID: candidate.ID}
		for _, osName := range []string{"windows-latest", "ubuntu-latest", "macos-latest"} {
			lane, err := ReadEvidence(artifacts["efficacy-"+candidate.ID+"-"+osName])
			if err != nil || len(lane.Candidates) != 1 || lane.Candidates[0].ID != candidate.ID { return Aggregate{}, errors.New("evidence inventory has invalid candidate lane") }
			row.Legacy = append(row.Legacy, lane.Candidates[0].Legacy...)
			row.Detector = append(row.Detector, lane.Candidates[0].Detector...)
		}
		evidence.Candidates = append(evidence.Candidates, row)
	}
	return Classify(manifest, evidence)
}

func Classify(manifest Manifest, evidence Evidence) (Aggregate, error) {
	if len(evidence.Baseline) != 2 { return Aggregate{}, errors.New("baseline evidence is incomplete") }
	for _, attempt := range evidence.Baseline { if attempt.Status != "passed" { return Aggregate{}, errors.New("baseline evidence is not green") } }
	if len(evidence.Candidates) != len(manifest.Candidates) { return Aggregate{}, errors.New("candidate evidence inventory is incomplete") }
	aggregate := Aggregate{Rows: make([]AggregateRow, 0, len(manifest.Candidates)), Decision: DecisionInsufficientSignal}
	correct, modules := 0, 0
	families := map[string]bool{}
	invalid := false
	for index, candidate := range manifest.Candidates {
		evidenceCandidate := evidence.Candidates[index]
		if evidenceCandidate.ID != candidate.ID { return Aggregate{}, errors.New("candidate evidence order differs from manifest") }
		if len(evidenceCandidate.Legacy) != 3 || len(evidenceCandidate.Detector) != 2 { return Aggregate{}, fmt.Errorf("candidate %q has incomplete attempt inventory", candidate.ID) }
		classification := ClassificationCorrectNewOnlyKill
		for _, attempt := range evidenceCandidate.Legacy { if attempt.Status != "passed" { classification = ClassificationAlreadyCovered } }
		for _, attempt := range evidenceCandidate.Detector { if attempt.Status != "failed" { classification = ClassificationSurvivor; continue }; if attempt.Failure != candidate.Expected { classification = ClassificationWrongKill } }
		aggregate.Rows = append(aggregate.Rows, AggregateRow{ID: candidate.ID, Classification: classification})
		if classification == ClassificationCorrectNewOnlyKill { correct++; families[candidate.Family] = true; if candidate.ModuleID != "" { modules++ } }
		if classification == ClassificationWrongKill || classification == ClassificationFlake || classification == ClassificationInfrastructureFailure { invalid = true }
	}
	if !invalid && correct >= 5 && modules == 3 && len(families) >= 2 { aggregate.Decision = DecisionMeaningfulSignal }
	return aggregate, nil
}

func ComputePatchedModuleRevision(data []byte) (string, error) { return modules.ComputeModuleRevision(data) }
