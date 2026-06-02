// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/events"
	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/provision"
	"github.com/Artexis10/endstate/go-engine/internal/realizer"
)

// ---------------------------------------------------------------------------
// fakeRealizer — configurable stub implementing realizer.Realizer
// ---------------------------------------------------------------------------

// fakeRealizer is a scriptable test double for realizer.Realizer. Fields that
// hold return values are used directly by the interface methods; counter fields
// let assertions verify call counts and captured arguments without depending on
// emitter output.
type fakeRealizer struct {
	// --- scripted returns ---
	currentSet realizer.Set
	currentErr error

	planDiff realizer.Diff
	planErr  error

	realizeResult realizer.Result

	// --- observation ---
	currentCalls    int
	planCalls       int
	realizeCalls    int
	lastPlanArgs    []realizer.Installable
	lastRealizeArgs []realizer.Installable

	// --- rollback (Phase 3) ---
	caps            provision.Capabilities // reported via CapabilityReporter
	rollbackErr     error                  // scripted Rollback return
	rollbackCalls   int
	lastRollbackArg int

	// --- prune / convergence (Phase 5) ---
	removeResult   realizer.Result // scripted Remove return
	removeCalls    int
	lastRemoveArgs []string

	// --- home-manager activation (config stage) ---
	homeGenNum      int   // scripted ActivateHome generation
	homeErr         error // scripted ActivateHome error
	activateCalls   int
	lastActivateArg string

	// --- home-manager config rollback ---
	homeRollbackGen     int   // scripted RollbackHome (new forward) generation
	homeRollbackErr     error // scripted RollbackHome error
	homeRollbackCalls   int
	lastHomeRollbackArg int
}

// RollbackHome satisfies realizer.HomeRollbacker, recording the requested
// home-manager generation and returning the scripted new generation/error. Its
// presence makes *fakeRealizer a HomeRollbacker, so the config-rollback path is
// exercised host-independently.
func (f *fakeRealizer) RollbackHome(generation int) (int, error) {
	f.homeRollbackCalls++
	f.lastHomeRollbackArg = generation
	return f.homeRollbackGen, f.homeRollbackErr
}

// ActivateHome satisfies realizer.HomeActivator, recording the requested flake
// and returning the scripted generation/error. Its presence makes *fakeRealizer a
// HomeActivator, so the config stage is exercised host-independently.
func (f *fakeRealizer) ActivateHome(flake string) (int, error) {
	f.activateCalls++
	f.lastActivateArg = flake
	return f.homeGenNum, f.homeErr
}

// Remove satisfies realizer.Pruner, recording the requested element names and
// returning the scripted result. Its presence makes *fakeRealizer a Pruner, so
// the convergence (--prune) path is exercised host-independently.
func (f *fakeRealizer) Remove(names []string) (realizer.Result, error) {
	f.removeCalls++
	f.lastRemoveArgs = names
	return f.removeResult, nil
}

// Capabilities satisfies provision.CapabilityReporter so the rollback command can
// discover native-rollback eligibility. Zero value reports all-false.
func (f *fakeRealizer) Capabilities() provision.Capabilities { return f.caps }

// Rollback satisfies provision.Rollbacker, recording the requested native version.
func (f *fakeRealizer) Rollback(to int) error {
	f.rollbackCalls++
	f.lastRollbackArg = to
	return f.rollbackErr
}

func (f *fakeRealizer) Name() string { return "nix" }

func (f *fakeRealizer) Current() (realizer.Set, error) {
	f.currentCalls++
	return f.currentSet, f.currentErr
}

func (f *fakeRealizer) Plan(desired []realizer.Installable) (realizer.Diff, error) {
	f.planCalls++
	f.lastPlanArgs = desired
	return f.planDiff, f.planErr
}

