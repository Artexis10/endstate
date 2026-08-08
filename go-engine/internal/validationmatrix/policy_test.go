// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationmatrix

import (
	"strings"
	"testing"
	"time"
)

const (
	testMiB = int64(1024 * 1024)
	testGiB = int64(1024 * 1024 * 1024)
)

func TestCanonicalWorkflowResourcePlanIsValidAndFresh(t *testing.T) {
	first := CanonicalWorkflowResourcePlan()
	if err := ValidateWorkflowResourcePlan(first); err != nil {
		t.Fatalf("ValidateWorkflowResourcePlan(CanonicalWorkflowResourcePlan()) error = %v", err)
	}

	first.Trust.RunnerLabels[0] = "self-hosted"
	first.Trust.ActionUses[0] = "actions/checkout@main"
	second := CanonicalWorkflowResourcePlan()
	if got := second.Trust.RunnerLabels[0]; got != "windows-latest" {
		t.Fatalf("canonical runner label after caller mutation = %q, want windows-latest", got)
	}
	if got := second.Trust.ActionUses[0]; got != "./.github/actions/verified-module-matrix" {
		t.Fatalf("canonical action use after caller mutation = %q, want repository-local action", got)
	}
	if err := ValidateWorkflowResourcePlan(second); err != nil {
		t.Fatalf("fresh canonical plan error = %v", err)
	}
}

func TestWorkflowTimeoutPolicyBoundaries(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*WorkflowResourcePlan)
		wantErr bool
	}{
		{name: "caps accepted", mutate: func(*WorkflowResourcePlan) {}},
		{name: "synthetic shard above cap", mutate: func(plan *WorkflowResourcePlan) { plan.Timeouts.SyntheticShard = 15*time.Minute + time.Nanosecond }, wantErr: true},
		{name: "synthetic shard zero", mutate: func(plan *WorkflowResourcePlan) { plan.Timeouts.SyntheticShard = 0 }, wantErr: true},
		{name: "pr live above cap", mutate: func(plan *WorkflowResourcePlan) { plan.Timeouts.PRLive = 25*time.Minute + time.Nanosecond }, wantErr: true},
		{name: "pr live zero", mutate: func(plan *WorkflowResourcePlan) { plan.Timeouts.PRLive = 0 }, wantErr: true},
		{name: "pr critical path above cap", mutate: func(plan *WorkflowResourcePlan) { plan.Timeouts.PRCriticalPath = 40*time.Minute + time.Nanosecond }, wantErr: true},
		{name: "pr critical path zero", mutate: func(plan *WorkflowResourcePlan) { plan.Timeouts.PRCriticalPath = 0 }, wantErr: true},
		{name: "scheduled default accepted", mutate: func(plan *WorkflowResourcePlan) { plan.Timeouts.ScheduledLive.Duration = 30 * time.Minute }},
		{name: "scheduled extended accepted at 31", mutate: func(plan *WorkflowResourcePlan) {
			plan.Timeouts.ScheduledLive = ScheduledLiveTimeout{Duration: 31 * time.Minute, ScheduledOnly: true, Justification: "module needs a longer cold install"}
		}},
		{name: "scheduled extended accepted at cap", mutate: func(plan *WorkflowResourcePlan) {
			plan.Timeouts.ScheduledLive = ScheduledLiveTimeout{Duration: 45 * time.Minute, ScheduledOnly: true, Justification: "module needs a longer cold install"}
		}},
		{name: "scheduled extended missing marker", mutate: func(plan *WorkflowResourcePlan) {
			plan.Timeouts.ScheduledLive = ScheduledLiveTimeout{Duration: 31 * time.Minute, Justification: "slow install"}
		}, wantErr: true},
		{name: "scheduled extended missing justification", mutate: func(plan *WorkflowResourcePlan) {
			plan.Timeouts.ScheduledLive = ScheduledLiveTimeout{Duration: 31 * time.Minute, ScheduledOnly: true, Justification: "  "}
		}, wantErr: true},
		{name: "scheduled above cap", mutate: func(plan *WorkflowResourcePlan) {
			plan.Timeouts.ScheduledLive = ScheduledLiveTimeout{Duration: 46 * time.Minute, ScheduledOnly: true, Justification: "slow install"}
		}, wantErr: true},
		{name: "scheduled zero", mutate: func(plan *WorkflowResourcePlan) { plan.Timeouts.ScheduledLive.Duration = 0 }, wantErr: true},
		{name: "pr cannot consume extended allowance", mutate: func(plan *WorkflowResourcePlan) {
			plan.Timeouts.PRLive = 31 * time.Minute
			plan.Timeouts.ScheduledLive = ScheduledLiveTimeout{Duration: 31 * time.Minute, ScheduledOnly: true, Justification: "scheduled only"}
		}, wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			plan := CanonicalWorkflowResourcePlan()
			test.mutate(&plan)
			err := ValidateWorkflowResourcePlan(plan)
			if (err != nil) != test.wantErr {
				t.Fatalf("ValidateWorkflowResourcePlan() error = %v, wantErr %v", err, test.wantErr)
			}
			if test.wantErr && ErrorCode(err) != CodeWorkflowTimeoutPolicy {
				t.Fatalf("timeout error code = %q, want %q (error: %v)", ErrorCode(err), CodeWorkflowTimeoutPolicy, err)
			}
		})
	}
}

