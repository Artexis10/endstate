// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package migration

// ProgressStage is the staging subset of the public config-migration stage
// vocabulary. Commit and rollback progress belong to configrestore.
type ProgressStage string

const (
	ProgressStaging    ProgressStage = "staging"
	ProgressEdge       ProgressStage = "edge"
	ProgressValidation ProgressStage = "validation"
)

// ProgressStatus is the closed progress transition vocabulary.
type ProgressStatus string

const (
	ProgressStarted   ProgressStatus = "started"
	ProgressCompleted ProgressStatus = "completed"
	ProgressFailed    ProgressStatus = "failed"
)

// StageProgress is one synchronous, engine-owned staging transition.
type StageProgress struct {
	CaptureID      string
	Stage          ProgressStage
	Status         ProgressStatus
	EdgeIndex      int
	FromGeneration string
	ToGeneration   string
	Code           ErrorCode
}

// StageObserver receives read-only progress. Implementations must not mutate
// the Stage request or staged files.
type StageObserver interface {
	ObserveStageProgress(StageProgress)
}

type progressReporter struct {
	observer  StageObserver
	captureID string
	current   *StageProgress
}

func newProgressReporter(captureID string, observer StageObserver) *progressReporter {
	return &progressReporter{observer: observer, captureID: captureID}
}

func (r *progressReporter) start(stage ProgressStage, edgeIndex int, from, to string) {
	progress := StageProgress{
		CaptureID: r.captureID, Stage: stage, Status: ProgressStarted, EdgeIndex: edgeIndex,
		FromGeneration: from, ToGeneration: to,
	}
	r.current = &progress
	r.emit(progress)
}

func (r *progressReporter) complete() {
	if r.current == nil {
		return
	}
	progress := *r.current
	progress.Status = ProgressCompleted
	r.emit(progress)
	r.current = nil
}

func (r *progressReporter) fail(code ErrorCode) {
	if r.current == nil {
		return
	}
	progress := *r.current
	progress.Status = ProgressFailed
	progress.Code = code
	r.emit(progress)
	r.current = nil
}

func (r *progressReporter) emit(progress StageProgress) {
	if r.observer != nil {
		r.observer.ObserveStageProgress(progress)
	}
}