func (f *fakeRealizer) Realize(toAdd []realizer.Installable) (realizer.Result, error) {
	f.realizeCalls++
	f.lastRealizeArgs = toAdd
	return f.realizeResult, nil
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// nixApp returns an App whose package ref is keyed by the CURRENT host OS
// (runtime.GOOS). The realizer path resolves app.Refs[runtime.GOOS] strictly,
// so keying by the host makes these fixtures exercise the nix path on whatever
// OS the test runs on — linux, darwin, OR windows CI (where the realizer
// functions are still invoked directly by these unit tests).
func nixApp(id, ref string) manifest.App {
	return manifest.App{
		ID:          id,
		DisplayName: id,
		Refs:        map[string]string{runtime.GOOS: ref},
	}
}

// foreignRefApp returns an App whose only ref is for an OS that is NOT the
// current host, so the realizer path always SKIPS it. Used to test the
// no-host-ref skip behavior deterministically on any OS.
func foreignRefApp(id, ref string) manifest.App {
	foreign := "windows"
	if runtime.GOOS == "windows" {
		foreign = "linux"
	}
	return manifest.App{ID: id, Refs: map[string]string{foreign: ref}}
}

// hostRef returns the flakeref for the current GOOS from an app, matching
// the strict host-only lookup in apply_realizer.go (app.Refs[runtime.GOOS]).
func hostRef(app manifest.App) string {
	return app.Refs[runtime.GOOS]
}

// nixManifest builds an in-memory Manifest from the given apps (each typically
// built with nixApp, so they carry a ref for the current host OS).
func nixManifest(apps ...manifest.App) *manifest.Manifest {
	return &manifest.Manifest{
		Version: 1,
		Name:    "nix-test",
		Apps:    apps,
	}
}

// withFakeRealizer replaces newRealizerFn with one that returns fr, calls f,
// then restores the original factory.
func withFakeRealizer(fr *fakeRealizer, f func()) {
	orig := newRealizerFn
	newRealizerFn = func() (realizer.Realizer, error) { return fr, nil }
	defer func() { newRealizerFn = orig }()
	f()
}

// noopEmitter returns a no-op emitter (streaming=false, discards all events).
func noopEmitter() *events.Emitter {
	return events.NewEmitter("test-run", false)
}

// ---------------------------------------------------------------------------
// runApplyRealizer tests
// ---------------------------------------------------------------------------

// TestRunApplyRealizer_AllToAdd_Success: Plan returns all packages as ToAdd,
// Realize returns Advanced:true with no error.
// Expected: Summary.Success==2, both actions status "installed".
func TestRunApplyRealizer_AllToAdd_Success(t *testing.T) {
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
		realizeResult: realizer.Result{
			Advanced:       true,
			FromGeneration: 1,
			ToGeneration:   2,
			After: realizer.Set{
				Generation: 2,
				Elements: map[string]realizer.Element{
					"ripgrep": {Name: "ripgrep", AttrPath: "ripgrep"},
					"jq":      {Name: "jq", AttrPath: "jq"},
				},
			},
		},
	}

	flags := ApplyFlags{Manifest: "nix-test", DryRun: false}
	raw, envelopeErr := runApplyRealizer(flags, mf, fr, noopEmitter(), "run-1", nil, nil)

	if envelopeErr != nil {
		t.Fatalf("runApplyRealizer returned envelope error: %v", envelopeErr)
	}
	result, ok := raw.(*ApplyResult)
	if !ok {
		t.Fatalf("expected *ApplyResult, got %T", raw)
	}

	if result.Summary.Success != 2 {
		t.Errorf("Summary.Success = %d, want 2", result.Summary.Success)
	}
	if result.Summary.Failed != 0 {
		t.Errorf("Summary.Failed = %d, want 0", result.Summary.Failed)
	}
	for _, action := range result.Actions {
		if action.Status != "installed" {
			t.Errorf("action %q: Status = %q, want installed", action.ID, action.Status)
		}
	}
	if fr.realizeCalls != 1 {
		t.Errorf("Realize called %d times, want 1", fr.realizeCalls)
	}
}

// TestRunApplyRealizer_SystemicError: Realize returns a systemic error
// (ErrRealizerUnavailable).
// Expected: non-nil envelope.Error with code REALIZER_UNAVAILABLE, nil data,
// and raw Nix text confined to Detail (not Message).
func TestRunApplyRealizer_SystemicError(t *testing.T) {
	app1 := nixApp("ripgrep", "nixpkgs#ripgrep")
	app2 := nixApp("jq", "nixpkgs#jq")
	mf := nixManifest(app1, app2)

	ins1 := realizer.Installable{ID: "ripgrep", Ref: hostRef(app1)}
	ins2 := realizer.Installable{ID: "jq", Ref: hostRef(app2)}

	rawText := "nix daemon error: connection refused"
	fr := &fakeRealizer{
		planDiff: realizer.Diff{
			ToAdd:   []realizer.Installable{ins1, ins2},
			Present: nil,
		},
		realizeResult: realizer.Result{
			Err: &realizer.Error{
				Code:    envelope.ErrRealizerUnavailable,
				Subcode: "daemon",
				Stage:   "spawn",
				Raw:     rawText,
			},
		},
	}

	flags := ApplyFlags{Manifest: "nix-test", DryRun: false}
	raw, envelopeErr := runApplyRealizer(flags, mf, fr, noopEmitter(), "run-2", nil, nil)

	if envelopeErr == nil {
		t.Fatal("expected envelope error for systemic realizer failure, got nil")
	}
	if raw != nil {
		t.Errorf("expected nil data on systemic error, got %T", raw)
	}
	if envelopeErr.Code != envelope.ErrRealizerUnavailable {
		t.Errorf("envelope error code = %q, want REALIZER_UNAVAILABLE", envelopeErr.Code)
	}
	// Raw Nix text must NOT appear in the user-facing message (the moat).
	if strings.Contains(envelopeErr.Message, rawText) {
		t.Errorf("raw nix text leaked into envelope Message: %q", envelopeErr.Message)
	}
	// Raw text should be confined to Detail.
	detailMap, ok := envelopeErr.Detail.(map[string]string)
	if !ok {
		t.Fatalf("expected detail to be map[string]string, got %T", envelopeErr.Detail)
	}
	if detailMap["raw"] != rawText {
		t.Errorf("detail.raw = %q, want %q", detailMap["raw"], rawText)
	}
}

