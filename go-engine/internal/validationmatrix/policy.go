// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationmatrix

import (
	"regexp"
	"strings"
	"time"
)

const (
	CodeWorkflowTimeoutPolicy  = "workflow_timeout_policy_violation"
	CodeWorkflowResourcePolicy = "workflow_resource_policy_violation"
	CodeWorkflowArtifactPolicy = "workflow_artifact_policy_violation"
	CodeWorkflowRunnerPolicy   = "workflow_runner_policy_violation"
	CodeWorkflowActionPolicy   = "workflow_action_policy_violation"
)

const (
	workflowMaxSyntheticShardTimeout = 15 * time.Minute
	workflowMaxPRLiveTimeout         = 25 * time.Minute
	workflowDefaultScheduledTimeout  = 30 * time.Minute
	workflowMaxScheduledTimeout      = 45 * time.Minute
	workflowMaxPRCriticalPath        = 40 * time.Minute

	workflowMaxPRRunnerMinutes      = 250
	workflowMaxChunkRunnerMinutes   = 2880
	workflowMaxChunkParallel        = 8
	workflowMaxReleaseRunnerMinutes = 17280

	workflowMaxChunkUploadedBytes               = int64(100 * 1024 * 1024)
	workflowMaxEvidenceRecordBytes              = int64(64 * 1024)
	workflowMaxFailureDiagnosticsBytesPerModule = int64(1024 * 1024)
	workflowMaxValidationOwnedBytes             = int64(1024 * 1024 * 1024)

	workflowMaxEngineRetention         = 24 * time.Hour
	workflowMaxPREvidenceRetention     = 7 * 24 * time.Hour
	workflowScheduledEvidenceRetention = 90 * 24 * time.Hour
	workflowMaxDiagnosticRetention     = 3 * 24 * time.Hour
)

