// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"fmt"

	"github.com/Artexis10/endstate/go-engine/internal/driver"
	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/events"
	"github.com/Artexis10/endstate/go-engine/internal/manifest"
)

// ApplyFlags holds the parsed CLI flags for the apply command.
type ApplyFlags struct {
	// Manifest is the path to the .jsonc manifest file.
	Manifest string
	// DryRun previews the plan without making any changes.
	DryRun bool
	// EnableRestore enables configuration restore operations during apply.
	// In Phase 1 this flag is accepted but restore is not yet implemented.
	EnableRestore bool
	// Events controls streaming event output. "jsonl" enables it; "" disables.
	Events string
}

// ApplyResult is the data payload for the apply command JSON envelope.
// Shape matches docs/contracts/cli-json-contract.md section "Command: apply".
type ApplyResult struct {
	DryRun   bool              `json:"dryRun"`
	Manifest ApplyManifestRef  `json:"manifest"`
	Summary  ApplySummary      `json:"summary"`
	Actions  []ApplyAction     `json:"actions"`
}

// ApplyManifestRef identifies the manifest used for the apply run.
type ApplyManifestRef struct {
	Path string `json:"path"`
	Name string `json:"name"`
	Hash string `json:"hash"`
}

// ApplySummary aggregates outcome counts for the apply run.
type ApplySummary struct {
	Total   int `json:"total"`
	Success int `json:"success"`
	Skipped int `json:"skipped"`
	Failed  int `json:"failed"`
}

// ApplyAction records the planned or executed action for a single app entry.
type ApplyAction struct {
	ID     string `json:"id"`
	Ref    string `json:"ref"`
	Driver string `json:"driver"`
	Status string `json:"status"`
	Reason string `json:"reason,omitempty"`
}