// TestRunApplyRealizer_InstallError: Realize returns a non-systemic install
// error (ErrInstallFailed, subcode "eval").
// Expected: nil envelope.Error, result.Summary.Failed==2, actions status
// "failed"/reason "install_failed", raw Nix text NOT in any action Message.
func TestRunApplyRealizer_InstallError(t *testing.T) {
	app1 := nixApp("ripgrep", "nixpkgs#ripgrep")
	app2 := nixApp("jq", "nixpkgs#jq")
	mf := nixManifest(app1, app2)

	ins1 := realizer.Installable{ID: "ripgrep", Ref: hostRef(app1)}
	ins2 := realizer.Installable{ID: "jq", Ref: hostRef(app2)}

	rawNixText := "error: attribute 'ripgrep' missing in flake output"
	fr := &fakeRealizer{
		planDiff: realizer.Diff{
			ToAdd:   []realizer.Installable{ins1, ins2},
			Present: nil,
		},
		realizeResult: realizer.Result{
			Err: &realizer.Error{
				Code:    envelope.ErrInstallFailed,
				Subcode: "eval",
				Stage:   "eval",
				Raw:     rawNixText,
			},
		},
	}

	flags := ApplyFlags{Manifest: "nix-test", DryRun: false}
	raw, envelopeErr := runApplyRealizer(flags, mf, fr, noopEmitter(), "run-3", nil, nil)

	// Non-systemic: envelope error must be nil.
	if envelopeErr != nil {
		t.Fatalf("expected nil envelope error for per-item install failure, got: %v", envelopeErr)
	}
	if raw == nil {
		t.Fatal("expected non-nil result for per-item install failure")
	}
	result, ok := raw.(*ApplyResult)
	if !ok {
		t.Fatalf("expected *ApplyResult, got %T", raw)
	}

	if result.Summary.Failed != 2 {
		t.Errorf("Summary.Failed = %d, want 2", result.Summary.Failed)
	}
	if result.Summary.Success != 0 {
		t.Errorf("Summary.Success = %d, want 0", result.Summary.Success)
	}
	for _, action := range result.Actions {
		if action.Status != "failed" {
			t.Errorf("action %q: Status = %q, want failed", action.ID, action.Status)
		}
		if action.Reason != "install_failed" {
			t.Errorf("action %q: Reason = %q, want install_failed", action.ID, action.Reason)
		}
		// Raw Nix text must NOT appear in per-item message (the moat).
		if strings.Contains(action.Message, rawNixText) {
			t.Errorf("action %q: raw nix text leaked into Message: %q", action.ID, action.Message)
		}
	}
}

// TestRunApplyRealizer_AllPresent_NoRealize: Plan returns all packages as
// Present (already installed).
// Expected: Summary.Skipped==2, Realize is never called.
func TestRunApplyRealizer_AllPresent_NoRealize(t *testing.T) {
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

	flags := ApplyFlags{Manifest: "nix-test", DryRun: false}
	raw, envelopeErr := runApplyRealizer(flags, mf, fr, noopEmitter(), "run-4", nil, nil)

	if envelopeErr != nil {
		t.Fatalf("runApplyRealizer returned envelope error: %v", envelopeErr)
	}
	result, ok := raw.(*ApplyResult)
	if !ok {
		t.Fatalf("expected *ApplyResult, got %T", raw)
	}

	if fr.realizeCalls != 0 {
		t.Errorf("Realize called %d times, want 0 (all present)", fr.realizeCalls)
	}
	// Already-present nix packages count as skipped in the apply phase.
	if result.Summary.Skipped != 2 {
		t.Errorf("Summary.Skipped = %d, want 2", result.Summary.Skipped)
	}
	if result.Summary.Failed != 0 {
		t.Errorf("Summary.Failed = %d, want 0", result.Summary.Failed)
	}
}

// TestRunApplyRealizer_DryRun: DryRun=true means Plan is called but Realize
// is not, and result.DryRun==true.
func TestRunApplyRealizer_DryRun(t *testing.T) {
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

	flags := ApplyFlags{Manifest: "nix-test", DryRun: true}
	raw, envelopeErr := runApplyRealizer(flags, mf, fr, noopEmitter(), "run-dry", nil, nil)

	if envelopeErr != nil {
		t.Fatalf("runApplyRealizer dry-run returned envelope error: %v", envelopeErr)
	}
	result, ok := raw.(*ApplyResult)
	if !ok {
		t.Fatalf("expected *ApplyResult, got %T", raw)
	}

	if !result.DryRun {
		t.Error("result.DryRun = false, want true")
	}
	if fr.planCalls != 1 {
		t.Errorf("Plan called %d times, want 1", fr.planCalls)
	}
	if fr.realizeCalls != 0 {
		t.Errorf("Realize called %d times in dry-run, want 0", fr.realizeCalls)
	}
}

