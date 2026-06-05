// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/driver"
	"github.com/Artexis10/endstate/go-engine/internal/provision"
)

// ---------------------------------------------------------------------------
// Best-effort brew rollback composed with the native (Nix) realizer rollback.
//
// On darwin RunRollback short-circuits to the realizer; these tests prove the
// brew uninstall lane runs ALONGSIDE the native rollback when an explicit
// --to target is given. Both seams are overridden (newRealizerFn via the
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

// TestRollback_Realizer_Bare_BrewUntouched: bare rollback (no --to) stays
// native-package-only and never engages the brew lane (the nix "previous" anchor
// cannot be reconciled with interleaved brew generations without a boundary).
func TestRollback_Realizer_Bare_BrewUntouched(t *testing.T) {
	t.Setenv("ENDSTATE_ROOT", t.TempDir())
	if err := provision.Write(&provision.Generation{Backend: "nix", Native: "4", AddedRefs: []string{"nixpkgs#jq"}}); err != nil {
		t.Fatalf("seed nix gen: %v", err) // gen 1
	}
	if err := provision.Write(&provision.Generation{Backend: "brew", AddedRefs: []string{"hello"}}); err != nil {
		t.Fatalf("seed brew gen: %v", err) // gen 2
	}

	fr := capableRealizer(setOf(4, "jq"))
	var result *RollbackResult
	// panicBrewDriverFn proves the bare path never resolves the brew driver.
	withRealizerAndBrew(fr, panicBrewDriverFn(t), func() {
		raw, env := RunRollback(RollbackFlags{Confirm: true}) // no --to
		if env != nil {
			t.Fatalf("unexpected envelope error: %+v", env)
		}
		result = raw.(*RollbackResult)
	})

	if fr.rollbackCalls != 1 || fr.lastRollbackArg != -1 {
		t.Errorf("want native Rollback(-1) once, got calls=%d arg=%d", fr.rollbackCalls, fr.lastRollbackArg)
	}
	if len(result.RemovedRefs) != 0 {
		t.Errorf("bare rollback must not touch brew; RemovedRefs=%v", result.RemovedRefs)
	}
}
