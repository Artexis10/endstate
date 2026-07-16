// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"fmt"
	"runtime"
	"strconv"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/driver"
	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/events"
	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/planner"
	"github.com/Artexis10/endstate/go-engine/internal/realizer"
	"github.com/Artexis10/endstate/go-engine/internal/verifier"
)

// runVerifyRealizer is the verify path for a realizer backend (Nix). It reads
// the current generation once and reports each host package present/missing,
// preserving the per-item event stream. Verify is read-only (verification-first
// invariant) and does not touch configs.
func runVerifyRealizer(flags VerifyFlags, mf *manifest.Manifest, r realizer.Realizer, emitter *events.Emitter, brewApps []manifest.App, brewDrv driver.Driver, unsupportedApps ...[]manifest.App) (interface{}, *envelope.Error) {
	driverName := r.Name()
	emitter.EmitPhase("verify")

	cur, cerr := r.Current()
	if cerr != nil {
		if rerr, ok := cerr.(*realizer.Error); ok && isSystemic(rerr.Code) {
			return nil, realizerEnvelopeError(rerr)
		}
		// Non-systemic read issue: treat as empty (apps report missing).
	}

	var results []VerifyItem
	pass, fail := 0, 0
	for _, app := range mf.Apps {
		ref := app.Refs[runtime.GOOS] // strict host ref
		name := realizerItemName(app)
		switch {
		case ref != "":
			item := VerifyItem{Type: "app", ID: app.ID, Ref: ref, Name: name}
			if presentInSet(cur, ref) {
				item.Status = "pass"
				emitter.EmitItem(ref, driverName, "present", "", "Verified installed", name)
				pass++
			} else {
				item.Status, item.Reason, item.Message = "fail", "missing", "Missing - not installed"
				emitter.EmitItem(ref, driverName, "failed", "missing", item.Message, name)
				fail++
			}
			results = append(results, item)
		case app.Manual != nil && app.Manual.VerifyPath != "":
			expanded, exists := checkVerifyPath(app.Manual.VerifyPath)
			item := VerifyItem{Type: "app", ID: app.ID, Name: name}
			if exists {
				item.Status, item.Message = "pass", fmt.Sprintf("Verified at %s", expanded)
				emitter.EmitItem(app.ID, "manual", "present", "", item.Message, name)
				pass++
			} else {
				item.Status, item.Reason, item.Message = "fail", "missing", fmt.Sprintf("Missing at %s", expanded)
				emitter.EmitItem(app.ID, "manual", "failed", "missing", item.Message, name)
				fail++
			}
			results = append(results, item)
		}
	}

	// Manifest verify entries run through the same verifier dispatcher.
	if len(mf.Verify) > 0 {
		for _, vr := range verifier.RunVerify(mf.Verify) {
			item := VerifyItem{Type: vr.Type, Status: "fail", Message: vr.Message}
			if vr.Pass {
				item.Status = "pass"
				pass++
			} else {
				fail++
			}
			results = append(results, item)
		}
	}

	// Home-manager config check: only when the manifest declares a homeManager
	// block AND the realizer implements HomeGenerationReader. Realizers that do
	// not implement the interface (e.g. stubs, non-Nix backends) skip silently.
	if mf.HomeManager != nil {
		if hgr, ok := r.(realizer.HomeGenerationReader); ok {
			item := checkHomeManagerGeneration(hgr, emitter)
			if item.Status == "pass" {
				pass++
			} else {
				fail++
			}
			results = append(results, item)
		}
	}

	// Brew presence lane — interleaved into THIS verify phase, folded into the
	// single verify summary (one summary per phase). A brew app present via
	// `brew list` passes; missing fails; an advisory version mismatch is surfaced
	// as version_drift (best-effort, like winget). When brewDrv is nil (non-darwin
	// host) each brew app is a visible skip rather than silently dropped.
	brewResults, brewPass, brewFail, brewSkipped := verifyBrewLane(brewApps, brewDrv, emitter)
	results = append(results, brewResults...)
	pass += brewPass
	fail += brewFail
	unsupportedResults := verifyUnsupportedDrivers(firstAppSlice(unsupportedApps), emitter)
	results = append(results, unsupportedResults...)
	skipped := brewSkipped + len(unsupportedResults)

	total := pass + fail + skipped
	emitter.EmitSummary("verify", total, pass, skipped, fail)
	return &VerifyResult{
		Manifest: VerifyManifestRef{Path: flags.Manifest, Name: mf.Name},
		Summary:  VerifySummary{Total: total, Pass: pass, Fail: fail, Skipped: skipped},
		Results:  results,
	}, nil
}

