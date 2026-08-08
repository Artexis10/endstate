// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationmatrix

import (
	"regexp"
	"sort"
	"time"
)

const (
	MaxLiveJobsPerChunk       = 64
	MaxScheduledHostedModules = MaxLiveJobsPerChunk * 7
	MaxReleaseChunks          = 6
	MaxReleaseHostedModules   = MaxLiveJobsPerChunk * MaxReleaseChunks

	CodeDuplicateScheduledState   = "duplicate_scheduled_state"
	CodeScheduledCapacityExceeded = "scheduled_freshness_capacity_exceeded"
	CodeInvalidEngineCommit       = "invalid_engine_commit"
	CodeInvalidDispatchSelection  = "invalid_dispatch_selection"
	CodeDuplicateDispatchModule   = "duplicate_dispatch_module"
	CodeUnknownDispatchModule     = "unknown_dispatch_module"
	CodeNonHostedDispatchModule   = "non_hosted_dispatch_module"
	CodeDispatchCapacityExceeded  = "dispatch_capacity_exceeded"
	CodeInvalidDispatchChunk      = "invalid_dispatch_chunk"
	CodeReleaseCapacityExceeded   = "release_campaign_capacity_exceeded"
)

const (
	PolicySourceTrustedMain PolicySource = "trusted-main"

	ReasonTrustedMainHosted         PlanReason = "trusted-main-hosted"
	ReasonScheduledNeverAttempted   PlanReason = "scheduled-never-attempted"
	ReasonScheduledOldestAttemptDay PlanReason = "scheduled-oldest-attempt-day"
	ReasonDispatchExplicitIDs       PlanReason = "dispatch-explicit-module-ids"
	ReasonDispatchChunkIndex        PlanReason = "dispatch-chunk-index"
	ReasonReleaseExactCommit        PlanReason = "release-exact-commit"
)

type DispatchSelection string

const (
	DispatchSelectionExplicitModuleIDs DispatchSelection = "explicit-module-ids"
	DispatchSelectionChunkIndex        DispatchSelection = "chunk-index"
)

type HostedPlan struct {
	HostedCount int         `json:"hostedCount"`
	Jobs        []LiveRow   `json:"jobs"`
	Chunks      []LiveChunk `json:"chunks"`
}

type LiveChunk struct {
	Index int       `json:"index"`
	Jobs  []LiveRow `json:"jobs"`
}

type ScheduledModuleState struct {
	ModuleID    string     `json:"moduleId"`
	LastAttempt *time.Time `json:"lastAttempt,omitempty"`
	Failing     bool       `json:"failing"`
	Stale       bool       `json:"stale"`
}

type ScheduledPlan struct {
	HostedCount int       `json:"hostedCount"`
	Jobs        []LiveRow `json:"jobs"`
}

type DispatchPlanOptions struct {
	EngineCommit string
	ModuleIDs    []string
	ChunkIndex   *int
}

type DispatchPlan struct {
	EngineCommit string            `json:"engineCommit"`
	HostedCount  int               `json:"hostedCount"`
	Selection    DispatchSelection `json:"selection"`
	ChunkIndex   *int              `json:"chunkIndex,omitempty"`
	Jobs         []LiveRow         `json:"jobs"`
}

type DispatchCapacityError struct {
	ValidationError       *ValidationError `json:"-"`
	RemainingChunkIndices []int            `json:"remainingChunkIndices"`
}

func (e *DispatchCapacityError) Error() string {
	return e.ValidationError.Error()
}

func (e *DispatchCapacityError) Unwrap() error {
	return e.ValidationError
}

type ReleasePlan struct {
	EngineCommit string      `json:"engineCommit"`
	HostedCount  int         `json:"hostedCount"`
	Chunks       []LiveChunk `json:"chunks"`
}

var exactEngineCommitPattern = regexp.MustCompile(`^(?:[0-9a-f]{40}|[0-9a-f]{64})$`)

func PlanHostedCatalog(catalog *Catalog) (HostedPlan, error) {
	if catalog == nil {
		return HostedPlan{}, validationError(CodeInvalidPlanCatalog, "", "", "catalog is required")
	}

	jobs := make([]LiveRow, 0)
	for _, record := range catalog.Records {
		if record.Live.Mode != LiveHosted {
			continue
		}
		row, err := liveRow(record, PlanStatusRequired, ReasonTrustedMainHosted, PolicySourceTrustedMain)
		if err != nil {
			return HostedPlan{}, err
		}
		jobs = append(jobs, row)
	}
	sort.Slice(jobs, func(left, right int) bool {
		return jobs[left].ModuleID < jobs[right].ModuleID
	})

	return HostedPlan{
		HostedCount: len(jobs),
		Jobs:        jobs,
		Chunks:      chunkLiveRows(jobs),
	}, nil
}

