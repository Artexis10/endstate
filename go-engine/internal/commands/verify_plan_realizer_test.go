// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"fmt"
	"runtime"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/driver"
	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/provision"
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
	raw, envelopeErr := runVerifyRealizer(flags, mf, fr, noopEmitter(), nil, nil)

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
	raw, envelopeErr := runVerifyRealizer(flags, mf, fr, noopEmitter(), nil, nil)

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
	_, _ = runVerifyRealizer(flags, mf, fr, noopEmitter(), nil, nil)

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
	raw, envelopeErr := runVerifyRealizer(flags, mf, fr, noopEmitter(), nil, nil)

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
	raw, envelopeErr := runPlanRealizer(flags, mf, fr, noopEmitter(), nil, nil)

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
	raw, _ := runPlanRealizer(flags, mf, fr, noopEmitter(), nil, nil)
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
	raw, _ := runPlanRealizer(flags, mf, fr, noopEmitter(), nil, nil)
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
	_, _ = runPlanRealizer(flags, mf, fr, noopEmitter(), nil, nil)

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
	_, envelopeErr := runPlanRealizer(flags, mf, fr, noopEmitter(), nil, nil)

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
	raw, _ := runPlanRealizer(flags, mf, fr, noopEmitter(), nil, nil)
	pr := raw.(*PlanResult)

	if len(pr.Actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(pr.Actions))
	}
	if pr.Actions[0].Driver != "nix" {
		t.Errorf("action Driver = %q, want nix", pr.Actions[0].Driver)
	}
}

// ---------------------------------------------------------------------------
// home-manager verify tests (HomeGenerationReader seam)
// ---------------------------------------------------------------------------

// seedHMGeneration writes a Provisioning Generation recording a home-manager
// config with the given generation number. The test must set ENDSTATE_ROOT.
func seedHMGeneration(t *testing.T, hmGen int) {
	t.Helper()
	if err := provision.Write(&provision.Generation{
		Backend: "nix",
		HomeManager: &provision.HomeGenRef{
			Flake:      "/dot#me",
			Generation: hmGen,
		},
	}); err != nil {
		t.Fatalf("seedHMGeneration: provision.Write: %v", err)
	}
}

// hmManifest builds a Manifest with one nix app and a homeManager block.
func hmManifest(app manifest.App) *manifest.Manifest {
	mf := nixManifest(app)
	mf.HomeManager = &manifest.HomeManagerConfig{Flake: "/dot#me"}
	return mf
}

// TestRunVerifyRealizer_HomeManager_Pass: declared hm, active==recorded → one
// home-manager item with status "pass"; counts in summary.
func TestRunVerifyRealizer_HomeManager_Pass(t *testing.T) {
	t.Setenv("ENDSTATE_ROOT", t.TempDir())
	seedHMGeneration(t, 5)

	app := nixApp("ripgrep", "nixpkgs#ripgrep")
	mf := hmManifest(app)

	fr := &fakeRealizer{
		currentSet: realizer.Set{
			Generation: 3,
			Elements:   map[string]realizer.Element{"ripgrep": {Name: "ripgrep", AttrPath: "ripgrep"}},
		},
		activeHomeGen: 5, // matches recorded
	}

	flags := VerifyFlags{Manifest: "hm-pass"}
	raw, eerr := runVerifyRealizer(flags, mf, fr, noopEmitter(), nil, nil)
	if eerr != nil {
		t.Fatalf("unexpected envelope error: %v", eerr)
	}
	vr := raw.(*VerifyResult)

	// Find the home-manager item.
	var hmItem *VerifyItem
	for i := range vr.Results {
		if vr.Results[i].Type == "home-manager" {
			hmItem = &vr.Results[i]
			break
		}
	}
	if hmItem == nil {
		t.Fatal("expected a home-manager result item, got none")
	}
	if hmItem.Status != "pass" {
		t.Errorf("home-manager item Status = %q, want pass", hmItem.Status)
	}
	if hmItem.ID != "home-manager" {
		t.Errorf("home-manager item ID = %q, want home-manager", hmItem.ID)
	}
	// The pass item must be counted.
	if vr.Summary.Pass != 2 { // ripgrep + hm
		t.Errorf("Summary.Pass = %d, want 2", vr.Summary.Pass)
	}
	if vr.Summary.Fail != 0 {
		t.Errorf("Summary.Fail = %d, want 0", vr.Summary.Fail)
	}
}

