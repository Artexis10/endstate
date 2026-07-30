// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationharness

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestRunHostedLiveAndPersistWritesPassedEvidenceAfterCleanup(t *testing.T) {
	runner := &fakeHostedLiveEvidenceRunner{base: hostedLiveEvidenceForTest()}
	writes := 0
	result := runHostedLiveAndPersistWithWriter(context.Background(), runner, func(evidence hostedLiveEvidence) error {
		writes++
		if len(runner.calls) != len(hostedLiveLifecycle)+len(hostedLiveCleanup) {
			t.Fatalf("evidence persisted before cleanup: %#v", runner.calls)
		}
		if evidence.Status != "passed" || evidence.Cleanup.Status != "passed" || evidence.Package.Version == "" || evidence.Capture.Size == 0 {
			t.Fatalf("persisted evidence = %#v, want complete passed evidence", evidence)
		}
		return nil
	})
	if result.err != nil || !result.eligible || writes != 1 {
		t.Fatalf("result = %#v, writes = %d; want eligible one-write success", result, writes)
	}
}

func TestRunHostedLiveAndPersistWritesSafeEarlyFailureAfterCleanup(t *testing.T) {
	base := hostedLiveEvidenceForTest()
	base.Capture = hostedLiveEvidenceCapture{}
	base.Package.Version = ""
	runner := &fakeHostedLiveEvidenceRunner{fakeHostedLiveRunner: fakeHostedLiveRunner{fail: "initial"}, base: base}
	writes := 0
	result := runHostedLiveAndPersistWithWriter(context.Background(), runner, func(evidence hostedLiveEvidence) error {
		writes++
		if evidence.Status != "failed" || evidence.Failure.Phase != "initial" || evidence.Capture != (hostedLiveEvidenceCapture{}) || evidence.Package.Version != "" || !evidence.Candidate || evidence.PublicEvidenceEligible {
			t.Fatalf("persisted early-failure evidence = %#v", evidence)
		}
		return nil
	})
	if result.err == nil || result.phase != "initial" || result.eligible || writes != 1 {
		t.Fatalf("result = %#v, writes = %d; want failed one-write result", result, writes)
	}
}

func TestRunHostedLiveAndPersistWritesCleanupFailureWithoutLeakingError(t *testing.T) {
	runner := &fakeHostedLiveEvidenceRunner{fakeHostedLiveRunner: fakeHostedLiveRunner{cleanupFail: "final-wipe", phaseErr: errors.New(`C:\Users\alice\secret-token stdout stderr`)}, base: hostedLiveEvidenceForTest()}
	runner.fail = "engine-apply"
	writes := 0
	result := runHostedLiveAndPersistWithWriter(context.Background(), runner, func(evidence hostedLiveEvidence) error {
		writes++
		encoded, err := encodeHostedLiveEvidence(evidence)
		if err != nil {
			return err
		}
		for _, forbidden := range []string{"C:\\Users\\alice", "secret-token", "stdout", "stderr"} {
			if strings.Contains(string(encoded), forbidden) {
				t.Fatalf("persisted evidence leaked %q: %s", forbidden, encoded)
			}
		}
		if evidence.Failure.Phase != "engine-apply" || evidence.Cleanup != (hostedLiveEvidenceCleanup{Status: "failed", FailureCode: "final_wipe_failed"}) {
			t.Fatalf("cleanup failure evidence = %#v", evidence)
		}
		return nil
	})
	if result.err == nil || result.phase != "engine-apply" || result.cleanupPhase != "final-wipe" || writes != 1 {
		t.Fatalf("result = %#v, writes = %d", result, writes)
	}
}

func TestRunHostedLiveAndPersistReportsConstructionAndWriterFailure(t *testing.T) {
	for _, test := range []struct {
		name   string
		runner *fakeHostedLiveEvidenceRunner
		writer func(hostedLiveEvidence) error
	}{
		{"source", &fakeHostedLiveEvidenceRunner{baseErr: errors.New("untrusted source")}, func(hostedLiveEvidence) error { t.Fatal("writer called after source failure"); return nil }},
		{"writer", &fakeHostedLiveEvidenceRunner{base: hostedLiveEvidenceForTest()}, func(hostedLiveEvidence) error { return errors.New("disk unavailable") }},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := runHostedLiveAndPersistWithWriter(context.Background(), test.runner, test.writer)
			if result.err == nil || result.eligible || result.evidenceErr == nil || result.phase != "evidence" {
				t.Fatalf("result = %#v, want reported ineligible evidence failure", result)
			}
		})
	}
}

type fakeHostedLiveEvidenceRunner struct {
	fakeHostedLiveRunner
	base    hostedLiveEvidence
	baseErr error
}

func (runner *fakeHostedLiveEvidenceRunner) hostedLiveEvidenceBase(hostedLiveRunResult) (hostedLiveEvidence, error) {
	if runner.baseErr != nil {
		return hostedLiveEvidence{}, runner.baseErr
	}
	return runner.base, nil
}
