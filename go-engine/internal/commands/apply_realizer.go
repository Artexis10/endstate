// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"fmt"
	"runtime"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/events"
	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/realizer"
)

// realizerEntry is one planned manifest app on the realizer path.
type realizerEntry struct {
	app      manifest.App
	ins      realizer.Installable
	isNix    bool
	isManual bool
	name     string
}

// realizerItemName returns a stable display name for a realizer item.
func realizerItemName(app manifest.App) string {
	if app.DisplayName != "" {
		return app.DisplayName
	}
	return app.ID
}

// isSystemic reports whether a realizer error is a whole-run infrastructure
// failure that should surface as a top-level envelope error (truncating the
// per-item stream) rather than a per-item install failure.
func isSystemic(code envelope.ErrorCode) bool {
	return code == envelope.ErrRealizerUnavailable || code == envelope.ErrPermissionDenied
}

// realizerEnvelopeError builds the top-level envelope error for a systemic
// realizer failure. Raw Nix text lands ONLY in error.detail (the moat).
func realizerEnvelopeError(rerr *realizer.Error) *envelope.Error {
	msg := "The package backend is unavailable."
	rem := "Ensure the Nix daemon is running and 'nix' is on PATH."
	if rerr.Code == envelope.ErrPermissionDenied {
		msg = "Insufficient permissions to realize the package set."
		rem = "Check write permissions on the Endstate Nix profile directory."
	}
	return envelope.NewError(rerr.Code, msg).
		WithDetail(map[string]string{"subcode": rerr.Subcode, "stage": rerr.Stage, "raw": rerr.Raw}).
		WithRemediation(rem)
}

// leafAttr returns the trailing attribute of an installable ref (after the last
// '#', then the last '.'), for matching against installed element names.
func leafAttr(s string) string {
	if i := strings.LastIndex(s, "#"); i >= 0 {
		s = s[i+1:]
	}
	if i := strings.LastIndex(s, "."); i >= 0 {
		s = s[i+1:]
	}
	return s
}

// presentInSet reports whether ref's leaf attribute matches an installed element.
func presentInSet(set realizer.Set, ref string) bool {
	leaf := leafAttr(ref)
	if leaf == "" {
		return false
	}
	if _, ok := set.Elements[leaf]; ok {
		return true
	}
	for _, e := range set.Elements {
		if e.Name == leaf || leafAttr(e.AttrPath) == leaf {
			return true
		}
	}
	return false
}

