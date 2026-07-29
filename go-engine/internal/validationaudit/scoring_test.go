// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationaudit

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestQualifyControlsClassifiesCompleteInventories(t *testing.T) {
	set := scoringCandidateSet(t)
	identity := scoringIdentity()
	evidence := scoringControls(set, ExitClassPassed)
	results, err := QualifyControls(set, identity, "qualification-v1", evidence)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 30 || results[0].State != QualificationControlSurvivor || results[29].State != QualificationControlSurvivor {
		t.Fatalf("QualifyControls() = %#v, want ordered control survivors", results)
	}
	evidence[1].ExitClass = ExitClassRejected
	evidence[1].Failure = &StableFailure{Class: "contract", Phase: "validation", Coordinate: "modules[0]"}
	results, err = QualifyControls(set, identity, "qualification-v1", evidence)
	if err != nil || results[0].State != QualificationAlreadyCovered {
		t.Fatalf("mixed complete inventory = %#v, %v; want already covered", results[0], err)
	}
	evidence[2].ExitClass = ExitClassInfrastructure
	evidence[2].Failure = nil
	results, err = QualifyControls(set, identity, "qualification-v1", evidence)
	if err != nil || results[0].State != QualificationInfrastructureFailure {
		t.Fatalf("infrastructure precedence = %#v, %v; want infrastructure", results[0], err)
	}
}

func TestQualifyControlsRejectsDetectorAndForeignEvidence(t *testing.T) {
	set := scoringCandidateSet(t)
	identity := scoringIdentity()
	evidence := scoringControls(set, ExitClassPassed)
	evidence = append(evidence, validBaselineEvidence(1))
	if _, err := QualifyControls(set, identity, "qualification-v1", evidence); !errors.Is(err, ErrInvalidQualification) {
		t.Fatalf("detector evidence error = %v, want %v", err, ErrInvalidQualification)
	}
	evidence = scoringControls(set, ExitClassPassed)
	evidence[0].PatchSHA256 = testDigest
	if _, err := QualifyControls(set, identity, "qualification-v1", evidence); !errors.Is(err, ErrInvalidQualification) {
		t.Fatalf("foreign patch error = %v, want %v", err, ErrInvalidQualification)
	}
	evidence = scoringControls(set, ExitClassPassed)
	evidence = append(evidence, evidence[0])
	if _, err := QualifyControls(set, identity, "qualification-v1", evidence); !errors.Is(err, ErrInvalidQualification) {
		t.Fatalf("duplicate lane error = %v, want %v", err, ErrInvalidQualification)
	}
	evidence = scoringControls(set, ExitClassPassed)
	evidence = evidence[1:]
	results, err := QualifyControls(set, identity, "qualification-v1", evidence)
	if err != nil || results[0].State != QualificationInfrastructureFailure {
		t.Fatalf("missing lane = %#v, %v; want infrastructure failure", results[0], err)
	}
}

func TestFreezeCandidatesRejectsInsufficientAndSubstitutedResults(t *testing.T) {
	set := scoringCandidateSet(t)
	results, err := QualifyControls(set, scoringIdentity(), "qualification-v1", scoringControls(set, ExitClassPassed))
	if err != nil {
		t.Fatal(err)
	}
	for index := range results {
		if results[index].Category == "module-data" {
			results[index].State = QualificationAlreadyCovered
		}
	}
	if _, err := FreezeCandidates(set, results); !errors.Is(err, ErrWholeCorpusInvalid) {
		t.Fatalf("insufficient survivors error = %v, want %v", err, ErrWholeCorpusInvalid)
	}
	results, err = QualifyControls(set, scoringIdentity(), "qualification-v1", scoringControls(set, ExitClassPassed))
	if err != nil {
		t.Fatal(err)
	}
	results[0], results[1] = results[1], results[0]
	if _, err := FreezeCandidates(set, results); !errors.Is(err, ErrWholeCorpusInvalid) {
		t.Fatalf("reordered results error = %v, want %v", err, ErrWholeCorpusInvalid)
	}
}

func TestFreezeCandidatesAndValidateManifest(t *testing.T) {
	set := scoringCandidateSet(t)
	results, err := QualifyControls(set, scoringIdentity(), "qualification-v1", scoringControls(set, ExitClassPassed))
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := FreezeCandidates(set, results)
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Items) != 30 || manifest.Items[0].ID != "module-data-01" || manifest.Items[29].ID != "critical-safety-06" {
		t.Fatalf("FreezeCandidates() = %#v, want quota-preserving order", manifest.Items)
	}
	if err := ValidateFrozenManifest(set, manifest); err != nil {
		t.Fatal(err)
	}
	first, firstDigest, err := EncodeFrozenManifest(manifest)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeFrozenManifest(first)
	if err != nil {
		t.Fatal(err)
	}
	second, secondDigest, err := EncodeFrozenManifest(decoded)
	if err != nil || string(first) != string(second) || firstDigest != secondDigest || !strings.HasSuffix(string(first), "\n") {
		t.Fatalf("frozen encode = %q, %q, %v; want deterministic newline bytes", second, secondDigest, err)
	}
	manifest.Items[0], manifest.Items[1] = manifest.Items[1], manifest.Items[0]
	if err := ValidateFrozenManifest(set, manifest); !errors.Is(err, ErrWholeCorpusInvalid) {
		t.Fatalf("reordered manifest error = %v, want %v", err, ErrWholeCorpusInvalid)
	}
}

