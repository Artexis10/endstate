// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"fmt"
	"runtime"

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

	total := pass + fail
	emitter.EmitSummary("verify", total, pass, 0, fail)
	return &VerifyResult{
		Manifest: VerifyManifestRef{Path: flags.Manifest, Name: mf.Name},
		Summary:  VerifySummary{Total: total, Pass: pass, Fail: fail},
		Results:  results,
	}, nil
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
