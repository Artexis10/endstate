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

func validV1CandidateForRepair(t *testing.T) V1Candidate {
	t.Helper()
	manifest, _ := validV1Proof()
	return manifest.Candidates[0]
}