// TestRunApplyRealizer_DryRun_ViaPackageVar: end-to-end test that exercises
// the newRealizerFn injection path through RunApply, covering the complete
// wiring from the exported command entry-point.
func TestRunApplyRealizer_DryRun_ViaPackageVar(t *testing.T) {
	app1 := nixApp("ripgrep", "nixpkgs#ripgrep")
	app2 := nixApp("jq", "nixpkgs#jq")

	fr := &fakeRealizer{
		planDiff: realizer.Diff{
			ToAdd: []realizer.Installable{
				{ID: "ripgrep", Ref: hostRef(app1)},
				{ID: "jq", Ref: hostRef(app2)},
			},
		},
	}

	// Write a temp manifest file so RunApply can load it via loadManifest.
	tmpDir := t.TempDir()
	manifestPath := tmpDir + "/nix.jsonc"
	manifestContent := `{
		"version": 1,
		"name": "nix-wiring-test",
		"apps": [
			{ "id": "ripgrep", "displayName": "ripgrep", "refs": { "linux": "nixpkgs#ripgrep", "darwin": "nixpkgs#ripgrep" } },
			{ "id": "jq",      "displayName": "jq",      "refs": { "linux": "nixpkgs#jq",      "darwin": "nixpkgs#jq"      } }
		]
	}`
	if err := os.WriteFile(manifestPath, []byte(manifestContent), 0644); err != nil {
		t.Fatalf("could not write temp manifest: %v", err)
	}

	var result *ApplyResult
	withFakeRealizer(fr, func() {
		r, err := RunApply(ApplyFlags{
			Manifest: manifestPath,
			DryRun:   true,
		})
		if err != nil {
			t.Fatalf("RunApply returned envelope error: %v", err)
		}
		result = r.(*ApplyResult)
	})

	if !result.DryRun {
		t.Error("result.DryRun = false, want true")
	}
	if fr.realizeCalls != 0 {
		t.Errorf("Realize called %d times in dry-run via RunApply, want 0", fr.realizeCalls)
	}
}

// ---------------------------------------------------------------------------
// home-manager config stage (gated by --enable-restore + homeManager.flake)
// ---------------------------------------------------------------------------

// TestRunApplyRealizer_HomeManager_ActivatesAndRecords: with --enable-restore
// and a declared homeManager.flake, the config stage activates the config with
// the right flake and records it (flakeref + hm generation) in the Provisioning
// Generation, alongside the package install.
func TestRunApplyRealizer_HomeManager_ActivatesAndRecords(t *testing.T) {
	t.Setenv("ENDSTATE_ROOT", t.TempDir())
	app := nixApp("ripgrep", "nixpkgs#ripgrep")
	mf := nixManifest(app)
	mf.HomeManager = &manifest.HomeManagerConfig{Flake: "/home/me/dotfiles#hugo"}

	ins := realizer.Installable{ID: "ripgrep", Ref: hostRef(app)}
	fr := &fakeRealizer{
		planDiff: realizer.Diff{ToAdd: []realizer.Installable{ins}},
		realizeResult: realizer.Result{
			Advanced: true, FromGeneration: 1, ToGeneration: 2,
			After: realizer.Set{Generation: 2, Elements: map[string]realizer.Element{"ripgrep": {Name: "ripgrep", AttrPath: "ripgrep"}}},
		},
		homeGenNum: 5,
	}

	flags := ApplyFlags{Manifest: "nix-test", EnableRestore: true}
	raw, eerr := runApplyRealizer(flags, mf, fr, noopEmitter(), "run-hm1", nil, nil)
	if eerr != nil {
		t.Fatalf("unexpected envelope error: %v", eerr)
	}
	if _, ok := raw.(*ApplyResult); !ok {
		t.Fatalf("expected *ApplyResult, got %T", raw)
	}
	if fr.activateCalls != 1 {
		t.Errorf("ActivateHome called %d times, want 1", fr.activateCalls)
	}
	if fr.lastActivateArg != "/home/me/dotfiles#hugo" {
		t.Errorf("ActivateHome flake = %q, want the manifest flake", fr.lastActivateArg)
	}
	gens, _ := provision.List()
	if len(gens) != 1 {
		t.Fatalf("want 1 generation, got %d", len(gens))
	}
	hm := gens[0].HomeManager
	if hm == nil || hm.Flake != "/home/me/dotfiles#hugo" || hm.Generation != 5 {
		t.Fatalf("generation HomeManager = %+v, want flake=/home/me/dotfiles#hugo gen=5", hm)
	}
}

