// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"github.com/Artexis10/endstate/go-engine/internal/planner"
	"github.com/Artexis10/endstate/go-engine/internal/restore"
)

// ConfigResultFields is the shared config-result vocabulary for restore-capable
// command envelopes. Embed a nil *ConfigResultFields to preserve the legacy
// config-free wire shape; NewConfigResultFields marks config payload presence.
type ConfigResultFields struct {
	ConfigResolutions       []planner.ConfigResolution      `json:"configResolutions"`
	ConfigResolutionSummary planner.ConfigResolutionSummary `json:"configResolutionSummary"`
	RestoreItems            []restore.RestoreResult         `json:"restoreItems"`
}

// NewConfigResultFields finalizes the envelope-facing config results for a
// restore-capable command. Calling it means config payloads were present, so
// collections are always non-nil even when planning produced no actions.
//
// Plan sets are projected at this boundary so callers cannot inject host-local
// roots or stale presentation. Restore results are cloned so later execution
// bookkeeping cannot mutate an already-finalized envelope.
func NewConfigResultFields(sets []planner.PlanSet, restoreItems []restore.RestoreResult) *ConfigResultFields {
	resolutions := make([]planner.ConfigResolution, len(sets))
	for index, set := range sets {
		resolutions[index] = planner.ProjectConfigResolution(set)
	}

	items := make([]restore.RestoreResult, len(restoreItems))
	copy(items, restoreItems)
	for index := range items {
		items[index].Warnings = append([]string{}, restoreItems[index].Warnings...)
	}

	return &ConfigResultFields{
		ConfigResolutions:       resolutions,
		ConfigResolutionSummary: planner.SummarizeConfigResolutions(resolutions),
		RestoreItems:            items,
	}
}
