// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"runtime"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/realizer"
)

// ---------------------------------------------------------------------------
// runVerifyRealizer tests
// ---------------------------------------------------------------------------

// TestRunVerifyRealizer_PresentAndMissing: Current() returns a Set containing
// element "ripgrep" but not "missingpkg".
// Expected: Summary.Pass==1 (ripgrep present), Summary.Fail==1 (missingpkg
// absent).
func TestRunVerifyRealizer_PresentAndMissing(t *testing.T) {
	appPresent := nixApp("ripgrep", "nixpkgs#ripgrep")
	appMissing := nixApp("missingpkg", "nixpkgs#missingpkg")
	mf := nixManifest(appPresent, appMissing)

	fr := &fakeRealizer{
		currentSet: realizer.Set{
			Generation: 5,
			Elements: map[string]realizer.Element{
				"ripgrep": {Name: "ripgrep", AttrPath: "ripgrep"},
				// "missingpkg" is intentionally absent
			},
		},
	}

	flags := VerifyFlags{Manifest: "verify-test"}
	raw, envelopeErr := runVerifyRealizer(flags, mf, fr, noopEmitter())

	if envelopeErr != nil {
		t.Fatalf("runVerifyRealizer returned envelope error: %v", envelopeErr)
	}
	vr, ok := raw.(*VerifyResult)
	if !ok {
		t.Fatalf("expected *VerifyResult, got %T", raw)
	}

	if vr.Summary.Pass != 1 {
		t.Errorf("Summary.Pass = %d, want 1", vr.Summary.Pass)
	}
	if vr.Summary.Fail != 1 {
		t.Errorf("Summary.Fail = %d, want 1", vr.Summary.Fail)
	}
	if vr.Summary.Total != 2 {
		t.Errorf("Summary.Total = %d, want 2", vr.Summary.Total)
	}

	// Find each item by ID and verify its status.
	byID := make(map[string]VerifyItem, len(vr.Results))
	for _, item := range vr.Results {
		byID[item.ID] = item
	}

	if item, found := byID["ripgrep"]; !found {
		t.Error("expected result item for ripgrep, not found")
	} else if item.Status != "pass" {
		t.Errorf("ripgrep status = %q, want pass", item.Status)
	}

	if item, found := byID["missingpkg"]; !found {
		t.Error("expected result item for missingpkg, not found")
	} else if item.Status != "fail" {
		t.Errorf("missingpkg status = %q, want fail", item.Status)
	}
}

// TestRunVerifyRealizer_AllPresent: Current() returns both elements.
// Expected: Summary.Pass==2, Summary.Fail==0.
func TestRunVerifyRealizer_AllPresent(t *testing.T) {
	app1 := nixApp("ripgrep", "nixpkgs#ripgrep")
	app2 := nixApp("jq", "nixpkgs#jq")
	mf := nixManifest(app1, app2)

	fr := &fakeRealizer{
		currentSet: realizer.Set{
			Generation: 3,
			Elements: map[string]realizer.Element{
				"ripgrep": {Name: "ripgrep", AttrPath: "ripgrep"},
				"jq":      {Name: "jq", AttrPath: "jq"},
			},
		},
	}

	flags := VerifyFlags{Manifest: "verify-all-present"}
	raw, envelopeErr := runVerifyRealizer(flags, mf, fr, noopEmitter())

	if envelopeErr != nil {
		t.Fatalf("runVerifyRealizer returned envelope error: %v", envelopeErr)
	}
	vr := raw.(*VerifyResult)

	if vr.Summary.Pass != 2 {
		t.Errorf("Summary.Pass = %d, want 2", vr.Summary.Pass)
	}
	if vr.Summary.Fail != 0 {
		t.Errorf("Summary.Fail = %d, want 0", vr.Summary.Fail)
	}
}

// TestRunVerifyRealizer_CurrentOnlyCalledOnce: Current() should be called
// exactly once per verify run (read-only; no mutations).
func TestRunVerifyRealizer_CurrentOnlyCalledOnce(t *testing.T) {
	app1 := nixApp("ripgrep", "nixpkgs#ripgrep")
	mf := nixManifest(app1)

	fr := &fakeRealizer{
		currentSet: realizer.Set{
			Elements: map[string]realizer.Element{
				"ripgrep": {Name: "ripgrep", AttrPath: "ripgrep"},
			},
		},
	}

	flags := VerifyFlags{Manifest: "verify-once"}
	_, _ = runVerifyRealizer(flags, mf, fr, noopEmitter())

	if fr.currentCalls != 1 {
		t.Errorf("Current() called %d times, want exactly 1", fr.currentCalls)
	}
	if fr.planCalls != 0 {
		t.Errorf("Plan() called %d times in verify, want 0", fr.planCalls)
	}
	if fr.realizeCalls != 0 {
		t.Errorf("Realize() called %d times in verify, want 0", fr.realizeCalls)
	}
}

