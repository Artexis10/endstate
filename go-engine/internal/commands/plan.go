// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/events"
	"github.com/Artexis10/endstate/go-engine/internal/planner"
)

// PlanFlags holds the parsed CLI flags for the plan command.
type PlanFlags struct {
	// Manifest is the path to the .jsonc manifest file.
	Manifest string
	// Events controls streaming event output. "jsonl" enables it; "" disables.
	Events string
}

// PlanResult is the data payload for the plan command JSON envelope.
// Shape matches docs/contracts/cli-json-contract.md section "Command: plan".
type PlanResult struct {
	Manifest PlanManifestRef       `json:"manifest"`
	Plan     planner.PlanSummary   `json:"plan"`
	Actions  []planner.PlanAction  `json:"actions"`
}

// PlanManifestRef identifies the manifest used for the plan.
type PlanManifestRef struct {
	Path string `json:"path"`
	Name string `json:"name"`
}

// RunPlan executes the plan command with the provided flags.
//
// The plan command detects each app in the manifest against the current system
// state and reports what actions would be taken by apply, without making any
// changes. This is equivalent to a read-only dry-run.
func RunPlan(flags PlanFlags) (interface{}, *envelope.Error) {
	runID := buildRunID("plan")
	emitter := events.NewEmitter(runID, flags.Events == "jsonl")

	// --- 1. Load manifest ---
	mf, envelopeErr := loadManifest(flags.Manifest)
	if envelopeErr != nil {
		return nil, envelopeErr
	}

	// --- 2. Create driver and compute plan ---
	d := newDriverFn()
	emitter.EmitPhase("plan")

	p, planErr := planner.ComputePlan(mf, d)
	if planErr != nil {
		return nil, envelope.NewError(envelope.ErrInternalError, planErr.Error())
	}

	// --- 3. Emit item events ---
	for _, action := range p.Actions {
		emitter.EmitItem(action.Ref, action.Driver, action.CurrentStatus, "", action.PlannedAction)
	}

	// --- 4. Summary event ---
	emitter.EmitSummary("plan", p.Summary.Total, p.Summary.AlreadyPresent, 0, p.Summary.ToInstall)

	return &PlanResult{
		Manifest: PlanManifestRef{Path: flags.Manifest, Name: mf.Name},
		Plan:     p.Summary,
		Actions:  p.Actions,
	}, nil
}