// RunApply executes the apply command with the provided flags.
//
// The algorithm mirrors Invoke-ApplyCore and Invoke-VerifyCore from
// bin/endstate.ps1 and follows three phases:
//
// Phase 1 — Plan
//   - Load manifest.
//   - Detect each app via winget.
//   - Build actions list: status "present" or "to_install".
//   - Emit PhaseEvent("plan"), ItemEvents, SummaryEvent("plan").
//
// Phase 2 — Apply (skipped when DryRun is true)
//   - For each "to_install" action, install via winget.
//   - Emit PhaseEvent("apply"), ItemEvents (installing → result), SummaryEvent("apply").
//
// Phase 3 — Verify (skipped when DryRun is true)
//   - Re-detect all apps with a fresh winget query.
//   - Emit PhaseEvent("verify"), ItemEvents, SummaryEvent("verify").
//
// EnableRestore is accepted but logs a no-op notice; restore is Phase 2 work.
func RunApply(flags ApplyFlags) (interface{}, *envelope.Error) {
	runID := buildRunID("apply")
	emitter := events.NewEmitter(runID, flags.Events == "jsonl")

	// EnableRestore: accepted, not yet implemented (Phase 2).
	// We note it but do not error — per instructions "do not error".
	_ = flags.EnableRestore

	// ----------------------------------------------------------------
	// Phase 1: Plan
	// ----------------------------------------------------------------

	mf, envelopeErr := loadManifest(flags.Manifest)
	if envelopeErr != nil {
		return nil, envelopeErr
	}

	d := newDriverFn()

	// First event in stream MUST be a phase event per event-contract.md.
	emitter.EmitPhase("plan")

	type appPlan struct {
		app    manifest.App
		ref    string
		action ApplyAction
	}

	var planEntries []appPlan
	presentCount := 0
	toInstallCount := 0

	for _, app := range mf.Apps {
		ref := resolveWindowsRef(app)
		if ref == "" {
			continue
		}

		installed, _ := d.Detect(ref)

		var action ApplyAction
		action.ID = app.ID
		action.Ref = ref
		action.Driver = d.Name()

		if installed {
			action.Status = "present"
			action.Reason = driver.ReasonAlreadyInstalled
			emitter.EmitItem(ref, d.Name(), "present", driver.ReasonAlreadyInstalled, "Already installed")
			presentCount++
		} else {
			action.Status = "to_install"
			action.Reason = driver.ReasonMissing
			emitter.EmitItem(ref, d.Name(), "to_install", driver.ReasonMissing, "Will be installed")
			toInstallCount++
		}

		planEntries = append(planEntries, appPlan{app: app, ref: ref, action: action})
	}

	totalApps := len(planEntries)
	emitter.EmitSummary("plan", totalApps, presentCount, 0, toInstallCount)

	// ----------------------------------------------------------------
	// Phase 2: Apply  (skip when dry-run)
	// ----------------------------------------------------------------

	successCount := 0
	skippedCount := 0
	failedCount := 0

	// Initialise final action slice from plan (will be mutated below).
	finalActions := make([]ApplyAction, len(planEntries))
	for i, entry := range planEntries {
		finalActions[i] = entry.action
	}

	if !flags.DryRun {
		emitter.EmitPhase("apply")

		for i, entry := range planEntries {
			if entry.action.Status != "to_install" {
				// Already present: counts as skipped in the apply phase.
				skippedCount++
				continue
			}

			emitter.EmitItem(entry.ref, d.Name(), "installing", "", fmt.Sprintf("Installing %s", entry.ref))

			result, installErr := d.Install(entry.ref)
			if installErr != nil {
				// Infrastructure failure (e.g. winget not available).
				finalActions[i].Status = driver.StatusFailed
				finalActions[i].Reason = driver.ReasonInstallFailed
				emitter.EmitItem(entry.ref, d.Name(), "failed", driver.ReasonInstallFailed, installErr.Error())
				failedCount++
				continue
			}

			finalActions[i].Status = result.Status
			finalActions[i].Reason = result.Reason

			switch result.Status {
			case driver.StatusInstalled:
				emitter.EmitItem(entry.ref, d.Name(), "installed", "", result.Message)
				successCount++
			case driver.StatusPresent:
				emitter.EmitItem(entry.ref, d.Name(), "present", result.Reason, result.Message)
				skippedCount++
			default:
				emitter.EmitItem(entry.ref, d.Name(), result.Status, result.Reason, result.Message)
				failedCount++
			}
		}

		applyTotal := successCount + skippedCount + failedCount
		emitter.EmitSummary("apply", applyTotal, successCount, skippedCount, failedCount)

		// ----------------------------------------------------------------
		// Phase 3: Verify  (fresh re-detection)
		// ----------------------------------------------------------------

		emitter.EmitPhase("verify")

		verifyPass := 0
		verifyFail := 0

		for _, entry := range planEntries {
			detected, _ := d.Detect(entry.ref)
			if detected {
				emitter.EmitItem(entry.ref, d.Name(), "present", "", "Verified installed")
				verifyPass++
			} else {
				emitter.EmitItem(entry.ref, d.Name(), "failed", driver.ReasonMissing, "Missing after apply")
				verifyFail++
			}
		}

		verifyTotal := verifyPass + verifyFail
		// Last event in stream is always a summary event per event-contract.md.
		emitter.EmitSummary("verify", verifyTotal, verifyPass, 0, verifyFail)
	}

	// Build the return summary from the apply phase counters.
	// When dry-run, we report the plan counts (present=skipped, to_install=pending).
	var outSummary ApplySummary
	outSummary.Total = totalApps
	if flags.DryRun {
		// Dry-run: no installs executed. Report plan state.
		outSummary.Success = 0
		outSummary.Skipped = presentCount  // already-present apps are effectively "skipped"
		outSummary.Failed = 0
	} else {
		outSummary.Success = successCount
		outSummary.Skipped = skippedCount
		outSummary.Failed = failedCount
	}

	return &ApplyResult{
		DryRun: flags.DryRun,
		Manifest: ApplyManifestRef{
			Path: flags.Manifest,
			Name: mf.Name,
			Hash: "", // hash computation is Phase 2 work
		},
		Summary: outSummary,
		Actions: finalActions,
	}, nil
}

