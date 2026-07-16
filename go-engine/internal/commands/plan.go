// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"github.com/Artexis10/endstate/go-engine/internal/driver"
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
	Manifest PlanManifestRef      `json:"manifest"`
	Plan     planner.PlanSummary  `json:"plan"`
	Actions  []planner.PlanAction `json:"actions"`
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

	// --- 2a. Realizer path (whole-set, e.g. Nix on linux/darwin) ---
	// On Windows newRealizerFn returns ErrNoRealizer and control falls through to
	// the winget driver plan below, byte-identical to prior behavior.
	if rz, rerr := newRealizerFn(); rerr == nil {
		brewApps, unsupportedApps, restApps := partitionRealizerLanes(mf.Apps)
		brewDrv := resolveReadOnlyBrewDriver(len(brewApps) > 0, emitter)
		rzMf := *mf
		rzMf.Apps = restApps
		return runPlanRealizer(flags, &rzMf, rz, emitter, brewApps, brewDrv, unsupportedApps)
	}

	// --- 2. Resolve authoritative per-package driver lanes and compute plan ---
	emitter.EmitPhase("plan")

	p, planErr := computeDriverLanePlanWithOverrides(mf, packageDriverReadOnlyOverrides(mf, emitter))
	if planErr != nil {
		return nil, envelope.NewError(envelope.ErrInternalError, planErr.Error())
	}

	// --- 3. Emit item events ---
	for _, action := range p.Actions {
		reason := ""
		if action.CurrentStatus == driver.StatusSkipped {
			reason = driver.ReasonFiltered
		}
		emitter.EmitItem(action.Ref, action.Driver, action.CurrentStatus, reason, action.PlannedAction, action.DisplayName)
	}

	// --- 4. Summary event ---
	emitter.EmitSummary("plan", p.Summary.Total, p.Summary.AlreadyPresent, p.Summary.Skipped, p.Summary.ToInstall)

	return &PlanResult{
		Manifest: PlanManifestRef{Path: flags.Manifest, Name: mf.Name},
		Plan:     p.Summary,
		Actions:  p.Actions,
	}, nil
}
