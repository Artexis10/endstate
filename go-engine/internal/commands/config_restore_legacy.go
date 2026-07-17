// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"fmt"
	"sort"

	"github.com/Artexis10/endstate/go-engine/internal/bundle"
	"github.com/Artexis10/endstate/go-engine/internal/planner"
	"github.com/Artexis10/endstate/go-engine/internal/restore"
)

// configRestoreLegacyExecution is the explicit result boundary for the
// existing consented legacy restore path. Results are grouped by canonical
// legacy capture ID by orchestration after the existing restore engine runs.
type configRestoreLegacyExecution struct {
	DryRun             bool
	ResultsByCaptureID map[string][]restore.RestoreResult
	BlockedReasons     map[string]planner.ResolutionReason
}

type configRestoreLegacyProjection struct {
	Plan         planner.ConfigPlan
	RestoreItems []restore.RestoreResult
}

// projectLegacyConfigRestores projects every explicit legacy lane without
// changing the legacy execution, journal, or revert path. Selected, enabled
// lanes require concrete results so the envelope never invents success.
func projectLegacyConfigRestores(
	inputs configRestoreInputs,
	restoreEnabled bool,
	execution configRestoreLegacyExecution,
) (configRestoreLegacyProjection, error) {
	projection := configRestoreLegacyProjection{
		Plan:         planner.ConfigPlan{Sets: []planner.PlanSet{}},
		RestoreItems: []restore.RestoreResult{},
	}

	lanes := append([]configRestoreLegacyLane(nil), inputs.legacyLanes...)
	knownCaptureIDs := make(map[string]struct{}, len(lanes))
	for _, lane := range lanes {
		captureID := bundle.LegacyCaptureID(lane.moduleID)
		if lane.captureID != captureID {
			return projection, fmt.Errorf("legacy lane %q has non-canonical capture ID %q", lane.moduleID, lane.captureID)
		}
		if _, duplicate := knownCaptureIDs[captureID]; duplicate {
			return projection, fmt.Errorf("duplicate legacy capture ID %q", captureID)
		}
		knownCaptureIDs[captureID] = struct{}{}
	}
	for captureID := range execution.ResultsByCaptureID {
		if _, known := knownCaptureIDs[captureID]; !known {
			return projection, fmt.Errorf("legacy results reference unknown capture ID %q", captureID)
		}
	}

	sort.Slice(lanes, func(left, right int) bool {
		leftCaptureID := bundle.LegacyCaptureID(lanes[left].moduleID)
		rightCaptureID := bundle.LegacyCaptureID(lanes[right].moduleID)
		if leftCaptureID != rightCaptureID {
			return leftCaptureID < rightCaptureID
		}
		return lanes[left].moduleID < lanes[right].moduleID
	})

	resolutions := make([]planner.ConfigResolution, 0, len(lanes))
	for _, lane := range lanes {
		status := planner.StatusSkipped
		var reason *planner.ResolutionReason

		switch {
		case !lane.selected:
			reason = legacyConfigReason(planner.ReasonRestoreFiltered)
		case !restoreEnabled:
			reason = legacyConfigReason(planner.ReasonRestoreNotEnabled)
		case execution.BlockedReasons[lane.captureID] != "":
			status = planner.StatusFailed
			reason = legacyConfigReason(execution.BlockedReasons[lane.captureID])
		default:
			results, exists := execution.ResultsByCaptureID[lane.captureID]
			if !exists || len(results) == 0 {
				return projection, fmt.Errorf("selected legacy capture %q has no concrete restore results", lane.captureID)
			}
			var err error
			status, reason, err = legacyConfigTerminalOutcome(execution.DryRun, results)
			if err != nil {
				return projection, fmt.Errorf("legacy capture %q: %w", lane.captureID, err)
			}
			projection.RestoreItems = append(projection.RestoreItems, linkLegacyRestoreItems(lane, results)...)
		}

		set := planner.PlanSet{
			Source: planner.SourceCapture{
				CaptureID:   bundle.LegacyCaptureID(lane.moduleID),
				ModuleID:    lane.moduleID,
				ConfigSetID: "legacy",
			},
			TargetInstances: []planner.TargetInstance{},
			Resolution: planner.ConfigResolution{
				Resolution:      planner.ResolutionLegacyUnverified,
				Reason:          reason,
				MigrationPath:   []string{},
				ResolvedTargets: []string{},
				Status:          status,
			},
		}
		set.Resolution = planner.ProjectConfigResolution(set)
		projection.Plan.Sets = append(projection.Plan.Sets, set)
		resolutions = append(resolutions, set.Resolution)
	}
	projection.Plan.Summary = planner.SummarizeConfigResolutions(resolutions)
	return projection, nil
}

func legacyConfigTerminalOutcome(
	dryRun bool,
	results []restore.RestoreResult,
) (planner.TerminalStatus, *planner.ResolutionReason, error) {
	restored := false
	failed := false
	allUpToDate := true
	for index, result := range results {
		switch result.Status {
		case "restored":
			restored = true
			allUpToDate = false
		case "failed":
			failed = true
			allUpToDate = false
		case "skipped_up_to_date":
		case "skipped_missing_source":
			allUpToDate = false
		default:
			return "", nil, fmt.Errorf("result %d has unsupported status %q", index, result.Status)
		}
	}

	if failed {
		if !dryRun && restored {
			return planner.StatusRollbackFailed, nil, nil
		}
		return planner.StatusFailed, nil, nil
	}
	if restored {
		if dryRun {
			return planner.StatusPlanned, nil, nil
		}
		return planner.StatusRestored, nil, nil
	}
	if allUpToDate {
		return planner.StatusSkipped, legacyConfigReason(planner.ReasonAlreadyUpToDate), nil
	}
	return planner.StatusSkipped, nil, nil
}

// linkLegacyRestoreItems clones concrete legacy results and adds only the
// stable legacy capture/config-set link. Generation and target-instance facts
// are deliberately scrubbed because legacy execution cannot prove them.
func linkLegacyRestoreItems(
	lane configRestoreLegacyLane,
	results []restore.RestoreResult,
) []restore.RestoreResult {
	items := make([]restore.RestoreResult, len(results))
	copy(items, results)
	for index := range items {
		items[index].Warnings = append([]string{}, results[index].Warnings...)
		items[index].CaptureID = bundle.LegacyCaptureID(lane.moduleID)
		items[index].ConfigSetID = "legacy"
		items[index].TargetInstanceID = ""
		items[index].SourceGeneration = ""
		items[index].TargetGeneration = ""
	}
	return items
}

func legacyConfigReason(value planner.ResolutionReason) *planner.ResolutionReason {
	return &value
}
