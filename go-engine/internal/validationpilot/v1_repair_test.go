// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationpilot

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/validationharness"
	"github.com/Artexis10/endstate/go-engine/internal/validationmatrix"
)

func TestDecodeV1ExternalResultRejectsUnknownDuplicateAndVacuousEvidence(t *testing.T) {
	candidate := validV1CandidateForRepair(t)
	for _, raw := range []string{
		`{"schemaVersion":1,"schemaVersion":1}`,
		`{"schemaVersion":1,"moduleId":"` + candidate.Target.ModuleID + `","moduleRevision":"` + strings.Repeat("a", 64) + `","scenarioId":"` + candidate.Target.ScenarioID + `","kind":"capture-contract","status":"passed","proofLevels":[],"assertionCounts":{},"phaseTimings":{},"foreign":true}`,
		`{"schemaVersion":1,"moduleId":"` + candidate.Target.ModuleID + `","moduleRevision":"` + strings.Repeat("a", 64) + `","scenarioId":"` + candidate.Target.ScenarioID + `","kind":"capture-contract","status":"passed","proofLevels":[],"assertionCounts":{},"phaseTimings":{}}`,
	} {
		if _, err := DecodeV1ExternalResult([]byte(raw), candidate, strings.Repeat("a", 64), V1ModeCapture); err == nil {
			t.Fatalf("DecodeV1ExternalResult() accepted %s", raw)
		}
	}
}

func TestDecodeV1ExternalResultRejectsVacuousFailedDetail(t *testing.T) {
	candidate := validV1CandidateForRepair(t)
	revision := strings.Repeat("a", 64)
	result := validationharness.Result{SchemaVersion: validationharness.ResultSchemaVersion, ModuleID: candidate.Target.ModuleID, ModuleRevision: revision, ScenarioID: candidate.Target.ScenarioID, Kind: validationmatrix.ScenarioCaptureContract, Status: validationharness.ResultStatusFailed, ProofLevels: []validationmatrix.ProofLevel{}, AssertionCounts: map[string]int{"capture": 1}, Failure: &validationharness.Failure{Code: validationharness.CodeArtifactContract, Phase: "capture", Coordinate: "payload"}, PhaseTimings: map[string]time.Duration{"capture": time.Millisecond}}
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeV1ExternalResult(raw, candidate, revision, V1ModeCapture); err == nil {
		t.Fatal("DecodeV1ExternalResult() accepted an empty failed-result diagnostic detail")
	}
}

func TestDecodeV1ExternalResultAcceptsCompletePassedAndFailedContracts(t *testing.T) {
	candidate := validV1CandidateForRepair(t)
	revision := strings.Repeat("a", 64)
	passed := validationharness.Result{SchemaVersion: validationharness.ResultSchemaVersion, ModuleID: candidate.Target.ModuleID, ModuleRevision: revision, ScenarioID: candidate.Target.ScenarioID, Kind: validationmatrix.ScenarioCaptureContract, Status: validationharness.ResultStatusPassed, ProofLevels: []validationmatrix.ProofLevel{validationmatrix.ProofCatalog}, AssertionCounts: map[string]int{"capture": 1}, PhaseTimings: map[string]time.Duration{"capture": time.Millisecond}}
	failed := passed
	failed.Status = validationharness.ResultStatusFailed
	failed.ProofLevels = []validationmatrix.ProofLevel{}
	failed.Failure = &validationharness.Failure{Code: validationharness.CodeArtifactContract, Phase: "capture", Coordinate: "payload", Detail: "captured payload differs"}
	for _, result := range []validationharness.Result{passed, failed} {
		raw, err := json.Marshal(result)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := DecodeV1ExternalResult(raw, candidate, revision, V1ModeCapture); err != nil {
			t.Fatalf("DecodeV1ExternalResult(%+v) = %v", result, err)
		}
	}
}

