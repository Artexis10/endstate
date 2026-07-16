// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"context"
	"fmt"
	"runtime"

	"github.com/Artexis10/endstate/go-engine/internal/driver"
	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/events"
	"github.com/Artexis10/endstate/go-engine/internal/manifest"
)

// configStageApplies reports whether the home-manager config stage would run this
// apply: it is realizer-only and gated by --enable-restore plus a declared
// home-manager input. It mirrors resolveHomeFlake's guard and is used to decide
// whether the Nix backend is NEEDED (so an absent Nix is offered for bootstrap).
func configStageApplies(flags ApplyFlags, mf *manifest.Manifest) bool {
	return flags.EnableRestore && mf.HomeManager != nil
}

// runApplyBrewOnly is the apply path taken when the Nix realizer lane is NOT
// available — either not needed (no realizer-lane apps and no config stage) or
// needed but unavailable (the user declined the Nix bootstrap, or it failed). It
// runs the brew lane standalone and surfaces any realizer-lane apps as visible
// skips, so a declined Nix with a consented brew still installs the brew apps and
// the run never aborts with a half-done apply.
//
// It is deliberately separate from runApplyRealizer (which REQUIRES a working
// realizer): the present-Nix path stays byte-identical, and this path is purely
// additive. Manual apps (backend-independent verifyPath checks) are still
// evaluated; only true Nix-ref apps are skipped as backend-unavailable. The
// home-manager config stage cannot run without the realizer and is therefore
// skipped here.
func runApplyBrewOnly(flags ApplyFlags, mf *manifest.Manifest, restApps, brewApps []manifest.App, brewDrv driver.Driver, emitter *events.Emitter, runID string, configModuleMap map[string]string, restoreModulesAvailable []RestoreModuleRef) (interface{}, *envelope.Error) {
	brew := newBrewLane(brewDrv, emitter, brewApps)

	// --- Phase 1: Plan ---
	emitter.EmitPhase("plan")

	type restEntry struct {
		app      manifest.App
		isManual bool
		name     string
		ref      string
	}
	var entries []restEntry
	for _, app := range restApps {
		ref := app.Refs[runtime.GOOS]
		name := realizerItemName(app)
		switch {
		case ref != "":
			entries = append(entries, restEntry{app: app, name: name, ref: ref})
		case app.Manual != nil && app.Manual.VerifyPath != "":
			entries = append(entries, restEntry{app: app, isManual: true, name: name})
		default:
			// No host ref and not a manual app: nothing to do (matches realizer path).
		}
	}

	actions := make([]ApplyAction, 0, len(entries))
	idx := map[string]int{}
	presentCount, skippedRealizer := 0, 0
	for _, e := range entries {
		if e.isManual {
			expanded, exists := checkVerifyPath(e.app.Manual.VerifyPath)
			a := ApplyAction{ID: e.app.ID, Driver: "manual", Name: e.name, Manual: e.app.Manual}
			if exists {
				a.Status, a.Reason, a.Message = "present", "already_installed", fmt.Sprintf("Verified at %s", expanded)
				emitter.EmitItem(e.app.ID, "manual", "present", "already_installed", a.Message, e.name)
				presentCount++
			} else {
				a.Status, a.Reason, a.Message = "to_install", "manual_required", fmt.Sprintf("Not found at %s", expanded)
				emitter.EmitItem(e.app.ID, "manual", "to_install", "manual_required", a.Message, e.name)
			}
			idx[e.app.ID] = len(actions)
			actions = append(actions, a)
			continue
		}
		// Nix-ref app, but the realizer backend is unavailable → visible skip.
		msg := "Package backend is not set up; skipped"
		a := ApplyAction{ID: e.app.ID, Ref: stringPtr(e.ref), Driver: "nix", Name: e.name,
			Status: "skipped", Reason: "backend_unavailable", Message: msg}
		emitter.EmitItem(e.ref, "nix", "skipped", "backend_unavailable", msg, e.name)
		idx[e.app.ID] = len(actions)
		actions = append(actions, a)
		skippedRealizer++
	}

	brewPresent, brewToInstall := brew.planBrew()
	totalApps := len(actions) + len(brew.brewActions())
	// Plan-phase convention (mirrors runApplyRealizer): present→success slot,
	// to_install→failed slot, skipped not counted in the slots (only in total).
	emitter.EmitSummary("plan", totalApps, presentCount+brewPresent, 0, brewToInstall)
	evidence := newFilesystemConfigRestoreEvidenceSource()
	if brewDrv != nil {
		evidence = newDriverConfigRestoreEvidenceSource(brewDrv, brewApps)
	}
	configSession, configSessionErr := prepareApplyConfigRestore(context.Background(), flags, evidence)
	if configSessionErr != nil {
		return nil, configSessionErr
	}
	var configFields *ConfigResultFields

	if flags.DryRun {
		var configErr *envelope.Error
		configFields, configErr = executePreparedApplyConfigRestore(
			context.Background(), flags, runID, emitter, configSession,
		)
		dryActions := append(append([]ApplyAction{}, actions...), brew.brewActions()...)
		result := &ApplyResult{
			DryRun:                  true,
			Manifest:                ApplyManifestRef{Path: flags.Manifest, Name: mf.Name},
			Summary:                 ApplySummary{Total: totalApps, Skipped: skippedRealizer + presentCount + brewPresent},
			Actions:                 dryActions,
			ConfigModuleMap:         configModuleMap,
			RestoreModulesAvailable: restoreModulesAvailable,
			ConfigResultFields:      configFields,
		}
		return result, configErr
	}

	// --- Phase 2: Apply ---
	emitter.EmitPhase("apply")
	successCount, skippedCount, failedCount := 0, 0, 0

	// Manual apps: present → success; otherwise skipped (manual_required).
	for _, e := range entries {
		if !e.isManual {
			continue
		}
		if actions[idx[e.app.ID]].Status == "present" {
			successCount++
		} else {
			actions[idx[e.app.ID]].Status = "skipped"
			actions[idx[e.app.ID]].Reason = "manual_required"
			emitter.EmitItem(e.app.ID, "manual", "skipped", "manual_required", actions[idx[e.app.ID]].Message, e.name)
			skippedCount++
		}
	}
	// Skipped Nix-ref apps stay skipped through apply.
	skippedCount += skippedRealizer

	brewInstalled, brewSkipped, brewFailed := brew.applyBrew()
	successCount += brewInstalled
	skippedCount += brewSkipped
	failedCount += brewFailed
	emitter.EmitSummary("apply", successCount+skippedCount+failedCount, successCount, skippedCount, failedCount)
	var configErr *envelope.Error
	configFields, configErr = executePreparedApplyConfigRestore(
		context.Background(), flags, runID, emitter, configSession,
	)
	if configErr != nil {
		return &ApplyResult{
			DryRun: false, Manifest: ApplyManifestRef{Path: flags.Manifest, Name: mf.Name},
			Summary:         ApplySummary{Total: totalApps, Success: successCount, Skipped: skippedCount, Failed: failedCount},
			Actions:         append(append([]ApplyAction{}, actions...), brew.brewActions()...),
			ConfigModuleMap: configModuleMap, RestoreModulesAvailable: restoreModulesAvailable,
			ConfigResultFields: configFields,
		}, configErr
	}

	// --- Phase 3: Verify ---
	emitter.EmitPhase("verify")
	verifyPass, verifyFail := 0, 0
	for _, e := range entries {
		if !e.isManual {
			continue
		}
		expanded, exists := checkVerifyPath(e.app.Manual.VerifyPath)
		if exists {
			emitter.EmitItem(e.app.ID, "manual", "present", "", fmt.Sprintf("Verified at %s", expanded), e.name)
			verifyPass++
		} else {
			emitter.EmitItem(e.app.ID, "manual", "failed", "missing", fmt.Sprintf("Missing at %s", expanded), e.name)
			verifyFail++
		}
	}
	brewPass, brewVerifyFail, _ := brew.verifyBrew()
	verifyPass += brewPass
	verifyFail += brewVerifyFail
	emitter.EmitSummary("verify", verifyPass+verifyFail, verifyPass, 0, verifyFail)

	// Record ONLY the brew generation (the realizer lane committed nothing). It is
	// append-only and no-ops when nothing was installed, so a declined-Nix run with
	// no brew installs records no generation at all.
	brewActions := brew.brewActions()
	writeProvisioningGeneration(runID, "brew", brewActions, nil, "", brewFailed > 0, nil)
	actions = append(actions, brewActions...)

	return &ApplyResult{
		DryRun:                  false,
		Manifest:                ApplyManifestRef{Path: flags.Manifest, Name: mf.Name},
		Summary:                 ApplySummary{Total: totalApps, Success: successCount, Skipped: skippedCount, Failed: failedCount},
		Actions:                 actions,
		ConfigModuleMap:         configModuleMap,
		RestoreModulesAvailable: restoreModulesAvailable,
		ConfigResultFields:      configFields,
	}, nil
}
