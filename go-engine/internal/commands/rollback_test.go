// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/provision"
	"github.com/Artexis10/endstate/go-engine/internal/realizer"
)

// capableRealizer returns a fakeRealizer that advertises native rollback, with
// the given current set.
func capableRealizer(cur realizer.Set) *fakeRealizer {
	return &fakeRealizer{
		caps:       provision.Capabilities{AtomicSet: true, NativeRollback: true, BatchInstall: true},
		currentSet: cur,
	}
}

func setOf(gen int, names ...string) realizer.Set {
	els := map[string]realizer.Element{}
	for _, n := range names {
		els[n] = realizer.Element{Name: n, AttrPath: n}
	}
	return realizer.Set{Generation: gen, Elements: els}
}

// --- ROLLBACK_UNSUPPORTED ----------------------------------------------------

// TestRollback_NoRealizer_Unsupported: a host with no realizer (e.g. Windows)
// refuses with ROLLBACK_UNSUPPORTED and changes nothing.
func TestRollback_NoRealizer_Unsupported(t *testing.T) {
	orig := newRealizerFn
	newRealizerFn = func() (realizer.Realizer, error) { return nil, ErrNoRealizer }
	defer func() { newRealizerFn = orig }()

	_, env := RunRollback(RollbackFlags{Confirm: true})
	if env == nil || env.Code != envelope.ErrRollbackUnsupported {
		t.Fatalf("want ROLLBACK_UNSUPPORTED, got %+v", env)
	}
}

// TestRollback_NotNativeCapable_Unsupported: a realizer that does not advertise
// NativeRollback refuses with ROLLBACK_UNSUPPORTED without calling Rollback.
func TestRollback_NotNativeCapable_Unsupported(t *testing.T) {
	fr := &fakeRealizer{caps: provision.Capabilities{}} // all false
	withFakeRealizer(fr, func() {
		_, env := RunRollback(RollbackFlags{Confirm: true})
		if env == nil || env.Code != envelope.ErrRollbackUnsupported {
			t.Fatalf("want ROLLBACK_UNSUPPORTED, got %+v", env)
		}
	})
	if fr.rollbackCalls != 0 {
		t.Errorf("Rollback called %d times, want 0", fr.rollbackCalls)
	}
}

// --- target resolution + append ---------------------------------------------

// TestRollback_ToGeneration_MapsNativeAndAppends: --to N maps engine generation
// N to its Native version, calls Rollback(native), and appends a new
// rollback-marked Provisioning Generation reflecting the now-active set.
func TestRollback_ToGeneration_MapsNativeAndAppends(t *testing.T) {
	t.Setenv("ENDSTATE_ROOT", t.TempDir())

	// Seed engine generation #1 with Native "4".
	if err := provision.Write(&provision.Generation{
		Backend:   "nix",
		Native:    "4",
		Items:     []provision.ProvItem{{ID: "jq", Ref: "nixpkgs#jq", Status: "installed"}},
		AddedRefs: []string{"nixpkgs#jq"},
	}); err != nil {
		t.Fatalf("seed generation: %v", err)
	}

	fr := capableRealizer(setOf(4, "jq"))
	var result *RollbackResult
	withFakeRealizer(fr, func() {
		raw, env := RunRollback(RollbackFlags{To: "1", Confirm: true})
		if env != nil {
			t.Fatalf("unexpected envelope error: %+v", env)
		}
		result = raw.(*RollbackResult)
	})

	if fr.rollbackCalls != 1 {
		t.Fatalf("Rollback called %d times, want 1", fr.rollbackCalls)
	}
	if fr.lastRollbackArg != 4 {
		t.Errorf("Rollback(to) = %d, want 4 (the Native of generation 1)", fr.lastRollbackArg)
	}
	if result.TargetGeneration != 1 {
		t.Errorf("TargetGeneration = %d, want 1", result.TargetGeneration)
	}
	if result.NewGeneration != 2 {
		t.Errorf("NewGeneration = %d, want 2", result.NewGeneration)
	}

	gens, _ := provision.List()
	if len(gens) != 2 {
		t.Fatalf("want 2 generations after rollback, got %d", len(gens))
	}
	newest := gens[0] // newest first
	if newest.Number != 2 || !newest.Rollback {
		t.Errorf("newest generation = #%d rollback=%v, want #2 rollback=true", newest.Number, newest.Rollback)
	}
	if len(newest.AddedRefs) != 0 {
		t.Errorf("rollback generation AddedRefs = %v, want empty", newest.AddedRefs)
	}
}