func TestScoreFrozenManifestClassifiesAndDecides(t *testing.T) {
	set := scoringCandidateSet(t)
	results, err := QualifyControls(set, scoringIdentity(), "qualification-v1", scoringControls(set, ExitClassPassed))
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := FreezeCandidates(set, results)
	if err != nil {
		t.Fatal(err)
	}
	evidence := scoringDetectorEvidence(manifest, ExitClassRejected)
	aggregate, err := ScoreFrozenManifest(set, manifest, scoringIdentity(), scoringDetectorCommands(), evidence)
	if err != nil {
		t.Fatal(err)
	}
	if aggregate.Decision != DecisionProceed || aggregate.CorrectKills != 30 || len(aggregate.Classifications) != 30 {
		t.Fatalf("ScoreFrozenManifest() = %#v, want 30 correct kills and proceed", aggregate)
	}
	first, firstDigest, err := EncodeAggregate(aggregate)
	if err != nil {
		t.Fatal(err)
	}
	second, secondDigest, err := EncodeAggregate(aggregate)
	if err != nil || string(first) != string(second) || firstDigest != secondDigest {
		t.Fatalf("aggregate encoding = %q, %q, %v; want deterministic bytes", second, secondDigest, err)
	}
}

func TestScoreFrozenManifestRejectsUnstableBaselineAndClassifiesFailures(t *testing.T) {
	set := scoringCandidateSet(t)
	results, err := QualifyControls(set, scoringIdentity(), "qualification-v1", scoringControls(set, ExitClassPassed))
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := FreezeCandidates(set, results)
	if err != nil {
		t.Fatal(err)
	}
	evidence := scoringDetectorEvidence(manifest, ExitClassRejected)
	evidence[1].Runner.Image = "windows-2025"
	if _, err := ScoreFrozenManifest(set, manifest, scoringIdentity(), scoringDetectorCommands(), evidence); !errors.Is(err, ErrInvalidAggregate) {
		t.Fatalf("unstable baseline error = %v, want %v", err, ErrInvalidAggregate)
	}
	evidence = scoringDetectorEvidence(manifest, ExitClassRejected)
	// First item, second mutation is a different behavioral rejection: a flake.
	evidence[3].Failure = &StableFailure{Class: "contract", Phase: "validation", Coordinate: "modules[1]"}
	aggregate, err := ScoreFrozenManifest(set, manifest, scoringIdentity(), scoringDetectorCommands(), evidence)
	if err != nil || aggregate.Classifications[0].State != ClassificationFlake || aggregate.Decision != DecisionStopAndRepair {
		t.Fatalf("flake = %#v, %v; want stop-and-repair flake", aggregate.Classifications[0], err)
	}
	evidence = scoringDetectorEvidence(manifest, ExitClassRejected)
	evidence[2].Failure = &StableFailure{Class: "contract", Phase: "validation", Coordinate: "modules[1]"}
	evidence[3].Failure = evidence[2].Failure
	aggregate, err = ScoreFrozenManifest(set, manifest, scoringIdentity(), scoringDetectorCommands(), evidence)
	if err != nil || aggregate.Classifications[0].State != ClassificationWrongKill {
		t.Fatalf("wrong rejection = %#v, %v; want wrong kill", aggregate.Classifications[0], err)
	}
}