func TestWorkflowComputePolicyBoundaries(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*WorkflowResourcePlan)
	}{
		{name: "pr runner minutes above cap", mutate: func(plan *WorkflowResourcePlan) { plan.Compute.PRRunnerMinutes = 251 }},
		{name: "pr runner minutes zero", mutate: func(plan *WorkflowResourcePlan) { plan.Compute.PRRunnerMinutes = 0 }},
		{name: "chunk jobs above cap", mutate: func(plan *WorkflowResourcePlan) { plan.Compute.Chunk.LiveJobs = 65 }},
		{name: "chunk jobs zero", mutate: func(plan *WorkflowResourcePlan) { plan.Compute.Chunk.LiveJobs = 0 }},
		{name: "chunk runner minutes above cap", mutate: func(plan *WorkflowResourcePlan) { plan.Compute.Chunk.RunnerMinutes = 2881 }},
		{name: "chunk runner minutes zero", mutate: func(plan *WorkflowResourcePlan) { plan.Compute.Chunk.RunnerMinutes = 0 }},
		{name: "chunk max parallel above cap", mutate: func(plan *WorkflowResourcePlan) { plan.Compute.Chunk.MaxParallel = 9 }},
		{name: "chunk max parallel zero", mutate: func(plan *WorkflowResourcePlan) { plan.Compute.Chunk.MaxParallel = 0 }},
		{name: "release chunks above cap", mutate: func(plan *WorkflowResourcePlan) { plan.Compute.Release.Chunks = 7 }},
		{name: "release chunks zero", mutate: func(plan *WorkflowResourcePlan) { plan.Compute.Release.Chunks = 0 }},
		{name: "release runner minutes above cap", mutate: func(plan *WorkflowResourcePlan) { plan.Compute.Release.RunnerMinutes = 17281 }},
		{name: "release runner minutes zero", mutate: func(plan *WorkflowResourcePlan) { plan.Compute.Release.RunnerMinutes = 0 }},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			plan := CanonicalWorkflowResourcePlan()
			test.mutate(&plan)
			err := ValidateWorkflowResourcePlan(plan)
			if err == nil {
				t.Fatal("ValidateWorkflowResourcePlan() error = nil, want resource violation")
			}
			if got := ErrorCode(err); got != CodeWorkflowResourcePolicy {
				t.Fatalf("resource error code = %q, want %q (error: %v)", got, CodeWorkflowResourcePolicy, err)
			}
		})
	}
}

