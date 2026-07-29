// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationpilot

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixedManifestForTest(t *testing.T) Manifest {
	t.Helper()
	return Manifest{Candidates: []Candidate{
		{ID: "bundle-duplicate", Family: "catalog", Expected: Failure{Code: "execution_failure", Phase: "catalog-plan", Coordinate: "success", ChildReason: "duplicate_membership"}},
		{ID: "bundle-missing", Family: "catalog", Expected: Failure{Code: "execution_failure", Phase: "catalog-plan", Coordinate: "success", ChildReason: "missing_module"}},
		{ID: "bundle-id-drift", Family: "catalog", Expected: Failure{Code: "envelope_contract", Phase: "catalog-plan", Coordinate: "envelope"}},
		{ID: "vlc-backup-off", Family: "module", ModuleID: "apps.vlc", ScenarioID: "default-v1", Expected: Failure{Code: "unsupported_fixture", Phase: "fixture", Coordinate: "restore[0]"}},
		{ID: "alacritty-source-drift", Family: "module", ModuleID: "apps.alacritty", ScenarioID: "default-v1", Expected: Failure{Code: "unsupported_fixture", Phase: "fixture", Coordinate: "capture.files[0]"}},
		{ID: "obs-target-drift", Family: "module", ModuleID: "apps.obs-studio", ScenarioID: "default-v1", Expected: Failure{Code: "unsupported_fixture", Phase: "fixture", Coordinate: "restore[1]"}},
	}}
}

func fixedEvidenceForTest(manifest Manifest) Evidence {
	evidence := Evidence{Baseline: []Attempt{{Status: "passed"}, {Status: "passed"}}}
	for _, candidate := range manifest.Candidates {
		evidence.Candidates = append(evidence.Candidates, CandidateEvidence{ID: candidate.ID, Legacy: []Attempt{{Status: "passed"}, {Status: "passed"}, {Status: "passed"}}, Detector: []Attempt{{Status: "failed", Failure: candidate.Expected}, {Status: "failed", Failure: candidate.Expected}}})
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
	wrong.Candidates[0].Detector[0].Failure.Coordinate = "wrong"
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
	if _, err := ReadEvidence(path); err == nil || !strings.Contains(err.Error(), "duration") {
		t.Fatalf("ReadEvidence() error = %v, want duration rejection", err)
	}
}
