// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationharness

import (
	"context"
	"fmt"
)

// hostedLiveEvidenceSource is implemented only by the concrete hosted runner.
// It returns a sanitized base assembled from retained session and execution
// proof; lifecycle truth is added separately from typed coordinator records.
type hostedLiveEvidenceSource interface {
	hostedLiveEvidenceBase(hostedLiveRunResult) (hostedLiveEvidence, error)
}

type hostedLiveEvidenceWriter func(hostedLiveEvidence) error

func runHostedLiveAndPersist(ctx context.Context, runner hostedLiveRunner, root hostedLiveEvidenceResultRoot) hostedLiveRunResult {
	return runHostedLiveAndPersistWithWriter(ctx, runner, func(evidence hostedLiveEvidence) error {
		return persistHostedLiveEvidence(root, evidence)
	})
}

func runHostedLiveAndPersistWithWriter(ctx context.Context, runner hostedLiveRunner, write hostedLiveEvidenceWriter) hostedLiveRunResult {
	result := runHostedLive(ctx, runner)
	source, ok := runner.(hostedLiveEvidenceSource)
	if !ok {
		return recordHostedLiveEvidenceFailure(result, "source")
	}
	base, err := source.hostedLiveEvidenceBase(result)
	if err != nil {
		return recordHostedLiveEvidenceFailure(result, "construction")
	}
	evidence, err := hostedLiveEvidenceFromRun(base, result)
	if err != nil {
		return recordHostedLiveEvidenceFailure(result, "construction")
	}
	if _, err := encodeHostedLiveEvidence(evidence); err != nil {
		return recordHostedLiveEvidenceFailure(result, "encoding")
	}
	if write == nil || write(evidence) != nil {
		return recordHostedLiveEvidenceFailure(result, "persistence")
	}
	return result
}

func recordHostedLiveEvidenceFailure(result hostedLiveRunResult, stage string) hostedLiveRunResult {
	result.evidenceErr = fmt.Errorf("hosted live evidence %s failed", stage)
	result.eligible = false
	if result.err == nil {
		result.phase, result.err = "evidence", result.evidenceErr
	}
	return result
}
