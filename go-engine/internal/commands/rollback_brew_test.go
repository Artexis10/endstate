// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/driver"
	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/provision"
)

// ---------------------------------------------------------------------------
// Best-effort brew rollback composed with the native (Nix) realizer rollback.
//
// On darwin RunRollback short-circuits to the realizer; these tests prove the
// brew uninstall lane runs ALONGSIDE the native rollback for both explicit
// targets and bare mixed-run rollback. Both seams are overridden (newRealizerFn via the
// capable fakeRealizer, newBrewDriverFn via withRealizerAndBrew) so the path is
// host-independent — overriding only one would run the wrong backend on a given
// OS (the recurring two-seam gotcha).
// ---------------------------------------------------------------------------

// brewRollbackGen returns the (single) Backend:"brew", Rollback-marked generation
// in the recorded history, or nil if none was appended.
func brewRollbackGen(t *testing.T) *provision.Generation {
	t.Helper()
	gens, err := provision.List()
	if err != nil {
		t.Fatalf("provision.List: %v", err)
	}
	var found *provision.Generation
	for _, g := range gens {
		if g.Backend == "brew" && g.Rollback {
			found = g
		}
	}
	return found
}

// TestRollback_Realizer_BrewLane_UninstallsAfterTarget: `rollback --to N --confirm`
// performs the native rollback AND uninstalls the brew refs added after N,
// recording a separate Backend:"brew" rollback generation.
func TestRollback_Realizer_BrewLane_UninstallsAfterTarget(t *testing.T) {
	t.Setenv("ENDSTATE_ROOT", t.TempDir())
	if err := provision.Write(&provision.Generation{Backend: "nix", Native: "4", AddedRefs: []string{"nixpkgs#jq"}}); err != nil {
		t.Fatalf("seed nix gen: %v", err) // gen 1
	}
	if err := provision.Write(&provision.Generation{Backend: "brew", AddedRefs: []string{"hello", "cask:firefox"}}); err != nil {
		t.Fatalf("seed brew gen: %v", err) // gen 2
	}

	fr := capableRealizer(setOf(4, "jq"))
	fb := &fakeBrewDriver{installed: map[string]bool{"hello": true, "cask:firefox": true}}
	var result *RollbackResult
	withRealizerAndBrew(fr, func() (driver.Driver, error) { return fb, nil }, func() {
		raw, env := RunRollback(RollbackFlags{To: "1", Confirm: true})
		if env != nil {
			t.Fatalf("unexpected envelope error: %+v", env)
		}
		result = raw.(*RollbackResult)
	})

	if fr.rollbackCalls != 1 || fr.lastRollbackArg != 4 {
		t.Errorf("want native Rollback(4) once, got calls=%d arg=%d", fr.rollbackCalls, fr.lastRollbackArg)
	}
	if !sameSet(fb.uninstallCalls, []string{"hello", "cask:firefox"}) {
		t.Errorf("brew uninstall calls = %v, want the set {hello, cask:firefox}", fb.uninstallCalls)
	}
	if !sameSet(result.RemovedRefs, []string{"hello", "cask:firefox"}) || result.Partial {
		t.Errorf("RemovedRefs=%v partial=%v, want both removed, not partial", result.RemovedRefs, result.Partial)
	}
	if result.Warning == "" {
		t.Error("expected the untracked-dependencies warning on a brew rollback")
	}
	g := brewRollbackGen(t)
	if g == nil {
		t.Fatal("expected a Backend:brew rollback-marked generation")
	}
	if !sameSet(g.RemovedRefs, []string{"hello", "cask:firefox"}) || len(g.AddedRefs) != 0 {
		t.Errorf("brew rollback gen wrong: removed=%v added=%v", g.RemovedRefs, g.AddedRefs)
	}
}