// TestRollback_Previous_NoTo: bare rollback (no --to) calls Rollback(-1).
func TestRollback_Previous_NoTo(t *testing.T) {
	t.Setenv("ENDSTATE_ROOT", t.TempDir())
	fr := capableRealizer(setOf(3, "ripgrep"))
	withFakeRealizer(fr, func() {
		_, env := RunRollback(RollbackFlags{Confirm: true})
		if env != nil {
			t.Fatalf("unexpected envelope error: %+v", env)
		}
	})
	if fr.rollbackCalls != 1 || fr.lastRollbackArg != -1 {
		t.Errorf("want Rollback(-1) once, got calls=%d arg=%d", fr.rollbackCalls, fr.lastRollbackArg)
	}
}

// TestRollback_UnknownGeneration_NotFound: --to N with no such generation →
// GENERATION_NOT_FOUND, no Rollback call.
func TestRollback_UnknownGeneration_NotFound(t *testing.T) {
	t.Setenv("ENDSTATE_ROOT", t.TempDir())
	fr := capableRealizer(setOf(1))
	withFakeRealizer(fr, func() {
		_, env := RunRollback(RollbackFlags{To: "99", Confirm: true})
		if env == nil || env.Code != envelope.ErrGenerationNotFound {
			t.Fatalf("want GENERATION_NOT_FOUND, got %+v", env)
		}
	})
	if fr.rollbackCalls != 0 {
		t.Errorf("Rollback called %d times, want 0", fr.rollbackCalls)
	}
}

// TestRollback_NoNativeAnchor_NotFound: --to N referencing a generation with no
// native anchor (e.g. a winget generation) → GENERATION_NOT_FOUND.
func TestRollback_NoNativeAnchor_NotFound(t *testing.T) {
	t.Setenv("ENDSTATE_ROOT", t.TempDir())
	if err := provision.Write(&provision.Generation{
		Backend:   "winget",
		Native:    "", // non-atomic backend: no native anchor
		AddedRefs: []string{"Some.App"},
	}); err != nil {
		t.Fatalf("seed generation: %v", err)
	}
	fr := capableRealizer(setOf(1))
	withFakeRealizer(fr, func() {
		_, env := RunRollback(RollbackFlags{To: "1", Confirm: true})
		if env == nil || env.Code != envelope.ErrGenerationNotFound {
			t.Fatalf("want GENERATION_NOT_FOUND, got %+v", env)
		}
	})
	if fr.rollbackCalls != 0 {
		t.Errorf("Rollback called %d times, want 0", fr.rollbackCalls)
	}
}

// --- confirm gate + dry-run --------------------------------------------------

// TestRollback_RequiresConfirm: without --confirm (and not --dry-run) the
// command refuses, calls nothing, and writes no generation.
func TestRollback_RequiresConfirm(t *testing.T) {
	t.Setenv("ENDSTATE_ROOT", t.TempDir())
	fr := capableRealizer(setOf(2, "jq"))
	withFakeRealizer(fr, func() {
		_, env := RunRollback(RollbackFlags{}) // no confirm, no dry-run
		if env == nil {
			t.Fatal("expected refusal, got nil error")
		}
		if !strings.Contains(env.Message, "--confirm") {
			t.Errorf("message %q should mention --confirm", env.Message)
		}
	})
	if fr.rollbackCalls != 0 {
		t.Errorf("Rollback called %d times, want 0", fr.rollbackCalls)
	}
	if gens, _ := provision.List(); len(gens) != 0 {
		t.Errorf("want no generation written on refusal, got %d", len(gens))
	}
}

