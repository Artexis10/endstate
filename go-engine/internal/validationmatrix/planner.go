// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationmatrix

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"sort"
	"strings"
)

const (
	DefaultSyntheticShardCount = 8
	MaxSyntheticShardCount     = 16
	MaxPullRequestLiveRows     = 3

	CodeInvalidShardCount        = "invalid_synthetic_shard_count"
	CodeInvalidPlanCatalog       = "invalid_validation_plan_catalog"
	CodeInvalidCandidateBaseline = "invalid_candidate_baseline"
)

type PolicySource string

const (
	PolicySourceCatalog   PolicySource = "catalog"
	PolicySourceMergeBase PolicySource = "merge-base"
	PolicySourceHead      PolicySource = "head"
)

type PlanStatus string

const (
	PlanStatusRequired  PlanStatus = "required"
	PlanStatusCandidate PlanStatus = "candidate"
	PlanStatusStale     PlanStatus = "stale"
)

type PlanReason string

const (
	ReasonMergeBaseHosted          PlanReason = "merge-base-hosted"
	ReasonHeadLiveDowngrade        PlanReason = "head-live-downgrade"
	ReasonHeadOnlyHosted           PlanReason = "head-only-hosted"
	ReasonHeadPolicyChanged        PlanReason = "head-live-policy-changed"
	ReasonHeadQuarantineIgnored    PlanReason = "head-quarantine-non-relaxing"
	ReasonPRLiveOverflow           PlanReason = "pr-live-cap-overflow"
	ReasonTrustedCandidateBaseline PlanReason = "trusted-candidate-baseline"
)

type SyntheticPlanOptions struct {
	ShardCount int
}

type PullRequestPlanOptions struct {
	ShardCount int
}

type SyntheticPlan struct {
	ShardCount int            `json:"shardCount"`
	Rows       []SyntheticRow `json:"rows"`
}

type SyntheticRow struct {
	ModuleID       string         `json:"moduleId"`
	ModuleRevision string         `json:"moduleRevision"`
	ScenarioID     string         `json:"scenarioId"`
	ScenarioKind   ScenarioKind   `json:"scenarioKind"`
	ScenarioDigest string         `json:"scenarioDigest"`
	Scenario       Scenario       `json:"scenario"`
	PolicySources  []PolicySource `json:"policySources"`
	Shard          int            `json:"shard"`
}

type PullRequestPlan struct {
	ChangedModuleIDs []string      `json:"changedModuleIds"`
	Synthetic        SyntheticPlan `json:"synthetic"`
	Live             LivePlan      `json:"live"`
}

type LivePlan struct {
	Run      []LiveRow `json:"run"`
	Deferred []LiveRow `json:"deferred"`
}

type LiveRow struct {
	ModuleID         string       `json:"moduleId"`
	ModuleRevision   string       `json:"moduleRevision"`
	ValidationDigest string       `json:"validationDigest"`
	Status           PlanStatus   `json:"status"`
	Reason           PlanReason   `json:"reason"`
	PolicySource     PolicySource `json:"policySource"`
	Policy           LivePolicy   `json:"policy"`
	Quarantines      []Quarantine `json:"quarantines,omitempty"`
}

// CandidateBaselineSelection intentionally requires separate trusted policy
// and identity rows so callers cannot execute an untrusted head policy under
// a trusted identity.
type CandidateBaselineSelection struct {
	TrustedPolicy   LiveRow
	TrustedIdentity LiveRow
}