// TestRollback_Realizer_BrewLane_PartialFailure: a per-ref brew uninstall failure
// is tolerated — reported partial, native rollback stands, run does not error.
func TestRollback_Realizer_BrewLane_PartialFailure(t *testing.T) {
	t.Setenv("ENDSTATE_ROOT", t.TempDir())
	if err := provision.Write(&provision.Generation{Backend: "nix", Native: "4", AddedRefs: []string{"nixpkgs#jq"}}); err != nil {
		t.Fatalf("seed nix gen: %v", err) // gen 1
	}
	if err := provision.Write(&provision.Generation{Backend: "brew", AddedRefs: []string{"a", "b"}}); err != nil {
		t.Fatalf("seed brew gen: %v", err) // gen 2
	}

	fr := capableRealizer(setOf(4, "jq"))
	fb := &fakeBrewDriver{
		installed: map[string]bool{"a": true, "b": true},
		uninstallResults: map[string]*driver.UninstallResult{
			"b": {Status: driver.StatusFailed, Message: "in use by another package"},
		},
	}
	var result *RollbackResult
	withRealizerAndBrew(fr, func() (driver.Driver, error) { return fb, nil }, func() {
		raw, env := RunRollback(RollbackFlags{To: "1", Confirm: true})
		if env != nil {
			t.Fatalf("a per-item brew failure must not top-level error: %+v", env)
		}
		result = raw.(*RollbackResult)
	})

	if fr.rollbackCalls != 1 {
		t.Errorf("native Rollback called %d times, want 1 (must still run)", fr.rollbackCalls)
	}
	if !result.Partial || len(result.FailedRefs) != 1 || result.FailedRefs[0] != "b" {
		t.Errorf("want partial with FailedRefs=[b], got partial=%v failed=%v", result.Partial, result.FailedRefs)
	}
	if len(result.RemovedRefs) != 1 || result.RemovedRefs[0] != "a" {
		t.Errorf("want RemovedRefs=[a], got %v", result.RemovedRefs)
	}
	if g := brewRollbackGen(t); g == nil || !g.Partial {
		t.Errorf("expected a partial Backend:brew rollback generation, got %+v", g)
	}
}

// TestRollback_Realizer_BrewLane_DryRun: --dry-run previews the brew removals
// without uninstalling, without the native rollback, and without a new generation.
func TestRollback_Realizer_BrewLane_DryRun(t *testing.T) {
	t.Setenv("ENDSTATE_ROOT", t.TempDir())
	if err := provision.Write(&provision.Generation{Backend: "nix", Native: "4", AddedRefs: []string{"nixpkgs#jq"}}); err != nil {
		t.Fatalf("seed nix gen: %v", err) // gen 1
	}
	if err := provision.Write(&provision.Generation{Backend: "brew", AddedRefs: []string{"hello"}}); err != nil {
		t.Fatalf("seed brew gen: %v", err) // gen 2
	}

	fr := capableRealizer(setOf(4, "jq"))
	fb := &fakeBrewDriver{installed: map[string]bool{"hello": true}}
	var result *RollbackResult
	withRealizerAndBrew(fr, func() (driver.Driver, error) { return fb, nil }, func() {
		raw, env := RunRollback(RollbackFlags{To: "1", DryRun: true}) // no confirm
		if env != nil {
			t.Fatalf("unexpected envelope error: %+v", env)
		}
		result = raw.(*RollbackResult)
	})

	if !result.DryRun {
		t.Error("result.DryRun = false, want true")
	}
	if len(result.RemovedRefs) != 1 || result.RemovedRefs[0] != "hello" {
		t.Errorf("dry-run preview RemovedRefs=%v, want [hello]", result.RemovedRefs)
	}
	if len(fb.uninstallCalls) != 0 {
		t.Errorf("dry-run must not uninstall; calls=%v", fb.uninstallCalls)
	}
	if fr.rollbackCalls != 0 {
		t.Errorf("dry-run must not native-rollback; calls=%d", fr.rollbackCalls)
	}
	if gens, _ := provision.List(); len(gens) != 2 {
		t.Errorf("dry-run must not append a generation; want 2 (seed), got %d", len(gens))
	}
}

