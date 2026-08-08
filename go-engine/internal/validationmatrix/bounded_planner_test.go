// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationmatrix

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

const testEngineCommit40 = "0123456789abcdef0123456789abcdef01234567"

func TestPlanHostedCatalogIsDeterministicChunkedAndSnapshotted(t *testing.T) {
	records := hostedPlannerRecords(130)
	forward := plannerCatalog(records...)
	reversed := plannerCatalog(reverseRecords(records)...)

	want, err := PlanHostedCatalog(forward)
	if err != nil {
		t.Fatalf("PlanHostedCatalog(forward) returned %v", err)
	}
	got, err := PlanHostedCatalog(reversed)
	if err != nil {
		t.Fatalf("PlanHostedCatalog(reversed) returned %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("PlanHostedCatalog is input-order dependent:\nforward: %#v\nreversed: %#v", want, got)
	}
	if got.HostedCount != 130 || len(got.Jobs) != 130 {
		t.Fatalf("hosted count/jobs = %d/%d, want 130/130", got.HostedCount, len(got.Jobs))
	}
	assertLiveRowsSorted(t, got.Jobs)
	assertLiveChunkSizes(t, got.Chunks, []int{64, 64, 2})
	for _, row := range got.Jobs {
		if row.Status != PlanStatusRequired || row.Reason != ReasonTrustedMainHosted || row.PolicySource != PolicySourceTrustedMain {
			t.Fatalf("hosted row status/reason/source = %q/%q/%q", row.Status, row.Reason, row.PolicySource)
		}
	}

	record := forward.Records["apps.module-000"]
	record.ModuleRevision = strings.Repeat("f", 64)
	record.Live.Ref = "Vendor.Mutated"
	record.Live.Trust.SeedSHA256 = strings.Repeat("e", 64)
	record.Quarantines[0].Owner = "mutated-owner"
	forward.Records[record.ModuleID] = record

	row := want.Jobs[0]
	if row.ModuleRevision == record.ModuleRevision || row.Policy.Ref == record.Live.Ref ||
		row.Policy.Trust.SeedSHA256 == record.Live.Trust.SeedSHA256 || row.Quarantines[0].Owner == record.Quarantines[0].Owner {
		t.Fatalf("planned hosted row aliases input record: %#v", row)
	}

	empty, err := PlanHostedCatalog(plannerCatalog())
	if err != nil {
		t.Fatal(err)
	}
	if empty.HostedCount != 0 || len(empty.Jobs) != 0 || len(empty.Chunks) != 0 {
		t.Fatalf("empty hosted plan = %#v, want no jobs or chunks", empty)
	}
}

func TestPlanScheduledOrdersByAttemptBucketThenFailureStalenessAndID(t *testing.T) {
	records := []ValidationRecord{
		plannerLiveRecord("apps.never-normal", plannerHostedInstallPolicy("Vendor.NeverNormal")),
		plannerLiveRecord("apps.same-day-normal", plannerHostedInstallPolicy("Vendor.SameNormal")),
		plannerLiveRecord("apps.never-stale", plannerHostedInstallPolicy("Vendor.NeverStale")),
		plannerLiveRecord("apps.oldest", plannerHostedInstallPolicy("Vendor.Oldest")),
		plannerLiveRecord("apps.same-day-stale", plannerHostedInstallPolicy("Vendor.SameStale")),
		plannerLiveRecord("apps.never-failing", plannerHostedInstallPolicy("Vendor.NeverFailing")),
		plannerLiveRecord("apps.same-day-failing", plannerHostedInstallPolicy("Vendor.SameFailing")),
		plannerLiveRecord("apps.not-hosted", plannerNonHostedPolicy(LiveManual)),
	}
	oldest := time.Date(2026, 7, 19, 23, 59, 0, 0, time.FixedZone("east", 3*60*60))
	sameDayEarly := time.Date(2026, 7, 21, 0, 1, 0, 0, time.UTC)
	sameDayLate := time.Date(2026, 7, 21, 23, 59, 0, 0, time.UTC)
	unknown := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	states := []ScheduledModuleState{
		{ModuleID: "apps.same-day-normal", LastAttempt: &sameDayEarly},
		{ModuleID: "apps.never-stale", Stale: true},
		{ModuleID: "apps.oldest", LastAttempt: &oldest},
		{ModuleID: "apps.same-day-stale", LastAttempt: &sameDayLate, Stale: true},
		{ModuleID: "apps.never-failing", Failing: true},
		{ModuleID: "apps.same-day-failing", LastAttempt: &sameDayLate, Failing: true},
		{ModuleID: "apps.unknown", LastAttempt: &unknown, Failing: true, Stale: true},
		{ModuleID: "apps.not-hosted", LastAttempt: &unknown, Failing: true, Stale: true},
	}

	plan, err := PlanScheduled(plannerCatalog(records...), states)
	if err != nil {
		t.Fatalf("PlanScheduled returned %v", err)
	}
	want := []string{
		"apps.never-failing", "apps.never-stale", "apps.never-normal",
		"apps.oldest", "apps.same-day-failing", "apps.same-day-stale", "apps.same-day-normal",
	}
	if got := liveModuleIDs(plan.Jobs); !reflect.DeepEqual(got, want) {
		t.Fatalf("scheduled order = %v, want %v", got, want)
	}
	for index, row := range plan.Jobs {
		wantReason := ReasonScheduledOldestAttemptDay
		if index < 3 {
			wantReason = ReasonScheduledNeverAttempted
		}
		if row.Status != PlanStatusRequired || row.Reason != wantReason || row.PolicySource != PolicySourceTrustedMain {
			t.Fatalf("scheduled row %q status/reason/source = %q/%q/%q", row.ModuleID, row.Status, row.Reason, row.PolicySource)
		}
	}
}

func TestPlanScheduledRejectsDuplicateHostedStateAndIgnoresUnknownState(t *testing.T) {
	catalog := plannerCatalog(plannerLiveRecord("apps.fixture", plannerHostedInstallPolicy("Vendor.Fixture")))
	when := time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC)

	_, err := PlanScheduled(catalog, []ScheduledModuleState{
		{ModuleID: "apps.fixture", LastAttempt: &when},
		{ModuleID: "apps.fixture"},
	})
	if got := ErrorCode(err); got != CodeDuplicateScheduledState {
		t.Fatalf("duplicate state error = %v (code %q), want %q", err, got, CodeDuplicateScheduledState)
	}

	plan, err := PlanScheduled(catalog, []ScheduledModuleState{
		{ModuleID: "apps.unknown", LastAttempt: &when},
		{ModuleID: "apps.unknown"},
	})
	if err != nil {
		t.Fatalf("unknown state should be ignored, got %v", err)
	}
	if got := liveModuleIDs(plan.Jobs); !reflect.DeepEqual(got, []string{"apps.fixture"}) {
		t.Fatalf("scheduled jobs = %v, want missing hosted state treated as never attempted", got)
	}
}

