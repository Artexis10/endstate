// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/driver"
	"github.com/Artexis10/endstate/go-engine/internal/planner"
	"github.com/Artexis10/endstate/go-engine/internal/realizer"
)

// withVerifyPlanRealizerAndBrew wires the realizer + a brew factory for the
// verify/plan gate tests.
func withVerifyPlanRealizerAndBrew(fr *fakeRealizer, brewFn func() (driver.Driver, error), f func()) {
	origRz := newRealizerFn
	origBrew := newBrewDriverFn
	newRealizerFn = func() (realizer.Realizer, error) { return fr, nil }
	newBrewDriverFn = brewFn
	defer func() {
		newRealizerFn = origRz
		newBrewDriverFn = origBrew
	}()
	f()
}

func TestRunPlan_AbsentBrewIsVisibleSkipWithoutConstructingDriver(t *testing.T) {
	mfPath := writeTempManifest(t, replaceGOOS(`{
  "version": 1, "name": "brew-plan-absent",
  "apps": [{"id":"hello","driver":"brew","refs":{"darwin":"hello"}}]
}`))

	withBootstrapAvail(nil, func() {
		withVerifyPlanRealizerAndBrew(&fakeRealizer{}, panicBrewDriverFn(t), func() {
			raw, eerr := RunPlan(PlanFlags{Manifest: mfPath})
			if eerr != nil {
				t.Fatalf("RunPlan error: %v", eerr)
			}
			action := raw.(*PlanResult).Actions[0]
			if action.Driver != "brew" || action.CurrentStatus != driver.StatusSkipped || action.PlannedAction != "skip" {
				t.Fatalf("plan action = %+v, want visible unavailable Brew skip", action)
			}
		})
	})
}

func TestRunVerify_AbsentBrewIsVisibleSkipWithoutConstructingDriver(t *testing.T) {
	mfPath := writeTempManifest(t, replaceGOOS(`{
  "version": 1, "name": "brew-verify-absent",
  "apps": [{"id":"hello","driver":"brew","refs":{"darwin":"hello"}}]
}`))

	withBootstrapAvail(nil, func() {
		withVerifyPlanRealizerAndBrew(&fakeRealizer{}, panicBrewDriverFn(t), func() {
			raw, eerr := RunVerify(VerifyFlags{Manifest: mfPath})
			if eerr != nil {
				t.Fatalf("RunVerify error: %v", eerr)
			}
			result := raw.(*VerifyResult)
			if len(result.Results) != 1 || result.Results[0].Driver != "brew" || result.Results[0].Status != driver.StatusSkipped {
				t.Fatalf("verify result = %+v, want visible unavailable Brew skip", result.Results)
			}
			if result.Summary.Skipped != 1 || result.Summary.Fail != 0 {
				t.Fatalf("verify summary = %+v, want one skip and no false missing failure", result.Summary)
			}
		})
	})
}

// TestRunVerify_BrewLane_PresenceReported: a driver:"brew" app present via brew
// is reported pass in the single verify summary alongside the realizer apps.
func TestRunVerify_BrewLane_PresenceReported(t *testing.T) {
	manifestJSON := replaceGOOS(`{
  "version": 1, "name": "brew-verify",
  "apps": [
    { "id": "ripgrep", "displayName": "ripgrep", "refs": { "GOOS": "nixpkgs#ripgrep" } },
    { "id": "hello", "displayName": "hello", "driver": "brew", "refs": { "darwin": "hello" } }
  ]
}`)
	mfPath := writeTempManifest(t, manifestJSON)

	fr := &fakeRealizer{currentSet: realizer.Set{Elements: map[string]realizer.Element{
		"ripgrep": {Name: "ripgrep"},
	}}}
	fb := &fakeBrewDriver{installed: map[string]bool{"hello": true}}

	var result interface{}
	withVerifyPlanRealizerAndBrew(fr, func() (driver.Driver, error) { return fb, nil }, func() {
		r, e := RunVerify(VerifyFlags{Manifest: mfPath})
		if e != nil {
			t.Fatalf("RunVerify error: %v", e)
		}
		result = r
	})

	vr := result.(*VerifyResult)
	if !hasVerifyItem(vr.Results, "hello", "pass") {
		t.Errorf("expected hello pass (present via brew), got %+v", vr.Results)
	}
	if !hasVerifyItem(vr.Results, "ripgrep", "pass") {
		t.Errorf("expected ripgrep pass (present via nix), got %+v", vr.Results)
	}
	if got := verifyDriverForID(vr.Results, "ripgrep"); got != "nix" {
		t.Errorf("ripgrep driver = %q, want nix", got)
	}
	if got := verifyDriverForID(vr.Results, "hello"); got != "brew" {
		t.Errorf("hello driver = %q, want brew", got)
	}
}