// TestRunVerifyRealizer_SkipsAppsWithNoHostRef: An app with refs only for
// "windows" should be silently skipped on linux/darwin.
func TestRunVerifyRealizer_SkipsAppsWithNoHostRef(t *testing.T) {
	noHostApp := foreignRefApp("vscode", "Some.Pkg")
	nixApp1 := nixApp("ripgrep", "nixpkgs#ripgrep")
	mf := nixManifest(noHostApp, nixApp1)

	fr := &fakeRealizer{
		currentSet: realizer.Set{
			Elements: map[string]realizer.Element{
				"ripgrep": {Name: "ripgrep", AttrPath: "ripgrep"},
			},
		},
	}

	flags := VerifyFlags{Manifest: "verify-skip-windows"}
	raw, envelopeErr := runVerifyRealizer(flags, mf, fr, noopEmitter())

	if envelopeErr != nil {
		t.Fatalf("runVerifyRealizer returned envelope error: %v", envelopeErr)
	}
	vr := raw.(*VerifyResult)

	// Only ripgrep should appear — windows-only app is skipped.
	if vr.Summary.Total != 1 {
		t.Errorf("Summary.Total = %d, want 1 (windows-only app must be skipped)", vr.Summary.Total)
	}
	if vr.Summary.Pass != 1 {
		t.Errorf("Summary.Pass = %d, want 1", vr.Summary.Pass)
	}
}

// ---------------------------------------------------------------------------
// runPlanRealizer tests
// ---------------------------------------------------------------------------

// TestRunPlanRealizer_PresentAndToAdd: Plan returns 1 Present + 1 ToAdd.
// Expected: Plan.AlreadyPresent==1, Plan.ToInstall==1, actions with
// currentStatus "present"/"missing" respectively.
func TestRunPlanRealizer_PresentAndToAdd(t *testing.T) {
	app1 := nixApp("ripgrep", "nixpkgs#ripgrep")
	app2 := nixApp("jq", "nixpkgs#jq")
	mf := nixManifest(app1, app2)

	ins1 := realizer.Installable{ID: "ripgrep", Ref: hostRef(app1)}
	ins2 := realizer.Installable{ID: "jq", Ref: hostRef(app2)}

	fr := &fakeRealizer{
		planDiff: realizer.Diff{
			Present: []realizer.Installable{ins1},
			ToAdd:   []realizer.Installable{ins2},
		},
	}

	flags := PlanFlags{Manifest: "plan-test"}
	raw, envelopeErr := runPlanRealizer(flags, mf, fr, noopEmitter())

	if envelopeErr != nil {
		t.Fatalf("runPlanRealizer returned envelope error: %v", envelopeErr)
	}
	pr, ok := raw.(*PlanResult)
	if !ok {
		t.Fatalf("expected *PlanResult, got %T", raw)
	}

	if pr.Plan.AlreadyPresent != 1 {
		t.Errorf("Plan.AlreadyPresent = %d, want 1", pr.Plan.AlreadyPresent)
	}
	if pr.Plan.ToInstall != 1 {
		t.Errorf("Plan.ToInstall = %d, want 1", pr.Plan.ToInstall)
	}
	if pr.Plan.Total != 2 {
		t.Errorf("Plan.Total = %d, want 2", pr.Plan.Total)
	}

	// Check per-action currentStatus.
	byID := make(map[string]string, len(pr.Actions))
	for _, a := range pr.Actions {
		byID[a.ID] = a.CurrentStatus
	}

	if status, found := byID["ripgrep"]; !found {
		t.Error("expected plan action for ripgrep")
	} else if status != "present" {
		t.Errorf("ripgrep currentStatus = %q, want present", status)
	}

	if status, found := byID["jq"]; !found {
		t.Error("expected plan action for jq")
	} else if status != "missing" {
		t.Errorf("jq currentStatus = %q, want missing", status)
	}
}

// TestRunPlanRealizer_AllPresent: Plan returns everything as Present.
// Expected: ToInstall==0, AlreadyPresent==2.
func TestRunPlanRealizer_AllPresent(t *testing.T) {
	app1 := nixApp("ripgrep", "nixpkgs#ripgrep")
	app2 := nixApp("jq", "nixpkgs#jq")
	mf := nixManifest(app1, app2)

	ins1 := realizer.Installable{ID: "ripgrep", Ref: hostRef(app1)}
	ins2 := realizer.Installable{ID: "jq", Ref: hostRef(app2)}

	fr := &fakeRealizer{
		planDiff: realizer.Diff{
			Present: []realizer.Installable{ins1, ins2},
			ToAdd:   nil,
		},
	}

	flags := PlanFlags{Manifest: "plan-all-present"}
	raw, _ := runPlanRealizer(flags, mf, fr, noopEmitter())
	pr := raw.(*PlanResult)

	if pr.Plan.AlreadyPresent != 2 {
		t.Errorf("Plan.AlreadyPresent = %d, want 2", pr.Plan.AlreadyPresent)
	}
	if pr.Plan.ToInstall != 0 {
		t.Errorf("Plan.ToInstall = %d, want 0", pr.Plan.ToInstall)
	}
}