func TestPlanScheduledCovers448HostedModulesInSevenDailyRuns(t *testing.T) {
	catalog := plannerCatalog(hostedPlannerRecords(448)...)
	states := make([]ScheduledModuleState, 448)
	for index := range states {
		states[index] = ScheduledModuleState{
			ModuleID: fmt.Sprintf("apps.module-%03d", index),
			Failing:  index%2 == 0,
			Stale:    index%3 == 0,
		}
	}
	seen := make(map[string]struct{}, 448)
	for day := 0; day < 7; day++ {
		plan, err := PlanScheduled(catalog, states)
		if err != nil {
			t.Fatalf("day %d PlanScheduled returned %v", day, err)
		}
		if len(plan.Jobs) != MaxLiveJobsPerChunk {
			t.Fatalf("day %d selected %d jobs, want %d", day, len(plan.Jobs), MaxLiveJobsPerChunk)
		}
		attempt := time.Date(2026, 7, 1+day, 12, 0, 0, 0, time.UTC)
		selected := make(map[string]struct{}, len(plan.Jobs))
		for _, row := range plan.Jobs {
			seen[row.ModuleID] = struct{}{}
			selected[row.ModuleID] = struct{}{}
		}
		for index := range states {
			if _, ok := selected[states[index].ModuleID]; ok {
				updated := attempt
				states[index].LastAttempt = &updated
			}
		}
	}
	if len(seen) != 448 {
		t.Fatalf("seven daily runs covered %d modules, want all 448", len(seen))
	}
}