func TestClassifyMutationPairOutcomeRules(t *testing.T) {
	expected := ExpectedFailure{Class: "contract", Phase: "validation", Coordinate: "modules[0]"}
	base := validMutationEvidence(1)
	base.ExitClass = ExitClassRejected
	base.Failure = &StableFailure{Class: expected.Class, Phase: expected.Phase, Coordinate: expected.Coordinate}
	tests := []struct {
		name   string
		first  func(*AttemptEvidence)
		second func(*AttemptEvidence)
		want   string
	}{
		{"exact rejection", func(*AttemptEvidence) {}, func(*AttemptEvidence) {}, ClassificationCorrectNewOnlyKill},
		{"both pass", func(e *AttemptEvidence) { e.ExitClass, e.Failure = ExitClassPassed, nil }, func(e *AttemptEvidence) { e.ExitClass, e.Failure = ExitClassPassed, nil }, ClassificationSurvivor},
		{"wrong stable failure", func(e *AttemptEvidence) { e.Failure.Coordinate = "modules[1]" }, func(e *AttemptEvidence) { e.Failure.Coordinate = "modules[1]" }, ClassificationWrongKill},
		{"different failures", func(e *AttemptEvidence) { e.Failure.Coordinate = "modules[1]" }, func(*AttemptEvidence) {}, ClassificationFlake},
		{"undeclared timeout", func(e *AttemptEvidence) { e.ExitClass = ExitClassTimeout }, func(e *AttemptEvidence) { e.ExitClass = ExitClassTimeout }, ClassificationInfrastructure},
		{"canceled", func(e *AttemptEvidence) { e.ExitClass, e.Failure = ExitClassCanceled, nil }, func(e *AttemptEvidence) { e.ExitClass, e.Failure = ExitClassCanceled, nil }, ClassificationInfrastructure},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			first, second := base, base
			first.Repetition, second.Repetition = 1, 2
			first.Failure = cloneFailure(base.Failure)
			second.Failure = cloneFailure(base.Failure)
			tt.first(&first)
			tt.second(&second)
			if got := classifyMutationPair(expected, first, second); got != tt.want {
				t.Fatalf("classifyMutationPair() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestEvaluateClassificationsDecisionBoundary(t *testing.T) {
	manifest, err := DecodeFrozenManifest([]byte(frozenManifestJSON()))
	if err != nil {
		t.Fatal(err)
	}
	classifications := make([]Classification, len(manifest.Items))
	for index, item := range manifest.Items {
		state := ClassificationSurvivor
		if index < 20 || item.Critical {
			state = ClassificationCorrectNewOnlyKill
		}
		classifications[index] = Classification{CandidateID: item.ID, Category: item.Category, State: state}
	}
	aggregate, err := EvaluateClassifications(manifest, classifications)
	if err != nil || aggregate.Decision != DecisionRejectDirection || aggregate.CorrectKills != 26 {
		t.Fatalf("26 with all critical correct = %#v, %v; want reject direction", aggregate, err)
	}
	classifications[20].State = ClassificationCorrectNewOnlyKill
	aggregate, err = EvaluateClassifications(manifest, classifications)
	if err != nil || aggregate.Decision != DecisionProceed {
		t.Fatalf("all critical correct = %#v, %v; want proceed", aggregate, err)
	}
	if _, err := EvaluateClassifications(manifest, classifications[:29]); !errors.Is(err, ErrInvalidAggregate) {
		t.Fatalf("short denominator error = %v, want %v", err, ErrInvalidAggregate)
	}
}

func scoringCandidateSet(t *testing.T) CandidateSet {
	t.Helper()
	set, err := DecodeCandidateSet([]byte(candidateSetJSON()))
	if err != nil {
		t.Fatal(err)
	}
	return set
}

func scoringIdentity() AuditIdentity {
	return AuditIdentity{AuditVersion: "v1", AuditSHA256: testDigest, CorpusVersion: "v1", CorpusSHA256: strings.Repeat("d", 64)}
}

func scoringControls(set CandidateSet, exit string) []AttemptEvidence {
	result := make([]AttemptEvidence, 0, 180)
	for _, queue := range set.Queues {
		for _, candidate := range queue.Candidates {
			for index, lane := range *set.LegacyLanes {
				evidence := validControlEvidence()
				evidence.AttemptID = fmt.Sprintf("control-%s-%d", candidate.ID, index+1)
				evidence.CandidateID, evidence.PatchSHA256, evidence.Lane = candidate.ID, candidate.PatchSHA256, lane.ID
				evidence.CommandSHA256, evidence.ExitClass = lane.CommandSHA256, exit
				if exit == ExitClassRejected {
					evidence.Failure = &StableFailure{Class: "contract", Phase: "validation", Coordinate: "modules[0]"}
				}
				result = append(result, evidence)
			}
		}
	}
	return result
}

func scoringDetectorCommands() []DetectorCommand {
	return []DetectorCommand{{ID: "module-contract", CommandSHA256: strings.Repeat("1", 64)}}
}

func scoringDetectorEvidence(manifest FrozenManifest, exit string) []AttemptEvidence {
	result := []AttemptEvidence{validBaselineEvidence(1), validBaselineEvidence(2)}
	result[0].AttemptID, result[1].AttemptID = "baseline-1", "baseline-2"
	for _, item := range manifest.Items {
		for repetition := 1; repetition <= 2; repetition++ {
			evidence := validMutationEvidence(repetition)
			evidence.AttemptID, evidence.CandidateID, evidence.PatchSHA256 = fmt.Sprintf("mutation-%s-%d", item.ID, repetition), item.ID, item.PatchSHA256
			evidence.ExitClass = exit
			if exit == ExitClassRejected {
				evidence.Failure = &StableFailure{Class: item.ExpectedFailure.Class, Phase: item.ExpectedFailure.Phase, Coordinate: item.ExpectedFailure.Coordinate}
			}
			result = append(result, evidence)
		}
	}
	return result
}

func cloneFailure(failure *StableFailure) *StableFailure {
	if failure == nil {
		return nil
	}
	copy := *failure
	return &copy
}