// verifyBrewLane checks the presence (and advisory version) of each brew app via
// the brew driver, emitting per-item events into the caller's already-open
// verify phase and returning the verify items + (pass, fail) to fold into the
// single verify summary. A nil drv (non-darwin host) reports each brew app as
// a visible skip rather than silently dropping it.
func verifyBrewLane(brewApps []manifest.App, brewDrv driver.Driver, emitter *events.Emitter) (items []VerifyItem, pass, fail, skipped int) {
	if len(brewApps) == 0 {
		return nil, 0, 0, 0
	}
	if brewDrv == nil {
		for _, app := range brewApps {
			ref := app.Refs["darwin"]
			name := brewItemName(app, ref)
			item := VerifyItem{Type: "app", ID: app.ID, Ref: ref, Driver: "brew", Name: name,
				Status: driver.StatusSkipped, Reason: driver.ReasonFiltered, Message: "brew driver unavailable on this host"}
			emitter.EmitItem(brewEventID(app.ID, ref), "brew", driver.StatusSkipped, driver.ReasonFiltered, item.Message, name)
			items = append(items, item)
			skipped++
		}
		return items, pass, fail, skipped
	}

	refs := make([]string, 0, len(brewApps))
	for _, app := range brewApps {
		if ref := app.Refs["darwin"]; ref != "" {
			refs = append(refs, ref)
		}
	}
	batch := brewDetectBatch(brewDrv, refs)

	for _, app := range brewApps {
		ref := app.Refs["darwin"]
		res := batch[ref]
		name := brewItemName(app, ref)
		if res.DisplayName != "" {
			name = res.DisplayName
		}
		item := VerifyItem{Type: "app", ID: app.ID, Ref: ref, Name: name}
		switch {
		case res.Installed && app.Version != "" && res.Version != "" &&
			strings.TrimSpace(res.Version) != strings.TrimSpace(app.Version):
			// Advisory version drift — brew's pin is weak, so this is informational
			// (apply never downgrades a present package), but verify surfaces it.
			item.Status = "fail"
			item.Reason = driver.ReasonVersionDrift
			item.Version = strings.TrimSpace(res.Version)
			item.Expected = strings.TrimSpace(app.Version)
			item.Message = fmt.Sprintf("installed %s, want %s (advisory; brew pinning is weak)", res.Version, app.Version)
			emitter.EmitItem(ref, "brew", "failed", driver.ReasonVersionDrift, item.Message, name)
			fail++
		case res.Installed:
			item.Status = "pass"
			item.Version = strings.TrimSpace(res.Version)
			emitter.EmitItem(ref, "brew", "present", "", "Verified installed", name)
			pass++
		default:
			item.Status = "fail"
			item.Reason = driver.ReasonMissing
			item.Message = "Missing - not installed"
			emitter.EmitItem(ref, "brew", "failed", driver.ReasonMissing, item.Message, name)
			fail++
		}
		items = append(items, item)
	}
	return items, pass, fail, skipped
}