func TestPlanScheduledRejectsCatalogBeyondSevenDayCapacity(t *testing.T) {
	_, err := PlanScheduled(plannerCatalog(hostedPlannerRecords(449)...), nil)
	if got := ErrorCode(err); got != CodeScheduledCapacityExceeded {
		t.Fatalf("449-hosted scheduled error = %v (code %q), want %q", err, got, CodeScheduledCapacityExceeded)
	}
}

func TestPlanDispatchSelectsExactHostedIDs(t *testing.T) {
	catalog := plannerCatalog(
		plannerLiveRecord("apps.alpha", plannerHostedInstallPolicy("Vendor.Alpha")),
		plannerLiveRecord("apps.bravo", plannerHostedInstallPolicy("Vendor.Bravo")),
		plannerLiveRecord("apps.charlie", plannerHostedInstallPolicy("Vendor.Charlie")),
		plannerLiveRecord("apps.manual", plannerNonHostedPolicy(LiveManual)),
	)
	plan, err := PlanDispatch(catalog, DispatchPlanOptions{
		EngineCommit: testEngineCommit40,
		ModuleIDs:    []string{"apps.charlie", "apps.alpha"},
	})
	if err != nil {
		t.Fatalf("PlanDispatch returned %v", err)
	}
	if plan.EngineCommit != testEngineCommit40 || plan.Selection != DispatchSelectionExplicitModuleIDs {
		t.Fatalf("dispatch identity/selection = %q/%q", plan.EngineCommit, plan.Selection)
	}
	if got := liveModuleIDs(plan.Jobs); !reflect.DeepEqual(got, []string{"apps.alpha", "apps.charlie"}) {
		t.Fatalf("dispatch jobs = %v", got)
	}
	for _, row := range plan.Jobs {
		if row.Status != PlanStatusRequired || row.Reason != ReasonDispatchExplicitIDs || row.PolicySource != PolicySourceTrustedMain {
			t.Fatalf("dispatch row = %#v", row)
		}
	}
}

func TestPlanDispatchRejectsInvalidExplicitSelections(t *testing.T) {
	catalog := plannerCatalog(
		plannerLiveRecord("apps.alpha", plannerHostedInstallPolicy("Vendor.Alpha")),
		plannerLiveRecord("apps.manual", plannerNonHostedPolicy(LiveManual)),
	)
	tests := []struct {
		name      string
		moduleIDs []string
		wantCode  string
	}{
		{name: "empty", moduleIDs: []string{}, wantCode: CodeInvalidDispatchSelection},
		{name: "duplicate", moduleIDs: []string{"apps.alpha", "apps.alpha"}, wantCode: CodeDuplicateDispatchModule},
		{name: "unknown", moduleIDs: []string{"apps.unknown"}, wantCode: CodeUnknownDispatchModule},
		{name: "non-hosted", moduleIDs: []string{"apps.manual"}, wantCode: CodeNonHostedDispatchModule},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := PlanDispatch(catalog, DispatchPlanOptions{EngineCommit: testEngineCommit40, ModuleIDs: test.moduleIDs})
			if got := ErrorCode(err); got != test.wantCode {
				t.Fatalf("PlanDispatch error = %v (code %q), want %q", err, got, test.wantCode)
			}
		})
	}

	ids := make([]string, 129)
	records := hostedPlannerRecords(129)
	for index := range ids {
		ids[index] = records[index].ModuleID
	}
	_, err := PlanDispatch(plannerCatalog(records...), DispatchPlanOptions{EngineCommit: testEngineCommit40, ModuleIDs: ids})
	if got := ErrorCode(err); got != CodeDispatchCapacityExceeded {
		t.Fatalf("oversized dispatch error = %v (code %q), want %q", err, got, CodeDispatchCapacityExceeded)
	}
	var capacityError *DispatchCapacityError
	if !errors.As(err, &capacityError) {
		t.Fatalf("oversized dispatch error type = %T, want *DispatchCapacityError", err)
	}
	if want := []int{1, 2}; !reflect.DeepEqual(capacityError.RemainingChunkIndices, want) {
		t.Fatalf("remaining chunk indices = %v, want %v", capacityError.RemainingChunkIndices, want)
	}
}

