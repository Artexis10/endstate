// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationpilot

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/validationharness"
)

func fixedManifestForTest(t *testing.T) Manifest {
	t.Helper()
	manifest := Manifest{Candidates: []Candidate{
		{ID: "bundle-duplicate", Family: "catalog", Expected: Failure{Code: "execution_failure", Phase: "catalog-plan", Coordinate: "success", ChildReason: "duplicate_membership"}},
		{ID: "bundle-missing", Family: "catalog", Expected: Failure{Code: "execution_failure", Phase: "catalog-plan", Coordinate: "success", ChildReason: "missing_module"}},
		{ID: "bundle-id-drift", Family: "catalog", Expected: Failure{Code: "envelope_contract", Phase: "catalog-plan", Coordinate: "envelope"}},
		{ID: "vlc-backup-off", Family: "module", ModuleID: "apps.vlc", ScenarioID: "default-v1", Expected: Failure{Code: "unsupported_fixture", Phase: "fixture", Coordinate: "restore[0]"}},
		{ID: "alacritty-source-drift", Family: "module", ModuleID: "apps.alacritty", ScenarioID: "default-v1", Expected: Failure{Code: "unsupported_fixture", Phase: "fixture", Coordinate: "capture.files[0]"}},
		{ID: "obs-target-drift", Family: "module", ModuleID: "apps.obs-studio", ScenarioID: "default-v1", Expected: Failure{Code: "unsupported_fixture", Phase: "fixture", Coordinate: "restore[1]"}},
	}}
	for index := range manifest.Candidates {
		manifest.Candidates[index].Legacy.SHA256 = strings.Repeat("c", 64)
		manifest.Candidates[index].Detector.SHA256 = strings.Repeat("d", 64)
	}
	return manifest
}

func fixedEvidenceForTest(manifest Manifest) Evidence {
	identity := func(module, scenario, proof string) ProofIdentity {
		return ProofIdentity{Commit: DetectorRef, EngineSHA256: strings.Repeat("a", 64), RepositoryHash: strings.Repeat("b", 64), ModuleID: module, ScenarioID: scenario, Proof: proof}
	}
	evidence := Evidence{Baseline: []Attempt{}}
	for _, baseline := range []struct{ module, scenario, proof string }{{"", "", "catalog"}, {"apps.vlc", "default-v1", "config"}, {"apps.alacritty", "default-v1", "config"}, {"apps.obs-studio", "default-v1", "config"}} {
		evidence.Baseline = append(evidence.Baseline, Attempt{Status: "passed", Identity: identity(baseline.module, baseline.scenario, baseline.proof)}, Attempt{Status: "passed", Identity: identity(baseline.module, baseline.scenario, baseline.proof)})
	}
	for _, candidate := range manifest.Candidates {
		legacy := []LegacyAttempt{}
		for _, contract := range []string{"windows-go", "windows-integration", "ubuntu-go", "macos-go"} {
			legacy = append(legacy, LegacyAttempt{Contract: contract, Ref: LegacyRef, CandidateID: candidate.ID, PatchSHA256: candidate.Legacy.SHA256, Status: "passed"})
		}
		failure := candidate.Expected
		evidence.Candidates = append(evidence.Candidates, CandidateEvidence{ID: candidate.ID, Legacy: legacy, Detector: []Attempt{{Status: "failed", Failure: &failure, Identity: identity(candidate.ModuleID, candidate.ScenarioID, ""), CandidateID: candidate.ID, PatchSHA256: strings.Repeat("d", 64)}, {Status: "failed", Failure: &failure, Identity: identity(candidate.ModuleID, candidate.ScenarioID, ""), CandidateID: candidate.ID, PatchSHA256: strings.Repeat("d", 64)}}})
		evidence.Candidates[len(evidence.Candidates)-1].Detector[0].PatchSHA256 = candidate.Detector.SHA256
		evidence.Candidates[len(evidence.Candidates)-1].Detector[1].PatchSHA256 = candidate.Detector.SHA256
	}
	return evidence
}