func TestRollback_Realizer_Bare_ConfigOnlyNixAndBrewSkipsNative(t *testing.T) {
	t.Setenv("ENDSTATE_ROOT", t.TempDir())
	if err := provision.Write(&provision.Generation{Backend: "nix", Native: "3", RunID: "apply-old"}); err != nil {
		t.Fatal(err)
	}
	if err := provision.Write(&provision.Generation{Backend: "nix", RunID: "apply-config-brew"}); err != nil {
		t.Fatal(err)
	}
	if err := provision.Write(&provision.Generation{Backend: "brew", RunID: "apply-config-brew", AddedRefs: []string{"hello"}}); err != nil {
		t.Fatal(err)
	}

	fr := capableRealizer(setOf(3, "old"))
	fb := &fakeBrewDriver{installed: map[string]bool{"hello": true}}
	var result *RollbackResult
	withRealizerAndBrew(fr, func() (driver.Driver, error) { return fb, nil }, func() {
		raw, env := RunRollback(RollbackFlags{Confirm: true})
		if env != nil {
			t.Fatalf("unexpected envelope error: %+v", env)
		}
		result = raw.(*RollbackResult)
	})

	if fr.rollbackCalls != 0 {
		t.Fatalf("config-only Nix generation called native rollback %d times, want 0", fr.rollbackCalls)
	}
	if result.Backend != "brew" || len(result.RemovedRefs) != 1 || result.RemovedRefs[0] != "hello" {
		t.Fatalf("config-only Nix + Brew result = %+v, want Brew-only package rollback", result)
	}
	gens, _ := provision.List()
	for _, g := range gens {
		if g.Rollback && g.Backend == "nix" {
			t.Fatalf("config-only Nix lane wrote a native rollback generation: %+v", g)
		}
	}
}

func TestRollback_Realizer_Bare_BrewOnlyAllFailedReturnsRollbackFailed(t *testing.T) {
	t.Setenv("ENDSTATE_ROOT", t.TempDir())
	if err := provision.Write(&provision.Generation{Backend: "nix", Native: "3", RunID: "apply-old"}); err != nil {
		t.Fatal(err)
	}
	if err := provision.Write(&provision.Generation{Backend: "brew", RunID: "apply-brew", AddedRefs: []string{"hello"}}); err != nil {
		t.Fatal(err)
	}

	fr := &fakeRealizer{currentSet: setOf(3, "old")}
	fb := &fakeBrewDriver{
		installed: map[string]bool{"hello": true},
		uninstallResults: map[string]*driver.UninstallResult{
			"hello": {Status: driver.StatusFailed},
		},
	}
	withRealizerAndBrew(fr, func() (driver.Driver, error) { return fb, nil }, func() {
		_, env := RunRollback(RollbackFlags{Confirm: true})
		if env == nil || env.Code != envelope.ErrRollbackFailed {
			t.Fatalf("want ROLLBACK_FAILED, got %+v", env)
		}
	})
	if fr.rollbackCalls != 0 {
		t.Fatalf("Brew-only failure called native rollback %d times, want 0", fr.rollbackCalls)
	}
	if gens, _ := provision.List(); len(gens) != 2 {
		t.Fatalf("all-failed Brew-only rollback wrote a generation: %+v", gens)
	}
}

func TestRollback_Realizer_Bare_NativeSuccessToleratesAllBrewFailures(t *testing.T) {
	t.Setenv("ENDSTATE_ROOT", t.TempDir())
	if err := provision.Write(&provision.Generation{Backend: "nix", Native: "3", RunID: "apply-old"}); err != nil {
		t.Fatal(err)
	}
	if err := provision.Write(&provision.Generation{Backend: "nix", Native: "4", RunID: "apply-mixed", AddedRefs: []string{"nixpkgs#jq"}}); err != nil {
		t.Fatal(err)
	}
	if err := provision.Write(&provision.Generation{Backend: "brew", RunID: "apply-mixed", AddedRefs: []string{"hello"}}); err != nil {
		t.Fatal(err)
	}

	fr := capableRealizer(setOf(4, "jq"))
	fb := &fakeBrewDriver{
		installed: map[string]bool{"hello": true},
		uninstallResults: map[string]*driver.UninstallResult{
			"hello": {Status: driver.StatusFailed},
		},
	}
	var result *RollbackResult
	withRealizerAndBrew(fr, func() (driver.Driver, error) { return fb, nil }, func() {
		raw, env := RunRollback(RollbackFlags{Confirm: true})
		if env != nil {
			t.Fatalf("native success must tolerate Brew failure: %+v", env)
		}
		result = raw.(*RollbackResult)
	})
	if fr.rollbackCalls != 1 || !result.Partial || len(result.RemovedRefs) != 0 || len(result.FailedRefs) != 1 || result.FailedRefs[0] != "hello" {
		t.Fatalf("native-success/Brew-failure result = %+v; native calls=%d", result, fr.rollbackCalls)
	}
}

