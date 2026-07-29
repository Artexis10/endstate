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
	evidence.Attempts[5].DiagnosticEngineSHA256 = strings.Repeat("e", 64)
	aggregate, err := ClassifyV1(manifest, evidence)
	if err != nil || aggregate.Decision != DecisionMeaningfulSignal {
		t.Fatalf("ClassifyV1(diagnostic drift) = %#v, %v", aggregate, err)
	}
	evidence.Attempts[5].Authorities.Freeze.Tree = strings.Repeat("f", 40)
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
	evidence.Attempts[5].Status = V1StatusInfrastructure
	evidence.Attempts[5].Failure = nil
	aggregate, err := ClassifyV1(manifest, evidence)
	if err != nil || aggregate.Rows[0].Classification != ClassificationInfrastructureFailure {
		t.Fatalf("ClassifyV1(infrastructure) = %#v, %v", aggregate, err)
	}
	manifest, evidence = validV1Proof()
	evidence.Attempts[5].Failure = &V1Failure{Class: "assertion_contract", Phase: "aggregate", Coordinate: "rows", Scope: V1FailureScopeGuard}
	evidence.Attempts[6].Failure = evidence.Attempts[5].Failure
	aggregate, err = ClassifyV1(manifest, evidence)
	if err != nil || aggregate.Rows[0].Classification != ClassificationWrongKill {
		t.Fatalf("ClassifyV1(shallow) = %#v, %v", aggregate, err)
	}
}

func TestClassifyV1RequiresDeclaredChildReason(t *testing.T) {
	manifest, evidence := validV1Proof()
	evidence.Attempts[5].Failure.ChildReason = "wrong_reason"
	evidence.Attempts[6].Failure.ChildReason = "wrong_reason"
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
		Calibration:              append([]V1Fingerprint(nil), V1CalibrationFingerprints...),
	}
	patches := []string{"3", "4", "5"}
	trees := []string{"e", "f", "a"}
	for index := 0; index < 3; index++ {
		lifecycle, productionFile := V1LifecycleSchemaV1, "restore/copy.go"
		failure := V1Failure{Class: "content_mismatch", Phase: "verify", Coordinate: "content", ChildReason: "domain_reason", Scope: V1FailureScopeDomain}
		if index == 0 {
			lifecycle, productionFile = V1LifecycleCapture, "bundle/capture_bundle.go"
			failure = V1Failure{Class: "artifact_contract", Phase: "capture", Coordinate: "payload", ChildReason: "domain_reason", Scope: V1FailureScopeDomain}
		}
		target := V1Target{ModuleID: "apps.module-" + string(rune('a'+index)), ScenarioID: "scenario-" + string(rune('a'+index))}
		manifest.Candidates = append(manifest.Candidates, V1Candidate{ID: "candidate-" + string(rune('a'+index)), Family: "production-go", PatchSHA256: digest(patches[index]), MutatedTree: strings.Repeat(trees[index], 40), OperatorFingerprint: "operator-" + string(rune('a'+index)), InvariantFingerprint: "invariant-" + string(rune('a'+index)), DetectorID: "module-detector-" + string(rune('a'+index)), Target: target, Expected: failure, Lifecycle: lifecycle, ProductionFile: productionFile, FaultDescription: "drops required product behavior", NormalEntrypoint: "endstate capture", LiveReachability: "the normal product path reaches the changed statement", ReviewRecordSHA256: digest("9")})
	}
	timing := func() (string, string, int64) { return "2026-07-29T00:00:00Z", "2026-07-29T00:00:00.001Z", 1 }
	runner := func(family, imageOS, imageVersion string) V1Runner {
		value, err := v1HostedRunner(family, imageOS, imageVersion)
		if err != nil {
			panic(err)
		}
		return value
	}
	comparatorRunner := func(lane string) V1Runner {
		switch lane {
		case V1LaneWindowsGo:
			return runner("windows", "windows", "2025")
		case V1LaneUbuntuGo:
			return runner("linux", "ubuntu", "2404")
		default:
			return runner("darwin", "macos", "15")
		}
	}
	evidence := V1Evidence{SchemaVersion: V1SchemaVersion}
	for _, candidate := range manifest.Candidates {
		started, ended, duration := timing()
		mode := lifecycleV1Mode(candidate.Lifecycle)
		for repetition := 1; repetition <= 2; repetition++ {
			evidence.Attempts = append(evidence.Attempts, V1Attempt{CandidateID: candidate.ID, DetectorID: candidate.DetectorID, Target: candidate.Target, Kind: V1KindBaseline, Lane: V1LaneWindowsDetector, Repetition: repetition, Authorities: authorities, RepositorySHA256: digest("a"), Toolchain: manifest.Toolchain, Runner: runner("windows", "windows", "2025"), StartedAt: started, EndedAt: ended, DurationMillis: duration, BaselineProof: V1BaselineProofIdentity{SourceTree: authorities.Evaluated.Tree, RepositorySHA256: digest("a"), Target: candidate.Target, Proof: "proof-" + candidate.ID, DiagnosticEngineSHA256: digest("b")}, DetectorContractSHA256: manifest.DetectorContractSHA256, Admission: V1AdmissionAdmitted, Status: V1StatusPassed, VerifiedMode: mode})
		}
		for _, lane := range []string{V1LaneWindowsGo, V1LaneUbuntuGo, V1LaneMacOSGo} {
			evidence.Attempts = append(evidence.Attempts, V1Attempt{CandidateID: candidate.ID, DetectorID: candidate.DetectorID, Target: candidate.Target, Kind: V1KindComparator, Lane: lane, Repetition: 1, Authorities: authorities, PatchSHA256: candidate.PatchSHA256, MutatedTree: candidate.MutatedTree, RepositorySHA256: digest("a"), Toolchain: manifest.Toolchain, Runner: comparatorRunner(lane), StartedAt: started, EndedAt: ended, DurationMillis: duration, ComparatorContractSHA256: manifest.ComparatorContractSHA256, Status: V1StatusPassed})
		}
		for repetition := 1; repetition <= 2; repetition++ {
			failure := candidate.Expected
			evidence.Attempts = append(evidence.Attempts, V1Attempt{CandidateID: candidate.ID, DetectorID: candidate.DetectorID, Target: candidate.Target, Kind: V1KindDetector, Lane: V1LaneWindowsDetector, Repetition: repetition, Authorities: authorities, PatchSHA256: candidate.PatchSHA256, MutatedTree: candidate.MutatedTree, RepositorySHA256: digest("a"), Toolchain: manifest.Toolchain, Runner: runner("windows", "windows", "2025"), StartedAt: started, EndedAt: ended, DurationMillis: duration, DetectorContractSHA256: manifest.DetectorContractSHA256, Admission: V1AdmissionAdmitted, Status: V1StatusRejected, Failure: &failure, DiagnosticEngineSHA256: digest("9"), VerifiedMode: mode})
		}
	}
	return manifest, evidence
}