// TestRunApplyRealizer_HomeManager_Config_RecordsDeclaredConfig: a homeManager.config
// apply records the user's DECLARED config path in the generation (so capture can
// round-trip it), alongside the engine-generated flake that was actually activated.
func TestRunApplyRealizer_HomeManager_Config_RecordsDeclaredConfig(t *testing.T) {
	t.Setenv("ENDSTATE_ROOT", t.TempDir())
	cfg := filepath.Join(t.TempDir(), "home.nix")
	if err := os.WriteFile(cfg, []byte("{ home.stateVersion = \"24.05\"; }\n"), 0o644); err != nil {
		t.Fatalf("write home.nix: %v", err)
	}
	app := nixApp("ripgrep", "nixpkgs#ripgrep")
	mf := nixManifest(app)
	mf.HomeManager = &manifest.HomeManagerConfig{Config: cfg}

	ins := realizer.Installable{ID: "ripgrep", Ref: hostRef(app)}
	fr := &fakeRealizer{
		planDiff: realizer.Diff{ToAdd: []realizer.Installable{ins}},
		realizeResult: realizer.Result{
			Advanced: true, FromGeneration: 1, ToGeneration: 2,
			After: realizer.Set{Generation: 2, Elements: map[string]realizer.Element{"ripgrep": {Name: "ripgrep", AttrPath: "ripgrep"}}},
		},
		homeGenNum: 7,
	}

	flags := ApplyFlags{Manifest: "nix-test", EnableRestore: true}
	if _, eerr := runApplyRealizer(flags, mf, fr, noopEmitter(), "run-hmcfg", nil, nil); eerr != nil {
		t.Fatalf("unexpected envelope error: %v", eerr)
	}
	gens, _ := provision.List()
	if len(gens) != 1 {
		t.Fatalf("want 1 generation, got %d", len(gens))
	}
	hm := gens[0].HomeManager
	if hm == nil {
		t.Fatalf("generation HomeManager is nil")
	}
	if hm.Config != cfg {
		t.Errorf("HomeManager.Config = %q, want the declared config %q", hm.Config, cfg)
	}
	if hm.Flake == "" {
		t.Error("HomeManager.Flake should record the generated (activated) flake, got empty")
	}
}

// TestRunApplyRealizer_HomeManager_NoFlag_NoActivation: a declared flake without
// --enable-restore does NOT activate (the config gate).
func TestRunApplyRealizer_HomeManager_NoFlag_NoActivation(t *testing.T) {
	app := nixApp("ripgrep", "nixpkgs#ripgrep")
	mf := nixManifest(app)
	mf.HomeManager = &manifest.HomeManagerConfig{Flake: "/dot#me"}
	fr := &fakeRealizer{planDiff: realizer.Diff{Present: []realizer.Installable{{ID: "ripgrep", Ref: hostRef(app)}}}}

	flags := ApplyFlags{Manifest: "nix-test", EnableRestore: false}
	if _, eerr := runApplyRealizer(flags, mf, fr, noopEmitter(), "run-hm2", nil, nil); eerr != nil {
		t.Fatalf("unexpected envelope error: %v", eerr)
	}
	if fr.activateCalls != 0 {
		t.Errorf("ActivateHome called %d times without --enable-restore, want 0", fr.activateCalls)
	}
}

// TestRunApplyRealizer_HomeManager_NoField_NoActivation: --enable-restore with no
// homeManager field does NOT activate (default apply unchanged).
func TestRunApplyRealizer_HomeManager_NoField_NoActivation(t *testing.T) {
	app := nixApp("ripgrep", "nixpkgs#ripgrep")
	mf := nixManifest(app) // no HomeManager
	fr := &fakeRealizer{planDiff: realizer.Diff{Present: []realizer.Installable{{ID: "ripgrep", Ref: hostRef(app)}}}}

	flags := ApplyFlags{Manifest: "nix-test", EnableRestore: true}
	if _, eerr := runApplyRealizer(flags, mf, fr, noopEmitter(), "run-hm3", nil, nil); eerr != nil {
		t.Fatalf("unexpected envelope error: %v", eerr)
	}
	if fr.activateCalls != 0 {
		t.Errorf("ActivateHome called %d times with no homeManager field, want 0", fr.activateCalls)
	}
}

