// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package planner

import "sort"

// ResolveSources deterministically selects at most one viable target for each
// captured config set. Explicit mappings are assumed to be syntactically
// validated by the command layer; this method validates them against the final
// detected target snapshot.
func (r *CompatibilityResolver) ResolveSources(
	sources []SourceCapture,
	targetsByModule map[string][]TargetInstance,
	explicitMappings map[string]string,
) ConfigPlan {
	orderedSources := append([]SourceCapture(nil), sources...)
	sort.Slice(orderedSources, func(left, right int) bool {
		return sourceCaptureLess(orderedSources[left], orderedSources[right])
	})

	plan := ConfigPlan{Sets: make([]PlanSet, 0, len(orderedSources))}
	for _, source := range orderedSources {
		targets := sortedTargetInstances(targetsByModule[source.ModuleID])
		selected := r.resolveSourceTargets(source, targets, explicitMappings)
		plan.Sets = append(plan.Sets, selected)
	}
	resolvePlanCollisions(&plan)
	resolutions := make([]ConfigResolution, len(plan.Sets))
	for index := range plan.Sets {
		resolutions[index] = plan.Sets[index].Resolution
	}
	plan.Summary = SummarizeConfigResolutions(resolutions)
	return plan
}

func (r *CompatibilityResolver) resolveSourceTargets(
	source SourceCapture,
	targets []TargetInstance,
	explicitMappings map[string]string,
) PlanSet {
	candidates := make([]PlanSet, len(targets))
	resolvedTargets := append([]TargetInstance(nil), targets...)
	for index, target := range targets {
		candidate := r.ResolveCandidate(source, target)
		candidates[index] = candidate
		if len(candidate.TargetInstances) == 1 {
			resolvedTargets[index] = candidate.TargetInstances[0]
		}
	}

	if mappedTargetID, mapped := explicitMappings[source.CaptureID]; mapped {
		for targetIndex, target := range resolvedTargets {
			if target.ID != mappedTargetID {
				continue
			}
			candidate := candidates[targetIndex]
			candidate.TargetInstances = resolvedTargets
			if isViableCompatibility(candidate.Resolution) {
				return candidate
			}
			if isTargetCompatibilityReason(candidate.Resolution.Reason) {
				candidate.Resolution.Reason = reasonPointerValue(ReasonMappedTargetIncompatible)
				candidate.Resolution.Status = StatusSkipped
			}
			return candidate
		}
		plan := unresolvedSourcePlan(source, resolvedTargets, ResolutionUnknown, ReasonMappedTargetNotDetected, StatusSkipped)
		plan.Resolution.TargetInstanceID = mappedTargetID
		return plan
	}

	if len(targets) == 0 {
		return unresolvedSourcePlan(source, resolvedTargets, ResolutionUnknown, ReasonTargetNotDetected, StatusSkipped)
	}

	viable := make([]PlanSet, 0, len(targets))
	for _, candidate := range candidates {
		if isViableCompatibility(candidate.Resolution) {
			viable = append(viable, candidate)
		}
	}
	if len(viable) == 1 {
		viable[0].TargetInstances = resolvedTargets
		return viable[0]
	}
	if len(viable) > 1 {
		exact := make([]PlanSet, 0, len(viable))
		for _, candidate := range viable {
			if exactVersionMatch(source.Instance, candidate.TargetInstances[0]) {
				exact = append(exact, candidate)
			}
		}
		if len(exact) == 1 {
			exact[0].TargetInstances = resolvedTargets
			return exact[0]
		}
		return unresolvedSourcePlan(source, resolvedTargets, ResolutionUnknown, ReasonAmbiguousTargetInstance, StatusSkipped)
	}

	if candidatesShareOutcome(candidates) {
		common := candidates[0]
		common.TargetInstances = resolvedTargets
		clearSelectedTarget(&common)
		return common
	}
	return unresolvedSourcePlan(source, resolvedTargets, ResolutionUnknown, ReasonAmbiguousTargetInstance, StatusSkipped)
}