// SelectCandidateBaseline is the only candidate execution selection path. It
// accepts only matching rows from trusted main or a trusted merge base; normal
// PR and hosted planning continue to exclude candidates.
func SelectCandidateBaseline(selection CandidateBaselineSelection) (LiveRow, error) {
	policy := selection.TrustedPolicy
	identity := selection.TrustedIdentity
	if !trustedCandidateSource(policy.PolicySource) || !trustedCandidateSource(identity.PolicySource) {
		return LiveRow{}, validationError(CodeInvalidCandidateBaseline, policy.ModuleID, "", "candidate baseline requires trusted-main or merge-base policy and identity rows")
	}
	if policy.Policy.Mode != LiveCandidate || !hasExecutionPolicy(policy.Policy) {
		return LiveRow{}, validationError(CodeInvalidCandidateBaseline, policy.ModuleID, "", "candidate baseline requires a proposed candidate execution policy")
	}
	if !equalLiveRowIdentity(policy, identity) {
		return LiveRow{}, validationError(CodeInvalidCandidateBaseline, policy.ModuleID, "", "candidate baseline policy and identity rows must match exactly")
	}
	if policy.ModuleID == "" || !lowerSHA256Pattern.MatchString(policy.ModuleRevision) || !lowerSHA256Pattern.MatchString(policy.ValidationDigest) {
		return LiveRow{}, validationError(CodeInvalidCandidateBaseline, policy.ModuleID, "", "candidate baseline row identity is incomplete")
	}
	return LiveRow{
		ModuleID: policy.ModuleID, ModuleRevision: policy.ModuleRevision, ValidationDigest: policy.ValidationDigest,
		Status: PlanStatusRequired, Reason: ReasonTrustedCandidateBaseline, PolicySource: policy.PolicySource,
		Policy: cloneLivePolicy(policy.Policy), Quarantines: canonicalQuarantines(policy.Quarantines),
	}, nil
}

func trustedCandidateSource(source PolicySource) bool {
	return source == PolicySourceTrustedMain || source == PolicySourceMergeBase
}

func equalLiveRowIdentity(left, right LiveRow) bool {
	return left.ModuleID == right.ModuleID && left.ModuleRevision == right.ModuleRevision && left.ValidationDigest == right.ValidationDigest
}

func PlanSynthetic(catalog *Catalog, options SyntheticPlanOptions) (SyntheticPlan, error) {
	shardCount, err := normalizedShardCount(options.ShardCount)
	if err != nil {
		return SyntheticPlan{}, err
	}
	rows, err := catalogSyntheticRows(catalog, PolicySourceCatalog)
	if err != nil {
		return SyntheticPlan{}, err
	}
	assignSyntheticShards(rows, shardCount)
	return SyntheticPlan{ShardCount: shardCount, Rows: rows}, nil
}