// TestRunApplyRealizer_HomeManager_EmptyFlake_NoActivation: a homeManager block
// with an empty flake does NOT activate.
func TestRunApplyRealizer_HomeManager_EmptyFlake_NoActivation(t *testing.T) {
	app := nixApp("ripgrep", "nixpkgs#ripgrep")
	mf := nixManifest(app)
	mf.HomeManager = &manifest.HomeManagerConfig{Flake: ""}
	fr := &fakeRealizer{planDiff: realizer.Diff{Present: []realizer.Installable{{ID: "ripgrep", Ref: hostRef(app)}}}}

	flags := ApplyFlags{Manifest: "nix-test", EnableRestore: true}
	if _, eerr := runApplyRealizer(flags, mf, fr, noopEmitter(), "run-hm4", nil, nil); eerr != nil {
		t.Fatalf("unexpected envelope error: %v", eerr)
	}
	if fr.activateCalls != 0 {
		t.Errorf("ActivateHome called %d times with empty flake, want 0", fr.activateCalls)
	}
}

// TestRunApplyRealizer_HomeManager_SystemicError: a systemic activation failure
// surfaces a top-level envelope error (nil data), raw text confined to detail.
func TestRunApplyRealizer_HomeManager_SystemicError(t *testing.T) {
	app := nixApp("ripgrep", "nixpkgs#ripgrep")
	mf := nixManifest(app)
	mf.HomeManager = &manifest.HomeManagerConfig{Flake: "/dot#me"}
	rawText := "error: cannot connect to socket"
	fr := &fakeRealizer{
		planDiff: realizer.Diff{Present: []realizer.Installable{{ID: "ripgrep", Ref: hostRef(app)}}},
		homeErr:  &realizer.Error{Code: envelope.ErrRealizerUnavailable, Subcode: "daemon", Stage: "spawn", Raw: rawText},
	}

	flags := ApplyFlags{Manifest: "nix-test", EnableRestore: true}
	data, eerr := runApplyRealizer(flags, mf, fr, noopEmitter(), "run-hm5", nil, nil)
	if eerr == nil {
		t.Fatal("expected envelope error for systemic activation failure, got nil")
	}
	if data != nil {
		t.Errorf("expected nil data on systemic error, got %T", data)
	}
	if eerr.Code != envelope.ErrRealizerUnavailable {
		t.Errorf("code = %q, want REALIZER_UNAVAILABLE", eerr.Code)
	}
	if strings.Contains(eerr.Message, rawText) {
		t.Errorf("raw text leaked into envelope Message: %q", eerr.Message)
	}
}

// TestRunApplyRealizer_HomeManager_ActivationFailed: a non-systemic activation
// failure surfaces INSTALL_FAILED with raw text confined to detail (the moat),
// mirroring the prune-failure precedent.
func TestRunApplyRealizer_HomeManager_ActivationFailed(t *testing.T) {
	app := nixApp("ripgrep", "nixpkgs#ripgrep")
	mf := nixManifest(app)
	mf.HomeManager = &manifest.HomeManagerConfig{Flake: "/dot#bad"}
	rawText := "error: flake does not provide attribute 'homeConfigurations.bad'"
	fr := &fakeRealizer{
		planDiff: realizer.Diff{Present: []realizer.Installable{{ID: "ripgrep", Ref: hostRef(app)}}},
		homeErr:  &realizer.Error{Code: envelope.ErrInstallFailed, Subcode: "eval", Stage: "eval", Raw: rawText},
	}

	flags := ApplyFlags{Manifest: "nix-test", EnableRestore: true}
	_, eerr := runApplyRealizer(flags, mf, fr, noopEmitter(), "run-hm6", nil, nil)
	if eerr == nil {
		t.Fatal("expected envelope error for activation failure, got nil")
	}
	if eerr.Code != envelope.ErrInstallFailed {
		t.Errorf("code = %q, want INSTALL_FAILED", eerr.Code)
	}
	if strings.Contains(eerr.Message, rawText) {
		t.Errorf("raw text leaked into envelope Message: %q", eerr.Message)
	}
	detail, ok := eerr.Detail.(map[string]string)
	if !ok || detail["raw"] != rawText {
		t.Errorf("raw text not confined to detail: %+v", eerr.Detail)
	}
}

// TestRunApplyRealizer_HomeManager_ConfigOnlyRecordsGeneration: when no package
// changed (all present) but a config was activated, a generation is STILL
// recorded (the write-gate counts an activation as "something happened").
func TestRunApplyRealizer_HomeManager_ConfigOnlyRecordsGeneration(t *testing.T) {
	t.Setenv("ENDSTATE_ROOT", t.TempDir())
	app := nixApp("ripgrep", "nixpkgs#ripgrep")
	mf := nixManifest(app)
	mf.HomeManager = &manifest.HomeManagerConfig{Flake: "/dot#me"}
	fr := &fakeRealizer{
		planDiff:   realizer.Diff{Present: []realizer.Installable{{ID: "ripgrep", Ref: hostRef(app)}}},
		homeGenNum: 3,
	}

	flags := ApplyFlags{Manifest: "nix-test", EnableRestore: true}
	if _, eerr := runApplyRealizer(flags, mf, fr, noopEmitter(), "run-hm7", nil, nil); eerr != nil {
		t.Fatalf("unexpected envelope error: %v", eerr)
	}
	if fr.activateCalls != 1 {
		t.Fatalf("ActivateHome called %d times, want 1", fr.activateCalls)
	}
	gens, _ := provision.List()
	if len(gens) != 1 {
		t.Fatalf("config-only activation must still record a generation, got %d", len(gens))
	}
	if gens[0].HomeManager == nil || gens[0].HomeManager.Generation != 3 {
		t.Fatalf("generation HomeManager = %+v, want gen=3", gens[0].HomeManager)
	}
	if len(gens[0].AddedRefs) != 0 || len(gens[0].RemovedRefs) != 0 {
		t.Errorf("config-only generation must record no package changes: added=%v removed=%v", gens[0].AddedRefs, gens[0].RemovedRefs)
	}
}