func PlanScheduled(catalog *Catalog, states []ScheduledModuleState) (ScheduledPlan, error) {
	hosted, err := PlanHostedCatalog(catalog)
	if err != nil {
		return ScheduledPlan{}, err
	}
	if hosted.HostedCount > MaxScheduledHostedModules {
		return ScheduledPlan{}, validationError(
			CodeScheduledCapacityExceeded, "", "",
			"%d hosted modules exceed the seven-day capacity of %d", hosted.HostedCount, MaxScheduledHostedModules,
		)
	}

	hostedIDs := make(map[string]struct{}, len(hosted.Jobs))
	for _, row := range hosted.Jobs {
		hostedIDs[row.ModuleID] = struct{}{}
	}
	stateByModule := make(map[string]ScheduledModuleState, len(states))
	for _, state := range states {
		if _, hosted := hostedIDs[state.ModuleID]; !hosted {
			continue
		}
		if _, duplicate := stateByModule[state.ModuleID]; duplicate {
			return ScheduledPlan{}, validationError(
				CodeDuplicateScheduledState, state.ModuleID, "", "scheduled state is declared more than once",
			)
		}
		stateByModule[state.ModuleID] = state
	}

	type scheduledCandidate struct {
		row          LiveRow
		state        ScheduledModuleState
		neverAttempt bool
		attemptDay   time.Time
	}
	candidates := make([]scheduledCandidate, len(hosted.Jobs))
	for index, row := range hosted.Jobs {
		state, found := stateByModule[row.ModuleID]
		neverAttempt := !found || state.LastAttempt == nil
		candidate := scheduledCandidate{row: row, state: state, neverAttempt: neverAttempt}
		if !neverAttempt {
			year, month, day := state.LastAttempt.UTC().Date()
			candidate.attemptDay = time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
		}
		candidates[index] = candidate
	}
	sort.Slice(candidates, func(left, right int) bool {
		leftCandidate := candidates[left]
		rightCandidate := candidates[right]
		if leftCandidate.neverAttempt != rightCandidate.neverAttempt {
			return leftCandidate.neverAttempt
		}
		if !leftCandidate.neverAttempt && !leftCandidate.attemptDay.Equal(rightCandidate.attemptDay) {
			return leftCandidate.attemptDay.Before(rightCandidate.attemptDay)
		}
		if leftCandidate.state.Failing != rightCandidate.state.Failing {
			return leftCandidate.state.Failing
		}
		if leftCandidate.state.Stale != rightCandidate.state.Stale {
			return leftCandidate.state.Stale
		}
		return leftCandidate.row.ModuleID < rightCandidate.row.ModuleID
	})

	jobCount := len(candidates)
	if jobCount > MaxLiveJobsPerChunk {
		jobCount = MaxLiveJobsPerChunk
	}
	jobs := make([]LiveRow, jobCount)
	for index := range jobs {
		jobs[index] = candidates[index].row
		if candidates[index].neverAttempt {
			jobs[index].Reason = ReasonScheduledNeverAttempted
		} else {
			jobs[index].Reason = ReasonScheduledOldestAttemptDay
		}
	}
	return ScheduledPlan{HostedCount: hosted.HostedCount, Jobs: jobs}, nil
}

func PlanDispatch(catalog *Catalog, options DispatchPlanOptions) (DispatchPlan, error) {
	if err := validateEngineCommit(options.EngineCommit); err != nil {
		return DispatchPlan{}, err
	}
	hosted, err := PlanHostedCatalog(catalog)
	if err != nil {
		return DispatchPlan{}, err
	}
	hasExplicitIDs := options.ModuleIDs != nil
	hasChunkIndex := options.ChunkIndex != nil
	if hasExplicitIDs == hasChunkIndex {
		return DispatchPlan{}, validationError(CodeInvalidDispatchSelection, "", "", "select exactly one of module IDs or chunk index")
	}

	if hasExplicitIDs {
		if len(options.ModuleIDs) == 0 {
			return DispatchPlan{}, validationError(CodeInvalidDispatchSelection, "", "", "explicit module ID selection cannot be empty")
		}
		jobs, selectErr := selectExplicitDispatchJobs(catalog, hosted.Jobs, options.ModuleIDs)
		if selectErr != nil {
			return DispatchPlan{}, selectErr
		}
		if len(jobs) > MaxLiveJobsPerChunk {
			return DispatchPlan{}, dispatchCapacityError(len(jobs))
		}
		return DispatchPlan{
			EngineCommit: options.EngineCommit,
			HostedCount:  hosted.HostedCount,
			Selection:    DispatchSelectionExplicitModuleIDs,
			Jobs:         jobs,
		}, nil
	}

	chunkIndex := *options.ChunkIndex
	if chunkIndex < 0 || chunkIndex >= len(hosted.Chunks) {
		return DispatchPlan{}, validationError(
			CodeInvalidDispatchChunk, "", "", "chunk index %d is outside [0,%d)", chunkIndex, len(hosted.Chunks),
		)
	}
	jobs := cloneLiveRows(hosted.Chunks[chunkIndex].Jobs)
	for index := range jobs {
		jobs[index].Reason = ReasonDispatchChunkIndex
	}
	selectedChunk := chunkIndex
	return DispatchPlan{
		EngineCommit: options.EngineCommit,
		HostedCount:  hosted.HostedCount,
		Selection:    DispatchSelectionChunkIndex,
		ChunkIndex:   &selectedChunk,
		Jobs:         jobs,
	}, nil
}

