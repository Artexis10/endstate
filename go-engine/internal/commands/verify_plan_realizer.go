// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"fmt"
	"runtime"
	"strconv"

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
func runVerifyRealizer(flags VerifyFlags, mf *manifest.Manifest, r realizer.Realizer, emitter *events.Emitter) (interface{}, *envelope.Error) {
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

	total := pass + fail
	emitter.EmitSummary("verify", total, pass, 0, fail)
	return &VerifyResult{
		Manifest: VerifyManifestRef{Path: flags.Manifest, Name: mf.Name},
		Summary:  VerifySummary{Total: total, Pass: pass, Fail: fail},
		Results:  results,
	}, nil
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
func runPlanRealizer(flags PlanFlags, mf *manifest.Manifest, r realizer.Realizer, emitter *events.Emitter) (interface{}, *envelope.Error) {
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

	total := present + toInstall
	emitter.EmitSummary("plan", total, present, 0, toInstall)
	return &PlanResult{
		Manifest: PlanManifestRef{Path: flags.Manifest, Name: mf.Name},
		Plan:     planner.PlanSummary{Total: total, ToInstall: toInstall, AlreadyPresent: present},
		Actions:  actions,
	}, nil
}