// TestRunApply_WingetPath_NeverActivatesHome: on the driver (winget) path a
// declared homeManager.flake is ignored — no activation, no recorded config.
func TestRunApply_WingetPath_NeverActivatesHome(t *testing.T) {
	t.Setenv("ENDSTATE_ROOT", t.TempDir())
	md := &mockDriver{installed: map[string]bool{}}
	tmpDir := t.TempDir()
	manifestPath := tmpDir + "/m.jsonc"
	manifestContent := `{
		"name": "winget-hm-test",
		"apps": [ { "id": "a", "refs": { "windows": "Vendor.A" } } ],
		"homeManager": { "flake": "/home/me/dotfiles#hugo" }
	}`
	if err := os.WriteFile(manifestPath, []byte(manifestContent), 0644); err != nil {
		t.Fatal(err)
	}

	var eerr *envelope.Error
	withMockDriver(md, func() {
		_, eerr = RunApply(ApplyFlags{Manifest: manifestPath, EnableRestore: true})
	})
	if eerr != nil {
		t.Fatalf("winget path with homeManager set must not error: %v", eerr)
	}
	gens, _ := provision.List()
	for _, g := range gens {
		if g.HomeManager != nil {
			t.Fatalf("winget path recorded a home-manager config: %+v", g.HomeManager)
		}
	}
}

// writeHomeNix writes a tiny home.nix beside a (notional) manifest and returns the
// manifest path whose dir holds it, so the config-file input resolves relative to
// the manifest exactly as in production.
func writeHomeNix(t *testing.T) (manifestPath, configRel string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "home.nix"), []byte("{ ... }:\n{\n  programs.git.userName = \"smoke\";\n}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(dir, "machine.jsonc"), "home.nix"
}

