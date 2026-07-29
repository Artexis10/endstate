// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationpilot

import (
	"strings"
	"testing"
)

func TestV1ProductionGoCandidateRequiresClosedMetadataAndLifecycleFile(t *testing.T) {
	manifest, _ := validV1Proof()
	candidate := manifest.Candidates[0]
	candidate.Lifecycle = V1LifecycleCapture
	candidate.ProductionFile = "bundle/capture_bundle.go"
	candidate.FaultDescription = "drops the captured payload manifest"
	candidate.NormalEntrypoint = "endstate capture"
	candidate.LiveReachability = "capture reaches the bundle writer"
	candidate.ReviewRecordSHA256 = strings.Repeat("d", 64)
	manifest.Candidates = []V1Candidate{candidate, manifest.Candidates[1], manifest.Candidates[2]}
	if _, _, err := EncodeV1Manifest(manifest); err != nil {
		t.Fatalf("EncodeV1Manifest() rejected a closed production-Go candidate: %v", err)
	}
	manifest.Candidates[0].ProductionFile = "bundle/not-allowed.go"
	if _, _, err := EncodeV1Manifest(manifest); err == nil {
		t.Fatal("EncodeV1Manifest() accepted a production file outside the closed lifecycle registry")
	}
	manifest, _ = validV1Proof()
	manifest.Candidates[0].Target = V1Target{ModuleID: "apps.notepad-plus-plus", ScenarioID: "default-v1"}
	if _, _, err := EncodeV1Manifest(manifest); err == nil {
		t.Fatal("EncodeV1Manifest() accepted a Windows Go-comparator target")
	}
}

func TestClassifyV1ComparatorRejectionIsAlreadyCovered(t *testing.T) {
	manifest, evidence := validV1Proof()
	evidence.Attempts[2].Status = V1StatusRejected
	evidence.Attempts[2].Failure = &V1Failure{Class: "execution_failure", Phase: "comparator", Coordinate: V1LaneWindowsGo, Scope: V1FailureScopeGuard}
	aggregate, err := ClassifyV1(manifest, evidence)
	if err != nil || aggregate.Rows[0].Classification != ClassificationAlreadyCovered || aggregate.Decision != DecisionInsufficientSignal {
		t.Fatalf("ClassifyV1(comparator rejection) = %#v, %v", aggregate, err)
	}
}

func TestV1CreditTableRejectsUnlistedDomainFailure(t *testing.T) {
	manifest, evidence := validV1Proof()
	for _, index := range []int{5, 6} {
		evidence.Attempts[index].Failure = &V1Failure{Class: "execution_failure", Phase: "catalog-plan", Coordinate: "target", Scope: V1FailureScopeDomain}
	}
	aggregate, err := ClassifyV1(manifest, evidence)
	if err != nil || aggregate.Rows[0].Classification != ClassificationWrongKill {
		t.Fatalf("ClassifyV1(uncreditable domain failure) = %#v, %v", aggregate, err)
	}
}

func TestClassifyV1UsesVerifiedSidecarModeInsteadOfManifestLabel(t *testing.T) {
	manifest, evidence := validV1Proof()
	for _, index := range []int{0, 1, 5, 6} {
		evidence.Attempts[index].VerifiedMode = V1ModeRoundtrip
	}
	aggregate, err := ClassifyV1(manifest, evidence)
	if err != nil || aggregate.Rows[0].Classification != ClassificationInfrastructureFailure {
		t.Fatalf("ClassifyV1(sidecar mode drift) = %#v, %v", aggregate, err)
	}
}
