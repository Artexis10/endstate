// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationpilot

import (
	"strings"
	"testing"
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
