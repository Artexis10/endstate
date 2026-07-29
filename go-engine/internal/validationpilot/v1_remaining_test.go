// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationpilot

import "testing"

func TestClassifyV1RequiresStableGreenBaselineProofIdentity(t *testing.T) {
	manifest, evidence := validV1Proof()
	evidence.Attempts[1].BaselineProof.Proof = "different-proof"
	aggregate, err := ClassifyV1(manifest, evidence)
	if err != nil || aggregate.Rows[0].Classification != ClassificationFlake {
		t.Fatalf("ClassifyV1(unstable baseline proof) = %#v, %v", aggregate, err)
	}
}

func TestClassifyV1RejectsGreenBaselineProofFromForeignSourceTree(t *testing.T) {
	manifest, evidence := validV1Proof()
	for _, index := range []int{0, 1} {
		evidence.Attempts[index].BaselineProof.SourceTree = "ffffffffffffffffffffffffffffffffffffffff"
	}
	if _, err := ClassifyV1(manifest, evidence); err == nil {
		t.Fatal("ClassifyV1() accepted a pairwise-stable foreign baseline source tree")
	}
}

func TestClassifyV1RepresentsRedBaselineWithoutKillCredit(t *testing.T) {
	manifest, evidence := validV1Proof()
	evidence.Attempts[0].Status = V1StatusRejected
	evidence.Attempts[0].Failure = &V1Failure{Class: "domain_failure", Phase: "detector", Coordinate: "target", Scope: V1FailureScopeDomain}
	evidence.Attempts[1].Status = V1StatusRejected
	evidence.Attempts[1].Failure = evidence.Attempts[0].Failure
	aggregate, err := ClassifyV1(manifest, evidence)
	if err != nil || aggregate.Rows[0].Classification != ClassificationWrongKill || aggregate.Decision == DecisionMeaningfulSignal {
		t.Fatalf("ClassifyV1(red baseline) = %#v, %v", aggregate, err)
	}
}

func TestV1AttemptRejectsRelabeledLaneAndBaselineMutantRunnerDrift(t *testing.T) {
	manifest, evidence := validV1Proof()
	evidence.Attempts[2].Runner.Family = "linux"
	if _, err := ClassifyV1(manifest, evidence); err == nil {
		t.Fatal("ClassifyV1() accepted a Windows comparator lane on Linux")
	}
	manifest, evidence = validV1Proof()
	evidence.Attempts[5].Runner.Image = "windows-2026"
	aggregate, err := ClassifyV1(manifest, evidence)
	if err != nil || aggregate.Rows[0].Classification != ClassificationFlake {
		t.Fatalf("ClassifyV1(baseline/mutant runner drift) = %#v, %v", aggregate, err)
	}
}

func TestClassifyV1ParseFailureIsNeverCorrectKill(t *testing.T) {
	manifest, evidence := validV1Proof()
	for _, index := range []int{5, 6} {
		evidence.Attempts[index].Failure = &V1Failure{Class: "parse_failure", Phase: "decode", Coordinate: "module", Scope: V1FailureScopeDomain}
	}
	aggregate, err := ClassifyV1(manifest, evidence)
	if err != nil || aggregate.Rows[0].Classification != ClassificationWrongKill {
		t.Fatalf("ClassifyV1(parse guard) = %#v, %v", aggregate, err)
	}
}
