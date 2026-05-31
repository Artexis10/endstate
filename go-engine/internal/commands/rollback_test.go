// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"errors"
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/driver"
	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/provision"
	"github.com/Artexis10/endstate/go-engine/internal/realizer"
)

// ---------------------------------------------------------------------------
// Driver (best-effort / winget) rollback test doubles
// ---------------------------------------------------------------------------

// plainDriver implements driver.Driver but NOT driver.Uninstaller — used to test
// the unsupported path on a host whose driver cannot uninstall.
type plainDriver struct{}

func (plainDriver) Name() string                                  { return "winget" }
func (plainDriver) Detect(string) (bool, string, error)           { return false, "", nil }
func (plainDriver) Install(string) (*driver.InstallResult, error) { return &driver.InstallResult{}, nil }

// fakeUninstaller implements driver.Driver + driver.Uninstaller with scriptable
// per-ref outcomes and call capture.
type fakeUninstaller struct {
	results map[string]*driver.UninstallResult // ref -> outcome (default: uninstalled)
	uerr    error                              // infrastructure error (e.g. winget missing)
	calls   []string                           // refs passed to Uninstall, in order
}

func (f *fakeUninstaller) Name() string                                  { return "winget" }
func (f *fakeUninstaller) Detect(string) (bool, string, error)           { return false, "", nil }
func (f *fakeUninstaller) Install(string) (*driver.InstallResult, error) { return &driver.InstallResult{}, nil }
func (f *fakeUninstaller) Uninstall(ref string) (*driver.UninstallResult, error) {
	f.calls = append(f.calls, ref)
	if f.uerr != nil {
		return nil, f.uerr
	}
	if r, ok := f.results[ref]; ok {
		return r, nil
	}
	return &driver.UninstallResult{Status: driver.StatusUninstalled}, nil
}

// withDriverOnly forces the no-realizer (driver) dispatch path: newRealizerFn
// returns ErrNoRealizer and newDriverFn returns d.
func withDriverOnly(d driver.Driver, fn func()) {
	origR, origD := newRealizerFn, newDriverFn
	newRealizerFn = func() (realizer.Realizer, error) { return nil, ErrNoRealizer }
	newDriverFn = func() (driver.Driver, error) { return d, nil }
	defer func() { newRealizerFn, newDriverFn = origR, origD }()
	fn()
}

// seedWingetGen writes a winget Provisioning Generation with the given addedRefs.
func seedWingetGen(t *testing.T, added ...string) {
	t.Helper()
	if err := provision.Write(&provision.Generation{Backend: "winget", AddedRefs: added}); err != nil {
		t.Fatalf("seed generation: %v", err)
	}
}

// sameSet reports whether got and want contain the same elements, ignoring order.
func sameSet(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	m := map[string]int{}
	for _, g := range got {
		m[g]++
	}
	for _, w := range want {
		m[w]--
	}
	for _, v := range m {
		if v != 0 {
			return false
		}
	}
	return true
}

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