func TestWorkflowArtifactPolicyBoundaries(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*WorkflowResourcePlan)
	}{
		{name: "chunk uploads above cap", mutate: func(plan *WorkflowResourcePlan) { plan.Artifacts.ChunkUploadedBytes = 100*testMiB + 1 }},
		{name: "chunk uploads zero", mutate: func(plan *WorkflowResourcePlan) { plan.Artifacts.ChunkUploadedBytes = 0 }},
		{name: "evidence record above cap", mutate: func(plan *WorkflowResourcePlan) { plan.Artifacts.EvidenceRecordBytes = 64*1024 + 1 }},
		{name: "evidence record zero", mutate: func(plan *WorkflowResourcePlan) { plan.Artifacts.EvidenceRecordBytes = 0 }},
		{name: "diagnostics above cap", mutate: func(plan *WorkflowResourcePlan) { plan.Artifacts.FailureDiagnosticsBytesPerModule = testMiB + 1 }},
		{name: "diagnostics zero", mutate: func(plan *WorkflowResourcePlan) { plan.Artifacts.FailureDiagnosticsBytesPerModule = 0 }},
		{name: "engine retention above cap", mutate: func(plan *WorkflowResourcePlan) { plan.Artifacts.EngineRetention = 24*time.Hour + time.Nanosecond }},
		{name: "engine retention zero", mutate: func(plan *WorkflowResourcePlan) { plan.Artifacts.EngineRetention = 0 }},
		{name: "pr evidence retention above cap", mutate: func(plan *WorkflowResourcePlan) {
			plan.Artifacts.PREvidenceRetention = 7*24*time.Hour + time.Nanosecond
		}},
		{name: "pr evidence retention zero", mutate: func(plan *WorkflowResourcePlan) { plan.Artifacts.PREvidenceRetention = 0 }},
		{name: "scheduled evidence retention below exact", mutate: func(plan *WorkflowResourcePlan) {
			plan.Artifacts.ScheduledEvidenceRetention = 90*24*time.Hour - time.Nanosecond
		}},
		{name: "scheduled evidence retention above exact", mutate: func(plan *WorkflowResourcePlan) {
			plan.Artifacts.ScheduledEvidenceRetention = 90*24*time.Hour + time.Nanosecond
		}},
		{name: "scheduled evidence retention zero", mutate: func(plan *WorkflowResourcePlan) { plan.Artifacts.ScheduledEvidenceRetention = 0 }},
		{name: "diagnostic retention above cap", mutate: func(plan *WorkflowResourcePlan) {
			plan.Artifacts.DiagnosticRetention = 3*24*time.Hour + time.Nanosecond
		}},
		{name: "diagnostic retention zero", mutate: func(plan *WorkflowResourcePlan) { plan.Artifacts.DiagnosticRetention = 0 }},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			plan := CanonicalWorkflowResourcePlan()
			test.mutate(&plan)
			err := ValidateWorkflowResourcePlan(plan)
			if err == nil {
				t.Fatal("ValidateWorkflowResourcePlan() error = nil, want artifact violation")
			}
			if got := ErrorCode(err); got != CodeWorkflowArtifactPolicy {
				t.Fatalf("artifact error code = %q, want %q (error: %v)", got, CodeWorkflowArtifactPolicy, err)
			}
		})
	}
}

func TestWorkflowStoragePolicyStrictBoundaries(t *testing.T) {
	tests := []struct {
		name    string
		storage WorkflowStorageSnapshot
		wantErr bool
	}{
		{name: "zero retained and reserved accepted", storage: WorkflowStorageSnapshot{IncludedStorageBytes: testGiB}},
		{name: "one byte below repository cap accepted", storage: WorkflowStorageSnapshot{RetainedBytes: testGiB - 2, ReservedBytes: 1, IncludedStorageBytes: 2 * testGiB}},
		{name: "repository cap equality rejected", storage: WorkflowStorageSnapshot{RetainedBytes: testGiB - 1, ReservedBytes: 1, IncludedStorageBytes: 2 * testGiB}, wantErr: true},
		{name: "exact 25 percent headroom accepted", storage: WorkflowStorageSnapshot{RetainedBytes: 3 * testGiB / 4, IncludedStorageBytes: testGiB}},
		{name: "one byte below 25 percent headroom rejected", storage: WorkflowStorageSnapshot{RetainedBytes: 3*testGiB/4 + 1, IncludedStorageBytes: testGiB}, wantErr: true},
		{name: "negative retained rejected", storage: WorkflowStorageSnapshot{RetainedBytes: -1, IncludedStorageBytes: testGiB}, wantErr: true},
		{name: "negative reserved rejected", storage: WorkflowStorageSnapshot{ReservedBytes: -1, IncludedStorageBytes: testGiB}, wantErr: true},
		{name: "included storage zero rejected", storage: WorkflowStorageSnapshot{}, wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			plan := CanonicalWorkflowResourcePlan()
			plan.Storage = test.storage
			err := ValidateWorkflowResourcePlan(plan)
			if (err != nil) != test.wantErr {
				t.Fatalf("ValidateWorkflowResourcePlan() error = %v, wantErr %v", err, test.wantErr)
			}
			if test.wantErr && ErrorCode(err) != CodeWorkflowArtifactPolicy {
				t.Fatalf("storage error code = %q, want %q (error: %v)", ErrorCode(err), CodeWorkflowArtifactPolicy, err)
			}
		})
	}
}