func TestRollback_Realizer_ConfigSuccessToleratesAllBrewFailures(t *testing.T) {
	t.Setenv("ENDSTATE_ROOT", t.TempDir())
	if err := provision.Write(&provision.Generation{
		Backend:     "nix",
		HomeManager: &provision.HomeGenRef{Flake: "/dot#me", Generation: 7},
	}); err != nil {
		t.Fatal(err)
	}
	if err := provision.Write(&provision.Generation{Backend: "brew", AddedRefs: []string{"hello"}}); err != nil {
		t.Fatal(err)
	}

	fr := &fakeRealizer{currentSet: setOf(0)}
	fr.homeRollbackGen = 12
	fb := &fakeBrewDriver{
		installed: map[string]bool{"hello": true},
		uninstallResults: map[string]*driver.UninstallResult{
			"hello": {Status: driver.StatusFailed},
		},
	}
	var result *RollbackResult
	withRealizerAndBrew(fr, func() (driver.Driver, error) { return fb, nil }, func() {
		raw, env := RunRollback(RollbackFlags{To: "1", EnableRestore: true, Confirm: true})
		if env != nil {
			t.Fatalf("config success must tolerate Brew failure: %+v", env)
		}
		result = raw.(*RollbackResult)
	})

	if fr.rollbackCalls != 0 || fr.homeRollbackCalls != 1 || fr.lastHomeRollbackArg != 7 {
		t.Fatalf("package/config rollback calls = %d/%d (config arg %d), want 0/1 (7)", fr.rollbackCalls, fr.homeRollbackCalls, fr.lastHomeRollbackArg)
	}
	if result.HomeManager == nil || !result.HomeManager.Reactivated || result.HomeManager.NewGeneration != 12 {
		t.Fatalf("config rollback result = %+v, want reactivated generation 12", result.HomeManager)
	}
	if !result.Partial || len(result.RemovedRefs) != 0 || len(result.FailedRefs) != 1 || result.FailedRefs[0] != "hello" {
		t.Fatalf("config-success/Brew-failure result = %+v", result)
	}

	gens, err := provision.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(gens) != 3 || !gens[0].Rollback || gens[0].Backend != "nix" || gens[0].HomeManager == nil || gens[0].HomeManager.Generation != 12 {
		t.Fatalf("config-success rollback generation = %+v, want one Nix config rollback and no Brew rollback generation", gens)
	}
}

// TestRollback_Realizer_BrewLane_RequiresConfirm: --to N without --confirm refuses,
// uninstalls nothing, native-rolls-back nothing, and appends no generation.
func TestRollback_Realizer_BrewLane_RequiresConfirm(t *testing.T) {
	t.Setenv("ENDSTATE_ROOT", t.TempDir())
	if err := provision.Write(&provision.Generation{Backend: "nix", Native: "4", AddedRefs: []string{"nixpkgs#jq"}}); err != nil {
		t.Fatalf("seed nix gen: %v", err) // gen 1
	}
	if err := provision.Write(&provision.Generation{Backend: "brew", AddedRefs: []string{"hello"}}); err != nil {
		t.Fatalf("seed brew gen: %v", err) // gen 2
	}

	fr := capableRealizer(setOf(4, "jq"))
	fb := &fakeBrewDriver{installed: map[string]bool{"hello": true}}
	withRealizerAndBrew(fr, func() (driver.Driver, error) { return fb, nil }, func() {
		_, env := RunRollback(RollbackFlags{To: "1"}) // no confirm, no dry-run
		if env == nil {
			t.Fatal("expected refusal, got nil error")
		}
	})
	if len(fb.uninstallCalls) != 0 {
		t.Errorf("refusal must not uninstall; calls=%v", fb.uninstallCalls)
	}
	if fr.rollbackCalls != 0 {
		t.Errorf("refusal must not native-rollback; calls=%d", fr.rollbackCalls)
	}
	if gens, _ := provision.List(); len(gens) != 2 {
		t.Errorf("refusal must not append a generation; want 2 (seed), got %d", len(gens))
	}
}