func TestDecodeV1ExternalResultAcceptsEarlyCaptureArtifactFailureWithoutCounts(t *testing.T) {
	candidate := validV1CandidateForRepair(t)
	revision := strings.Repeat("a", 64)
	result := validationharness.Result{
		SchemaVersion:   validationharness.ResultSchemaVersion,
		ModuleID:        candidate.Target.ModuleID,
		ModuleRevision:  revision,
		ScenarioID:      candidate.Target.ScenarioID,
		Kind:            validationmatrix.ScenarioCaptureContract,
		Status:          validationharness.ResultStatusFailed,
		ProofLevels:     []validationmatrix.ProofLevel{},
		AssertionCounts: map[string]int{},
		Failure: &validationharness.Failure{
			Code: validationharness.CodeArtifactContract, Phase: "capture", Coordinate: "artifact",
			Detail: `capture artifact missing at C:\runner\temporary`,
		},
		PhaseTimings: map[string]time.Duration{"capture": time.Millisecond},
	}
	raw := mustV1ExternalJSON(t, result)
	decoded, err := DecodeV1ExternalResult(raw, candidate, revision, V1ModeCapture)
	if err != nil {
		t.Fatalf("DecodeV1ExternalResult(real early artifact failure) = %v", err)
	}
	failure := v1FailureFromLegacy(&Failure{Code: decoded.Failure.Code, Phase: decoded.Failure.Phase, Coordinate: decoded.Failure.Coordinate})
	if failure == nil || failure.ChildReason != "" {
		t.Fatalf("failure published diagnostic detail: %#v", failure)
	}
	result.Failure.Detail = "capture artifact\x07"
	if _, err := DecodeV1ExternalResult(mustV1ExternalJSON(t, result), candidate, revision, V1ModeCapture); err == nil {
		t.Fatal("DecodeV1ExternalResult() accepted a control character in diagnostic detail")
	}
}

func TestDecodeV1ExternalResultRejectsHostileCompleteShapes(t *testing.T) {
	candidate := validV1CandidateForRepair(t)
	revision := strings.Repeat("a", 64)
	valid := validationharness.Result{SchemaVersion: validationharness.ResultSchemaVersion, ModuleID: candidate.Target.ModuleID, ModuleRevision: revision, ScenarioID: candidate.Target.ScenarioID, Kind: validationmatrix.ScenarioCaptureContract, Status: validationharness.ResultStatusPassed, ProofLevels: []validationmatrix.ProofLevel{validationmatrix.ProofCatalog}, AssertionCounts: map[string]int{"capture": 1}, PhaseTimings: map[string]time.Duration{"capture": time.Millisecond}}
	for _, mutate := range []func(*validationharness.Result){
		func(result *validationharness.Result) { result.SchemaVersion++ },
		func(result *validationharness.Result) { result.ModuleRevision = strings.Repeat("b", 64) },
		func(result *validationharness.Result) { result.Kind = validationmatrix.ScenarioConfigRoundtripV1 },
		func(result *validationharness.Result) { result.ProofLevels = nil },
		func(result *validationharness.Result) { result.AssertionCounts = nil },
		func(result *validationharness.Result) { result.PhaseTimings = nil },
	} {
		result := valid
		mutate(&result)
		raw, err := json.Marshal(result)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := DecodeV1ExternalResult(raw, candidate, revision, V1ModeCapture); err == nil {
			t.Fatal("DecodeV1ExternalResult() accepted a hostile result shape")
		}
	}
	foreign := append([]byte(`{"foreign":true,`), mustV1ExternalJSON(t, valid)[1:]...)
	if _, err := DecodeV1ExternalResult(foreign, candidate, revision, V1ModeCapture); err == nil {
		t.Fatal("DecodeV1ExternalResult() accepted foreign result data")
	}
	if _, err := DecodeV1ExternalResult(make([]byte, V1MaxDocumentSize+1), candidate, revision, V1ModeCapture); err == nil {
		t.Fatal("DecodeV1ExternalResult() accepted oversized result data")
	}
}