// TestRunVerify_BrewLane_MissingReported: a brew app not installed is reported
// fail/missing.
func TestRunVerify_BrewLane_MissingReported(t *testing.T) {
	manifestJSON := replaceGOOS(`{
  "version": 1, "name": "brew-verify-miss",
  "apps": [
    { "id": "hello", "displayName": "hello", "driver": "brew", "refs": { "darwin": "hello" } }
  ]
}`)
	mfPath := writeTempManifest(t, manifestJSON)
	fr := &fakeRealizer{currentSet: realizer.Set{Elements: map[string]realizer.Element{}}}
	fb := &fakeBrewDriver{installed: map[string]bool{}}

	var result interface{}
	withVerifyPlanRealizerAndBrew(fr, func() (driver.Driver, error) { return fb, nil }, func() {
		r, e := RunVerify(VerifyFlags{Manifest: mfPath})
		if e != nil {
			t.Fatalf("RunVerify error: %v", e)
		}
		result = r
	})
	vr := result.(*VerifyResult)
	if !hasVerifyItem(vr.Results, "hello", "fail") {
		t.Errorf("expected hello fail (missing), got %+v", vr.Results)
	}
}

// TestRunPlan_BrewLane_InstallReported: a missing brew app is reported as an
// install action in the single plan summary.
func TestRunPlan_BrewLane_InstallReported(t *testing.T) {
	manifestJSON := replaceGOOS(`{
  "version": 1, "name": "brew-plan",
  "apps": [
    { "id": "ripgrep", "displayName": "ripgrep", "refs": { "GOOS": "nixpkgs#ripgrep" } },
    { "id": "hello", "displayName": "hello", "driver": "brew", "refs": { "darwin": "hello" } }
  ]
}`)
	mfPath := writeTempManifest(t, manifestJSON)

	fr := &fakeRealizer{planDiff: realizer.Diff{ToAdd: []realizer.Installable{{ID: "ripgrep", Ref: "nixpkgs#ripgrep"}}}}
	fb := &fakeBrewDriver{installed: map[string]bool{}}

	var result interface{}
	withVerifyPlanRealizerAndBrew(fr, func() (driver.Driver, error) { return fb, nil }, func() {
		r, e := RunPlan(PlanFlags{Manifest: mfPath})
		if e != nil {
			t.Fatalf("RunPlan error: %v", e)
		}
		result = r
	})

	pr := result.(*PlanResult)
	if !hasPlanAction(pr.Actions, "hello", "brew", "install") {
		t.Errorf("expected hello install via brew in plan, got %+v", pr.Actions)
	}
	if pr.Plan.ToInstall < 2 {
		t.Errorf("plan.toInstall = %d, want >=2 (ripgrep nix + hello brew)", pr.Plan.ToInstall)
	}
}

// TestRunPlan_BrewLane_PresentReported: a present brew app is reported as a
// no-op (present) in the plan.
func TestRunPlan_BrewLane_PresentReported(t *testing.T) {
	manifestJSON := replaceGOOS(`{
  "version": 1, "name": "brew-plan-present",
  "apps": [
    { "id": "hello", "displayName": "hello", "driver": "brew", "refs": { "darwin": "hello" } }
  ]
}`)
	mfPath := writeTempManifest(t, manifestJSON)
	fr := &fakeRealizer{}
	fb := &fakeBrewDriver{installed: map[string]bool{"hello": true}}

	var result interface{}
	withVerifyPlanRealizerAndBrew(fr, func() (driver.Driver, error) { return fb, nil }, func() {
		r, e := RunPlan(PlanFlags{Manifest: mfPath})
		if e != nil {
			t.Fatalf("RunPlan error: %v", e)
		}
		result = r
	})
	pr := result.(*PlanResult)
	if !hasPlanAction(pr.Actions, "hello", "brew", "none") {
		t.Errorf("expected hello present (none) via brew in plan, got %+v", pr.Actions)
	}
}