func unresolvedSourcePlan(
	source SourceCapture,
	targets []TargetInstance,
	resolution Resolution,
	reason ResolutionReason,
	status TerminalStatus,
) PlanSet {
	return PlanSet{
		Source:          source,
		TargetInstances: append([]TargetInstance(nil), targets...),
		Resolution: ConfigResolution{
			CaptureID:                   source.CaptureID,
			ModuleID:                    source.ModuleID,
			ConfigSetID:                 source.ConfigSetID,
			SourceInstanceID:            source.Instance.ID,
			SourceGeneration:            source.Generation,
			SourceGenerationFingerprint: source.GenerationFingerprint,
			Resolution:                  resolution,
			Reason:                      reasonPointerValue(reason),
			MigrationPath:               []string{},
			CaptureModuleRevision:       source.ModuleRevision,
			ResolvedTargets:             []string{},
			Status:                      status,
		},
	}
}

func clearSelectedTarget(plan *PlanSet) {
	plan.Resolution.TargetInstanceID = ""
	plan.Resolution.TargetGeneration = ""
	plan.Resolution.MigrationPath = []string{}
	plan.Resolution.ResolvedTargets = []string{}
	plan.TargetGenerationDef = nil
	plan.MigrationEdges = nil
}

func isViableCompatibility(resolution ConfigResolution) bool {
	return resolution.Reason == nil &&
		(resolution.Resolution == ResolutionDirect || resolution.Resolution == ResolutionMigrate)
}

func isTargetCompatibilityReason(reason *ResolutionReason) bool {
	if reason == nil {
		return false
	}
	switch *reason {
	case ReasonUnknownGeneration, ReasonAmbiguousGeneration,
		ReasonDowngradeUnsupported, ReasonMigrationPathMissing:
		return true
	default:
		return false
	}
}

func exactVersionMatch(source SourceInstance, target TargetInstance) bool {
	rawExact := source.RawVersion != "" && target.RawVersion != "" && source.RawVersion == target.RawVersion
	normalizedExact := source.NormalizedVersion != "" && target.NormalizedVersion != "" &&
		source.NormalizedVersion == target.NormalizedVersion
	return rawExact || normalizedExact
}

func candidatesShareOutcome(candidates []PlanSet) bool {
	if len(candidates) == 0 {
		return false
	}
	wanted := candidates[0].Resolution
	for _, candidate := range candidates[1:] {
		if candidate.Resolution.Resolution != wanted.Resolution ||
			!sameResolutionReason(candidate.Resolution.Reason, wanted.Reason) {
			return false
		}
	}
	return true
}

func sameResolutionReason(left, right *ResolutionReason) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func sortedTargetInstances(targets []TargetInstance) []TargetInstance {
	ordered := append([]TargetInstance(nil), targets...)
	sort.Slice(ordered, func(left, right int) bool {
		leftTarget := ordered[left]
		rightTarget := ordered[right]
		if leftTarget.ID != rightTarget.ID {
			return leftTarget.ID < rightTarget.ID
		}
		if leftTarget.DetectorID != rightTarget.DetectorID {
			return leftTarget.DetectorID < rightTarget.DetectorID
		}
		if leftTarget.RawVersion != rightTarget.RawVersion {
			return leftTarget.RawVersion < rightTarget.RawVersion
		}
		if leftTarget.NormalizedVersion != rightTarget.NormalizedVersion {
			return leftTarget.NormalizedVersion < rightTarget.NormalizedVersion
		}
		return leftTarget.Root < rightTarget.Root
	})
	return ordered
}

func sourceCaptureLess(left, right SourceCapture) bool {
	if left.CaptureID != right.CaptureID {
		return left.CaptureID < right.CaptureID
	}
	if left.ModuleID != right.ModuleID {
		return left.ModuleID < right.ModuleID
	}
	if left.ConfigSetID != right.ConfigSetID {
		return left.ConfigSetID < right.ConfigSetID
	}
	return left.Instance.ID < right.Instance.ID
}