func TestLoadManifestRejectsUnknownFieldsAndRequiresFixedCandidates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pilot.json")
	if err := os.WriteFile(path, []byte(`{"schemaVersion":1,"legacyRef":"ab8065cd67ab3f4e9e876e07a25facf3100c28c7","detectorRef":"437c0ca4167c09bc9f2de515daa6d55d35257d4f","candidates":[],"extra":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadManifest(path); err == nil || !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("LoadManifest() error = %v, want unknown-field rejection", err)
	}

	if err := os.WriteFile(path, []byte(`{"schemaVersion":1,"legacyRef":"ab8065cd67ab3f4e9e876e07a25facf3100c28c7","detectorRef":"437c0ca4167c09bc9f2de515daa6d55d35257d4f","candidates":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadManifest(path); err == nil || !strings.Contains(err.Error(), "six") {
		t.Fatalf("LoadManifest() error = %v, want fixed-candidate rejection", err)
	}
}

func TestValidateCorpusBindsCandidatePatchesAndModuleCompanions(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	manifest, err := LoadManifest(filepath.Join(root, "validation", "ci-efficacy", "pilot-v0", "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateCorpus(root, manifest); err != nil {
		t.Fatal(err)
	}
}

func TestClassifyEvidenceRejectsReorderedInventoryAndRequiresExactDetectorFailure(t *testing.T) {
	manifest := fixedManifestForTest(t)
	valid := fixedEvidenceForTest(manifest)
	if _, err := Classify(manifest, valid); err != nil {
		t.Fatalf("Classify(valid) error = %v", err)
	}

	reordered := fixedEvidenceForTest(manifest)
	reordered.Candidates[0], reordered.Candidates[1] = reordered.Candidates[1], reordered.Candidates[0]
	if _, err := Classify(manifest, reordered); err == nil || !strings.Contains(err.Error(), "order") {
		t.Fatalf("Classify(reordered) error = %v, want ordered-inventory rejection", err)
	}

	wrong := fixedEvidenceForTest(manifest)
	wrongFailure := *wrong.Candidates[0].Detector[0].Failure
	wrongFailure.Coordinate = "wrong"
	wrong.Candidates[0].Detector[0].Failure = &wrongFailure
	wrong.Candidates[0].Detector[1].Failure = &wrongFailure
	aggregate, err := Classify(manifest, wrong)
	if err != nil {
		t.Fatal(err)
	}
	if aggregate.Rows[0].Classification != ClassificationWrongKill || aggregate.Decision != DecisionInsufficientSignal {
		t.Fatalf("Classify(wrong) = %#v, want wrong kill and insufficient signal", aggregate)
	}
}

func TestReadEvidenceRejectsUnknownFieldsAndNonFiniteTimings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "evidence.json")
	if err := os.WriteFile(path, []byte(`{"schemaVersion":1,"baseline":[{"status":"passed","durationSeconds":1}],"unknown":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadEvidence(path); err == nil || !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("ReadEvidence() error = %v, want unknown field rejection", err)
	}
	if err := os.WriteFile(path, []byte(`{"schemaVersion":1,"baseline":[{"status":"passed","durationSeconds":-1}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadEvidence(path); err == nil {
		t.Fatalf("ReadEvidence() error = %v, want malformed evidence rejection", err)
	}
}

func TestReadEvidenceRequiresBoundedProofAndContractIdentities(t *testing.T) {
	path := filepath.Join(t.TempDir(), "evidence.json")
	if err := os.WriteFile(path, []byte(`{"schemaVersion":1,"baseline":[{"status":"passed","durationSeconds":1}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadEvidence(path); err == nil || !strings.Contains(err.Error(), "baseline") {
		t.Fatalf("ReadEvidence() error = %v, want baseline identity rejection", err)
	}
}

func TestAggregateArtifactInventoryRequiresBaselineAndEighteenCandidateLanes(t *testing.T) {
	manifest := fixedManifestForTest(t)
	dir := t.TempDir()
	if _, err := AggregateArtifacts(manifest, dir); err == nil || !strings.Contains(err.Error(), "inventory") {
		t.Fatalf("AggregateArtifacts() error = %v, want inventory rejection", err)
	}
}

func TestClassifyTreatsDetectorDisagreementAndInfrastructureAsNonProof(t *testing.T) {
	manifest := fixedManifestForTest(t)
	evidence := fixedEvidenceForTest(manifest)
	different := *evidence.Candidates[0].Detector[1].Failure
	different.Coordinate = "different"
	evidence.Candidates[0].Detector[1].Failure = &different
	aggregate, err := Classify(manifest, evidence)
	if err != nil {
		t.Fatal(err)
	}
	if aggregate.Rows[0].Classification != ClassificationFlake || aggregate.Decision != DecisionInsufficientSignal {
		t.Fatalf("Classify(disagreement) = %#v, want flake and insufficient signal", aggregate)
	}
}

func TestClassifyRequiresBothDetectorPatchIdentities(t *testing.T) {
	manifest := fixedManifestForTest(t)
	evidence := fixedEvidenceForTest(manifest)
	evidence.Candidates[0].Detector[1].PatchSHA256 = strings.Repeat("e", 64)
	if _, err := Classify(manifest, evidence); err == nil || !strings.Contains(err.Error(), "patch") {
		t.Fatalf("Classify() error = %v, want detector patch identity rejection", err)
	}
}

func TestAggregateArtifactsAcceptsCompleteGoodInventory(t *testing.T) {
	manifest := fixedManifestForTest(t)
	dir := t.TempDir()
	writeEvidence := func(path string, evidence Evidence) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		data, err := json.Marshal(evidence)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	all := fixedEvidenceForTest(manifest)
	writeEvidence(filepath.Join(dir, "efficacy-baseline", "evidence.json"), Evidence{SchemaVersion: 1, Baseline: all.Baseline})
	for _, candidate := range all.Candidates {
		for _, lane := range []struct {
			os        string
			contracts []LegacyAttempt
			detector  []Attempt
		}{
			{"windows-latest", candidate.Legacy[:2], candidate.Detector},
			{"ubuntu-latest", candidate.Legacy[2:3], nil},
			{"macos-latest", candidate.Legacy[3:4], nil},
		} {
			writeEvidence(filepath.Join(dir, "efficacy-"+candidate.ID+"-"+lane.os, "evidence.json"), Evidence{SchemaVersion: 1, Candidates: []CandidateEvidence{{ID: candidate.ID, Legacy: lane.contracts, Detector: lane.detector}}})
		}
	}
	aggregate, err := AggregateArtifacts(manifest, dir)
	if err != nil {
		t.Fatal(err)
	}
	if aggregate.Decision != DecisionMeaningfulSignal || len(aggregate.Rows) != 6 {
		t.Fatalf("AggregateArtifacts() = %#v, want six-row meaningful signal", aggregate)
	}
}

func TestRunDetectorRejectsForeignCommitBeforeExecuting(t *testing.T) {
	_, err := RunDetector(context.Background(), DetectorRequest{Commit: LegacyRef})
	if err == nil || !strings.Contains(err.Error(), "fixed") {
		t.Fatalf("RunDetector() error = %v, want fixed-commit rejection", err)
	}
}

func TestRunDetectorPreservesStructuredModuleFailure(t *testing.T) {
	engine := filepath.Join(t.TempDir(), "engine")
	if err := os.WriteFile(engine, []byte("engine"), 0o700); err != nil {
		t.Fatal(err)
	}
	repo := t.TempDir()
	original := runScenario
	runScenario = func(context.Context, validationharness.Request) (validationharness.Result, error) {
		return validationharness.Result{Status: "failed", ModuleID: "apps.vlc", ScenarioID: "default-v1", Failure: &validationharness.Failure{Code: "unsupported_fixture", Phase: "fixture", Coordinate: "restore[0]"}}, nil
	}
	t.Cleanup(func() { runScenario = original })
	attempt, err := RunDetector(context.Background(), DetectorRequest{Commit: DetectorRef, EnginePath: engine, RepoRoot: repo, ModuleID: "apps.vlc", ScenarioID: "default-v1"})
	if err != nil {
		t.Fatal(err)
	}
	if attempt.Status != "failed" || attempt.Failure == nil || attempt.Failure.Code != "unsupported_fixture" || attempt.Failure.Coordinate != "restore[0]" {
		t.Fatalf("RunDetector() = %#v, want preserved structured failure", attempt)
	}
}