// TestRollback_Realizer_BrewOnlyTarget: a brew-only target generation (no native
// anchor) with a later brew generation is valid — the brew lane runs, the native
// package rollback is skipped (there is no anchor).
func TestRollback_Realizer_BrewOnlyTarget(t *testing.T) {
	t.Setenv("ENDSTATE_ROOT", t.TempDir())
	if err := provision.Write(&provision.Generation{Backend: "brew", AddedRefs: []string{"old"}}); err != nil {
		t.Fatalf("seed brew gen: %v", err) // gen 1 (the target; no native anchor)
	}
	if err := provision.Write(&provision.Generation{Backend: "brew", AddedRefs: []string{"new"}}); err != nil {
		t.Fatalf("seed brew gen: %v", err) // gen 2
	}

	fr := capableRealizer(setOf(0))
	fb := &fakeBrewDriver{installed: map[string]bool{"old": true, "new": true}}
	var result *RollbackResult
	withRealizerAndBrew(fr, func() (driver.Driver, error) { return fb, nil }, func() {
		raw, env := RunRollback(RollbackFlags{To: "1", Confirm: true})
		if env != nil {
			t.Fatalf("a brew-only target must be valid, got: %+v", env)
		}
		result = raw.(*RollbackResult)
	})

	if fr.rollbackCalls != 0 {
		t.Errorf("native Rollback called %d times for a brew-only target, want 0", fr.rollbackCalls)
	}
	if !sameSet(fb.uninstallCalls, []string{"new"}) {
		t.Errorf("brew uninstall calls = %v, want [new]", fb.uninstallCalls)
	}
	if !sameSet(result.RemovedRefs, []string{"new"}) {
		t.Errorf("RemovedRefs=%v, want [new]", result.RemovedRefs)
	}
}

// TestRollback_Realizer_NoBrewGens_NonRegression: a no-brew history with --to N
// never resolves the brew driver and yields the native-only result. The
// panicBrewDriverFn fails the test if newBrewDriverFn is ever called.
func TestRollback_Realizer_NoBrewGens_NonRegression(t *testing.T) {
	t.Setenv("ENDSTATE_ROOT", t.TempDir())
	if err := provision.Write(&provision.Generation{Backend: "nix", Native: "4", AddedRefs: []string{"nixpkgs#jq"}}); err != nil {
		t.Fatalf("seed nix gen: %v", err) // gen 1
	}

	fr := capableRealizer(setOf(4, "jq"))
	var result *RollbackResult
	withRealizerAndBrew(fr, panicBrewDriverFn(t), func() {
		raw, env := RunRollback(RollbackFlags{To: "1", Confirm: true})
		if env != nil {
			t.Fatalf("unexpected envelope error: %+v", env)
		}
		result = raw.(*RollbackResult)
	})

	if fr.rollbackCalls != 1 || fr.lastRollbackArg != 4 {
		t.Errorf("want native Rollback(4) once, got calls=%d arg=%d", fr.rollbackCalls, fr.lastRollbackArg)
	}
	if len(result.RemovedRefs) != 0 {
		t.Errorf("a no-brew history must remove nothing; RemovedRefs=%v", result.RemovedRefs)
	}
	// seed nix gen (1) + native rollback append (2) — no brew generation.
	gens, _ := provision.List()
	if len(gens) != 2 {
		t.Fatalf("want 2 generations (seed + native rollback), got %d: %+v", len(gens), gens)
	}
	for _, g := range gens {
		if g.Backend == "brew" {
			t.Errorf("a no-brew history must append no brew generation, got %+v", g)
		}
	}
}

