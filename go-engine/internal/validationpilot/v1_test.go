// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationpilot

import (
	"strings"
	"testing"
)

func TestClassifyV1MeaningfulSignalRequiresCompleteSixCaseProof(t *testing.T) {
	manifest := V1Manifest{SchemaVersion: V1SchemaVersion}
	if _, err := ClassifyV1(manifest, V1Evidence{}); err == nil {
		t.Fatal("ClassifyV1() accepted an incomplete v1 proof")
	}
}

func TestV1ProofTreatsDiagnosticEngineHashAsNonGoverningButAuthorityDriftAsFlake(t *testing.T) {
	manifest, evidence := validV1Proof()
	evidence.Attempts[3].DiagnosticEngineSHA256 = strings.Repeat("e", 64)
	aggregate, err := ClassifyV1(manifest, evidence)
	if err != nil || aggregate.Decision != DecisionMeaningfulSignal {
		t.Fatalf("ClassifyV1(diagnostic drift) = %#v, %v", aggregate, err)
	}
	evidence.Attempts[3].Authorities.Freeze.Tree = strings.Repeat("f", 40)
	aggregate, err = ClassifyV1(manifest, evidence)
	if err != nil || aggregate.Rows[0].Classification != ClassificationFlake {
		t.Fatalf("ClassifyV1(authority drift) = %#v, %v", aggregate, err)
	}
}

func TestClassifyV1RejectsMissingDuplicateAndForeignAttemptInventory(t *testing.T) {
	manifest, _ := validV1Proof()
	for _, mutate := range []func(*V1Evidence){
		func(e *V1Evidence) { e.Attempts = e.Attempts[1:] },
		func(e *V1Evidence) { e.Attempts = append(e.Attempts, e.Attempts[0]) },
		func(e *V1Evidence) { e.Attempts[0].CandidateID = "foreign" },
	} {
		_, changed := validV1Proof()
		mutate(&changed)
		if _, err := ClassifyV1(manifest, changed); err == nil {
			t.Fatal("ClassifyV1() accepted invalid attempt inventory")
		}
	}
}

func TestClassifyV1InfrastructureAndShallowFailuresCannotBeCorrectKills(t *testing.T) {
	manifest, evidence := validV1Proof()
	evidence.Attempts[3].Status = V1StatusInfrastructure
	evidence.Attempts[3].Failure = nil
	aggregate, err := ClassifyV1(manifest, evidence)
	if err != nil || aggregate.Rows[0].Classification != ClassificationInfrastructureFailure {
		t.Fatalf("ClassifyV1(infrastructure) = %#v, %v", aggregate, err)
	}
	manifest, evidence = validV1Proof()
	evidence.Attempts[3].Failure = &V1Failure{Class: "assertion_contract", Phase: "aggregate", Coordinate: "rows"}
	evidence.Attempts[4].Failure = evidence.Attempts[3].Failure
	aggregate, err = ClassifyV1(manifest, evidence)
	if err != nil || aggregate.Rows[0].Classification != ClassificationWrongKill {
		t.Fatalf("ClassifyV1(shallow) = %#v, %v", aggregate, err)
	}
}

func TestClassifyV1RequiresDeclaredChildReason(t *testing.T) {
	manifest, evidence := validV1Proof()
	evidence.Attempts[3].Failure.ChildReason = "wrong_reason"
	evidence.Attempts[4].Failure.ChildReason = "wrong_reason"
	aggregate, err := ClassifyV1(manifest, evidence)
	if err != nil || aggregate.Rows[0].Classification != ClassificationWrongKill {
		t.Fatalf("ClassifyV1(wrong child) = %#v, %v", aggregate, err)
	}
}

func TestV1ManifestRejectsCalibrationFingerprints(t *testing.T) {
	manifest, _ := validV1Proof()
	manifest.Candidates[0].OperatorFingerprint = manifest.Calibration[0].OperatorFingerprint
	if _, _, err := EncodeV1Manifest(manifest); err == nil {
		t.Fatal("EncodeV1Manifest() accepted a v0 operator fingerprint")
	}
}