func ChangedAppModuleIDs(paths []string) []string {
	moduleIDs := make(map[string]struct{})
	for _, path := range paths {
		normalized := strings.ReplaceAll(path, `\`, "/")
		for strings.HasPrefix(normalized, "./") {
			normalized = strings.TrimPrefix(normalized, "./")
		}
		parts := strings.Split(normalized, "/")
		if len(parts) < 4 || parts[0] != "modules" || parts[1] != "apps" {
			continue
		}
		valid := true
		for _, part := range parts {
			if part == "" || part == "." || part == ".." {
				valid = false
				break
			}
		}
		if valid {
			moduleIDs["apps."+parts[2]] = struct{}{}
		}
	}

	stable := make([]string, 0, len(moduleIDs))
	for moduleID := range moduleIDs {
		stable = append(stable, moduleID)
	}
	sort.Strings(stable)
	return stable
}

func PlanPullRequest(base, head *Catalog, changedPaths []string, options PullRequestPlanOptions) (PullRequestPlan, error) {
	shardCount, err := normalizedShardCount(options.ShardCount)
	if err != nil {
		return PullRequestPlan{}, err
	}
	synthetic, err := planPullRequestSynthetic(base, head, shardCount)
	if err != nil {
		return PullRequestPlan{}, err
	}
	changedModuleIDs := ChangedAppModuleIDs(changedPaths)
	live, err := planPullRequestLive(base, head, changedModuleIDs)
	if err != nil {
		return PullRequestPlan{}, err
	}
	return PullRequestPlan{ChangedModuleIDs: changedModuleIDs, Synthetic: synthetic, Live: live}, nil
}

func normalizedShardCount(requested int) (int, error) {
	if requested == 0 {
		return DefaultSyntheticShardCount, nil
	}
	if requested < 1 || requested > MaxSyntheticShardCount {
		return 0, validationError(CodeInvalidShardCount, "", "", "synthetic shard count must be between 1 and %d", MaxSyntheticShardCount)
	}
	return requested, nil
}

func catalogSyntheticRows(catalog *Catalog, source PolicySource) ([]SyntheticRow, error) {
	if catalog == nil {
		return nil, validationError(CodeInvalidPlanCatalog, "", "", "catalog is required")
	}
	rows := make([]SyntheticRow, 0)
	for moduleID, record := range catalog.Records {
		for _, scenario := range record.Synthetic.Scenarios {
			scenarioSnapshot := cloneScenario(scenario)
			digest, err := canonicalDigest(scenarioSnapshot)
			if err != nil {
				return nil, validationErrorWithCause(CodeInvalidPlanCatalog, moduleID, record.FilePath, "digest validation scenario", err)
			}
			rows = append(rows, SyntheticRow{
				ModuleID: moduleID, ModuleRevision: record.ModuleRevision,
				ScenarioID: scenario.ID, ScenarioKind: scenario.Mode, ScenarioDigest: digest,
				Scenario: scenarioSnapshot, PolicySources: []PolicySource{source},
			})
		}
	}
	sortSyntheticRows(rows)
	return rows, nil
}

func cloneScenario(scenario Scenario) Scenario {
	clone := scenario
	if scenario.MinimumAssertions != nil {
		clone.MinimumAssertions = make(map[string]int, len(scenario.MinimumAssertions))
		for name, minimum := range scenario.MinimumAssertions {
			clone.MinimumAssertions[name] = minimum
		}
	}
	if scenario.Expected != nil {
		expected := *scenario.Expected
		clone.Expected = &expected
	}
	return clone
}

func planPullRequestSynthetic(base, head *Catalog, shardCount int) (SyntheticPlan, error) {
	baseRows, err := catalogSyntheticRows(base, PolicySourceMergeBase)
	if err != nil {
		return SyntheticPlan{}, err
	}
	headRows, err := catalogSyntheticRows(head, PolicySourceHead)
	if err != nil {
		return SyntheticPlan{}, err
	}

	union := make(map[string]SyntheticRow, len(baseRows)+len(headRows))
	for _, row := range append(baseRows, headRows...) {
		identity := syntheticProofIdentity(row)
		if existing, found := union[identity]; found {
			existing.PolicySources = appendPolicySource(existing.PolicySources, row.PolicySources[0])
			union[identity] = existing
			continue
		}
		union[identity] = row
	}
	rows := make([]SyntheticRow, 0, len(union))
	for _, row := range union {
		rows = append(rows, row)
	}
	sortSyntheticRows(rows)
	assignSyntheticShards(rows, shardCount)
	return SyntheticPlan{ShardCount: shardCount, Rows: rows}, nil
}

func appendPolicySource(sources []PolicySource, source PolicySource) []PolicySource {
	for _, existing := range sources {
		if existing == source {
			return sources
		}
	}
	return append(sources, source)
}

func sortSyntheticRows(rows []SyntheticRow) {
	sort.Slice(rows, func(left, right int) bool {
		return syntheticProofIdentity(rows[left]) < syntheticProofIdentity(rows[right])
	})
}

func assignSyntheticShards(rows []SyntheticRow, shardCount int) {
	for index := range rows {
		rows[index].Shard = index % shardCount
	}
}

func syntheticProofIdentity(row SyntheticRow) string {
	return strings.Join([]string{
		row.ModuleID, row.ModuleRevision, string(row.ScenarioKind), row.ScenarioID, row.ScenarioDigest,
	}, "\x00")
}

func planPullRequestLive(base, head *Catalog, changedModuleIDs []string) (LivePlan, error) {
	if base == nil || head == nil {
		return LivePlan{}, validationError(CodeInvalidPlanCatalog, "", "", "merge-base and head catalogs are required")
	}
	var runCandidates []LiveRow
	var deferred []LiveRow
	for _, moduleID := range changedModuleIDs {
		baseRecord, hasBase := base.Records[moduleID]
		headRecord, hasHead := head.Records[moduleID]
		baseHosted := hasBase && baseRecord.Live.Mode == LiveHosted
		headHosted := hasHead && headRecord.Live.Mode == LiveHosted

		if !baseHosted {
			if headHosted {
				row, err := liveRow(headRecord, PlanStatusCandidate, ReasonHeadOnlyHosted, PolicySourceHead)
				if err != nil {
					return LivePlan{}, err
				}
				deferred = append(deferred, row)
			}
			continue
		}

		executionRecord := baseRecord
		if hasHead {
			executionRecord = headRecord
		}
		trusted, err := liveRowFrom(executionRecord, baseRecord, PlanStatusRequired, ReasonMergeBaseHosted, PolicySourceMergeBase)
		if err != nil {
			return LivePlan{}, err
		}
		runCandidates = append(runCandidates, trusted)

		switch {
		case !hasHead || headRecord.Live.Mode != LiveHosted:
			row, rowErr := deferredHeadRow(moduleID, headRecord, hasHead, ReasonHeadLiveDowngrade)
			if rowErr != nil {
				return LivePlan{}, rowErr
			}
			deferred = append(deferred, row)
		case !reflect.DeepEqual(baseRecord.Live, headRecord.Live):
			row, rowErr := liveRow(headRecord, PlanStatusCandidate, ReasonHeadPolicyChanged, PolicySourceHead)
			if rowErr != nil {
				return LivePlan{}, rowErr
			}
			deferred = append(deferred, row)
		case !equalQuarantineSets(baseRecord.Quarantines, headRecord.Quarantines):
			row, rowErr := liveRow(headRecord, PlanStatusCandidate, ReasonHeadQuarantineIgnored, PolicySourceHead)
			if rowErr != nil {
				return LivePlan{}, rowErr
			}
			deferred = append(deferred, row)
		}
	}

	plan := LivePlan{}
	for index, row := range runCandidates {
		if index < MaxPullRequestLiveRows {
			plan.Run = append(plan.Run, row)
			continue
		}
		row.Status = PlanStatusStale
		row.Reason = ReasonPRLiveOverflow
		plan.Deferred = append(plan.Deferred, row)
	}
	plan.Deferred = append(plan.Deferred, deferred...)
	sort.Slice(plan.Deferred, func(left, right int) bool {
		if plan.Deferred[left].ModuleID != plan.Deferred[right].ModuleID {
			return plan.Deferred[left].ModuleID < plan.Deferred[right].ModuleID
		}
		if plan.Deferred[left].PolicySource != plan.Deferred[right].PolicySource {
			return plan.Deferred[left].PolicySource < plan.Deferred[right].PolicySource
		}
		return plan.Deferred[left].Reason < plan.Deferred[right].Reason
	})
	return plan, nil
}

func deferredHeadRow(moduleID string, record ValidationRecord, exists bool, reason PlanReason) (LiveRow, error) {
	if exists {
		return liveRow(record, PlanStatusCandidate, reason, PolicySourceHead)
	}
	return LiveRow{
		ModuleID: moduleID, Status: PlanStatusCandidate, Reason: reason, PolicySource: PolicySourceHead,
	}, nil
}

func liveRow(record ValidationRecord, status PlanStatus, reason PlanReason, source PolicySource) (LiveRow, error) {
	return liveRowFrom(record, record, status, reason, source)
}

func liveRowFrom(identityRecord, policyRecord ValidationRecord, status PlanStatus, reason PlanReason, source PolicySource) (LiveRow, error) {
	digest, err := canonicalDigest(identityRecord)
	if err != nil {
		return LiveRow{}, validationErrorWithCause(CodeInvalidPlanCatalog, identityRecord.ModuleID, identityRecord.FilePath, "digest validation record", err)
	}
	return LiveRow{
		ModuleID: identityRecord.ModuleID, ModuleRevision: identityRecord.ModuleRevision, ValidationDigest: digest,
		Status: status, Reason: reason, PolicySource: source,
		Policy: cloneLivePolicy(policyRecord.Live), Quarantines: canonicalQuarantines(policyRecord.Quarantines),
	}, nil
}

func equalQuarantineSets(left, right []Quarantine) bool {
	return reflect.DeepEqual(canonicalQuarantines(left), canonicalQuarantines(right))
}

func canonicalQuarantines(quarantines []Quarantine) []Quarantine {
	canonical := append([]Quarantine(nil), quarantines...)
	sort.Slice(canonical, func(left, right int) bool {
		return quarantineIdentity(canonical[left]) < quarantineIdentity(canonical[right])
	})
	return canonical
}

func quarantineIdentity(quarantine Quarantine) string {
	return strings.Join([]string{
		string(quarantine.ProofLevel), quarantine.OS, quarantine.RunnerImage,
		quarantine.FailureFingerprint, quarantine.IssueURL, quarantine.ReasonCode,
		quarantine.Owner, quarantine.ExpiresOn,
	}, "\x00")
}

func cloneLivePolicy(policy LivePolicy) LivePolicy {
	clone := policy
	if policy.Trust != nil {
		trust := *policy.Trust
		clone.Trust = &trust
	}
	return clone
}

func canonicalDigest(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}