// TestRunPlan_BrewLane_NonDarwinHost_VisibleSkip: when the brew driver is
// unavailable (non-darwin host, ErrNoBrewDriver), a driver:"brew" app must be
// surfaced in the PLAN as a visible skip — mirroring apply's nil-drv arm — NOT
// reported as a "will install". This is the plan↔apply parity guarantee: plan
// must predict what apply does (apply skips it with "brew driver unavailable on
// this host"), so plan must not promise an install that apply won't perform.
func TestRunPlan_BrewLane_NonDarwinHost_VisibleSkip(t *testing.T) {
	manifestJSON := replaceGOOS(`{
  "version": 1, "name": "brew-plan-skip",
  "apps": [
    { "id": "ripgrep", "displayName": "ripgrep", "refs": { "GOOS": "nixpkgs#ripgrep" } },
    { "id": "hello", "displayName": "hello", "driver": "brew", "refs": { "darwin": "hello" } }
  ]
}`)
	mfPath := writeTempManifest(t, manifestJSON)

	fr := &fakeRealizer{planDiff: realizer.Diff{ToAdd: []realizer.Installable{{ID: "ripgrep", Ref: "nixpkgs#ripgrep"}}}}

	var result interface{}
	// brew factory returns ErrNoBrewDriver (the non-darwin host posture).
	withVerifyPlanRealizerAndBrew(fr, func() (driver.Driver, error) { return nil, ErrNoBrewDriver }, func() {
		r, e := RunPlan(PlanFlags{Manifest: mfPath})
		if e != nil {
			t.Fatalf("RunPlan error: %v", e)
		}
		result = r
	})

	pr := result.(*PlanResult)
	// The brew app must be a visible skip, NOT an install.
	if hasPlanAction(pr.Actions, "hello", "brew", "install") {
		t.Errorf("non-darwin host: brew app must NOT be planned as install, got %+v", pr.Actions)
	}
	if !hasPlanAction(pr.Actions, "hello", "brew", "skip") {
		t.Errorf("non-darwin host: expected hello as a visible skip in plan, got %+v", pr.Actions)
	}
	// Parity with apply's wording.
	var brewAction *planner.PlanAction
	for i := range pr.Actions {
		if pr.Actions[i].ID == "hello" {
			brewAction = &pr.Actions[i]
		}
	}
	if brewAction == nil {
		t.Fatalf("hello brew action absent from plan: %+v", pr.Actions)
	}
	if brewAction.CurrentStatus != "skipped" {
		t.Errorf("brew skip CurrentStatus = %q, want %q", brewAction.CurrentStatus, "skipped")
	}
	// It must count as neither present nor toInstall (the brew app is filtered out
	// of the convergence counts on a host that cannot run brew).
	if pr.Plan.ToInstall != 1 {
		t.Errorf("plan.toInstall = %d, want 1 (only the nix ripgrep; the brew app is skipped, not installed)", pr.Plan.ToInstall)
	}
	if pr.Plan.AlreadyPresent != 0 {
		t.Errorf("plan.alreadyPresent = %d, want 0 (the brew skip is neither present nor toInstall)", pr.Plan.AlreadyPresent)
	}
}

// hasVerifyItem reports whether results contains an item with the given id and
// status.
func hasVerifyItem(results []VerifyItem, id, status string) bool {
	for _, r := range results {
		if r.ID == id && r.Status == status {
			return true
		}
	}
	return false
}

func verifyDriverForID(results []VerifyItem, id string) string {
	for _, result := range results {
		if result.ID == id {
			return result.Driver
		}
	}
	return ""
}

// hasPlanAction reports whether actions contains one with the given id, driver,
// and planned action.
func hasPlanAction(actions []planner.PlanAction, id, drv, planned string) bool {
	for _, a := range actions {
		if a.ID == id && a.Driver == drv && a.PlannedAction == planned {
			return true
		}
	}
	return false
}