func TestPlanDispatchSelectsExactlyOneDeterministicChunk(t *testing.T) {
	records := hostedPlannerRecords(65)
	catalog := plannerCatalog(reverseRecords(records)...)
	chunkIndex := 1
	plan, err := PlanDispatch(catalog, DispatchPlanOptions{EngineCommit: testEngineCommit40, ChunkIndex: &chunkIndex})
	if err != nil {
		t.Fatalf("PlanDispatch returned %v", err)
	}
	if plan.Selection != DispatchSelectionChunkIndex || plan.ChunkIndex == nil || *plan.ChunkIndex != 1 {
		t.Fatalf("dispatch chunk selection = %#v", plan)
	}
	if got := liveModuleIDs(plan.Jobs); !reflect.DeepEqual(got, []string{"apps.module-064"}) {
		t.Fatalf("chunk-1 jobs = %v, want only the second deterministic chunk", got)
	}
	if plan.Jobs[0].Reason != ReasonDispatchChunkIndex {
		t.Fatalf("chunk row reason = %q, want %q", plan.Jobs[0].Reason, ReasonDispatchChunkIndex)
	}
}

func TestPlanDispatchRejectsBothNeitherAndInvalidChunk(t *testing.T) {
	catalog := plannerCatalog(hostedPlannerRecords(65)...)
	zero := 0
	negative := -1
	outOfRange := 2
	tests := []struct {
		name     string
		options  DispatchPlanOptions
		wantCode string
	}{
		{name: "neither", options: DispatchPlanOptions{EngineCommit: testEngineCommit40}, wantCode: CodeInvalidDispatchSelection},
		{name: "both", options: DispatchPlanOptions{EngineCommit: testEngineCommit40, ModuleIDs: []string{"apps.module-000"}, ChunkIndex: &zero}, wantCode: CodeInvalidDispatchSelection},
		{name: "negative chunk", options: DispatchPlanOptions{EngineCommit: testEngineCommit40, ChunkIndex: &negative}, wantCode: CodeInvalidDispatchChunk},
		{name: "out of range chunk", options: DispatchPlanOptions{EngineCommit: testEngineCommit40, ChunkIndex: &outOfRange}, wantCode: CodeInvalidDispatchChunk},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := PlanDispatch(catalog, test.options)
			if got := ErrorCode(err); got != test.wantCode {
				t.Fatalf("PlanDispatch error = %v (code %q), want %q", err, got, test.wantCode)
			}
		})
	}
}

func TestDispatchAndReleaseRequireExactEngineCommit(t *testing.T) {
	catalog := plannerCatalog(plannerLiveRecord("apps.alpha", plannerHostedInstallPolicy("Vendor.Alpha")))
	ids := []string{"apps.alpha"}
	invalid := []string{
		"",
		strings.Repeat("a", 39),
		strings.Repeat("a", 41),
		strings.Repeat("a", 63),
		strings.Repeat("a", 65),
		strings.Repeat("A", 40),
		strings.Repeat("g", 40),
		" " + strings.Repeat("a", 40),
	}
	for _, commit := range invalid {
		_, err := PlanDispatch(catalog, DispatchPlanOptions{EngineCommit: commit, ModuleIDs: ids})
		if got := ErrorCode(err); got != CodeInvalidEngineCommit {
			t.Fatalf("dispatch commit %q error = %v (code %q), want %q", commit, err, got, CodeInvalidEngineCommit)
		}
		_, err = PlanRelease(catalog, commit)
		if got := ErrorCode(err); got != CodeInvalidEngineCommit {
			t.Fatalf("release commit %q error = %v (code %q), want %q", commit, err, got, CodeInvalidEngineCommit)
		}
	}
}