func mustV1ExternalJSON(t *testing.T, result validationharness.Result) []byte {
	t.Helper()
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestV1ManifestRejectsProductionCausalReason(t *testing.T) {
	manifest, _ := validV1Proof()
	manifest.Candidates[0].Expected.ChildReason = "free-form-reason"
	if _, _, err := EncodeV1Manifest(manifest); err == nil {
		t.Fatal("EncodeV1Manifest() accepted a production causal reason")
	}
}

func TestValidV1FailureCoordinateGrammar(t *testing.T) {
	for _, coordinate := range []string{
		"capture.files[0]",
		"manifest.configModules",
		"capture.excludeGlobs[12]",
		"configCaptures[0].sourceInstance.evidence",
		"restore-item.status",
		"PATH",
	} {
		failure := V1Failure{Class: "artifact_contract", Phase: "capture", Coordinate: coordinate, Scope: V1FailureScopeDomain}
		if !validV1Failure(failure) {
			t.Fatalf("validV1Failure(%q) = false", coordinate)
		}
	}
	for _, coordinate := range []string{
		"/abs/path", "C:\\path", "capture/files", "capture\\files", ".capture", "capture.", "capture..files",
		"capture.files[]", "capture.files[-1]", "capture.files[01]", "capture.files[", "capture.files]", "capture.files[0",
		"capture\nfiles", strings.Repeat("a", 65),
	} {
		failure := V1Failure{Class: "artifact_contract", Phase: "capture", Coordinate: coordinate, Scope: V1FailureScopeDomain}
		if validV1Failure(failure) {
			t.Fatalf("validV1Failure(%q) = true", coordinate)
		}
	}
}

func TestV1FailureCoordinateDoesNotRelaxOtherIdentifiers(t *testing.T) {
	for _, invalid := range []V1Failure{
		{Class: "artifactContract", Phase: "capture", Coordinate: "capture.files[0]", Scope: V1FailureScopeDomain},
		{Class: "artifact_contract", Phase: "captureFiles[0]", Coordinate: "capture.files[0]", Scope: V1FailureScopeDomain},
		{Class: "artifact_contract", Phase: "capture", Coordinate: "capture.files[0]", ChildReason: "childReason[0]", Scope: V1FailureScopeDomain},
	} {
		if validV1Failure(invalid) {
			t.Fatalf("validV1Failure(%#v) = true", invalid)
		}
	}
}

func TestV1FailureCoordinatesRoundTripAndClassify(t *testing.T) {
	manifest, evidence := validV1Proof()
	coordinate := "configCaptures[0].sourceInstance.evidence"
	manifest.Candidates[0].Expected.Coordinate = coordinate
	for index := range evidence.Attempts {
		if evidence.Attempts[index].CandidateID == manifest.Candidates[0].ID && evidence.Attempts[index].Failure != nil {
			evidence.Attempts[index].Failure.Coordinate = coordinate
		}
	}
	encodedManifest, _, err := EncodeV1Manifest(manifest)
	if err != nil {
		t.Fatalf("EncodeV1Manifest() = %v", err)
	}
	decodedManifest, err := DecodeV1Manifest(encodedManifest)
	if err != nil || decodedManifest.Candidates[0].Expected.Coordinate != coordinate {
		t.Fatalf("DecodeV1Manifest() = %#v, %v", decodedManifest, err)
	}
	encodedEvidence, _, err := EncodeV1Evidence(evidence)
	if err != nil {
		t.Fatalf("EncodeV1Evidence() = %v", err)
	}
	decodedEvidence, err := DecodeV1Evidence(encodedEvidence)
	if err != nil || decodedEvidence.Attempts[5].Failure.Coordinate != coordinate {
		t.Fatalf("DecodeV1Evidence() = %#v, %v", decodedEvidence, err)
	}
	aggregate, err := ClassifyV1(manifest, evidence)
	if err != nil || aggregate.Rows[0].Classification != ClassificationCorrectNewOnlyKill {
		t.Fatalf("ClassifyV1() = %#v, %v", aggregate, err)
	}
	failure := v1FailureFromLegacy(&Failure{Code: "artifact_contract", Phase: "capture", Coordinate: coordinate})
	if failure == nil || failure.Coordinate != coordinate || shallowV1Failure(failure) {
		t.Fatalf("v1FailureFromLegacy() = %#v", failure)
	}
}

func TestDecodeV1ExternalResultPreservesHarnessCoordinate(t *testing.T) {
	candidate := validV1CandidateForRepair(t)
	revision := strings.Repeat("a", 64)
	coordinate := "capture.files[0]"
	result := validationharness.Result{
		SchemaVersion:  validationharness.ResultSchemaVersion,
		ModuleID:       candidate.Target.ModuleID,
		ModuleRevision: revision,
		ScenarioID:     candidate.Target.ScenarioID,
		Kind:           validationmatrix.ScenarioCaptureContract,
		Status:         validationharness.ResultStatusFailed,
		ProofLevels:    []validationmatrix.ProofLevel{},
		AssertionCounts: map[string]int{
			"capture": 1,
		},
		Failure:      &validationharness.Failure{Code: validationharness.CodeArtifactContract, Phase: "capture", Coordinate: coordinate, Detail: "captured file differs"},
		PhaseTimings: map[string]time.Duration{"capture": time.Millisecond},
	}
	decoded, err := DecodeV1ExternalResult(mustV1ExternalJSON(t, result), candidate, revision, V1ModeCapture)
	if err != nil || decoded.Failure == nil || decoded.Failure.Coordinate != coordinate {
		t.Fatalf("DecodeV1ExternalResult() = %#v, %v", decoded, err)
	}
}

func validV1CandidateForRepair(t *testing.T) V1Candidate {
	t.Helper()
	manifest, _ := validV1Proof()
	return manifest.Candidates[0]
}