// checkHomeManagerGeneration reads the active home-manager generation via hgr
// and compares it to the most-recently recorded generation in the provisioning
// history. It returns a VerifyItem of type "home-manager" with:
//   - status "pass" when active == recorded
//   - status "fail", reason "config_drift" when active > 0 and active != recorded
//   - status "fail", reason "missing" when active == 0
//
// It also emits a streaming item event via emitter. The listGenerationsFn seam
// (shared with capture_realizer.go) is used so tests can inject a fake history.
func checkHomeManagerGeneration(hgr realizer.HomeGenerationReader, emitter interface {
	EmitItem(id, driver, status, reason, message, name string)
}) VerifyItem {
	item := VerifyItem{
		Type: "home-manager",
		ID:   "home-manager",
	}

	active := hgr.ActiveHomeGeneration()

	// Find the most-recent provisioning generation that recorded a home-manager
	// config (package-only applies record HomeManager=nil, so skip those).
	// Mirrors recoverHomeManager in capture_realizer.go.
	recorded := 0
	if gens, err := listGenerationsFn(); err == nil {
		for _, g := range gens {
			if g.HomeManager != nil {
				recorded = g.HomeManager.Generation
				break
			}
		}
	}

	switch {
	case active == 0:
		item.Status = "fail"
		item.Reason = driver.ReasonMissing
		item.Message = "No active home-manager generation — home-manager config was never applied or has been removed"
		emitter.EmitItem("home-manager", "nix", "failed", driver.ReasonMissing, item.Message, "home-manager")
	case active != recorded:
		item.Status = "fail"
		item.Reason = driver.ReasonConfigDrift
		item.Version = strconv.Itoa(active)
		item.Expected = strconv.Itoa(recorded)
		item.Message = fmt.Sprintf("home-manager generation %d is active, want %d (recorded)", active, recorded)
		emitter.EmitItem("home-manager", "nix", "failed", driver.ReasonConfigDrift, item.Message, "home-manager")
	default:
		item.Status = "pass"
		item.Version = strconv.Itoa(active)
		item.Message = fmt.Sprintf("home-manager generation %d matches recorded", active)
		emitter.EmitItem("home-manager", "nix", "present", "", item.Message, "home-manager")
	}

	return item
}

// runPlanRealizer is the plan (read-only preview) path for a realizer backend.
// It computes one whole-set diff and reports it as planner actions.
func runPlanRealizer(flags PlanFlags, mf *manifest.Manifest, r realizer.Realizer, emitter *events.Emitter, brewApps []manifest.App, brewDrv driver.Driver, unsupportedApps ...[]manifest.App) (interface{}, *envelope.Error) {
	driverName := r.Name()
	emitter.EmitPhase("plan")

	var desired []realizer.Installable
	nameByID := map[string]string{}
	for _, app := range mf.Apps {
		ref := app.Refs[runtime.GOOS]
		if ref == "" {
			continue
		}
		desired = append(desired, realizer.Installable{ID: app.ID, Ref: ref})
		nameByID[app.ID] = realizerItemName(app)
	}

	diff, perr := r.Plan(desired)
	if perr != nil {
		if rerr, ok := perr.(*realizer.Error); ok && isSystemic(rerr.Code) {
			return nil, realizerEnvelopeError(rerr)
		}
		return nil, envelope.NewError(envelope.ErrInternalError, "Failed to plan the package set.")
	}

	var actions []planner.PlanAction
	toInstall, present := 0, 0
	for _, ins := range diff.Present {
		actions = append(actions, planner.PlanAction{Type: "app", ID: ins.ID, Ref: ins.Ref, Driver: driverName, CurrentStatus: "present", PlannedAction: "none", DisplayName: nameByID[ins.ID]})
		emitter.EmitItem(ins.Ref, driverName, "present", "", "none", nameByID[ins.ID])
		present++
	}
	for _, ins := range diff.ToAdd {
		actions = append(actions, planner.PlanAction{Type: "app", ID: ins.ID, Ref: ins.Ref, Driver: driverName, CurrentStatus: "missing", PlannedAction: "install", DisplayName: nameByID[ins.ID]})
		emitter.EmitItem(ins.Ref, driverName, "missing", "", "install", nameByID[ins.ID])
		toInstall++
	}

	// Brew plan lane — interleaved into THIS plan phase, folded into the single
	// plan summary. A present brew app is a no-op (none); a missing one is an
	// install. A nil drv (non-darwin host) reports each brew app as a missing
	// install (visible) rather than silently dropping it.
	brewActions, brewPresent, brewToInstall := planBrewLane(brewApps, brewDrv, emitter)
	actions = append(actions, brewActions...)
	present += brewPresent
	toInstall += brewToInstall
	brewSkipped := len(brewActions) - brewPresent - brewToInstall
	unsupportedActions := planUnsupportedDrivers(firstAppSlice(unsupportedApps), emitter)
	actions = append(actions, unsupportedActions...)
	skipped := brewSkipped + len(unsupportedActions)

	total := present + toInstall + skipped
	emitter.EmitSummary("plan", total, present, skipped, toInstall)
	return &PlanResult{
		Manifest: PlanManifestRef{Path: flags.Manifest, Name: mf.Name},
		Plan:     planner.PlanSummary{Total: total, ToInstall: toInstall, AlreadyPresent: present, Skipped: skipped},
		Actions:  actions,
	}, nil
}

