// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationmatrix

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/modules"
)

func TestPlanSyntheticIsDeterministicCompleteAndBalanced(t *testing.T) {
	records := []ValidationRecord{
		plannerRecord("apps.charlie", plannerScenarios("zeta", "alpha")...),
		plannerRecord("apps.alpha", plannerScenarios("beta", "alpha", "gamma")...),
		plannerRecord("apps.bravo", plannerScenarios("only")...),
	}
	forward := plannerCatalog(records...)
	reversed := plannerCatalog(reverseRecords(records)...)
	for moduleID, record := range reversed.Records {
		record.Synthetic.Scenarios = reverseScenarios(record.Synthetic.Scenarios)
		reversed.Records[moduleID] = record
	}

	want, err := PlanSynthetic(forward, SyntheticPlanOptions{})
	if err != nil {
		t.Fatalf("PlanSynthetic(forward) returned %v", err)
	}
	got, err := PlanSynthetic(reversed, SyntheticPlanOptions{})
	if err != nil {
		t.Fatalf("PlanSynthetic(reversed) returned %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("PlanSynthetic is input-order dependent:\nforward: %#v\nreversed: %#v", want, got)
	}
	if got.ShardCount != DefaultSyntheticShardCount {
		t.Fatalf("ShardCount = %d, want default %d", got.ShardCount, DefaultSyntheticShardCount)
	}
	if len(got.Rows) != 6 {
		t.Fatalf("len(Rows) = %d, want every one of 6 scenarios", len(got.Rows))
	}
	assertSyntheticRowsSorted(t, got.Rows)
	assertShardBalance(t, got.Rows, got.ShardCount)
}

func TestPlanSyntheticValidatesShardBoundsAndBalancesRows(t *testing.T) {
	scenarios := make([]Scenario, 33)
	for index := range scenarios {
		scenarios[index] = plannerScenario(fmt.Sprintf("scenario-%02d", index))
	}
	catalog := plannerCatalog(plannerRecord("apps.fixture", scenarios...))

	for _, shardCount := range []int{1, 2, 7, MaxSyntheticShardCount} {
		t.Run(fmt.Sprintf("shards-%d", shardCount), func(t *testing.T) {
			plan, err := PlanSynthetic(catalog, SyntheticPlanOptions{ShardCount: shardCount})
			if err != nil {
				t.Fatalf("PlanSynthetic returned %v", err)
			}
			assertShardBalance(t, plan.Rows, shardCount)
		})
	}

	for _, shardCount := range []int{-1, MaxSyntheticShardCount + 1} {
		t.Run(fmt.Sprintf("invalid-%d", shardCount), func(t *testing.T) {
			_, err := PlanSynthetic(catalog, SyntheticPlanOptions{ShardCount: shardCount})
			if got := ErrorCode(err); got != CodeInvalidShardCount {
				t.Fatalf("PlanSynthetic error = %v (code %q), want %q", err, got, CodeInvalidShardCount)
			}
		})
	}
}

func TestPlanSyntheticSnapshotsScenarioDefinition(t *testing.T) {
	expected := &SchemaV2Expectation{CaptureID: "capture", ConfigSetID: "preferences", InstanceID: "default", GenerationID: "g1", Fingerprint: strings.Repeat("a", 64)}
	record := plannerRecord("apps.fixture", plannerScenario("default"))
	record.Synthetic.Scenarios[0].MinimumAssertions = map[string]int{AssertionContent: 1}
	record.Synthetic.Scenarios[0].Expected = expected
	catalog := plannerCatalog(record)

	plan, err := PlanSynthetic(catalog, SyntheticPlanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	record.Synthetic.Scenarios[0].MinimumAssertions[AssertionContent] = 99
	expected.GenerationID = "mutated"

	row := plan.Rows[0]
	if got := row.Scenario.MinimumAssertions[AssertionContent]; got != 1 {
		t.Fatalf("planned assertion minimum changed to %d after input mutation", got)
	}
	if got := row.Scenario.Expected.GenerationID; got != "g1" {
		t.Fatalf("planned expectation changed to %q after input mutation", got)
	}
}

func TestChangedAppModuleIDsNormalizesSlashesAndDeduplicates(t *testing.T) {
	paths := []string{
		`modules/apps/zeta/module.jsonc`,
		`modules\apps\alpha\validation.jsonc`,
		`./modules/apps/zeta/fixtures/default.json`,
		`modules/apps/alpha/seed.ps1`,
		`modules/windows/taskbar/module.jsonc`,
		`payload/apps/zeta/settings.json`,
		`nested/modules/apps/ignored/module.jsonc`,
		` modules/apps/leading-space/module.jsonc`,
		`modules/apps`,
		`modules/apps/../escape/module.jsonc`,
	}
	want := []string{"apps.alpha", "apps.zeta"}
	if got := ChangedAppModuleIDs(paths); !reflect.DeepEqual(got, want) {
		t.Fatalf("ChangedAppModuleIDs() = %v, want %v", got, want)
	}
}

func TestPlanPullRequestBindsTrustedPolicyToHeadExecutionIdentity(t *testing.T) {
	policy := plannerHostedInstallPolicy("Vendor.Base")
	baseRecord := plannerLiveRecord("apps.fixture", policy)
	headRecord := plannerLiveRecord("apps.fixture", policy)
	headRecord.ModuleRevision = strings.Repeat("2", 64)

	plan, err := PlanPullRequest(
		plannerCatalog(baseRecord), plannerCatalog(headRecord),
		[]string{"modules/apps/fixture/module.jsonc"}, PullRequestPlanOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}
	row := plan.Live.Run[0]
	if row.ModuleRevision != headRecord.ModuleRevision {
		t.Errorf("live module revision = %q, want tested head revision %q", row.ModuleRevision, headRecord.ModuleRevision)
	}
	wantDigest, err := canonicalDigest(headRecord)
	if err != nil {
		t.Fatal(err)
	}
	if row.ValidationDigest != wantDigest {
		t.Errorf("live validation digest = %q, want tested head digest %q", row.ValidationDigest, wantDigest)
	}
	if !reflect.DeepEqual(row.Policy, baseRecord.Live) || row.PolicySource != PolicySourceMergeBase {
		t.Errorf("live policy/source = %#v/%q, want trusted base policy", row.Policy, row.PolicySource)
	}
}

func TestPlanPullRequestUsesFullProofIdentityForScenarioUnion(t *testing.T) {
	baseRecord := plannerRecordWithMode("apps.fixture", "default", ScenarioConfigRoundtripV1)
	headRecord := plannerRecordWithMode("apps.fixture", "default", ScenarioConfigRoundtripV1)
	headRecord.Synthetic.Scenarios[0].TimeoutSeconds++
	base := plannerCatalog(baseRecord)
	head := plannerCatalog(headRecord)

	plan, err := PlanPullRequest(base, head, []string{"modules/apps/fixture/validation.jsonc"}, PullRequestPlanOptions{ShardCount: 2})
	if err != nil {
		t.Fatalf("PlanPullRequest returned %v", err)
	}
	if len(plan.Synthetic.Rows) != 2 {
		t.Fatalf("synthetic rows = %#v, want both changed full proof identities", plan.Synthetic.Rows)
	}
	if plan.Synthetic.Rows[0].ScenarioDigest == plan.Synthetic.Rows[1].ScenarioDigest {
		t.Fatalf("changed timeout produced identical digests: %#v", plan.Synthetic.Rows)
	}
	sources := []PolicySource{plan.Synthetic.Rows[0].PolicySources[0], plan.Synthetic.Rows[1].PolicySources[0]}
	sort.Slice(sources, func(i, j int) bool { return sources[i] < sources[j] })
	if !reflect.DeepEqual(sources, []PolicySource{PolicySourceHead, PolicySourceMergeBase}) {
		t.Fatalf("changed row sources = %v", sources)
	}

	identical, err := PlanPullRequest(base, plannerCatalog(baseRecord), nil, PullRequestPlanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(identical.Synthetic.Rows) != 1 {
		t.Fatalf("identical proof identities produced %d rows", len(identical.Synthetic.Rows))
	}
	if got := identical.Synthetic.Rows[0].PolicySources; !reflect.DeepEqual(got, []PolicySource{PolicySourceMergeBase, PolicySourceHead}) {
		t.Fatalf("identical row sources = %v", got)
	}
}

func TestPlanPullRequestKeepsMergeBaseHostedDowngradeRequired(t *testing.T) {
	basePolicy := plannerHostedConfigPolicy("Vendor.Base")
	base := plannerCatalog(plannerLiveRecord("apps.fixture", basePolicy))
	head := plannerCatalog(plannerLiveRecord("apps.fixture", plannerNonHostedPolicy(LiveBlocked)))

	plan, err := PlanPullRequest(base, head, []string{`modules\apps\fixture\validation.jsonc`}, PullRequestPlanOptions{})
	if err != nil {
		t.Fatalf("PlanPullRequest returned %v", err)
	}
	if len(plan.Live.Run) != 1 {
		t.Fatalf("live run rows = %#v, want one required merge-base row", plan.Live.Run)
	}
	row := plan.Live.Run[0]
	if row.Status != PlanStatusRequired || row.Reason != ReasonMergeBaseHosted || row.PolicySource != PolicySourceMergeBase {
		t.Fatalf("live row status/reason/source = %q/%q/%q", row.Status, row.Reason, row.PolicySource)
	}
	if !reflect.DeepEqual(row.Policy, basePolicy) {
		t.Fatalf("live row policy = %#v, want trusted merge-base policy %#v", row.Policy, basePolicy)
	}
	if len(plan.Live.Deferred) != 1 || plan.Live.Deferred[0].Reason != ReasonHeadLiveDowngrade {
		t.Fatalf("deferred rows = %#v, want explicit non-relaxing downgrade", plan.Live.Deferred)
	}
}

func TestPlanPullRequestDefersUntrustedHostedPolicies(t *testing.T) {
	t.Run("head-only hosted authorization", func(t *testing.T) {
		base := plannerCatalog(plannerLiveRecord("apps.fixture", plannerNonHostedPolicy(LiveCandidate)))
		head := plannerCatalog(plannerLiveRecord("apps.fixture", plannerHostedInstallPolicy("Vendor.Head")))
		plan, err := PlanPullRequest(base, head, []string{"modules/apps/fixture/module.jsonc"}, PullRequestPlanOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if len(plan.Live.Run) != 0 || len(plan.Live.Deferred) != 1 {
			t.Fatalf("live plan = %#v, want only one deferred row", plan.Live)
		}
		row := plan.Live.Deferred[0]
		if row.Status != PlanStatusCandidate || row.Reason != ReasonHeadOnlyHosted || row.PolicySource != PolicySourceHead {
			t.Fatalf("deferred row = %#v", row)
		}
	})

	t.Run("material policy change", func(t *testing.T) {
		basePolicy := plannerHostedConfigPolicy("Vendor.Base")
		headPolicy := plannerHostedConfigPolicy("Vendor.Head")
		headPolicy.PRTimeoutMinutes++
		headPolicy.Trust = &TrustHashes{SeedSHA256: strings.Repeat("c", 64), ComparatorSHA256: strings.Repeat("d", 64)}
		base := plannerCatalog(plannerLiveRecord("apps.fixture", basePolicy))
		head := plannerCatalog(plannerLiveRecord("apps.fixture", headPolicy))
		plan, err := PlanPullRequest(base, head, []string{"modules/apps/fixture/validation.jsonc"}, PullRequestPlanOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if len(plan.Live.Run) != 1 || !reflect.DeepEqual(plan.Live.Run[0].Policy, basePolicy) {
			t.Fatalf("run rows = %#v, want trusted base policy", plan.Live.Run)
		}
		if len(plan.Live.Deferred) != 1 || plan.Live.Deferred[0].Reason != ReasonHeadPolicyChanged || plan.Live.Deferred[0].PolicySource != PolicySourceHead {
			t.Fatalf("deferred rows = %#v, want material head policy candidate", plan.Live.Deferred)
		}
	})
}

func TestCandidatePolicyLoadsButNoPlannerEmitsIt(t *testing.T) {
	root := t.TempDir()
	mod := writeModule(t, root, "fixture", schemaV1Module("apps.fixture", true))
	if err := os.WriteFile(filepath.Join(root, "modules", "apps", "fixture", "seed.ps1"), []byte("seed"), 0o644); err != nil {
		t.Fatal(err)
	}
	record := validV1Validation("apps.fixture", mod.Revision)
	record.Live = candidatePolicy("19b25856e1c150ca834cffc8b59b23adbd0ec0389e58eb22b3b64768098d002b")
	writeValidation(t, root, "fixture", record)
	catalog, err := LoadCatalog(root, time.Date(2026, 7, 22, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if got := catalog.Records[record.ModuleID].Live; !reflect.DeepEqual(got, record.Live) {
		t.Fatalf("loaded candidate policy = %#v, want %#v", got, record.Live)
	}

	pr, err := PlanPullRequest(catalog, catalog, []string{"modules/apps/fixture/validation.jsonc"}, PullRequestPlanOptions{})
	if err != nil {
		t.Fatalf("PlanPullRequest returned %v", err)
	}
	if len(pr.Live.Run) != 0 || len(pr.Live.Deferred) != 0 {
		t.Fatalf("pull-request live plan = %#v, want no candidate rows", pr.Live)
	}

	hosted, err := PlanHostedCatalog(catalog)
	if err != nil {
		t.Fatalf("PlanHostedCatalog returned %v", err)
	}
	if hosted.HostedCount != 0 || len(hosted.Jobs) != 0 || len(hosted.Chunks) != 0 {
		t.Fatalf("hosted plan = %#v, want no candidate rows", hosted)
	}

	scheduled, err := PlanScheduled(catalog, nil)
	if err != nil {
		t.Fatal(err)
	}
	if scheduled.HostedCount != 0 || len(scheduled.Jobs) != 0 {
		t.Fatalf("scheduled plan = %#v, want no candidate rows", scheduled)
	}

	release, err := PlanRelease(catalog, testEngineCommit40)
	if err != nil {
		t.Fatal(err)
	}
	if release.HostedCount != 0 || len(release.Chunks) != 0 {
		t.Fatalf("release plan = %#v, want no candidate rows", release)
	}
}

func TestPlanPullRequestHeadQuarantineCannotRelaxBasePolicy(t *testing.T) {
	policy := plannerHostedInstallPolicy("Vendor.Base")
	baseRecord := plannerLiveRecord("apps.fixture", policy)
	headRecord := plannerLiveRecord("apps.fixture", policy)
	headRecord.Quarantines = []Quarantine{{
		ProofLevel: ProofLiveInstall, OS: "windows", RunnerImage: "windows-latest",
		FailureFingerprint: strings.Repeat("e", 64), IssueURL: "https://example.com/issues/1",
		ReasonCode: "head-only", Owner: "owner", ExpiresOn: "2026-08-01",
	}}
	plan, err := PlanPullRequest(
		plannerCatalog(baseRecord), plannerCatalog(headRecord),
		[]string{"modules/apps/fixture/validation.jsonc"}, PullRequestPlanOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Live.Run) != 1 || plan.Live.Run[0].Reason != ReasonMergeBaseHosted {
		t.Fatalf("run rows = %#v, want merge-base requirement", plan.Live.Run)
	}
	if len(plan.Live.Deferred) != 1 || plan.Live.Deferred[0].Reason != ReasonHeadQuarantineIgnored {
		t.Fatalf("deferred rows = %#v, want explicit ignored head quarantine", plan.Live.Deferred)
	}
}

func TestPlanPullRequestIgnoresQuarantineOrderOnlyChange(t *testing.T) {
	policy := plannerHostedInstallPolicy("Vendor.Base")
	first := Quarantine{
		ProofLevel: ProofLiveInstall, OS: "windows", RunnerImage: "windows-latest",
		FailureFingerprint: strings.Repeat("a", 64), IssueURL: "https://example.com/issues/1",
		ReasonCode: "first", Owner: "owner", ExpiresOn: "2026-08-01",
	}
	second := first
	second.FailureFingerprint = strings.Repeat("b", 64)
	second.IssueURL = "https://example.com/issues/2"
	second.ReasonCode = "second"
	baseRecord := plannerLiveRecord("apps.fixture", policy)
	baseRecord.Quarantines = []Quarantine{first, second}
	headRecord := plannerLiveRecord("apps.fixture", policy)
	headRecord.Quarantines = []Quarantine{second, first}

	plan, err := PlanPullRequest(
		plannerCatalog(baseRecord), plannerCatalog(headRecord),
		[]string{"modules/apps/fixture/validation.jsonc"}, PullRequestPlanOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Live.Deferred) != 0 {
		t.Fatalf("quarantine reorder produced deferred rows: %#v", plan.Live.Deferred)
	}
}

func TestPlanPullRequestCapsLiveRowsAndReportsOverflow(t *testing.T) {
	baseRecords := make([]ValidationRecord, 0, 5)
	headRecords := make([]ValidationRecord, 0, 5)
	paths := make([]string, 0, 5)
	for index := 4; index >= 0; index-- {
		moduleID := fmt.Sprintf("apps.module-%d", index)
		policy := plannerHostedInstallPolicy(fmt.Sprintf("Vendor.%d", index))
		baseRecords = append(baseRecords, plannerLiveRecord(moduleID, policy))
		headRecords = append(headRecords, plannerLiveRecord(moduleID, policy))
		paths = append(paths, fmt.Sprintf("modules/apps/module-%d/module.jsonc", index))
	}
	plan, err := PlanPullRequest(plannerCatalog(baseRecords...), plannerCatalog(headRecords...), paths, PullRequestPlanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got := liveModuleIDs(plan.Live.Run); !reflect.DeepEqual(got, []string{"apps.module-0", "apps.module-1", "apps.module-2"}) {
		t.Fatalf("run module IDs = %v", got)
	}
	if got := liveModuleIDs(plan.Live.Deferred); !reflect.DeepEqual(got, []string{"apps.module-3", "apps.module-4"}) {
		t.Fatalf("deferred module IDs = %v", got)
	}
	for _, row := range plan.Live.Deferred {
		if row.Status != PlanStatusStale || row.Reason != ReasonPRLiveOverflow {
			t.Fatalf("overflow row = %#v, want stale overflow", row)
		}
	}
}

func plannerCatalog(records ...ValidationRecord) *Catalog {
	catalog := &Catalog{Modules: make(map[string]*modules.Module, len(records)), Records: make(map[string]ValidationRecord, len(records))}
	for _, record := range records {
		revision := record.ModuleRevision
		if revision == "" {
			revision = strings.Repeat("1", 64)
			record.ModuleRevision = revision
		}
		catalog.Modules[record.ModuleID] = &modules.Module{ID: record.ModuleID, Revision: revision}
		catalog.Records[record.ModuleID] = record
	}
	return catalog
}

func plannerRecord(moduleID string, scenarios ...Scenario) ValidationRecord {
	return ValidationRecord{
		SchemaVersion: 1, ModuleID: moduleID, ModuleRevision: strings.Repeat("1", 64),
		Synthetic: SyntheticPolicy{Scenarios: scenarios}, Live: plannerNonHostedPolicy(LiveNotApplicable),
		FilePath: "modules/apps/" + strings.TrimPrefix(moduleID, "apps.") + "/validation.jsonc",
	}
}

func plannerRecordWithMode(moduleID, scenarioID string, mode ScenarioKind) ValidationRecord {
	record := plannerRecord(moduleID, plannerScenario(scenarioID))
	record.Synthetic.Scenarios[0].Mode = mode
	return record
}

func plannerLiveRecord(moduleID string, policy LivePolicy) ValidationRecord {
	record := plannerRecord(moduleID, plannerScenario("default"))
	record.Live = policy
	return record
}

func plannerScenarios(ids ...string) []Scenario {
	scenarios := make([]Scenario, len(ids))
	for index, id := range ids {
		scenarios[index] = plannerScenario(id)
	}
	return scenarios
}

func plannerScenario(id string) Scenario {
	return Scenario{ID: id, Mode: ScenarioConfigRoundtripV1, Fixture: Fixture{Type: FixtureAuto}, TimeoutSeconds: 60}
}

func plannerNonHostedPolicy(mode LiveMode) LivePolicy {
	return LivePolicy{Mode: mode, ReasonCode: "not-hosted", Explanation: "not hosted"}
}

func plannerHostedInstallPolicy(ref string) LivePolicy {
	return LivePolicy{
		Mode: LiveHosted, Driver: "winget", Ref: ref, ProofMode: ProofLiveInstall,
		PRTimeoutMinutes: 20, ScheduledTimeoutMinutes: 30, RunnerLabel: "windows-latest",
	}
}

func plannerHostedConfigPolicy(ref string) LivePolicy {
	policy := plannerHostedInstallPolicy(ref)
	policy.ProofMode = ProofLiveConfigRoundtrip
	policy.Seed = "seed.ps1"
	policy.Comparator = ComparatorExactBytes
	policy.Trust = &TrustHashes{SeedSHA256: strings.Repeat("a", 64)}
	return policy
}

func reverseRecords(records []ValidationRecord) []ValidationRecord {
	reversed := make([]ValidationRecord, len(records))
	for index := range records {
		reversed[len(records)-1-index] = records[index]
	}
	return reversed
}

func reverseScenarios(scenarios []Scenario) []Scenario {
	reversed := make([]Scenario, len(scenarios))
	for index := range scenarios {
		reversed[len(scenarios)-1-index] = scenarios[index]
	}
	return reversed
}

func assertSyntheticRowsSorted(t *testing.T, rows []SyntheticRow) {
	t.Helper()
	for index := 1; index < len(rows); index++ {
		previous := rows[index-1]
		current := rows[index]
		if syntheticRowKey(previous) > syntheticRowKey(current) {
			t.Fatalf("rows are not sorted at %d: %#v before %#v", index, previous, current)
		}
	}
}

func assertShardBalance(t *testing.T, rows []SyntheticRow, shardCount int) {
	t.Helper()
	counts := make([]int, shardCount)
	for _, row := range rows {
		if row.Shard < 0 || row.Shard >= shardCount {
			t.Fatalf("row shard = %d, want [0,%d)", row.Shard, shardCount)
		}
		counts[row.Shard]++
	}
	minimum, maximum := counts[0], counts[0]
	for _, count := range counts[1:] {
		if count < minimum {
			minimum = count
		}
		if count > maximum {
			maximum = count
		}
	}
	if maximum-minimum > 1 {
		t.Fatalf("shard counts = %v, imbalance exceeds one", counts)
	}
}

func syntheticRowKey(row SyntheticRow) string {
	return fmt.Sprintf("%s\x00%s\x00%s", row.ModuleID, row.ScenarioID, row.ScenarioKind)
}

func liveModuleIDs(rows []LiveRow) []string {
	ids := make([]string, len(rows))
	for index, row := range rows {
		ids[index] = row.ModuleID
	}
	return ids
}