// TestRunPlanRealizer_AllToAdd: Plan returns everything as ToAdd.
// Expected: ToInstall==2, AlreadyPresent==0.
func TestRunPlanRealizer_AllToAdd(t *testing.T) {
	app1 := nixApp("ripgrep", "nixpkgs#ripgrep")
	app2 := nixApp("jq", "nixpkgs#jq")
	mf := nixManifest(app1, app2)

	ins1 := realizer.Installable{ID: "ripgrep", Ref: hostRef(app1)}
	ins2 := realizer.Installable{ID: "jq", Ref: hostRef(app2)}

	fr := &fakeRealizer{
		planDiff: realizer.Diff{
			ToAdd:   []realizer.Installable{ins1, ins2},
			Present: nil,
		},
	}

	flags := PlanFlags{Manifest: "plan-all-toadd"}
	raw, _ := runPlanRealizer(flags, mf, fr, noopEmitter())
	pr := raw.(*PlanResult)

	if pr.Plan.ToInstall != 2 {
		t.Errorf("Plan.ToInstall = %d, want 2", pr.Plan.ToInstall)
	}
	if pr.Plan.AlreadyPresent != 0 {
		t.Errorf("Plan.AlreadyPresent = %d, want 0", pr.Plan.AlreadyPresent)
	}
}

// TestRunPlanRealizer_PlanOnlyCalledOnce: Plan() is called exactly once and
// Realize is never called (plan is read-only).
func TestRunPlanRealizer_PlanOnlyCalledOnce(t *testing.T) {
	app1 := nixApp("ripgrep", "nixpkgs#ripgrep")
	mf := nixManifest(app1)

	fr := &fakeRealizer{
		planDiff: realizer.Diff{
			Present: []realizer.Installable{{ID: "ripgrep", Ref: hostRef(app1)}},
		},
	}

	flags := PlanFlags{Manifest: "plan-calls"}
	_, _ = runPlanRealizer(flags, mf, fr, noopEmitter())

	if fr.planCalls != 1 {
		t.Errorf("Plan() called %d times, want 1", fr.planCalls)
	}
	if fr.realizeCalls != 0 {
		t.Errorf("Realize() called %d times in plan, want 0", fr.realizeCalls)
	}
	if fr.currentCalls != 0 {
		t.Errorf("Current() called %d times in plan, want 0", fr.currentCalls)
	}
}

// TestRunPlanRealizer_SkipsAppsWithNoHostRef: Apps without a ref for the
// current GOOS are excluded from the planned installables.
func TestRunPlanRealizer_SkipsAppsWithNoHostRef(t *testing.T) {
	noHostApp := foreignRefApp("vscode", "Some.Pkg")
	nixApp1 := nixApp("ripgrep", "nixpkgs#ripgrep")
	mf := nixManifest(noHostApp, nixApp1)

	fr := &fakeRealizer{
		planDiff: realizer.Diff{
			ToAdd: []realizer.Installable{{ID: "ripgrep", Ref: "nixpkgs#ripgrep"}},
		},
	}

	flags := PlanFlags{Manifest: "plan-skip-windows"}
	_, envelopeErr := runPlanRealizer(flags, mf, fr, noopEmitter())

	if envelopeErr != nil {
		t.Fatalf("runPlanRealizer returned envelope error: %v", envelopeErr)
	}

	// Plan() should only receive the nix-applicable installable — not the
	// windows ref.
	if len(fr.lastPlanArgs) != 1 {
		t.Errorf("Plan received %d installables, want 1 (windows-only app excluded)", len(fr.lastPlanArgs))
	}
	if len(fr.lastPlanArgs) == 1 && fr.lastPlanArgs[0].Ref != nixApp1.Refs[runtime.GOOS] {
		t.Errorf("Plan installable ref = %q, want %q", fr.lastPlanArgs[0].Ref, nixApp1.Refs[runtime.GOOS])
	}
}

// TestRunPlanRealizer_ActionDriver: Each plan action should carry driver="nix".
func TestRunPlanRealizer_ActionDriver(t *testing.T) {
	app1 := nixApp("ripgrep", "nixpkgs#ripgrep")
	mf := nixManifest(app1)

	fr := &fakeRealizer{
		planDiff: realizer.Diff{
			Present: []realizer.Installable{{ID: "ripgrep", Ref: hostRef(app1)}},
		},
	}

	flags := PlanFlags{Manifest: "plan-driver"}
	raw, _ := runPlanRealizer(flags, mf, fr, noopEmitter())
	pr := raw.(*PlanResult)

	if len(pr.Actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(pr.Actions))
	}
	if pr.Actions[0].Driver != "nix" {
		t.Errorf("action Driver = %q, want nix", pr.Actions[0].Driver)
	}
}
