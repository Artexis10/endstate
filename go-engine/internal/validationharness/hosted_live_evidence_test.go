// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationharness

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestHostedLiveEvidenceEncodesCompleteSanitizedTypedRecord(t *testing.T) {
	evidence := hostedLiveEvidenceForTest()
	encoded, err := encodeHostedLiveEvidence(evidence)
	if err != nil {
		t.Fatalf("encodeHostedLiveEvidence() error = %v", err)
	}
	if len(encoded) > maxHostedLiveEvidenceBytes {
		t.Fatalf("encoded evidence is %d bytes, want <= %d", len(encoded), maxHostedLiveEvidenceBytes)
	}
	for _, forbidden := range []string{"C:\\Users\\alice", "secret-token", "stdout", "stderr", "settings", "zip-bytes"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("encoded evidence leaked %q: %s", forbidden, encoded)
		}
	}
	var wire map[string]any
	if err := json.Unmarshal(encoded, &wire); err != nil {
		t.Fatalf("decode encoded evidence: %v", err)
	}
	for _, field := range []string{"schemaVersion", "campaign", "run", "engine", "inputs", "capture", "runner", "package", "phases", "status", "candidate", "publicEvidenceEligible", "cleanup"} {
		if _, ok := wire[field]; !ok {
			t.Fatalf("encoded evidence omitted %q", field)
		}
	}
	if wire["candidate"] != true || wire["publicEvidenceEligible"] != false {
		t.Fatalf("candidate evidence eligibility = %#v/%#v", wire["candidate"], wire["publicEvidenceEligible"])
	}
	for _, forbidden := range []string{`"ID"`, `"SHA256"`, `"DurationMilliseconds"`} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("encoded evidence used an implicit Go field name %q: %s", forbidden, encoded)
		}
	}
}

func TestHostedLiveEvidenceRejectsIdentityAndPhaseTampering(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*hostedLiveEvidence)
	}{
		{"invalid campaign", func(e *hostedLiveEvidence) { e.Campaign = strings.Repeat("A", 64) }},
		{"invalid run", func(e *hostedLiveEvidence) { e.Run.ID = 0 }},
		{"invalid commit", func(e *hostedLiveEvidence) { e.Engine.Commit = "not-a-commit" }},
		{"invalid hash", func(e *hostedLiveEvidence) { e.Inputs.TargetsSHA256 = "invalid" }},
		{"duplicate phase", func(e *hostedLiveEvidence) { e.Phases[1] = e.Phases[0] }},
		{"out of order phase", func(e *hostedLiveEvidence) { e.Phases[0], e.Phases[1] = e.Phases[1], e.Phases[0] }},
		{"missing phase", func(e *hostedLiveEvidence) { e.Phases = e.Phases[:len(e.Phases)-1] }},
		{"bad phase index", func(e *hostedLiveEvidence) { e.Phases[0].Index = 9 }},
		{"skipped successful phase", func(e *hostedLiveEvidence) { e.Phases[0].Status = "skipped" }},
		{"public eligible", func(e *hostedLiveEvidence) { e.PublicEvidenceEligible = true }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			evidence := hostedLiveEvidenceForTest()
			test.mutate(&evidence)
			if _, err := encodeHostedLiveEvidence(evidence); err == nil {
				t.Fatal("encodeHostedLiveEvidence() accepted tampered evidence")
			}
		})
	}
}

func TestHostedLiveEvidenceRejectsRecordAtInjectedCompactLimit(t *testing.T) {
	evidence := hostedLiveEvidenceForTest()
	encoded, err := encodeHostedLiveEvidence(evidence)
	if err != nil {
		t.Fatalf("encodeHostedLiveEvidence() error = %v", err)
	}
	if _, err := encodeHostedLiveEvidenceWithLimit(evidence, len(encoded)-1); err == nil {
		t.Fatal("encodeHostedLiveEvidenceWithLimit() accepted a record beyond its byte limit")
	}
}

func TestHostedLiveEvidenceFailureIsBoundedAndCleanupFailureOverridesPass(t *testing.T) {
	evidence := hostedLiveEvidenceForTest()
	evidence.Status = "failed"
	evidence.Phases[len(hostedLiveLifecycle)].Status = "failed"
	evidence.Failure = hostedLiveEvidenceFailure{Code: "final_uninstall_failed", Phase: "final-uninstall", PhaseIndex: len(hostedLiveLifecycle) + 1}
	evidence.Cleanup = hostedLiveEvidenceCleanup{Status: "failed", FailureCode: "final_uninstall_failed"}
	encoded, err := encodeHostedLiveEvidence(evidence)
	if err != nil {
		t.Fatalf("encode cleanup failure evidence: %v", err)
	}
	if strings.Contains(string(encoded), "message") || strings.Contains(string(encoded), `C:\\`) {
		t.Fatalf("failure evidence leaked free-form material: %s", encoded)
	}
}

func TestHostedLiveEvidenceRejectsSkippedOrderingAndCampaignMismatch(t *testing.T) {
	evidence := hostedLiveEvidenceForTest()
	evidence.Status = "failed"
	evidence.Phases[1].Status = "failed"
	for index := 2; index < len(hostedLiveLifecycle); index++ {
		evidence.Phases[index].Status = "skipped"
		evidence.Phases[index].DurationMilliseconds = 0
		evidence.Phases[index].Assertions = 0
	}
	evidence.Failure = hostedLiveEvidenceFailure{Code: "compile_failed", Phase: "compile", PhaseIndex: 2}
	if _, err := encodeHostedLiveEvidence(evidence); err != nil {
		t.Fatalf("encode ordered failure evidence: %v", err)
	}

}