func TestWorkflowRunnerLabelPolicy(t *testing.T) {
	for _, label := range []string{"windows-latest", "windows-2025", "windows-2022"} {
		t.Run("accepts "+label, func(t *testing.T) {
			plan := CanonicalWorkflowResourcePlan()
			plan.Trust.RunnerLabels = []string{label}
			if err := ValidateWorkflowResourcePlan(plan); err != nil {
				t.Fatalf("ValidateWorkflowResourcePlan() error = %v", err)
			}
		})
	}

	for _, labels := range [][]string{
		nil,
		{""},
		{"self-hosted"},
		{"windows-latest-8-core"},
		{"ubuntu-latest"},
		{"windows-latest", "windows-2022"},
		{"windows-latest", "self-hosted"},
	} {
		t.Run("rejects "+strings.Join(labels, "+"), func(t *testing.T) {
			plan := CanonicalWorkflowResourcePlan()
			plan.Trust.RunnerLabels = labels
			err := ValidateWorkflowResourcePlan(plan)
			if err == nil {
				t.Fatal("ValidateWorkflowResourcePlan() error = nil, want runner violation")
			}
			if got := ErrorCode(err); got != CodeWorkflowRunnerPolicy {
				t.Fatalf("runner error code = %q, want %q (error: %v)", got, CodeWorkflowRunnerPolicy, err)
			}
		})
	}
}

func TestWorkflowActionPinPolicy(t *testing.T) {
	valid := []string{
		"./.github/actions/verified-module-matrix",
		"actions/checkout@0123456789abcdef0123456789abcdef01234567",
	}
	for _, action := range valid {
		t.Run("accepts "+action, func(t *testing.T) {
			plan := CanonicalWorkflowResourcePlan()
			plan.Trust.ActionUses = []string{action}
			if err := ValidateWorkflowResourcePlan(plan); err != nil {
				t.Fatalf("ValidateWorkflowResourcePlan() error = %v", err)
			}
		})
	}

	invalid := []string{
		"",
		"actions/checkout",
		"actions/checkout@main",
		"actions/checkout@v4",
		"actions/checkout@0123456",
		"actions/checkout@0123456789ABCDEF0123456789ABCDEF01234567",
		"checkout@0123456789abcdef0123456789abcdef01234567",
		"owner/repo/extra@0123456789abcdef0123456789abcdef01234567",
		"owner/repo@0123456789abcdef0123456789abcdef0123456g",
	}
	for _, action := range invalid {
		t.Run("rejects "+action, func(t *testing.T) {
			plan := CanonicalWorkflowResourcePlan()
			plan.Trust.ActionUses = []string{action}
			err := ValidateWorkflowResourcePlan(plan)
			if err == nil {
				t.Fatal("ValidateWorkflowResourcePlan() error = nil, want action violation")
			}
			if got := ErrorCode(err); got != CodeWorkflowActionPolicy {
				t.Fatalf("action error code = %q, want %q (error: %v)", got, CodeWorkflowActionPolicy, err)
			}
		})
	}
}

func TestWorkflowPolicyErrorsAreDeterministic(t *testing.T) {
	plan := CanonicalWorkflowResourcePlan()
	plan.Compute.Chunk.LiveJobs = 65

	first := ValidateWorkflowResourcePlan(plan)
	second := ValidateWorkflowResourcePlan(plan)
	if first == nil || second == nil {
		t.Fatalf("ValidateWorkflowResourcePlan() errors = (%v, %v), want two errors", first, second)
	}
	if first.Error() != second.Error() || ErrorCode(first) != ErrorCode(second) {
		t.Fatalf("policy errors are not deterministic: first=(%q, %q), second=(%q, %q)", first.Error(), ErrorCode(first), second.Error(), ErrorCode(second))
	}
}