var immutableRemoteActionPattern = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9_.-]*[A-Za-z0-9])?/[A-Za-z0-9](?:[A-Za-z0-9_.-]*[A-Za-z0-9])?@[0-9a-f]{40}$`)

// WorkflowResourcePlan is the declared workflow policy that must pass before
// validation jobs or uploads are spawned.
type WorkflowResourcePlan struct {
	Timeouts  WorkflowTimeoutPlan     `json:"timeouts"`
	Compute   WorkflowComputePlan     `json:"compute"`
	Artifacts WorkflowArtifactPlan    `json:"artifacts"`
	Storage   WorkflowStorageSnapshot `json:"storage"`
	Trust     WorkflowTrustPlan       `json:"trust"`
}

type WorkflowTimeoutPlan struct {
	SyntheticShard time.Duration        `json:"syntheticShard"`
	PRLive         time.Duration        `json:"prLive"`
	ScheduledLive  ScheduledLiveTimeout `json:"scheduledLive"`
	PRCriticalPath time.Duration        `json:"prCriticalPath"`
}

type ScheduledLiveTimeout struct {
	Duration      time.Duration `json:"duration"`
	ScheduledOnly bool          `json:"scheduledOnly"`
	Justification string        `json:"justification,omitempty"`
}

type WorkflowComputePlan struct {
	PRRunnerMinutes int                 `json:"prRunnerMinutes"`
	Chunk           WorkflowChunkPlan   `json:"chunk"`
	Release         WorkflowReleasePlan `json:"release"`
}

type WorkflowChunkPlan struct {
	LiveJobs      int `json:"liveJobs"`
	RunnerMinutes int `json:"runnerMinutes"`
	MaxParallel   int `json:"maxParallel"`
}

type WorkflowReleasePlan struct {
	Chunks        int `json:"chunks"`
	RunnerMinutes int `json:"runnerMinutes"`
}

type WorkflowArtifactPlan struct {
	ChunkUploadedBytes               int64         `json:"chunkUploadedBytes"`
	EvidenceRecordBytes              int64         `json:"evidenceRecordBytes"`
	FailureDiagnosticsBytesPerModule int64         `json:"failureDiagnosticsBytesPerModule"`
	EngineRetention                  time.Duration `json:"engineRetention"`
	PREvidenceRetention              time.Duration `json:"prEvidenceRetention"`
	ScheduledEvidenceRetention       time.Duration `json:"scheduledEvidenceRetention"`
	DiagnosticRetention              time.Duration `json:"diagnosticRetention"`
}

type WorkflowStorageSnapshot struct {
	RetainedBytes        int64 `json:"retainedBytes"`
	ReservedBytes        int64 `json:"reservedBytes"`
	IncludedStorageBytes int64 `json:"includedStorageBytes"`
}

type WorkflowTrustPlan struct {
	RunnerLabels []string `json:"runnerLabels"`
	ActionUses   []string `json:"actionUses"`
}

// CanonicalWorkflowResourcePlan returns a fresh valid default candidate.
func CanonicalWorkflowResourcePlan() WorkflowResourcePlan {
	return WorkflowResourcePlan{
		Timeouts: WorkflowTimeoutPlan{
			SyntheticShard: workflowMaxSyntheticShardTimeout,
			PRLive:         workflowMaxPRLiveTimeout,
			PRCriticalPath: workflowMaxPRCriticalPath,
			ScheduledLive: ScheduledLiveTimeout{
				Duration: workflowDefaultScheduledTimeout,
			},
		},
		Compute: WorkflowComputePlan{
			PRRunnerMinutes: workflowMaxPRRunnerMinutes,
			Chunk: WorkflowChunkPlan{
				LiveJobs:      MaxLiveJobsPerChunk,
				RunnerMinutes: workflowMaxChunkRunnerMinutes,
				MaxParallel:   workflowMaxChunkParallel,
			},
			Release: WorkflowReleasePlan{
				Chunks:        MaxReleaseChunks,
				RunnerMinutes: workflowMaxReleaseRunnerMinutes,
			},
		},
		Artifacts: WorkflowArtifactPlan{
			ChunkUploadedBytes:               workflowMaxChunkUploadedBytes,
			EvidenceRecordBytes:              workflowMaxEvidenceRecordBytes,
			FailureDiagnosticsBytesPerModule: workflowMaxFailureDiagnosticsBytesPerModule,
			EngineRetention:                  workflowMaxEngineRetention,
			PREvidenceRetention:              workflowMaxPREvidenceRetention,
			ScheduledEvidenceRetention:       workflowScheduledEvidenceRetention,
			DiagnosticRetention:              workflowMaxDiagnosticRetention,
		},
		Storage: WorkflowStorageSnapshot{IncludedStorageBytes: workflowMaxValidationOwnedBytes},
		Trust: WorkflowTrustPlan{
			RunnerLabels: []string{"windows-latest"},
			ActionUses: []string{
				"./.github/actions/verified-module-matrix",
				"actions/checkout@0123456789abcdef0123456789abcdef01234567",
			},
		},
	}
}

// ValidateWorkflowResourcePlan deterministically rejects policy violations
// before callers start validation jobs or artifact uploads.
func ValidateWorkflowResourcePlan(plan WorkflowResourcePlan) error {
	if err := validateWorkflowTimeouts(plan.Timeouts); err != nil {
		return err
	}
	if err := validateWorkflowCompute(plan.Compute); err != nil {
		return err
	}
	if err := validateWorkflowArtifacts(plan.Artifacts, plan.Storage); err != nil {
		return err
	}
	if err := validateWorkflowTrust(plan.Trust); err != nil {
		return err
	}
	return nil
}

func validateWorkflowTimeouts(plan WorkflowTimeoutPlan) error {
	if plan.SyntheticShard <= 0 || plan.SyntheticShard > workflowMaxSyntheticShardTimeout {
		return validationError(CodeWorkflowTimeoutPolicy, "", "", "synthetic shard timeout must be positive and at most 15 minutes")
	}
	if plan.PRLive <= 0 || plan.PRLive > workflowMaxPRLiveTimeout {
		return validationError(CodeWorkflowTimeoutPolicy, "", "", "PR live timeout must be positive and at most 25 minutes")
	}
	if plan.PRCriticalPath <= 0 || plan.PRCriticalPath > workflowMaxPRCriticalPath {
		return validationError(CodeWorkflowTimeoutPolicy, "", "", "PR critical path must be positive and at most 40 minutes")
	}
	if plan.ScheduledLive.Duration <= 0 || plan.ScheduledLive.Duration > workflowMaxScheduledTimeout {
		return validationError(CodeWorkflowTimeoutPolicy, "", "", "scheduled live timeout must be positive and at most 45 minutes")
	}
	if plan.ScheduledLive.Duration > workflowDefaultScheduledTimeout &&
		(!plan.ScheduledLive.ScheduledOnly || strings.TrimSpace(plan.ScheduledLive.Justification) == "") {
		return validationError(CodeWorkflowTimeoutPolicy, "", "", "scheduled live timeout above 30 minutes requires a scheduled-only marker and justification")
	}
	return nil
}

func validateWorkflowCompute(plan WorkflowComputePlan) error {
	if plan.PRRunnerMinutes <= 0 || plan.PRRunnerMinutes > workflowMaxPRRunnerMinutes {
		return validationError(CodeWorkflowResourcePolicy, "", "", "PR runner-minutes must be positive and at most 250")
	}
	if plan.Chunk.LiveJobs <= 0 || plan.Chunk.LiveJobs > MaxLiveJobsPerChunk {
		return validationError(CodeWorkflowResourcePolicy, "", "", "chunk live jobs must be positive and at most 64")
	}
	if plan.Chunk.RunnerMinutes <= 0 || plan.Chunk.RunnerMinutes > workflowMaxChunkRunnerMinutes {
		return validationError(CodeWorkflowResourcePolicy, "", "", "chunk runner-minutes must be positive and at most 2880")
	}
	if plan.Chunk.MaxParallel <= 0 || plan.Chunk.MaxParallel > workflowMaxChunkParallel {
		return validationError(CodeWorkflowResourcePolicy, "", "", "chunk max-parallel must be positive and at most 8")
	}
	if plan.Release.Chunks <= 0 || plan.Release.Chunks > MaxReleaseChunks {
		return validationError(CodeWorkflowResourcePolicy, "", "", "release chunks must be positive and at most 6")
	}
	if plan.Release.RunnerMinutes <= 0 || plan.Release.RunnerMinutes > workflowMaxReleaseRunnerMinutes {
		return validationError(CodeWorkflowResourcePolicy, "", "", "release runner-minutes must be positive and at most 17280")
	}
	return nil
}

func validateWorkflowArtifacts(artifacts WorkflowArtifactPlan, storage WorkflowStorageSnapshot) error {
	if artifacts.ChunkUploadedBytes <= 0 || artifacts.ChunkUploadedBytes > workflowMaxChunkUploadedBytes {
		return validationError(CodeWorkflowArtifactPolicy, "", "", "chunk uploaded bytes must be positive and at most 100 MiB")
	}
	if artifacts.EvidenceRecordBytes <= 0 || artifacts.EvidenceRecordBytes > workflowMaxEvidenceRecordBytes {
		return validationError(CodeWorkflowArtifactPolicy, "", "", "evidence record bytes must be positive and at most 64 KiB")
	}
	if artifacts.FailureDiagnosticsBytesPerModule <= 0 || artifacts.FailureDiagnosticsBytesPerModule > workflowMaxFailureDiagnosticsBytesPerModule {
		return validationError(CodeWorkflowArtifactPolicy, "", "", "failure diagnostics bytes per module must be positive and at most 1 MiB")
	}
	if artifacts.EngineRetention <= 0 || artifacts.EngineRetention > workflowMaxEngineRetention {
		return validationError(CodeWorkflowArtifactPolicy, "", "", "engine artifact retention must be positive and at most one day")
	}
	if artifacts.PREvidenceRetention <= 0 || artifacts.PREvidenceRetention > workflowMaxPREvidenceRetention {
		return validationError(CodeWorkflowArtifactPolicy, "", "", "PR evidence retention must be positive and at most seven days")
	}
	if artifacts.ScheduledEvidenceRetention != workflowScheduledEvidenceRetention {
		return validationError(CodeWorkflowArtifactPolicy, "", "", "scheduled evidence retention must be exactly 90 days")
	}
	if artifacts.DiagnosticRetention <= 0 || artifacts.DiagnosticRetention > workflowMaxDiagnosticRetention {
		return validationError(CodeWorkflowArtifactPolicy, "", "", "diagnostic retention must be positive and at most three days")
	}
	return validateWorkflowStorage(storage)
}

func validateWorkflowStorage(storage WorkflowStorageSnapshot) error {
	if storage.RetainedBytes < 0 || storage.ReservedBytes < 0 {
		return validationError(CodeWorkflowArtifactPolicy, "", "", "retained and reserved bytes cannot be negative")
	}
	if storage.IncludedStorageBytes <= 0 {
		return validationError(CodeWorkflowArtifactPolicy, "", "", "included storage bytes must be positive")
	}
	if storage.RetainedBytes >= workflowMaxValidationOwnedBytes ||
		storage.ReservedBytes >= workflowMaxValidationOwnedBytes ||
		storage.RetainedBytes > workflowMaxValidationOwnedBytes-1-storage.ReservedBytes {
		return validationError(CodeWorkflowArtifactPolicy, "", "", "validation-owned retained bytes plus reservations must stay below 1 GiB")
	}
	ownedBytes := storage.RetainedBytes + storage.ReservedBytes
	if ownedBytes > storage.IncludedStorageBytes {
		return validationError(CodeWorkflowArtifactPolicy, "", "", "validation-owned bytes exceed included storage")
	}
	requiredHeadroom := storage.IncludedStorageBytes / 4
	if storage.IncludedStorageBytes%4 != 0 {
		requiredHeadroom++
	}
	if storage.IncludedStorageBytes-ownedBytes < requiredHeadroom {
		return validationError(CodeWorkflowArtifactPolicy, "", "", "included storage headroom must be at least 25 percent")
	}
	return nil
}

func validateWorkflowTrust(plan WorkflowTrustPlan) error {
	if len(plan.RunnerLabels) != 1 || !standardWindowsRunnerLabel(plan.RunnerLabels[0]) {
		return validationError(CodeWorkflowRunnerPolicy, "", "", "runner labels must contain exactly one standard Windows hosted label")
	}
	for _, action := range plan.ActionUses {
		if strings.HasPrefix(action, "./") {
			continue
		}
		if !immutableRemoteActionPattern.MatchString(action) {
			return validationError(CodeWorkflowActionPolicy, "", "", "action %q must be repository-local or pinned to a full lowercase 40-hex commit SHA", action)
		}
	}
	return nil
}

func standardWindowsRunnerLabel(label string) bool {
	switch label {
	case "windows-latest", "windows-2025", "windows-2022":
		return true
	default:
		return false
	}
}