func TestHostedLiveEvidenceFromRunUsesOnlyTypedPhaseRecords(t *testing.T) {
	result := runHostedLiveWithClock(context.Background(), &fakeHostedLiveRunner{fail: "compile", cleanupFail: "final-wipe"}, hostedLiveTestClock(time.Unix(0, 0), time.Millisecond))
	result.err = errors.New("C:\\Users\\alice\\secret-token")
	result.cleanupErr = errors.New("captured stdout and stderr")
	evidence, err := hostedLiveEvidenceFromRun(hostedLiveEvidenceForTest(), result)
	if err != nil {
		t.Fatalf("hostedLiveEvidenceFromRun() error = %v", err)
	}
	if evidence.Status != "failed" || evidence.Failure != (hostedLiveEvidenceFailure{Code: "compile_failed", Phase: "compile", PhaseIndex: 2}) {
		t.Fatalf("failure = %#v, want deterministic compile failure", evidence.Failure)
	}
	if evidence.Cleanup != (hostedLiveEvidenceCleanup{Status: "failed", FailureCode: "final_wipe_failed"}) {
		t.Fatalf("cleanup = %#v, want deterministic final-wipe failure", evidence.Cleanup)
	}
	if evidence.Candidate != true || evidence.PublicEvidenceEligible {
		t.Fatalf("eligibility = %#v/%#v", evidence.Candidate, evidence.PublicEvidenceEligible)
	}
	encoded, err := encodeHostedLiveEvidence(evidence)
	if err != nil {
		t.Fatalf("encode typed failure evidence: %v", err)
	}
	for _, forbidden := range []string{"C:\\Users\\alice", "secret-token", "stdout", "stderr"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("typed evidence leaked %q: %s", forbidden, encoded)
		}
	}
}

func TestHostedLiveEvidenceFailureAllowsUnobservedCaptureAndPackageVersion(t *testing.T) {
	result := runHostedLiveWithClock(context.Background(), &fakeHostedLiveRunner{fail: "initial"}, hostedLiveTestClock(time.Unix(0, 0), time.Millisecond))
	base := hostedLiveEvidenceForTest()
	base.Capture = hostedLiveEvidenceCapture{}
	base.Package.Version = ""
	evidence, err := hostedLiveEvidenceFromRun(base, result)
	if err != nil {
		t.Fatalf("hostedLiveEvidenceFromRun() error = %v", err)
	}
	if _, err := encodeHostedLiveEvidence(evidence); err != nil {
		t.Fatalf("encode early failure evidence: %v", err)
	}
}

func TestHostedLiveEvidencePassedRequiresObservedPackageAndCapture(t *testing.T) {
	for _, mutate := range []func(*hostedLiveEvidence){
		func(e *hostedLiveEvidence) { e.Capture = hostedLiveEvidenceCapture{} },
		func(e *hostedLiveEvidence) { e.Package.Version = "" },
	} {
		evidence := hostedLiveEvidenceForTest()
		mutate(&evidence)
		if _, err := encodeHostedLiveEvidence(evidence); err == nil {
			t.Fatal("encodeHostedLiveEvidence() accepted incomplete passed evidence")
		}
	}
}

func TestHostedLiveEvidenceFailedAfterObservationRequiresObservedValues(t *testing.T) {
	result := runHostedLiveWithClock(context.Background(), &fakeHostedLiveRunner{fail: "winget-exact-uninstall"}, hostedLiveTestClock(time.Unix(0, 0), time.Millisecond))
	base := hostedLiveEvidenceForTest()
	base.Capture = hostedLiveEvidenceCapture{}
	base.Package.Version = ""
	if _, err := hostedLiveEvidenceFromRun(base, result); err == nil {
		t.Fatal("hostedLiveEvidenceFromRun() accepted missing observations after their lifecycle phases")
	}
}

func hostedLiveEvidenceForTest() hostedLiveEvidence {
	hash := func(character string) string { return strings.Repeat(character, 64) }
	phases := make([]hostedLiveEvidencePhase, 0, len(hostedLiveLifecycle)+len(hostedLiveCleanup))
	for _, name := range append(append([]string(nil), hostedLiveLifecycle...), hostedLiveCleanup...) {
		phases = append(phases, hostedLiveEvidencePhase{Index: len(phases) + 1, Name: name, Status: "passed", DurationMilliseconds: 1, Assertions: 1})
	}
	return hostedLiveEvidence{
		Campaign: hash("0"),
		Run:      hostedLiveEvidenceRun{ID: 1234, Attempt: 1, Event: "schedule", Ref: "refs/heads/main", TrustedCommit: strings.Repeat("a", 40)},
		Engine:   hostedLiveEvidenceEngine{Commit: strings.Repeat("b", 40), Version: "0.1.0", SHA256: hash("c"), ValidatorSHA256: hash("d")},
		Inputs:   hostedLiveEvidenceInputs{DefinitionSHA256: hash("1"), ModuleSHA256: hash("e"), ValidationSourceSHA256: hash("f"), SeedSHA256: hash("a"), ComparatorSHA256: hash("b"), TargetsSHA256: hash("c"), ObserverSHA256: hash("d"), WorkflowSHA256: hash("e")},
		Capture:  hostedLiveEvidenceCapture{SHA256: hash("8"), Size: 42},
		Runner:   hostedLiveEvidenceRunner{OS: "windows", Image: "windows-2022"},
		Package:  hostedLiveEvidencePackage{Driver: "winget", Source: "winget", Ref: "Notepad++.Notepad++", Version: "8.7.7"},
		Phases:   phases, Status: "passed", Candidate: true, PublicEvidenceEligible: false,
		Cleanup: hostedLiveEvidenceCleanup{Status: "passed"},
	}
}
