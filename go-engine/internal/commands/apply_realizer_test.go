// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"os"
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