// runApplyRealizer is the whole-set apply path for a realizer backend (Nix). It
// computes one plan over the declared package set, performs ONE atomic
// generation switch, and fans the single result back into the per-item event
// stream so the event contract is preserved. Package install ONLY — config and
// verify stages keep their own concerns. Raw backend text is confined to
// error.detail.
func runApplyRealizer(flags ApplyFlags, mf *manifest.Manifest, r realizer.Realizer, emitter *events.Emitter, runID string, configModuleMap map[string]string, restoreModulesAvailable []RestoreModuleRef) (interface{}, *envelope.Error) {
	driverName := r.Name()

	// --- Phase 1: Plan ---
	emitter.EmitPhase("plan")

	var entries []realizerEntry
	var desired []realizer.Installable
	names := map[string]string{}
	for _, app := range mf.Apps {
		ref := app.Refs[runtime.GOOS] // STRICT: no fallback to a non-host (e.g. winget) ref
		name := realizerItemName(app)
		names[app.ID] = name
		switch {
		case ref != "":
			ins := realizer.Installable{ID: app.ID, Ref: ref}
			entries = append(entries, realizerEntry{app: app, ins: ins, isNix: true, name: name})
			desired = append(desired, ins)
		case app.Manual != nil && app.Manual.VerifyPath != "":
			entries = append(entries, realizerEntry{app: app, isManual: true, name: name})
		default:
			// No host package ref and not a manual app: skip (never pass a
			// foreign ref to the realizer).
			emitter.EmitItem(app.ID, driverName, "skipped", "filtered", "No linux/darwin package ref", name)
		}
	}

	diff, planErr := r.Plan(desired)
	if planErr != nil {
		if rerr, ok := planErr.(*realizer.Error); ok && isSystemic(rerr.Code) {
			return nil, realizerEnvelopeError(rerr)
		}
		return nil, envelope.NewError(envelope.ErrInternalError, "Failed to plan the package set.")
	}

	present := map[string]bool{} // app.ID -> already present
	for _, ins := range diff.Present {
		present[ins.ID] = true
	}

	actions := make([]ApplyAction, 0, len(entries))
	idx := map[string]int{}
	presentCount, toInstallCount := 0, 0
	for _, e := range entries {
		switch {
		case e.isManual:
			expanded, exists := checkVerifyPath(e.app.Manual.VerifyPath)
			a := ApplyAction{ID: e.app.ID, Driver: "manual", Name: e.name}
			if exists {
				a.Status, a.Reason, a.Message = "present", "already_installed", fmt.Sprintf("Verified at %s", expanded)
				emitter.EmitItem(e.app.ID, "manual", "present", "already_installed", a.Message, e.name)
				presentCount++
			} else {
				a.Status, a.Reason, a.Message = "to_install", "manual_required", fmt.Sprintf("Not found at %s", expanded)
				a.Manual = e.app.Manual
				emitter.EmitItem(e.app.ID, "manual", "to_install", "manual_required", a.Message, e.name)
				toInstallCount++
			}
			idx[e.app.ID] = len(actions)
			actions = append(actions, a)
		case e.isNix:
			ref := e.ins.Ref
			a := ApplyAction{ID: e.app.ID, Ref: stringPtr(ref), Driver: driverName, Name: e.name}
			if present[e.app.ID] {
				a.Status, a.Reason = "present", "already_installed"
				emitter.EmitItem(ref, driverName, "present", "already_installed", "Already installed", e.name)
				presentCount++
			} else {
				a.Status, a.Reason = "to_install", "missing"
				emitter.EmitItem(ref, driverName, "to_install", "missing", "Will be installed", e.name)
				toInstallCount++
			}
			idx[e.app.ID] = len(actions)
			actions = append(actions, a)
		}
	}

	totalApps := len(actions)
	emitter.EmitSummary("plan", totalApps, presentCount, 0, toInstallCount)

	if flags.DryRun {
		return &ApplyResult{
			DryRun:                  true,
			Manifest:                ApplyManifestRef{Path: flags.Manifest, Name: mf.Name},
			Summary:                 ApplySummary{Total: totalApps, Skipped: presentCount},
			Actions:                 actions,
			ConfigModuleMap:         configModuleMap,
			RestoreModulesAvailable: restoreModulesAvailable,
		}, nil
	}

	// --- Phase 2: Apply (one atomic generation switch over diff.ToAdd) ---
	emitter.EmitPhase("apply")
	successCount, skippedCount, failedCount := 0, 0, 0

	// Manual apps: present -> success; otherwise skipped (manual_required).
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

	if len(diff.ToAdd) > 0 {
		for _, ins := range diff.ToAdd {
			emitter.EmitItem(ins.Ref, driverName, "installing", "", fmt.Sprintf("Installing %s", ins.Ref), names[ins.ID])
		}
		res, _ := r.Realize(diff.ToAdd)
		if res.Err != nil {
			if isSystemic(res.Err.Code) {
				// Whole-run infra failure: top-level error, stream truncates here.
				return nil, realizerEnvelopeError(res.Err)
			}
			// Atomic install failure: nothing was installed; every to-add fails.
			msg := "Install failed"
			if res.Err.Subcode != "" {
				msg = fmt.Sprintf("Install failed (%s)", res.Err.Subcode)
			}
			for _, ins := range diff.ToAdd {
				emitter.EmitItem(ins.Ref, driverName, "failed", "install_failed", msg, names[ins.ID])
				if i, ok := idx[ins.ID]; ok {
					actions[i].Status, actions[i].Reason, actions[i].Message = "failed", "install_failed", msg
				}
				failedCount++
			}
		} else {
			for _, ins := range diff.ToAdd {
				emitter.EmitItem(ins.Ref, driverName, "installed", "", "Installed", names[ins.ID])
				if i, ok := idx[ins.ID]; ok {
					actions[i].Status, actions[i].Reason = "installed", ""
				}
				successCount++
			}
			// Record a Provisioning Generation: the atomic switch committed (full
			// success advanced the profile generation). Install-only, best-effort.
			if res.Advanced {
				writeProvisioningGeneration(runID, driverName, actions, fmt.Sprintf("%d", res.ToGeneration), false)
			}
		}
	}
	// Already-present nix packages count as skipped in the apply phase.
	skippedCount += len(diff.Present)

	emitter.EmitSummary("apply", successCount+skippedCount+failedCount, successCount, skippedCount, failedCount)

	// --- Phase 3: Verify (read current generation) ---
	emitter.EmitPhase("verify")
	cur, _ := r.Current()
	verifyPass, verifyFail := 0, 0
	for _, e := range entries {
		switch {
		case e.isManual:
			expanded, exists := checkVerifyPath(e.app.Manual.VerifyPath)
			if exists {
				emitter.EmitItem(e.app.ID, "manual", "present", "", fmt.Sprintf("Verified at %s", expanded), e.name)
				verifyPass++
			} else {
				emitter.EmitItem(e.app.ID, "manual", "failed", "missing", fmt.Sprintf("Missing at %s", expanded), e.name)
				verifyFail++
			}
		case e.isNix:
			if presentInSet(cur, e.ins.Ref) {
				emitter.EmitItem(e.ins.Ref, driverName, "present", "", "Verified installed", e.name)
				verifyPass++
			} else {
				emitter.EmitItem(e.ins.Ref, driverName, "failed", "missing", "Missing after apply", e.name)
				verifyFail++
			}
		}
	}
	emitter.EmitSummary("verify", verifyPass+verifyFail, verifyPass, 0, verifyFail)

	return &ApplyResult{
		DryRun:                  false,
		Manifest:                ApplyManifestRef{Path: flags.Manifest, Name: mf.Name},
		Summary:                 ApplySummary{Total: totalApps, Success: successCount, Skipped: skippedCount, Failed: failedCount},
		Actions:                 actions,
		ConfigModuleMap:         configModuleMap,
		RestoreModulesAvailable: restoreModulesAvailable,
	}, nil
}