func PlanRelease(catalog *Catalog, engineCommit string) (ReleasePlan, error) {
	if err := validateEngineCommit(engineCommit); err != nil {
		return ReleasePlan{}, err
	}
	hosted, err := PlanHostedCatalog(catalog)
	if err != nil {
		return ReleasePlan{}, err
	}
	if hosted.HostedCount > MaxReleaseHostedModules {
		return ReleasePlan{}, validationError(
			CodeReleaseCapacityExceeded, "", "",
			"%d hosted modules require more than %d release chunks", hosted.HostedCount, MaxReleaseChunks,
		)
	}
	chunks := make([]LiveChunk, len(hosted.Chunks))
	for index, chunk := range hosted.Chunks {
		jobs := cloneLiveRows(chunk.Jobs)
		for jobIndex := range jobs {
			jobs[jobIndex].Reason = ReasonReleaseExactCommit
		}
		chunks[index] = LiveChunk{Index: chunk.Index, Jobs: jobs}
	}
	return ReleasePlan{EngineCommit: engineCommit, HostedCount: hosted.HostedCount, Chunks: chunks}, nil
}

func selectExplicitDispatchJobs(catalog *Catalog, hostedJobs []LiveRow, moduleIDs []string) ([]LiveRow, error) {
	hostedByModule := make(map[string]LiveRow, len(hostedJobs))
	for _, row := range hostedJobs {
		hostedByModule[row.ModuleID] = row
	}

	selectedIDs := append([]string(nil), moduleIDs...)
	seen := make(map[string]struct{}, len(selectedIDs))
	for _, moduleID := range selectedIDs {
		if _, duplicate := seen[moduleID]; duplicate {
			return nil, validationError(CodeDuplicateDispatchModule, moduleID, "", "dispatch module ID is declared more than once")
		}
		seen[moduleID] = struct{}{}
		record, known := catalog.Records[moduleID]
		if !known {
			return nil, validationError(CodeUnknownDispatchModule, moduleID, "", "dispatch module ID is not in the current catalog")
		}
		if record.Live.Mode != LiveHosted {
			return nil, validationError(CodeNonHostedDispatchModule, moduleID, record.FilePath, "dispatch module is not hosted")
		}
	}
	sort.Strings(selectedIDs)
	jobs := make([]LiveRow, len(selectedIDs))
	for index, moduleID := range selectedIDs {
		jobs[index] = hostedByModule[moduleID]
		jobs[index].Reason = ReasonDispatchExplicitIDs
	}
	return jobs, nil
}

func chunkLiveRows(jobs []LiveRow) []LiveChunk {
	if len(jobs) == 0 {
		return nil
	}
	chunkCount := (len(jobs) + MaxLiveJobsPerChunk - 1) / MaxLiveJobsPerChunk
	chunks := make([]LiveChunk, 0, chunkCount)
	for start := 0; start < len(jobs); start += MaxLiveJobsPerChunk {
		end := start + MaxLiveJobsPerChunk
		if end > len(jobs) {
			end = len(jobs)
		}
		chunks = append(chunks, LiveChunk{Index: len(chunks), Jobs: cloneLiveRows(jobs[start:end])})
	}
	return chunks
}

func cloneLiveRows(rows []LiveRow) []LiveRow {
	clones := make([]LiveRow, len(rows))
	for index, row := range rows {
		row.Policy = cloneLivePolicy(row.Policy)
		row.Quarantines = canonicalQuarantines(row.Quarantines)
		clones[index] = row
	}
	return clones
}

func dispatchCapacityError(jobCount int) error {
	chunkCount := (jobCount + MaxLiveJobsPerChunk - 1) / MaxLiveJobsPerChunk
	remaining := make([]int, chunkCount-1)
	for index := range remaining {
		remaining[index] = index + 1
	}
	return &DispatchCapacityError{
		ValidationError: &ValidationError{
			Code:   CodeDispatchCapacityExceeded,
			Detail: "dispatch selects more than one deterministic chunk",
		},
		RemainingChunkIndices: remaining,
	}
}

func validateEngineCommit(engineCommit string) error {
	if !exactEngineCommitPattern.MatchString(engineCommit) {
		return validationError(CodeInvalidEngineCommit, "", "", "engine commit must be exactly 40 or 64 lowercase hexadecimal characters")
	}
	return nil
}
