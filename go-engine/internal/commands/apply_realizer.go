// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/driver"
	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/events"
	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/provision"
	"github.com/Artexis10/endstate/go-engine/internal/realizer"
	"github.com/Artexis10/endstate/go-engine/internal/realizer/nix"
	"github.com/Artexis10/endstate/go-engine/internal/state"
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

// computeDrift returns the element names in the current set that match no desired
// ref — the installed-but-undeclared packages to prune. It compares on leaf
// attributes (the inverse of presentInSet) so e.g. "nixpkgs#ripgrep" matches an
// element named "ripgrep".
func computeDrift(current realizer.Set, desired []realizer.Installable) []string {
	want := map[string]bool{}
	for _, d := range desired {
		if leaf := leafAttr(d.Ref); leaf != "" {
			want[leaf] = true
		}
	}
	var drift []string
	for name, el := range current.Elements {
		if want[leafAttr(name)] || (el.AttrPath != "" && want[leafAttr(el.AttrPath)]) {
			continue
		}
		drift = append(drift, name)
	}
	return drift
}

// requirePruner returns the realizer's optional Pruner capability, or a
// CONVERGENCE_UNSUPPORTED envelope error when the backend cannot converge.
func requirePruner(r realizer.Realizer) (realizer.Pruner, *envelope.Error) {
	if p, ok := r.(realizer.Pruner); ok {
		return p, nil
	}
	return nil, envelope.NewError(
		envelope.ErrConvergenceUnsupported,
		"this backend does not support convergence (--prune)")
}

// resolveHomeFlake resolves the home-manager flakeref the config stage activates
// this apply. There are three mutually-exclusive inputs (enforced at load):
//   - homeManager.settings — a declarative Endstate catalog the engine compiles
//     into a home.nix, then wraps into a generated flake (generated=true);
//   - homeManager.config — a path to a home.nix the engine wraps (generated=true);
//   - homeManager.flake — a direct flakeref returned unchanged (#81; generated=false).
//
// Both generated inputs reuse the wrapper's GenerateHomeFlake* and the EXISTING
// ActivateHome — no new activation/recording path. File/config paths are resolved
// relative to the manifest dir. It returns an empty flakeref (nil error) when no
// config stage applies (no --enable-restore, or no home-manager input). A
// generation failure surfaces as INSTALL_FAILED with raw text confined to
// error.detail (the moat), mirroring an activation failure.
func resolveHomeFlake(flags ApplyFlags, mf *manifest.Manifest) (flake string, generated bool, eerr *envelope.Error) {
	if !flags.EnableRestore || mf.HomeManager == nil {
		return "", false, nil
	}
	switch {
	case mf.HomeManager.Settings != nil:
		ref, gerr := nix.GenerateHomeFlakeFromSettings(state.StateDir(), mf.HomeManager.Settings, filepath.Dir(flags.Manifest), mf.HomeManager.Secrets)
		if gerr != nil {
			return "", false, envelope.NewError(
				envelope.ErrInstallFailed, "Failed to generate the home-manager configuration from settings.").
				WithDetail(map[string]string{"raw": gerr.Error()}).
				WithRemediation("Check homeManager.settings: curated keys, the raw programs block, and file sources.")
		}
		return ref, true, nil
	case mf.HomeManager.Config != "":
		cfgPath := mf.HomeManager.Config
		if !filepath.IsAbs(cfgPath) {
			cfgPath = filepath.Join(filepath.Dir(flags.Manifest), cfgPath)
		}
		ref, gerr := nix.GenerateHomeFlake(state.StateDir(), cfgPath, mf.HomeManager.Secrets)
		if gerr != nil {
			return "", false, envelope.NewError(
				envelope.ErrInstallFailed, "Failed to generate the home-manager configuration flake.").
				WithDetail(map[string]string{"raw": gerr.Error()}).
				WithRemediation("Check that homeManager.config points to a readable home.nix.")
		}
		return ref, true, nil
	case mf.HomeManager.Flake != "":
		return mf.HomeManager.Flake, false, nil
	}
	return "", false, nil
}

