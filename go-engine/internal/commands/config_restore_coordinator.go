// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"context"
	"errors"

	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/planner"
)

type configRestoreDetectionPass string

const (
	configRestoreDetectionPreview configRestoreDetectionPass = "preview"
	configRestoreDetectionFinal   configRestoreDetectionPass = "final"
)

// configRestoreDetectionRequest gives one fresh evidence pass only the
// selected generation-aware module declarations. Modules are defensive copies
// from the command's pinned catalog; filtered and legacy lanes are omitted.
type configRestoreDetectionRequest struct {
	Pass    configRestoreDetectionPass
	Modules map[string]*modules.Module
}

// configRestoreEvidenceSource is the host boundary for read-only target
// inventory. Implementations must query current host state on every call and
// must preserve raw vendor version strings in the returned evidence.
type configRestoreEvidenceSource interface {
	Snapshot(context.Context, configRestoreDetectionRequest) (configRestoreDetectionEvidence, error)
}

// configRestoreCoordinator is the command-facing, read-only planning seam.
// It deliberately exposes no mutation operation; transaction execution is
// added only after the restore transaction coordinator is complete.
type configRestoreCoordinator interface {
	Preview(context.Context) (planner.ConfigPlan, error)
	Final(context.Context, bool) (planner.ConfigPlan, error)
	ExecutionPlan() (planner.ConfigPlan, bool)
}

type planningConfigRestoreCoordinator struct {
	runtime          *configRestoreRuntime
	planning         *configRestorePlanningSession
	evidence         configRestoreEvidenceSource
	executionAllowed bool
}

func newConfigRestoreCoordinator(
	runtime *configRestoreRuntime,
	evidence configRestoreEvidenceSource,
) configRestoreCoordinator {
	return &planningConfigRestoreCoordinator{
		runtime:  runtime,
		planning: newConfigRestorePlanningSession(runtime),
		evidence: evidence,
	}
}

func (coordinator *planningConfigRestoreCoordinator) Preview(
	ctx context.Context,
) (planner.ConfigPlan, error) {
	coordinator.executionAllowed = false
	evidence, err := coordinator.freshEvidence(ctx, configRestoreDetectionPreview)
	if err != nil {
		return emptyConfigRestorePlan(), err
	}
	return coordinator.planning.Preview(evidence), nil
}

func (coordinator *planningConfigRestoreCoordinator) Final(
	ctx context.Context,
	restoreEnabled bool,
) (planner.ConfigPlan, error) {
	coordinator.executionAllowed = false
	evidence, err := coordinator.freshEvidence(ctx, configRestoreDetectionFinal)
	if err != nil {
		return emptyConfigRestorePlan(), err
	}
	plan := coordinator.planning.Final(evidence)
	if !restoreEnabled {
		overlayConfigRestoreNotEnabled(&plan)
		return plan, nil
	}
	coordinator.executionAllowed = true
	return plan, nil
}

func (coordinator *planningConfigRestoreCoordinator) ExecutionPlan() (planner.ConfigPlan, bool) {
	if coordinator == nil || !coordinator.executionAllowed {
		return emptyConfigRestorePlan(), false
	}
	return coordinator.planning.ExecutionPlan()
}

func (coordinator *planningConfigRestoreCoordinator) freshEvidence(
	ctx context.Context,
	pass configRestoreDetectionPass,
) (configRestoreDetectionEvidence, error) {
	if coordinator == nil || coordinator.runtime == nil || coordinator.planning == nil {
		return configRestoreDetectionEvidence{}, errors.New("config restore coordinator is not initialized")
	}
	moduleIDs := selectedConfigRestoreModuleIDs(selectedConfigRestoreSources(
		coordinator.runtime.inputs.generationSources,
	))
	if len(moduleIDs) == 0 {
		return configRestoreDetectionEvidence{
			PackagesByModule: map[string][]modules.PackageEvidence{},
		}, nil
	}
	if coordinator.evidence == nil {
		return configRestoreDetectionEvidence{}, errors.New("config restore evidence source is nil")
	}
	return coordinator.evidence.Snapshot(ctx, configRestoreDetectionRequest{
		Pass:    pass,
		Modules: coordinator.runtime.catalog.modulesFor(moduleIDs),
	})
}

func overlayConfigRestoreNotEnabled(plan *planner.ConfigPlan) {
	if plan == nil {
		return
	}
	for index := range plan.Sets {
		set := &plan.Sets[index]
		if set.Resolution.Reason != nil && *set.Resolution.Reason == planner.ReasonRestoreFiltered {
			continue
		}
		reason := planner.ReasonRestoreNotEnabled
		set.Resolution.Reason = &reason
		set.Resolution.Status = planner.StatusSkipped
		set.Resolution = planner.ProjectConfigResolution(*set)
	}
	recomputeConfigPlanSummary(plan)
}

func emptyConfigRestorePlan() planner.ConfigPlan {
	return planner.ConfigPlan{Sets: []planner.PlanSet{}}
}
