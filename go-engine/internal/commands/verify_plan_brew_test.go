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