// TestRunVerifyRealizer_HomeManager_ConfigDrift: active != recorded → fail
// config_drift, Version and Expected set to active/recorded gen numbers.
func TestRunVerifyRealizer_HomeManager_ConfigDrift(t *testing.T) {
	t.Setenv("ENDSTATE_ROOT", t.TempDir())
	seedHMGeneration(t, 5)

	app := nixApp("ripgrep", "nixpkgs#ripgrep")
	mf := hmManifest(app)

	fr := &fakeRealizer{
		currentSet: realizer.Set{
			Elements: map[string]realizer.Element{"ripgrep": {Name: "ripgrep", AttrPath: "ripgrep"}},
		},
		activeHomeGen: 7, // differs from recorded (5)
	}

	flags := VerifyFlags{Manifest: "hm-drift"}
	raw, eerr := runVerifyRealizer(flags, mf, fr, noopEmitter(), nil, nil)
	if eerr != nil {
		t.Fatalf("unexpected envelope error: %v", eerr)
	}
	vr := raw.(*VerifyResult)

	var hmItem *VerifyItem
	for i := range vr.Results {
		if vr.Results[i].Type == "home-manager" {
			hmItem = &vr.Results[i]
			break
		}
	}
	if hmItem == nil {
		t.Fatal("expected a home-manager result item, got none")
	}
	if hmItem.Status != "fail" {
		t.Errorf("Status = %q, want fail", hmItem.Status)
	}
	if hmItem.Reason != driver.ReasonConfigDrift {
		t.Errorf("Reason = %q, want config_drift", hmItem.Reason)
	}
	if hmItem.Version != fmt.Sprintf("%d", 7) {
		t.Errorf("Version (active) = %q, want 7", hmItem.Version)
	}
	if hmItem.Expected != fmt.Sprintf("%d", 5) {
		t.Errorf("Expected (recorded) = %q, want 5", hmItem.Expected)
	}
	if vr.Summary.Fail != 1 {
		t.Errorf("Summary.Fail = %d, want 1 (hm drift)", vr.Summary.Fail)
	}
}

// TestRunVerifyRealizer_HomeManager_Missing: active==0 → fail missing.
func TestRunVerifyRealizer_HomeManager_Missing(t *testing.T) {
	t.Setenv("ENDSTATE_ROOT", t.TempDir())
	seedHMGeneration(t, 5)

	app := nixApp("ripgrep", "nixpkgs#ripgrep")
	mf := hmManifest(app)

	fr := &fakeRealizer{
		currentSet: realizer.Set{
			Elements: map[string]realizer.Element{"ripgrep": {Name: "ripgrep", AttrPath: "ripgrep"}},
		},
		activeHomeGen: 0, // nothing active
	}

	flags := VerifyFlags{Manifest: "hm-missing"}
	raw, eerr := runVerifyRealizer(flags, mf, fr, noopEmitter(), nil, nil)
	if eerr != nil {
		t.Fatalf("unexpected envelope error: %v", eerr)
	}
	vr := raw.(*VerifyResult)

	var hmItem *VerifyItem
	for i := range vr.Results {
		if vr.Results[i].Type == "home-manager" {
			hmItem = &vr.Results[i]
			break
		}
	}
	if hmItem == nil {
		t.Fatal("expected a home-manager result item, got none")
	}
	if hmItem.Status != "fail" {
		t.Errorf("Status = %q, want fail", hmItem.Status)
	}
	if hmItem.Reason != driver.ReasonMissing {
		t.Errorf("Reason = %q, want missing", hmItem.Reason)
	}
}

