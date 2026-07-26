// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/catalogplan"
	"github.com/Artexis10/endstate/go-engine/internal/config"
	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/events"
)

// CatalogPlanFlags holds the parsed catalog-plan command flags.
type CatalogPlanFlags struct {
	Bundle string
	Events string
}

// CatalogPlanResult is the stable catalog-only resolution result.
type CatalogPlanResult = catalogplan.Result

// RunCatalogPlan resolves one tracked catalog bundle without constructing a
// package driver, probing inventory, mutating state, or selecting package intent.
func RunCatalogPlan(flags CatalogPlanFlags) (interface{}, *envelope.Error) {
	result, err := catalogplan.Resolve(config.ResolveRepoRoot(), flags.Bundle, time.Now().UTC())
	if err != nil {
		var detail interface{}
		if result != nil && len(result.Failures) > 0 {
			detail = struct {
				Failures []catalogplan.Failure `json:"failures"`
			}{Failures: result.Failures}
		}
		return result, envelope.NewError(envelope.ErrCatalogPlanInvalid, "Catalog bundle cannot be resolved.").WithDetail(detail)
	}

	emitter := events.NewEmitter(buildRunID("catalog-plan"), flags.Events == "jsonl")
	emitter.EmitPhase("plan")
	for _, action := range result.Actions {
		emitter.EmitItem(action.ModuleID, "catalog", "present", "detected", "catalog module resolved", "")
	}
	emitter.EmitSummary("plan", result.ActionCount, result.ActionCount, 0, 0)
	return result, nil
}