func TestV1CanonicalEncodingIsDeterministicBoundedAndStrict(t *testing.T) {
	manifest, evidence := validV1Proof()
	first, _, err := EncodeV1Manifest(manifest)
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := EncodeV1Manifest(manifest)
	if err != nil || string(first) != string(second) || !strings.HasSuffix(string(first), "\n") {
		t.Fatalf("manifest encoding = %q, %q, %v", first, second, err)
	}
	encoded, _, err := EncodeV1Evidence(evidence)
	if err != nil || strings.Contains(string(encoded), `C:\\`) {
		t.Fatalf("evidence encoding = %q, %v", encoded, err)
	}
	if _, err := DecodeV1Manifest([]byte(`{"schemaVersion":1,"schemaVersion":1}`)); err == nil {
		t.Fatal("DecodeV1Manifest() accepted duplicate keys")
	}
	if _, err := DecodeV1Evidence([]byte(`{"schemaVersion":1,"attempts":[],"unknown":true}`)); err == nil {
		t.Fatal("DecodeV1Evidence() accepted an unknown key")
	}
	aggregate, err := ClassifyV1(manifest, evidence)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := EncodeV1Aggregate(aggregate); err != nil {
		t.Fatalf("EncodeV1Aggregate() = %v", err)
	}
}

func validV1Proof() (V1Manifest, V1Evidence) {
	digest := func(value string) string { return strings.Repeat(value, 64) }
	commit := func(value string) V1Reference {
		return V1Reference{Commit: strings.Repeat(value, 40), Tree: strings.Repeat(value, 40)}
	}
	authorities := V1Authorities{Evaluated: commit("a"), Freeze: commit("b"), Corpus: commit("c"), Dispatch: commit("d")}
	manifest := V1Manifest{
		SchemaVersion:            V1SchemaVersion,
		Authorities:              authorities,
		Toolchain:                "go1.26.1",
		ComparatorContractSHA256: digest("1"),
		DetectorContractSHA256:   digest("2"),
		Calibration:              []V1Fingerprint{{OperatorFingerprint: "v0-operator", InvariantFingerprint: "v0-invariant"}},
	}
	for index := 0; index < 6; index++ {
		family := "catalog"
		if index >= 3 {
			family = "module"
		}
		manifest.Candidates = append(manifest.Candidates, V1Candidate{ID: "candidate-" + string(rune('a'+index)), Family: family, PatchSHA256: digest("3"), MutatedTree: strings.Repeat("e", 40), OperatorFingerprint: "operator-" + string(rune('a'+index)), InvariantFingerprint: "invariant-" + string(rune('a'+index)), Expected: V1Failure{Class: "execution_failure", Phase: "catalog-plan", Coordinate: "success", ChildReason: "domain_reason"}})
	}
	evidence := V1Evidence{SchemaVersion: V1SchemaVersion}
	for _, candidate := range manifest.Candidates {
		for _, lane := range []string{V1LaneWindowsGo, V1LaneUbuntuGo, V1LaneMacOSGo} {
			evidence.Attempts = append(evidence.Attempts, V1Attempt{CandidateID: candidate.ID, Kind: V1KindComparator, Lane: lane, Repetition: 1, Authorities: authorities, PatchSHA256: candidate.PatchSHA256, MutatedTree: candidate.MutatedTree, Toolchain: manifest.Toolchain, ComparatorContractSHA256: manifest.ComparatorContractSHA256, Status: V1StatusPassed})
		}
		for repetition := 1; repetition <= 2; repetition++ {
			failure := candidate.Expected
			evidence.Attempts = append(evidence.Attempts, V1Attempt{CandidateID: candidate.ID, Kind: V1KindDetector, Repetition: repetition, Authorities: authorities, PatchSHA256: candidate.PatchSHA256, MutatedTree: candidate.MutatedTree, Toolchain: manifest.Toolchain, DetectorContractSHA256: manifest.DetectorContractSHA256, Admission: V1AdmissionAdmitted, Status: V1StatusRejected, Failure: &failure, DiagnosticEngineSHA256: digest("4")})
		}
	}
	return manifest, evidence
}