// TestRunVerifyRealizer_HomeManager_NoRecordedGen_Missing: no history at all,
// active==0 → fail missing.
func TestRunVerifyRealizer_HomeManager_NoRecordedGen_Missing(t *testing.T) {
	t.Setenv("ENDSTATE_ROOT", t.TempDir())
	// No generations written — empty history.

	app := nixApp("ripgrep", "nixpkgs#ripgrep")
	mf := hmManifest(app)

	fr := &fakeRealizer{
		currentSet:    realizer.Set{Elements: map[string]realizer.Element{}},
		activeHomeGen: 0,
	}

	flags := VerifyFlags{Manifest: "hm-no-history"}
	raw, eerr := runVerifyRealizer(flags, mf, fr, noopEmitter(), nil, nil)
	if eerr != nil {
		t.Fatalf("unexpected envelope error: %v", eerr)
	}
	vr := raw.(*VerifyResult)

	var hmItem *VerifyItem
	for i := range vr.Results {
		if vr.Results[i].Type == "home-manager" {
			hmItem = &vr.Results[i]
			break
		}
	}
	if hmItem == nil {
		t.Fatal("expected a home-manager result item, got none")
	}
	if hmItem.Status != "fail" {
		t.Errorf("Status = %q, want fail", hmItem.Status)
	}
	if hmItem.Reason != driver.ReasonMissing {
		t.Errorf("Reason = %q, want missing", hmItem.Reason)
	}
}

// TestRunVerifyRealizer_NoHomeManager_NoHmItem: manifest without homeManager →
// no home-manager item, existing package verify unaffected.
func TestRunVerifyRealizer_NoHomeManager_NoHmItem(t *testing.T) {
	t.Setenv("ENDSTATE_ROOT", t.TempDir())

	app := nixApp("ripgrep", "nixpkgs#ripgrep")
	mf := nixManifest(app) // no HomeManager field

	fr := &fakeRealizer{
		currentSet: realizer.Set{
			Elements: map[string]realizer.Element{"ripgrep": {Name: "ripgrep", AttrPath: "ripgrep"}},
		},
		activeHomeGen: 3,
	}

	flags := VerifyFlags{Manifest: "no-hm-field"}
	raw, eerr := runVerifyRealizer(flags, mf, fr, noopEmitter(), nil, nil)
	if eerr != nil {
		t.Fatalf("unexpected envelope error: %v", eerr)
	}
	vr := raw.(*VerifyResult)

	for _, item := range vr.Results {
		if item.Type == "home-manager" {
			t.Errorf("unexpected home-manager item in results when manifest has no homeManager block")
		}
	}
	if vr.Summary.Total != 1 {
		t.Errorf("Summary.Total = %d, want 1 (only ripgrep)", vr.Summary.Total)
	}
	if vr.Summary.Pass != 1 {
		t.Errorf("Summary.Pass = %d, want 1", vr.Summary.Pass)
	}
}

// minimalRealizer implements only realizer.Realizer — NOT HomeGenerationReader —
// so the verify path must skip the hm check when using it.
type minimalRealizer struct {
	currentSet realizer.Set
}

func (m *minimalRealizer) Name() string                                           { return "minimal" }
func (m *minimalRealizer) Current() (realizer.Set, error)                         { return m.currentSet, nil }
func (m *minimalRealizer) Plan([]realizer.Installable) (realizer.Diff, error)     { return realizer.Diff{}, nil }
func (m *minimalRealizer) Realize([]realizer.Installable) (realizer.Result, error) {
	return realizer.Result{}, nil
}

// TestRunVerifyRealizer_NoHomeGenerationReader_Skips: realizer does not implement
// HomeGenerationReader → no home-manager item even when manifest declares hm.
func TestRunVerifyRealizer_NoHomeGenerationReader_Skips(t *testing.T) {
	t.Setenv("ENDSTATE_ROOT", t.TempDir())
	seedHMGeneration(t, 5)

	app := nixApp("ripgrep", "nixpkgs#ripgrep")
	mf := hmManifest(app)

	mr := &minimalRealizer{
		currentSet: realizer.Set{
			Elements: map[string]realizer.Element{"ripgrep": {Name: "ripgrep", AttrPath: "ripgrep"}},
		},
	}

	flags := VerifyFlags{Manifest: "no-hgr"}
	raw, eerr := runVerifyRealizer(flags, mf, mr, noopEmitter(), nil, nil)
	if eerr != nil {
		t.Fatalf("unexpected envelope error: %v", eerr)
	}
	vr := raw.(*VerifyResult)

	for _, item := range vr.Results {
		if item.Type == "home-manager" {
			t.Errorf("unexpected home-manager item: backend does not implement HomeGenerationReader")
		}
	}
}