func TestRollback_Realizer_Bare_RevertsNewestMixedRun(t *testing.T) {
	t.Setenv("ENDSTATE_ROOT", t.TempDir())
	if err := provision.Write(&provision.Generation{Backend: "nix", Native: "3", RunID: "apply-old", AddedRefs: []string{"nixpkgs#old"}}); err != nil {
		t.Fatalf("seed old nix gen: %v", err) // gen 1
	}
	if err := provision.Write(&provision.Generation{Backend: "nix", Native: "4", RunID: "apply-mixed", AddedRefs: []string{"nixpkgs#jq"}}); err != nil {
		t.Fatalf("seed mixed nix gen: %v", err) // gen 2
	}
	if err := provision.Write(&provision.Generation{Backend: "brew", RunID: "apply-mixed", AddedRefs: []string{"hello"}}); err != nil {
		t.Fatalf("seed mixed brew gen: %v", err) // gen 3
	}

	fr := capableRealizer(setOf(4, "jq"))
	fb := &fakeBrewDriver{installed: map[string]bool{"hello": true}}
	var result *RollbackResult
	withRealizerAndBrew(fr, func() (driver.Driver, error) { return fb, nil }, func() {
		raw, env := RunRollback(RollbackFlags{Confirm: true}) // no --to
		if env != nil {
			t.Fatalf("unexpected envelope error: %+v", env)
		}
		result = raw.(*RollbackResult)
	})

	if fr.rollbackCalls != 1 || fr.lastRollbackArg != -1 {
		t.Errorf("want native Rollback(-1) once, got calls=%d arg=%d", fr.rollbackCalls, fr.lastRollbackArg)
	}
	if got := len(fb.uninstallCalls); got != 1 || fb.uninstallCalls[0] != "hello" {
		t.Fatalf("brew uninstall calls = %v, want [hello]", fb.uninstallCalls)
	}
	if result.Backend != "mixed" || result.TargetGeneration != 1 || len(result.RemovedRefs) != 1 || result.RemovedRefs[0] != "hello" {
		t.Errorf("mixed bare result = %+v, want mixed target 1 removing hello", result)
	}

	gens, err := provision.List()
	if err != nil {
		t.Fatal(err)
	}
	var rollbackGens []*provision.Generation
	for _, g := range gens {
		if g.Rollback {
			rollbackGens = append(rollbackGens, g)
		}
	}
	if len(rollbackGens) != 2 {
		t.Fatalf("rollback generations = %d, want nix + brew: %+v", len(rollbackGens), rollbackGens)
	}
	if rollbackGens[0].RunID == "" || rollbackGens[0].RunID != rollbackGens[1].RunID {
		t.Fatalf("rollback run IDs = %q and %q, want one shared non-empty ID", rollbackGens[0].RunID, rollbackGens[1].RunID)
	}
	if result.NewGeneration != rollbackGens[0].Number {
		t.Errorf("NewGeneration = %d, want newest backend-scoped generation %d", result.NewGeneration, rollbackGens[0].Number)
	}
}

func TestRollback_Realizer_Bare_BrewOnlyNewestRun(t *testing.T) {
	t.Setenv("ENDSTATE_ROOT", t.TempDir())
	if err := provision.Write(&provision.Generation{Backend: "nix", Native: "4", RunID: "apply-old", AddedRefs: []string{"nixpkgs#jq"}}); err != nil {
		t.Fatalf("seed old nix gen: %v", err)
	}
	if err := provision.Write(&provision.Generation{Backend: "brew", RunID: "apply-brew", AddedRefs: []string{"hello"}}); err != nil {
		t.Fatalf("seed brew gen: %v", err)
	}

	fr := &fakeRealizer{currentSet: setOf(4, "jq")}
	fb := &fakeBrewDriver{installed: map[string]bool{"hello": true}}
	var result *RollbackResult
	withRealizerAndBrew(fr, func() (driver.Driver, error) { return fb, nil }, func() {
		raw, env := RunRollback(RollbackFlags{Confirm: true})
		if env != nil {
			t.Fatalf("unexpected envelope error: %+v", env)
		}
		result = raw.(*RollbackResult)
	})

	if fr.rollbackCalls != 0 {
		t.Fatalf("brew-only run called native rollback %d times, want 0", fr.rollbackCalls)
	}
	if result.Backend != "brew" || result.TargetGeneration != 1 || len(result.RemovedRefs) != 1 || result.RemovedRefs[0] != "hello" {
		t.Fatalf("brew-only result = %+v", result)
	}
	gens, _ := provision.List()
	for _, g := range gens {
		if g.Rollback && g.Backend == "nix" {
			t.Fatalf("brew-only rollback wrote a Nix rollback generation: %+v", g)
		}
	}
}