func TestPlanReleaseChunksExactCommitWithinCampaignCap(t *testing.T) {
	commit64 := strings.Repeat("a", 64)
	tests := []struct {
		hosted    int
		wantSizes []int
	}{
		{hosted: 0, wantSizes: nil},
		{hosted: 64, wantSizes: []int{64}},
		{hosted: 65, wantSizes: []int{64, 1}},
		{hosted: 384, wantSizes: []int{64, 64, 64, 64, 64, 64}},
	}
	for _, test := range tests {
		t.Run(fmt.Sprintf("hosted-%d", test.hosted), func(t *testing.T) {
			plan, err := PlanRelease(plannerCatalog(hostedPlannerRecords(test.hosted)...), commit64)
			if err != nil {
				t.Fatalf("PlanRelease returned %v", err)
			}
			if plan.EngineCommit != commit64 || plan.HostedCount != test.hosted {
				t.Fatalf("release identity/count = %q/%d", plan.EngineCommit, plan.HostedCount)
			}
			assertLiveChunkSizes(t, plan.Chunks, test.wantSizes)
			for _, chunk := range plan.Chunks {
				for _, row := range chunk.Jobs {
					if row.Status != PlanStatusRequired || row.Reason != ReasonReleaseExactCommit || row.PolicySource != PolicySourceTrustedMain {
						t.Fatalf("release row = %#v", row)
					}
				}
			}
		})
	}
}

func TestPlanReleaseRejectsCampaignBeyondSixChunks(t *testing.T) {
	_, err := PlanRelease(plannerCatalog(hostedPlannerRecords(385)...), testEngineCommit40)
	if got := ErrorCode(err); got != CodeReleaseCapacityExceeded {
		t.Fatalf("385-hosted release error = %v (code %q), want %q", err, got, CodeReleaseCapacityExceeded)
	}
}

func hostedPlannerRecords(count int) []ValidationRecord {
	records := make([]ValidationRecord, count)
	for index := range records {
		moduleID := fmt.Sprintf("apps.module-%03d", index)
		record := plannerLiveRecord(moduleID, plannerHostedConfigPolicy(fmt.Sprintf("Vendor.%03d", index)))
		record.ModuleRevision = fmt.Sprintf("%064x", index+1)
		record.Quarantines = []Quarantine{{
			ProofLevel: ProofLiveConfigRoundtrip, OS: "windows", RunnerImage: "windows-latest",
			FailureFingerprint: fmt.Sprintf("%064x", index+1000), IssueURL: fmt.Sprintf("https://example.com/issues/%d", index+1),
			ReasonCode: "known-failure", Owner: "validation-owner", ExpiresOn: "2027-01-01",
		}}
		records[index] = record
	}
	return records
}

func assertLiveRowsSorted(t *testing.T, rows []LiveRow) {
	t.Helper()
	for index := 1; index < len(rows); index++ {
		if rows[index-1].ModuleID > rows[index].ModuleID {
			t.Fatalf("live rows not sorted at %d: %q before %q", index, rows[index-1].ModuleID, rows[index].ModuleID)
		}
	}
}

func assertLiveChunkSizes(t *testing.T, chunks []LiveChunk, want []int) {
	t.Helper()
	if len(chunks) != len(want) {
		t.Fatalf("chunk count = %d, want %d (%v)", len(chunks), len(want), want)
	}
	for index, chunk := range chunks {
		if chunk.Index != index || len(chunk.Jobs) != want[index] {
			t.Fatalf("chunk %d index/size = %d/%d, want %d/%d", index, chunk.Index, len(chunk.Jobs), index, want[index])
		}
		if len(chunk.Jobs) > MaxLiveJobsPerChunk {
			t.Fatalf("chunk %d has %d jobs, exceeds %d", index, len(chunk.Jobs), MaxLiveJobsPerChunk)
		}
		assertLiveRowsSorted(t, chunk.Jobs)
	}
}