// TestRunApplyRealizer_HomeManagerConfig_GeneratesAndActivates: with
// --enable-restore and a homeManager.config (a path to a home.nix), the config
// stage GENERATES a wrapper flake, activates the GENERATED flakeref (not the raw
// config path) via the existing ActivateHome, writes an inspectable flake to the
// state dir, and records the generated flakeref in the Provisioning Generation.
func TestRunApplyRealizer_HomeManagerConfig_GeneratesAndActivates(t *testing.T) {
	t.Setenv("ENDSTATE_ROOT", t.TempDir())
	manifestPath, cfgRel := writeHomeNix(t)

	app := nixApp("ripgrep", "nixpkgs#ripgrep")
	mf := nixManifest(app)
	mf.HomeManager = &manifest.HomeManagerConfig{Config: cfgRel}

	ins := realizer.Installable{ID: "ripgrep", Ref: hostRef(app)}
	fr := &fakeRealizer{
		planDiff: realizer.Diff{ToAdd: []realizer.Installable{ins}},
		realizeResult: realizer.Result{
			Advanced: true, FromGeneration: 1, ToGeneration: 2,
			After: realizer.Set{Generation: 2, Elements: map[string]realizer.Element{"ripgrep": {Name: "ripgrep", AttrPath: "ripgrep"}}},
		},
		homeGenNum: 9,
	}

	flags := ApplyFlags{Manifest: manifestPath, EnableRestore: true}
	raw, eerr := runApplyRealizer(flags, mf, fr, noopEmitter(), "run-cfg1", nil, nil)
	if eerr != nil {
		t.Fatalf("unexpected envelope error: %v", eerr)
	}
	res, ok := raw.(*ApplyResult)
	if !ok {
		t.Fatalf("expected *ApplyResult, got %T", raw)
	}
	if fr.activateCalls != 1 {
		t.Fatalf("ActivateHome calls = %d, want 1", fr.activateCalls)
	}

	// ActivateHome received the GENERATED flakeref (<stateDir>/home-manager/<name>#<name>),
	// NOT the raw home.nix path.
	if !strings.Contains(fr.lastActivateArg, filepath.FromSlash("home-manager")) || !strings.Contains(fr.lastActivateArg, "#") {
		t.Fatalf("ActivateHome arg = %q, want a generated <dir>#<name> flakeref", fr.lastActivateArg)
	}
	if strings.HasSuffix(fr.lastActivateArg, "home.nix") {
		t.Fatalf("ActivateHome got the raw config path, want a generated flakeref: %q", fr.lastActivateArg)
	}

	// The generated flake is on disk: flake.nix + a copy of the user's home.nix.
	dir := strings.SplitN(fr.lastActivateArg, "#", 2)[0]
	if _, err := os.Stat(filepath.Join(dir, "flake.nix")); err != nil {
		t.Fatalf("generated flake.nix not written: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "home.nix")); err != nil {
		t.Fatalf("user home.nix not copied into the generated flake dir: %v", err)
	}

	// Recorded under the generated flakeref.
	gens, _ := provision.List()
	if len(gens) != 1 || gens[0].HomeManager == nil ||
		gens[0].HomeManager.Flake != fr.lastActivateArg || gens[0].HomeManager.Generation != 9 {
		t.Fatalf("generation HomeManager = %+v, want flake=%q gen=9", gens, fr.lastActivateArg)
	}

	// The result surfaces the generated, activated flake.
	if res.HomeManager == nil || !res.HomeManager.Generated || !res.HomeManager.Activated ||
		res.HomeManager.Flake != fr.lastActivateArg {
		t.Fatalf("result HomeManager = %+v, want generated+activated with the flakeref", res.HomeManager)
	}
}

// TestRunApplyRealizer_HomeManagerConfig_DryRunRevealsNoActivate: --dry-run with a
// homeManager.config GENERATES the inspectable flake and REVEALS its path, but
// activates nothing.
func TestRunApplyRealizer_HomeManagerConfig_DryRunRevealsNoActivate(t *testing.T) {
	t.Setenv("ENDSTATE_ROOT", t.TempDir())
	manifestPath, cfgRel := writeHomeNix(t)

	app := nixApp("ripgrep", "nixpkgs#ripgrep")
	mf := nixManifest(app)
	mf.HomeManager = &manifest.HomeManagerConfig{Config: cfgRel}
	fr := &fakeRealizer{planDiff: realizer.Diff{Present: []realizer.Installable{{ID: "ripgrep", Ref: hostRef(app)}}}}

	flags := ApplyFlags{Manifest: manifestPath, EnableRestore: true, DryRun: true}
	raw, eerr := runApplyRealizer(flags, mf, fr, noopEmitter(), "run-cfg2", nil, nil)
	if eerr != nil {
		t.Fatalf("unexpected envelope error: %v", eerr)
	}
	res := raw.(*ApplyResult)
	if !res.DryRun {
		t.Fatal("expected a dry-run result")
	}
	if fr.activateCalls != 0 {
		t.Fatalf("dry-run must NOT activate; ActivateHome calls = %d", fr.activateCalls)
	}
	if res.HomeManager == nil || res.HomeManager.Activated {
		t.Fatalf("dry-run result HomeManager = %+v, want revealed but not activated", res.HomeManager)
	}
	if !res.HomeManager.Generated || res.HomeManager.Flake == "" {
		t.Fatalf("dry-run must reveal the generated flake: %+v", res.HomeManager)
	}
	// The generated flake exists on disk so the user can inspect it even on dry-run.
	dir := strings.SplitN(res.HomeManager.Flake, "#", 2)[0]
	if _, err := os.Stat(filepath.Join(dir, "flake.nix")); err != nil {
		t.Fatalf("dry-run did not write the inspectable flake: %v", err)
	}
}

// TestRunApply_WingetPath_ConfigNeverGenerates: on the driver (winget) path a
// declared homeManager.config is ignored — the engine never generates a flake and
// records no config.
func TestRunApply_WingetPath_ConfigNeverGenerates(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ENDSTATE_ROOT", root)
	md := &mockDriver{installed: map[string]bool{}}
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "home.nix"), []byte("{ ... }: {}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(tmpDir, "m.jsonc")
	manifestContent := `{
		"name": "winget-hm-cfg-test",
		"apps": [ { "id": "a", "refs": { "windows": "Vendor.A" } } ],
		"homeManager": { "config": "home.nix" }
	}`
	if err := os.WriteFile(manifestPath, []byte(manifestContent), 0644); err != nil {
		t.Fatal(err)
	}

	var eerr *envelope.Error
	withMockDriver(md, func() {
		_, eerr = RunApply(ApplyFlags{Manifest: manifestPath, EnableRestore: true})
	})
	if eerr != nil {
		t.Fatalf("winget path with homeManager.config must not error: %v", eerr)
	}
	gens, _ := provision.List()
	for _, g := range gens {
		if g.HomeManager != nil {
			t.Fatalf("winget path recorded a home-manager config: %+v", g.HomeManager)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "state", "home-manager")); !os.IsNotExist(err) {
		t.Fatalf("winget path generated a home-manager flake dir (err=%v); it must never generate", err)
	}
}
