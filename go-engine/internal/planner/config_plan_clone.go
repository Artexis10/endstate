// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package planner

// CloneConfigPlan returns a deep caller-owned copy, including the internal
// pinned declarations later staging consumes.
func CloneConfigPlan(plan ConfigPlan) ConfigPlan {
	cloned := ConfigPlan{
		Sets:    make([]PlanSet, len(plan.Sets)),
		Summary: plan.Summary,
	}
	for index := range plan.Sets {
		cloned.Sets[index] = clonePlanSet(plan.Sets[index])
	}
	return cloned
}

func clonePlanSet(set PlanSet) PlanSet {
	cloned := set
	cloned.TargetInstances = append([]TargetInstance(nil), set.TargetInstances...)
	cloned.Resolution = cloneConfigResolution(set.Resolution)
	cloned.TargetGenerationDef = cloneGeneration(set.TargetGenerationDef)
	cloned.MigrationEdges = cloneMigrationEdges(set.MigrationEdges)
	return cloned
}

func cloneConfigResolution(resolution ConfigResolution) ConfigResolution {
	cloned := resolution
	if resolution.SourceInstance != nil {
		source := *resolution.SourceInstance
		cloned.SourceInstance = &source
	}
	cloned.TargetCandidates = append([]TargetInstance(nil), resolution.TargetCandidates...)
	cloned.MigrationPath = append([]string(nil), resolution.MigrationPath...)
	cloned.ResolvedTargets = append([]string(nil), resolution.ResolvedTargets...)
	if resolution.Reason != nil {
		reason := *resolution.Reason
		cloned.Reason = &reason
	}
	if resolution.Remediation != nil {
		remediation := *resolution.Remediation
		cloned.Remediation = &remediation
	}
	return cloned
}
