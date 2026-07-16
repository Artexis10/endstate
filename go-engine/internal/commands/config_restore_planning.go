// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"sort"

	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/planner"
)

// configRestoreDetectionEvidence contains the replaceable machine evidence for
// one detection pass. It is consumed immediately and never retained.
type configRestoreDetectionEvidence struct {
	PackagesByModule map[string][]modules.PackageEvidence
	Glob             func(pattern string) ([]string, error)
}

// configRestorePlanningSession owns one command's latest plan. A preview
// invalidates execution eligibility; only a subsequent final pass can publish
// an execution plan.
type configRestorePlanningSession struct {
	runtime   *configRestoreRuntime
	latest    planner.ConfigPlan
	finalized bool
}

func newConfigRestorePlanningSession(runtime *configRestoreRuntime) *configRestorePlanningSession {
	return &configRestorePlanningSession{
		runtime: runtime,
		latest:  planner.ConfigPlan{Sets: []planner.PlanSet{}},
	}
}

// Preview detects and resolves against fresh evidence but never makes its plan
// eligible for execution.
func (session *configRestorePlanningSession) Preview(evidence configRestoreDetectionEvidence) planner.ConfigPlan {
	session.latest = session.buildFreshPlan(evidence)
	session.finalized = false
	return planner.CloneConfigPlan(session.latest)
}

// Final detects and resolves from scratch, wholly replacing preview candidates
// and failures. This is the only pass that can make a plan executable.
func (session *configRestorePlanningSession) Final(evidence configRestoreDetectionEvidence) planner.ConfigPlan {
	session.latest = session.buildFreshPlan(evidence)
	session.finalized = true
	return planner.CloneConfigPlan(session.latest)
}

// ExecutionPlan returns a defensive copy of the final plan. Preview-only state
// is never eligible for execution.
func (session *configRestorePlanningSession) ExecutionPlan() (planner.ConfigPlan, bool) {
	if session == nil || !session.finalized {
		return planner.ConfigPlan{Sets: []planner.PlanSet{}}, false
	}
	return planner.CloneConfigPlan(session.latest), true
}

func (session *configRestorePlanningSession) buildFreshPlan(evidence configRestoreDetectionEvidence) planner.ConfigPlan {
	if session == nil || session.runtime == nil || session.runtime.catalog.resolver == nil {
		return planner.ConfigPlan{Sets: []planner.PlanSet{}}
	}

	sources := selectedConfigRestoreSources(session.runtime.inputs.generationSources)
	moduleIDs := selectedConfigRestoreModuleIDs(sources)
	targetsByModule := make(map[string][]planner.TargetInstance, len(moduleIDs))
	failedModules := make(map[string]struct{})
	for _, moduleID := range moduleIDs {
		targets, err := session.runtime.catalog.resolver.DiscoverTargets(
			moduleID,
			evidence.PackagesByModule[moduleID],
			modules.DiscoveryOptions{Glob: evidence.Glob},
		)
		if err != nil {
			targetsByModule[moduleID] = []planner.TargetInstance{}
			failedModules[moduleID] = struct{}{}
			continue
		}
		targetsByModule[moduleID] = targets
	}

	plan := session.runtime.catalog.resolver.ResolveSources(
		sources,
		targetsByModule,
		session.runtime.inputs.targetMappings,
	)
	applyTargetDetectionFailures(&plan, failedModules)
	return plan
}

func selectedConfigRestoreSources(values []configRestoreSource) []planner.SourceCapture {
	selected := make([]planner.SourceCapture, 0, len(values))
	for _, value := range values {
		if value.selected {
			selected = append(selected, value.source)
		}
	}
	return selected
}

func selectedConfigRestoreModuleIDs(sources []planner.SourceCapture) []string {
	seen := make(map[string]struct{}, len(sources))
	for _, source := range sources {
		seen[source.ModuleID] = struct{}{}
	}
	moduleIDs := make([]string, 0, len(seen))
	for moduleID := range seen {
		moduleIDs = append(moduleIDs, moduleID)
	}
	sort.Strings(moduleIDs)
	return moduleIDs
}

func applyTargetDetectionFailures(plan *planner.ConfigPlan, failedModules map[string]struct{}) {
	if plan == nil || len(failedModules) == 0 {
		return
	}
	for index := range plan.Sets {
		set := &plan.Sets[index]
		if _, failed := failedModules[set.Source.ModuleID]; !failed {
			continue
		}
		reason := planner.ReasonTargetDetectionFailed
		set.TargetInstances = []planner.TargetInstance{}
		set.TargetGenerationDef = nil
		set.MigrationEdges = nil
		set.Resolution = planner.ConfigResolution{
			Resolution:      planner.ResolutionUnknown,
			Reason:          &reason,
			MigrationPath:   []string{},
			ResolvedTargets: []string{},
			Status:          planner.StatusSkipped,
		}
		set.Resolution = planner.ProjectConfigResolution(*set)
	}
	resolutions := make([]planner.ConfigResolution, len(plan.Sets))
	for index := range plan.Sets {
		resolutions[index] = plan.Sets[index].Resolution
	}
	plan.Summary = planner.SummarizeConfigResolutions(resolutions)
}