// TestRollback_NoBackend_Unsupported: a host with neither a realizer nor an
// uninstall-capable driver refuses with ROLLBACK_UNSUPPORTED and changes
// nothing. Both seams are overridden so the test is host-independent — on
// Windows the real winget driver now implements driver.Uninstaller, so leaving
// newDriverFn at its default would take the best-effort path instead.
func TestRollback_NoBackend_Unsupported(t *testing.T) {
	origR, origD := newRealizerFn, newDriverFn
	newRealizerFn = func() (realizer.Realizer, error) { return nil, ErrNoRealizer }
	newDriverFn = func() (driver.Driver, error) { return nil, ErrNoBackend }
	defer func() { newRealizerFn, newDriverFn = origR, origD }()

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

// ---------------------------------------------------------------------------
// Best-effort (driver / winget) rollback tests
// ---------------------------------------------------------------------------

// TestRollback_Driver_ToGeneration_UninstallsLaterAdditions: --to N uninstalls
// the union of addedRefs of every generation after N, and appends a
// rollback-marked generation recording what was removed.
func TestRollback_Driver_ToGeneration_UninstallsLaterAdditions(t *testing.T) {
	t.Setenv("ENDSTATE_ROOT", t.TempDir())
	seedWingetGen(t, "A")      // gen 1
	seedWingetGen(t, "B")      // gen 2
	seedWingetGen(t, "C", "D") // gen 3

	fr := &fakeUninstaller{}
	var result *RollbackResult
	withDriverOnly(fr, func() {
		raw, env := RunRollback(RollbackFlags{To: "1", Confirm: true})
		if env != nil {
			t.Fatalf("unexpected envelope error: %+v", env)
		}
		result = raw.(*RollbackResult)
	})

	// gens > 1 = gen2[B] ∪ gen3[C,D] = {B, C, D} (order is newest-gen-first, i.e.
	// reverse-install order — assert as a set, not a sequence).
	if !sameSet(fr.calls, []string{"B", "C", "D"}) {
		t.Errorf("uninstall calls = %v, want the set {B,C,D}", fr.calls)
	}
	if len(result.RemovedRefs) != 3 || result.Partial {
		t.Errorf("RemovedRefs=%v partial=%v, want 3 removed, not partial", result.RemovedRefs, result.Partial)
	}
	if result.NewGeneration != 4 {
		t.Errorf("NewGeneration = %d, want 4", result.NewGeneration)
	}
	gens, _ := provision.List()
	newest := gens[0]
	if newest.Number != 4 || !newest.Rollback || len(newest.AddedRefs) != 0 || len(newest.RemovedRefs) != 3 {
		t.Errorf("appended gen wrong: #%d rollback=%v added=%v removed=%v", newest.Number, newest.Rollback, newest.AddedRefs, newest.RemovedRefs)
	}
}

// TestRollback_Driver_Bare_RevertsMostRecent: bare rollback removes the most
// recent generation's additions.
func TestRollback_Driver_Bare_RevertsMostRecent(t *testing.T) {
	t.Setenv("ENDSTATE_ROOT", t.TempDir())
	seedWingetGen(t, "A") // gen 1
	seedWingetGen(t, "B") // gen 2

	fr := &fakeUninstaller{}
	withDriverOnly(fr, func() {
		_, env := RunRollback(RollbackFlags{Confirm: true})
		if env != nil {
			t.Fatalf("unexpected envelope error: %+v", env)
		}
	})
	if got := strings.Join(fr.calls, ","); got != "B" {
		t.Errorf("bare rollback uninstall calls = %q, want B (most recent gen)", got)
	}
}

// TestRollback_Driver_NotUninstaller_Unsupported: a driver that cannot uninstall
// (and no realizer) → ROLLBACK_UNSUPPORTED.
func TestRollback_Driver_NotUninstaller_Unsupported(t *testing.T) {
	t.Setenv("ENDSTATE_ROOT", t.TempDir())
	withDriverOnly(plainDriver{}, func() {
		_, env := RunRollback(RollbackFlags{To: "1", Confirm: true})
		if env == nil || env.Code != envelope.ErrRollbackUnsupported {
			t.Fatalf("want ROLLBACK_UNSUPPORTED, got %+v", env)
		}
	})
}

// TestRollback_Driver_UnknownGeneration_NotFound: --to N with no such generation.
func TestRollback_Driver_UnknownGeneration_NotFound(t *testing.T) {
	t.Setenv("ENDSTATE_ROOT", t.TempDir())
	seedWingetGen(t, "A") // gen 1
	fr := &fakeUninstaller{}
	withDriverOnly(fr, func() {
		_, env := RunRollback(RollbackFlags{To: "99", Confirm: true})
		if env == nil || env.Code != envelope.ErrGenerationNotFound {
			t.Fatalf("want GENERATION_NOT_FOUND, got %+v", env)
		}
	})
	if len(fr.calls) != 0 {
		t.Errorf("Uninstall called %v, want none", fr.calls)
	}
}

// TestRollback_Driver_PartialFailure: a per-ref failure does not abort; the run
// is marked partial and the failed ref is reported.
func TestRollback_Driver_PartialFailure(t *testing.T) {
	t.Setenv("ENDSTATE_ROOT", t.TempDir())
	seedWingetGen(t, "A")      // gen 1
	seedWingetGen(t, "B", "C") // gen 2

	fr := &fakeUninstaller{results: map[string]*driver.UninstallResult{
		"B": {Status: driver.StatusFailed, Message: "in use by another package"},
		"C": {Status: driver.StatusUninstalled},
	}}
	var result *RollbackResult
	withDriverOnly(fr, func() {
		raw, env := RunRollback(RollbackFlags{To: "1", Confirm: true})
		if env != nil {
			t.Fatalf("unexpected envelope error: %+v", env)
		}
		result = raw.(*RollbackResult)
	})
	if !result.Partial || len(result.FailedRefs) != 1 || result.FailedRefs[0] != "B" {
		t.Errorf("want partial with FailedRefs=[B], got partial=%v failed=%v", result.Partial, result.FailedRefs)
	}
	if len(result.RemovedRefs) != 1 || result.RemovedRefs[0] != "C" {
		t.Errorf("want RemovedRefs=[C], got %v", result.RemovedRefs)
	}
	gens, _ := provision.List()
	if !gens[0].Partial {
		t.Errorf("appended generation should be marked partial")
	}
}

// TestRollback_Driver_AlreadyAbsent_CountsRemoved: an already-absent package is a
// successful no-op (counted as removed, not failed).
func TestRollback_Driver_AlreadyAbsent_CountsRemoved(t *testing.T) {
	t.Setenv("ENDSTATE_ROOT", t.TempDir())
	seedWingetGen(t, "A") // gen 1
	seedWingetGen(t, "B") // gen 2

	fr := &fakeUninstaller{results: map[string]*driver.UninstallResult{
		"B": {Status: driver.StatusAbsent},
	}}
	var result *RollbackResult
	withDriverOnly(fr, func() {
		raw, env := RunRollback(RollbackFlags{To: "1", Confirm: true})
		if env != nil {
			t.Fatalf("unexpected envelope error: %+v", env)
		}
		result = raw.(*RollbackResult)
	})
	if result.Partial || len(result.FailedRefs) != 0 || len(result.RemovedRefs) != 1 {
		t.Errorf("absent should count as removed: partial=%v failed=%v removed=%v", result.Partial, result.FailedRefs, result.RemovedRefs)
	}
}

// TestRollback_Driver_AllFailed_RollbackFailed: when every targeted uninstall
// fails, the command returns ROLLBACK_FAILED and writes no generation.
func TestRollback_Driver_AllFailed_RollbackFailed(t *testing.T) {
	t.Setenv("ENDSTATE_ROOT", t.TempDir())
	seedWingetGen(t, "A") // gen 1
	seedWingetGen(t, "B") // gen 2

	fr := &fakeUninstaller{results: map[string]*driver.UninstallResult{
		"B": {Status: driver.StatusFailed},
	}}
	withDriverOnly(fr, func() {
		_, env := RunRollback(RollbackFlags{To: "1", Confirm: true})
		if env == nil || env.Code != envelope.ErrRollbackFailed {
			t.Fatalf("want ROLLBACK_FAILED, got %+v", env)
		}
	})
	if gens, _ := provision.List(); len(gens) != 2 {
		t.Errorf("no generation should be appended on total failure; want 2, got %d", len(gens))
	}
}

// TestRollback_Driver_RequiresConfirm: without --confirm the command refuses,
// uninstalls nothing, and writes no generation.
func TestRollback_Driver_RequiresConfirm(t *testing.T) {
	t.Setenv("ENDSTATE_ROOT", t.TempDir())
	seedWingetGen(t, "A") // gen 1
	seedWingetGen(t, "B") // gen 2

	fr := &fakeUninstaller{}
	withDriverOnly(fr, func() {
		_, env := RunRollback(RollbackFlags{To: "1"})
		if env == nil || !strings.Contains(env.Message, "--confirm") {
			t.Fatalf("want refusal mentioning --confirm, got %+v", env)
		}
	})
	if len(fr.calls) != 0 {
		t.Errorf("Uninstall called %v, want none", fr.calls)
	}
	if gens, _ := provision.List(); len(gens) != 2 {
		t.Errorf("no generation should be appended on refusal; want 2, got %d", len(gens))
	}
}

// TestRollback_Driver_DryRun_PreviewsNoMutation: --dry-run lists what would be
// removed without uninstalling or appending a generation.
func TestRollback_Driver_DryRun_PreviewsNoMutation(t *testing.T) {
	t.Setenv("ENDSTATE_ROOT", t.TempDir())
	seedWingetGen(t, "A") // gen 1
	seedWingetGen(t, "B") // gen 2

	fr := &fakeUninstaller{}
	var result *RollbackResult
	withDriverOnly(fr, func() {
		raw, env := RunRollback(RollbackFlags{To: "1", DryRun: true})
		if env != nil {
			t.Fatalf("unexpected envelope error: %+v", env)
		}
		result = raw.(*RollbackResult)
	})
	if !result.DryRun || len(result.RemovedRefs) != 1 || result.RemovedRefs[0] != "B" {
		t.Errorf("dry-run preview wrong: dryRun=%v removed=%v", result.DryRun, result.RemovedRefs)
	}
	if len(fr.calls) != 0 {
		t.Errorf("dry-run must not uninstall; calls=%v", fr.calls)
	}
	if gens, _ := provision.List(); len(gens) != 2 {
		t.Errorf("dry-run must not append a generation; want 2, got %d", len(gens))
	}
}

// TestRollback_Driver_NothingToRollback: target is already the most recent → a
// no-op success with nothing removed.
func TestRollback_Driver_NothingToRollback(t *testing.T) {
	t.Setenv("ENDSTATE_ROOT", t.TempDir())
	seedWingetGen(t, "A") // gen 1 (the only/most-recent generation)

	fr := &fakeUninstaller{}
	var result *RollbackResult
	withDriverOnly(fr, func() {
		raw, env := RunRollback(RollbackFlags{To: "1", Confirm: true})
		if env != nil {
			t.Fatalf("unexpected envelope error: %+v", env)
		}
		result = raw.(*RollbackResult)
	})
	if len(fr.calls) != 0 || len(result.RemovedRefs) != 0 {
		t.Errorf("nothing-to-roll-back should be a no-op: calls=%v removed=%v", fr.calls, result.RemovedRefs)
	}
}

// TestRollback_Driver_MissingBinary_WingetUnavailable: an infrastructure error
// from Uninstall surfaces WINGET_NOT_AVAILABLE.
func TestRollback_Driver_MissingBinary_WingetUnavailable(t *testing.T) {
	t.Setenv("ENDSTATE_ROOT", t.TempDir())
	seedWingetGen(t, "A") // gen 1
	seedWingetGen(t, "B") // gen 2

	fr := &fakeUninstaller{uerr: errors.New("winget: executable file not found")}
	withDriverOnly(fr, func() {
		_, env := RunRollback(RollbackFlags{To: "1", Confirm: true})
		if env == nil || env.Code != envelope.ErrWingetNotAvailable {
			t.Fatalf("want WINGET_NOT_AVAILABLE, got %+v", env)
		}
	})
}