// planBrewLane reports the planned action (none|install) for each brew app via
// the brew driver's presence detection, emitting per-item events into the
// caller's already-open plan phase and returning the planner actions +
// (present, toInstall) to fold into the single plan summary. A nil drv (non-
// darwin host) reports each brew app as a missing install.
func planBrewLane(brewApps []manifest.App, brewDrv driver.Driver, emitter *events.Emitter) (actions []planner.PlanAction, present, toInstall int) {
	if len(brewApps) == 0 {
		return nil, 0, 0
	}

	// Non-darwin host (brew driver unavailable): every brew app is a visible skip,
	// counted as NEITHER present NOR toInstall — mirroring apply_brew.go planBrew's
	// nil-drv arm so PLAN PREDICTS APPLY (apply skips these with the same wording
	// instead of installing them). Reporting "missing"/"install" here would promise
	// an install that apply never performs.
	if brewDrv == nil {
		for _, app := range brewApps {
			ref := app.Refs["darwin"]
			name := brewItemName(app, ref)
			actions = append(actions, planner.PlanAction{Type: "app", ID: app.ID, Ref: ref, Driver: "brew",
				CurrentStatus: "skipped", PlannedAction: "skip", DisplayName: name})
			emitter.EmitItem(brewEventID(app.ID, ref), "brew", "skipped", "filtered", "brew driver unavailable on this host", name)
		}
		return actions, 0, 0
	}

	// brewDrv is non-nil here (the nil case returned above).
	refs := make([]string, 0, len(brewApps))
	for _, app := range brewApps {
		if ref := app.Refs["darwin"]; ref != "" {
			refs = append(refs, ref)
		}
	}
	detect := brewDetectBatch(brewDrv, refs)

	for _, app := range brewApps {
		ref := app.Refs["darwin"]
		res := detect[ref]
		name := brewItemName(app, ref)
		if res.DisplayName != "" {
			name = res.DisplayName
		}
		if res.Installed {
			actions = append(actions, planner.PlanAction{Type: "app", ID: app.ID, Ref: ref, Driver: "brew", CurrentStatus: "present", PlannedAction: "none", DisplayName: name})
			emitter.EmitItem(ref, "brew", "present", "", "none", name)
			present++
		} else {
			actions = append(actions, planner.PlanAction{Type: "app", ID: app.ID, Ref: ref, Driver: "brew", CurrentStatus: "missing", PlannedAction: "install", DisplayName: name})
			emitter.EmitItem(brewEventID(app.ID, ref), "brew", "missing", "", "install", name)
			toInstall++
		}
	}
	return actions, present, toInstall
}