// runApplyRealizer is the whole-set apply path for a realizer backend (Nix). It
// computes one plan over the declared package set, performs ONE atomic
// generation switch, and fans the single result back into the per-item event
// stream so the event contract is preserved. Package install ONLY — config and
// verify stages keep their own concerns. Raw backend text is confined to
// error.detail.
func runApplyRealizer(flags ApplyFlags, mf *manifest.Manifest, r realizer.Realizer, emitter *events.Emitter, runID string, configModuleMap map[string]string, restoreModulesAvailable []RestoreModuleRef, brewApps []manifest.App, brewDrv driver.Driver) (interface{}, *envelope.Error) {
	driverName := r.Name()

	// Brew lane (driver:"brew" apps) runs interleaved INSIDE the realizer's
	// plan/apply/verify phases — one event stream, one summary per phase. Counts
	// from each brew phase merge into the matching realizer EmitSummary below.
	// brewActions are appended to the realizer actions and recorded in a SEPARATE
	// Backend:"brew" provisioning generation (after the nix one). nil/empty
	// brewApps ⇒ the lane no-ops.
	brew := newBrewLane(brewDrv, emitter, brewApps)

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

	// Read the current set once so we can extract installed versions for
	// already-present packages (best-effort; ignored on error).
	curSet, _ := r.Current()

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
				// Best-effort: parse the installed version from the current set.
				if el, ok := curSet.Elements[e.app.ID]; ok {
					a.Version = nix.StorePathVersion(el.Name, el.StorePaths)
				}
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

	// Interleave the brew plan items into THIS plan phase, then fold the brew
	// counts into the single plan summary (one summary per phase).
	brewPresent, brewToInstall := brew.planBrew()
	totalApps := len(actions) + len(brew.brewActions())
	emitter.EmitSummary("plan", totalApps, presentCount+brewPresent, 0, toInstallCount+brewToInstall)

	if flags.DryRun {
		// --prune --dry-run previews the prune set without mutating anything and
		// without requiring --confirm.
		var pruned []string
		if flags.Prune {
			if _, perr := requirePruner(r); perr != nil {
				return nil, perr
			}
			cur, _ := r.Current()
			pruned = computeDrift(cur, desired)
		}

		// Config stage preview (home-manager, realizer-only): generate the
		// inspectable flake and REVEAL what would activate; activate nothing.
		var hmPreview *ApplyHomeManager
		if _, ok := r.(realizer.HomeActivator); ok {
			flake, generated, herr := resolveHomeFlake(flags, mf)
			if herr != nil {
				return nil, herr
			}
			if flake != "" {
				emitter.EmitPhase("config")
				emitter.EmitItem(flake, driverName, "would_configure", "dry_run", "Would activate home-manager configuration", "home-manager")
				hmPreview = &ApplyHomeManager{Flake: flake, Generated: generated, Activated: false}
			}
		}

		// Append the brew plan actions; brew present apps add to the skipped count.
		dryActions := append(append([]ApplyAction{}, actions...), brew.brewActions()...)
		return &ApplyResult{
			DryRun:                  true,
			Manifest:                ApplyManifestRef{Path: flags.Manifest, Name: mf.Name},
			Summary:                 ApplySummary{Total: totalApps, Skipped: presentCount + brewPresent},
			Actions:                 dryActions,
			ConfigModuleMap:         configModuleMap,
			RestoreModulesAvailable: restoreModulesAvailable,
			Pruned:                  pruned,
			HomeManager:             hmPreview,
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

	// Track whether each mutating phase advanced a generation and to which
	// number, so the single Provisioning Generation below records the converged
	// set with the final advancing op's native generation.
	installAdvanced := false
	installGen := 0
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
					// Best-effort: parse the installed version from the post-realize set.
					if el, ok := res.After.Elements[ins.ID]; ok {
						actions[i].Version = nix.StorePathVersion(el.Name, el.StorePaths)
					}
				}
				successCount++
			}
			if res.Advanced {
				installAdvanced = true
				installGen = res.ToGeneration
			}
		}
	}

	// --- Phase 2c: Convergence (prune) ---
	// After install, optionally remove installed-but-undeclared drift from the
	// engine-managed set. Realizer-only and gated behind --confirm. Element names
	// removed this run are recorded in the Provisioning Generation below.
	var removed []string
	pruneAdvanced := false
	pruneGen := 0
	if flags.Prune {
		pruner, perr := requirePruner(r)
		if perr != nil {
			return nil, perr
		}
		if !flags.Confirm {
			// Refuse the destructive prune; the install phase results stand.
			return nil, envelope.NewError(
				envelope.ErrInternalError,
				"convergence (--prune) requires --confirm (it uninstalls undeclared packages)").
				WithRemediation("Re-run with --prune --confirm, or use --prune --dry-run to preview.")
		}
		cur, _ := r.Current()
		if drift := computeDrift(cur, desired); len(drift) > 0 {
			pres, _ := pruner.Remove(drift)
			if pres.Err != nil {
				if isSystemic(pres.Err.Code) {
					return nil, realizerEnvelopeError(pres.Err)
				}
				return nil, envelope.NewError(envelope.ErrInstallFailed, "Convergence (prune) failed.").
					WithDetail(map[string]string{"subcode": pres.Err.Subcode, "stage": pres.Err.Stage, "raw": pres.Err.Raw})
			}
			removed = drift
			if pres.Advanced {
				pruneAdvanced = true
				pruneGen = pres.ToGeneration
			}
		}
	}

	// Already-present nix packages count as skipped in the apply phase.
	skippedCount += len(diff.Present)

	// Brew install lane — runs AFTER the nix generation committed, interleaving
	// its install items into THIS apply phase. A brew failure is per-item and
	// never rolls back or aborts the nix generation. Counts fold into the single
	// apply summary.
	brewInstalled, brewSkipped, brewFailed := brew.applyBrew()
	successCount += brewInstalled
	skippedCount += brewSkipped
	failedCount += brewFailed

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
	// Brew verify lane — re-detect presence, interleaved into THIS verify phase.
	brewPass, brewVerifyFail, brewVerifySkip := brew.verifyBrew()
	verifyPass += brewPass
	verifyFail += brewVerifyFail
	_ = brewVerifySkip // unavailable-host skips are not pass/fail; tracked in apply skipped

	emitter.EmitSummary("verify", verifyPass+verifyFail, verifyPass, 0, verifyFail)

	// --- Phase 4: Config (home-manager) ---
	// Realizer-only config stage, gated by --enable-restore AND a declared
	// home-manager input — either a direct homeManager.flake (#81) or a
	// homeManager.config the engine wraps into a generated flake (resolveHomeFlake).
	// The engine owns the home-manager invocation (the user installs nothing). A
	// backend without home-manager support simply does not implement HomeActivator
	// and this stage no-ops; the winget/driver path never reaches here. On failure
	// the stage mirrors the prune precedent: a systemic failure becomes a top-level
	// envelope error, anything else INSTALL_FAILED with raw text confined to
	// error.detail (the moat).
	var homeRef *provision.HomeGenRef
	var homeResult *ApplyHomeManager
	if activator, ok := r.(realizer.HomeActivator); ok {
		// resolveHomeFlake returns "" unless --enable-restore and a home-manager
		// input are set; for homeManager.config it generates the wrapper flake here.
		flake, generated, herr := resolveHomeFlake(flags, mf)
		if herr != nil {
			return nil, herr
		}
		if flake != "" {
			emitter.EmitPhase("config")
			emitter.EmitItem(flake, driverName, "configuring", "", "Activating home-manager configuration", "home-manager")
			hmGen, aerr := activator.ActivateHome(flake)
			if aerr != nil {
				rerr, ok := aerr.(*realizer.Error)
				if !ok || rerr == nil {
					rerr = &realizer.Error{Code: envelope.ErrInstallFailed, Raw: aerr.Error()}
				}
				emitter.EmitItem(flake, driverName, "failed", "install_failed", "Home-manager configuration activation failed", "home-manager")
				emitter.EmitSummary("config", 1, 0, 0, 1)
				if isSystemic(rerr.Code) {
					return nil, realizerEnvelopeError(rerr)
				}
				return nil, envelope.NewError(envelope.ErrInstallFailed, "Home-manager configuration activation failed.").
					WithDetail(map[string]string{"subcode": rerr.Subcode, "stage": rerr.Stage, "raw": rerr.Raw})
			}
			homeRef = &provision.HomeGenRef{Flake: flake, Generation: hmGen}
			if generated {
				// flake is the engine-generated, machine-local wrapper; record the
				// user's declared input (config path OR catalog settings) so capture
				// can round-trip it. Exactly one of Config/Settings is set.
				homeRef.Config = mf.HomeManager.Config
				homeRef.Settings = mf.HomeManager.Settings
				// Phase-1 documented-boundary secret REFERENCES compose with the
				// generated modes; record them (references only, never material) so
				// capture round-trips them alongside config/settings.
				homeRef.Secrets = mf.HomeManager.Secrets
			}
			homeResult = &ApplyHomeManager{Flake: flake, Generated: generated, Activated: true}
			emitter.EmitItem(flake, driverName, "configured", "", fmt.Sprintf("Activated home-manager generation %d", hmGen), "home-manager")
			emitter.EmitSummary("config", 1, 1, 0, 0)
		}
	}

	// Record one Provisioning Generation reflecting the converged set: refs added
	// this run, refs removed (pruned) this run, and any home-manager config
	// activated this run. Native = the final advancing package op's generation
	// (prune's if it advanced, else install's), empty for a config-only apply.
	// Write when a mutating package phase advanced a generation OR a config was
	// activated; an idempotent re-run (no install, no prune, no config) records
	// nothing.
	if installAdvanced || pruneAdvanced || homeRef != nil {
		native := ""
		if installAdvanced || pruneAdvanced {
			finalGen := installGen
			if pruneAdvanced {
				finalGen = pruneGen
			}
			native = fmt.Sprintf("%d", finalGen)
		}
		// The nix generation records ONLY the realizer actions — brew items are
		// recorded in their own Backend:"brew" generation below, never merged here.
		writeProvisioningGeneration(runID, driverName, actions, removed, native, failedCount > 0, homeRef)
	}

	// Brew writes a SEPARATE generation (Backend:"brew") AFTER the nix one. It is
	// best-effort and append-only; writeProvisioningGeneration no-ops when nothing
	// was installed (so a no-brew apply records exactly ONE nix generation — the
	// non-regression guarantee). brewFailed>0 marks it Partial.
	brewActions := brew.brewActions()
	writeProvisioningGeneration(runID, "brew", brewActions, nil, "", brewFailed > 0, nil)

	// Append the brew actions to the realizer actions for the result envelope.
	actions = append(actions, brewActions...)

	return &ApplyResult{
		DryRun:                  false,
		Manifest:                ApplyManifestRef{Path: flags.Manifest, Name: mf.Name},
		Summary:                 ApplySummary{Total: totalApps, Success: successCount, Skipped: skippedCount, Failed: failedCount},
		Actions:                 actions,
		ConfigModuleMap:         configModuleMap,
		RestoreModulesAvailable: restoreModulesAvailable,
		Pruned:                  removed,
		HomeManager:             homeResult,
	}, nil
}