// TestRollback_DryRun_NoMutation: --dry-run resolves and previews the target
// without confirm, without calling Rollback, and without appending a generation.
func TestRollback_DryRun_NoMutation(t *testing.T) {
	t.Setenv("ENDSTATE_ROOT", t.TempDir())
	if err := provision.Write(&provision.Generation{Backend: "nix", Native: "4", AddedRefs: []string{"nixpkgs#jq"}}); err != nil {
		t.Fatalf("seed generation: %v", err)
	}
	fr := capableRealizer(setOf(5, "jq"))
	var result *RollbackResult
	withFakeRealizer(fr, func() {
		raw, env := RunRollback(RollbackFlags{To: "1", DryRun: true}) // no confirm
		if env != nil {
			t.Fatalf("unexpected envelope error: %+v", env)
		}
		result = raw.(*RollbackResult)
	})
	if !result.DryRun {
		t.Error("result.DryRun = false, want true")
	}
	if result.ToNative != "4" {
		t.Errorf("result.ToNative = %q, want 4", result.ToNative)
	}
	if fr.rollbackCalls != 0 {
		t.Errorf("Rollback called %d times in dry-run, want 0", fr.rollbackCalls)
	}
	if gens, _ := provision.List(); len(gens) != 1 {
		t.Errorf("dry-run must not append a generation; want 1 (seed), got %d", len(gens))
	}
}

// --- failure classification (the moat) ---------------------------------------

// TestRollback_SystemicError_RawInDetailOnly: a systemic backend failure
// surfaces its code with raw text confined to Detail (never Message).
func TestRollback_SystemicError_RawInDetailOnly(t *testing.T) {
	t.Setenv("ENDSTATE_ROOT", t.TempDir())
	raw := "nix daemon: connection refused at /nix/var/nix/daemon-socket/socket"
	fr := capableRealizer(setOf(3, "jq"))
	fr.rollbackErr = &realizer.Error{Code: envelope.ErrRealizerUnavailable, Subcode: "daemon", Stage: "spawn", Raw: raw}

	var env *envelope.Error
	withFakeRealizer(fr, func() {
		_, env = RunRollback(RollbackFlags{Confirm: true})
	})
	if env == nil || env.Code != envelope.ErrRealizerUnavailable {
		t.Fatalf("want REALIZER_UNAVAILABLE, got %+v", env)
	}
	if strings.Contains(env.Message, raw) {
		t.Errorf("raw text leaked into Message: %q", env.Message)
	}
	dm, ok := env.Detail.(map[string]string)
	if !ok || dm["raw"] != raw {
		t.Errorf("raw text not confined to Detail: %+v", env.Detail)
	}
	if gens, _ := provision.List(); len(gens) != 0 {
		t.Errorf("failed rollback must not append a generation, got %d", len(gens))
	}
}

// TestRollback_Failed_RawInDetailOnly: a non-systemic backend failure surfaces
// ROLLBACK_FAILED with raw text confined to Detail.
func TestRollback_Failed_RawInDetailOnly(t *testing.T) {
	t.Setenv("ENDSTATE_ROOT", t.TempDir())
	raw := "error: profile version 99 does not exist"
	fr := capableRealizer(setOf(3, "jq"))
	fr.rollbackErr = &realizer.Error{Code: envelope.ErrRollbackFailed, Stage: "rollback", Raw: raw}

	var env *envelope.Error
	withFakeRealizer(fr, func() {
		_, env = RunRollback(RollbackFlags{Confirm: true})
	})
	if env == nil || env.Code != envelope.ErrRollbackFailed {
		t.Fatalf("want ROLLBACK_FAILED, got %+v", env)
	}
	if strings.Contains(env.Message, raw) {
		t.Errorf("raw text leaked into Message: %q", env.Message)
	}
	dm, ok := env.Detail.(map[string]string)
	if !ok || dm["raw"] != raw {
		t.Errorf("raw text not confined to Detail: %+v", env.Detail)
	}
	if fr.rollbackCalls != 1 {
		t.Errorf("Rollback called %d times, want 1", fr.rollbackCalls)
	}
}

// --- separation of concerns guard --------------------------------------------

// TestRollbackStaysPackageOnly: the rollback command file must not import the
// config/restore layer — rollback (packages) is distinct from revert (configs).
func TestRollbackStaysPackageOnly(t *testing.T) {
	fset := token.NewFileSet()
	af, err := parser.ParseFile(fset, "rollback.go", nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("parse rollback.go: %v", err)
	}
	for _, imp := range af.Imports {
		if strings.Contains(imp.Path.Value, "internal/restore") {
			t.Fatalf("rollback.go imports %s; rollback must stay package-stage only", imp.Path.Value)
		}
	}
}