func TestRollback_Realizer_Bare_MixedPartialBrewFailure(t *testing.T) {
	t.Setenv("ENDSTATE_ROOT", t.TempDir())
	if err := provision.Write(&provision.Generation{Backend: "nix", Native: "3", RunID: "apply-old"}); err != nil {
		t.Fatal(err)
	}
	if err := provision.Write(&provision.Generation{Backend: "nix", Native: "4", RunID: "apply-mixed", AddedRefs: []string{"nixpkgs#jq"}}); err != nil {
		t.Fatal(err)
	}
	if err := provision.Write(&provision.Generation{Backend: "brew", RunID: "apply-mixed", AddedRefs: []string{"a", "b"}}); err != nil {
		t.Fatal(err)
	}

	fr := capableRealizer(setOf(4, "jq"))
	fb := &fakeBrewDriver{
		installed: map[string]bool{"a": true, "b": true},
		uninstallResults: map[string]*driver.UninstallResult{
			"b": {Status: driver.StatusFailed},
		},
	}
	var result *RollbackResult
	withRealizerAndBrew(fr, func() (driver.Driver, error) { return fb, nil }, func() {
		raw, env := RunRollback(RollbackFlags{Confirm: true})
		if env != nil {
			t.Fatalf("partial Brew failure must not undo native success: %+v", env)
		}
		result = raw.(*RollbackResult)
	})

	if fr.rollbackCalls != 1 || !result.Partial || len(result.RemovedRefs) != 1 || result.RemovedRefs[0] != "a" || len(result.FailedRefs) != 1 || result.FailedRefs[0] != "b" {
		t.Fatalf("partial mixed result = %+v; native calls=%d", result, fr.rollbackCalls)
	}
}

func TestRollback_Realizer_Bare_MixedDryRunDoesNotResolveBrew(t *testing.T) {
	t.Setenv("ENDSTATE_ROOT", t.TempDir())
	if err := provision.Write(&provision.Generation{Backend: "nix", Native: "3", RunID: "apply-old"}); err != nil {
		t.Fatal(err)
	}
	if err := provision.Write(&provision.Generation{Backend: "nix", Native: "4", RunID: "apply-mixed", AddedRefs: []string{"nixpkgs#jq"}}); err != nil {
		t.Fatal(err)
	}
	if err := provision.Write(&provision.Generation{Backend: "brew", RunID: "apply-mixed", AddedRefs: []string{"hello"}}); err != nil {
		t.Fatal(err)
	}

	fr := capableRealizer(setOf(4, "jq"))
	var result *RollbackResult
	withRealizerAndBrew(fr, panicBrewDriverFn(t), func() {
		raw, env := RunRollback(RollbackFlags{DryRun: true})
		if env != nil {
			t.Fatalf("unexpected envelope error: %+v", env)
		}
		result = raw.(*RollbackResult)
	})

	if fr.rollbackCalls != 0 || result.Backend != "mixed" || result.TargetGeneration != 1 || len(result.RemovedRefs) != 1 || result.RemovedRefs[0] != "hello" {
		t.Fatalf("dry-run result = %+v; native rollback calls=%d", result, fr.rollbackCalls)
	}
	if gens, _ := provision.List(); len(gens) != 3 {
		t.Fatalf("dry-run mutated generations: got %d, want 3", len(gens))
	}
}
