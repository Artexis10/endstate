// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationpilot

import (
	"strings"
	"testing"
)

func TestClassifyV1RequiresTwoGreenTargetedBaselines(t *testing.T) {
	manifest, evidence := validV1Proof()
	evidence.Attempts = evidence.Attempts[2:]
	if _, err := ClassifyV1(manifest, evidence); err == nil {
		t.Fatal("ClassifyV1() accepted missing unmodified baselines")
	}
}

func TestV1ManifestRejectsDuplicatePatchAndMutatedTree(t *testing.T) {
	manifest, _ := validV1Proof()
	manifest.Candidates[1].PatchSHA256 = manifest.Candidates[0].PatchSHA256
	if _, _, err := EncodeV1Manifest(manifest); err == nil {
		t.Fatal("EncodeV1Manifest() accepted a duplicate patch digest")
	}
	manifest, _ = validV1Proof()
	manifest.Candidates[1].MutatedTree = manifest.Candidates[0].MutatedTree
	if _, _, err := EncodeV1Manifest(manifest); err == nil {
		t.Fatal("EncodeV1Manifest() accepted a duplicate mutated tree")
	}
}

func TestV1ManifestRequiresThreeDistinctModuleDetectors(t *testing.T) {
	manifest, _ := validV1Proof()
	manifest.Candidates[2].DetectorID = manifest.Candidates[1].DetectorID
	if _, _, err := EncodeV1Manifest(manifest); err == nil {
		t.Fatal("EncodeV1Manifest() accepted repeated module detector identity")
	}
}

func TestClassifyV1RequiresExactTargetAndDetectorIdentity(t *testing.T) {
	manifest, evidence := validV1Proof()
	evidence.Attempts[5].Target.ModuleID = "apps.foreign"
	if _, err := ClassifyV1(manifest, evidence); err == nil {
		t.Fatal("ClassifyV1() accepted foreign detector target")
	}
	manifest, evidence = validV1Proof()
	evidence.Attempts[5].DetectorID = "foreign-detector"
	if _, err := ClassifyV1(manifest, evidence); err == nil {
		t.Fatal("ClassifyV1() accepted foreign detector identity")
	}
}

func TestClassifyV1StableAdmittedDetectorPassIsSurvivor(t *testing.T) {
	manifest, evidence := validV1Proof()
	for _, index := range []int{5, 6} {
		evidence.Attempts[index].Status = V1StatusPassed
		evidence.Attempts[index].Failure = nil
	}
	aggregate, err := ClassifyV1(manifest, evidence)
	if err != nil || aggregate.Rows[0].Classification != ClassificationSurvivor {
		t.Fatalf("ClassifyV1() = %#v, %v", aggregate, err)
	}
}

func TestClassifyV1RunnerAndTimingIdentityAreRepeatabilityGoverning(t *testing.T) {
	manifest, evidence := validV1Proof()
	evidence.Attempts[6].Runner.ImageVersion = "2026"
	evidence.Attempts[6].Runner.Image = "windows-2026"
	aggregate, err := ClassifyV1(manifest, evidence)
	if err != nil || aggregate.Rows[0].Classification != ClassificationFlake {
		t.Fatalf("ClassifyV1(runner drift) = %#v, %v", aggregate, err)
	}
	manifest, evidence = validV1Proof()
	evidence.Attempts[6].DurationMillis++
	if _, err := ClassifyV1(manifest, evidence); err == nil {
		t.Fatal("ClassifyV1() accepted mismatched timing evidence")
	}
}

func TestClassifyV1GuardFailuresAndComparatorInfrastructureCannotEarnCredit(t *testing.T) {
	manifest, evidence := validV1Proof()
	for _, index := range []int{5, 6} {
		evidence.Attempts[index].Failure = &V1Failure{Class: "revision_contract", Phase: "revision", Coordinate: "module", Scope: V1FailureScopeGuard}
	}
	aggregate, err := ClassifyV1(manifest, evidence)
	if err != nil || aggregate.Rows[0].Classification != ClassificationWrongKill {
		t.Fatalf("ClassifyV1(guard) = %#v, %v", aggregate, err)
	}
	manifest, evidence = validV1Proof()
	evidence.Attempts[2].Status = V1StatusInfrastructure
	evidence.Attempts[2].InfrastructureCoordinate = "comparator"
	aggregate, err = ClassifyV1(manifest, evidence)
	if err != nil || aggregate.Rows[0].Classification != ClassificationInfrastructureFailure || aggregate.Decision != DecisionInsufficientSignal {
		t.Fatalf("ClassifyV1(comparator infrastructure) = %#v, %v", aggregate, err)
	}
}

func TestV1ManifestRequiresExactCalibrationRegistry(t *testing.T) {
	manifest, _ := validV1Proof()
	manifest.Calibration = nil
	if _, _, err := EncodeV1Manifest(manifest); err == nil {
		t.Fatal("EncodeV1Manifest() accepted an empty calibration registry")
	}
	manifest, _ = validV1Proof()
	manifest.Calibration[0].OperatorFingerprint = "altered"
	if _, _, err := EncodeV1Manifest(manifest); err == nil {
		t.Fatal("EncodeV1Manifest() accepted an altered calibration registry")
	}
}

func TestV1AttemptRejectsHostPathsAndFreeFormLogs(t *testing.T) {
	manifest, evidence := validV1Proof()
	evidence.Attempts[0].Runner.Image = strings.Repeat("x", 65)
	if _, err := ClassifyV1(manifest, evidence); err == nil {
		t.Fatal("ClassifyV1() accepted unbounded runner evidence")
	}
}
