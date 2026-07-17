// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/provision"
	"github.com/Artexis10/endstate/go-engine/internal/realizer"
)

// ---------------------------------------------------------------------------
// realizerOnlyStub — implements realizer.Realizer but NOT realizer.Pruner, so
// the convergence (--prune) path must refuse with CONVERGENCE_UNSUPPORTED.
// ---------------------------------------------------------------------------

type realizerOnlyStub struct {
	currentSet    realizer.Set
	planDiff      realizer.Diff
	realizeResult realizer.Result
}

func (s *realizerOnlyStub) Name() string                   { return "stub" }
func (s *realizerOnlyStub) Current() (realizer.Set, error) { return s.currentSet, nil }
func (s *realizerOnlyStub) Realize(_ []realizer.Installable) (realizer.Result, error) {
	return s.realizeResult, nil
}
func (s *realizerOnlyStub) Plan(_ []realizer.Installable) (realizer.Diff, error) {
	return s.planDiff, nil
}

// driftSet builds a current Set whose elements are jq (declared) plus ripgrep
// (undeclared drift), so a manifest declaring only jq prunes ripgrep.
func driftSet(generation int) realizer.Set {
	return realizer.Set{
		Generation: generation,
		Elements: map[string]realizer.Element{
			"jq":      {Name: "jq", AttrPath: "jq"},
			"ripgrep": {Name: "ripgrep", AttrPath: "ripgrep"},
		},
	}
}

// jqManifest is a single-app manifest declaring only jq, keyed by the host OS so
// the realizer path resolves it on any platform.
func jqManifest() *manifest.Manifest {
	return &manifest.Manifest{Apps: []manifest.App{nixApp("jq", "nixpkgs#jq")}}
}

// ---------------------------------------------------------------------------
// Convergence (--prune) command-level tests — call runApplyRealizer directly so
// they are host-independent (no real Nix, no driver fork).
// ---------------------------------------------------------------------------

// Converged apply with --prune --confirm: jq is installed AND undeclared drift
// (ripgrep) is removed via Remove. result.Pruned is set, and the single recorded
// generation carries BOTH addedRefs (jq) and removedRefs (ripgrep), with native
// set to the prune's advancing generation (the final mutating op).
func TestRunApplyRealizer_PruneConfirm_RemovesDrift(t *testing.T) {
	t.Setenv("ENDSTATE_ROOT", t.TempDir())

	fr := &fakeRealizer{
		// jq is missing (install it); ripgrep is installed-but-undeclared (prune it).
		planDiff:      realizer.Diff{ToAdd: []realizer.Installable{{ID: "jq", Ref: "nixpkgs#jq"}}},
		currentSet:    driftSet(5),
		realizeResult: realizer.Result{Advanced: true, ToGeneration: 5},
		removeResult:  realizer.Result{Advanced: true, ToGeneration: 6},
	}
	flags := ApplyFlags{Manifest: "m.jsonc", Prune: true, Confirm: true}

	raw, eerr := runApplyRealizer(flags, jqManifest(), fr, noopEmitter(), "prune-1", nil, nil, nil, nil, nil)
	if eerr != nil {
		t.Fatalf("unexpected envelope error: %v", eerr)
	}
	if fr.removeCalls != 1 {
		t.Fatalf("Remove called %d times, want 1", fr.removeCalls)
	}
	if len(fr.lastRemoveArgs) != 1 || fr.lastRemoveArgs[0] != "ripgrep" {
		t.Fatalf("Remove args = %v, want [ripgrep]", fr.lastRemoveArgs)
	}
	result := raw.(*ApplyResult)
	if len(result.Pruned) != 1 || result.Pruned[0] != "ripgrep" {
		t.Fatalf("result.Pruned = %v, want [ripgrep]", result.Pruned)
	}

	gens, _ := provision.List()
	if len(gens) != 1 {
		t.Fatalf("want 1 generation recording the converged set, got %d", len(gens))
	}
	g := gens[0]
	if len(g.AddedRefs) != 1 || g.AddedRefs[0] != "nixpkgs#jq" {
		t.Fatalf("generation AddedRefs = %v, want [nixpkgs#jq]", g.AddedRefs)
	}
	if len(g.RemovedRefs) != 1 || g.RemovedRefs[0] != "ripgrep" {
		t.Fatalf("generation RemovedRefs = %v, want [ripgrep]", g.RemovedRefs)
	}
	if g.Native != "6" {
		t.Fatalf("generation Native = %q, want \"6\" (prune's advancing gen)", g.Native)
	}
}

// No-op convergence: --prune --confirm with everything already declared and no
// drift installs nothing and removes nothing, so it records no generation and
// never calls Remove (an atomic backend writes a generation only when it
// advances).
func TestRunApplyRealizer_PruneConfirm_NoDrift_NoGeneration(t *testing.T) {
	t.Setenv("ENDSTATE_ROOT", t.TempDir())

	fr := &fakeRealizer{
		planDiff: realizer.Diff{Present: []realizer.Installable{{ID: "jq", Ref: "nixpkgs#jq"}}},
		currentSet: realizer.Set{
			Generation: 4,
			Elements:   map[string]realizer.Element{"jq": {Name: "jq", AttrPath: "jq"}},
		},
	}
	flags := ApplyFlags{Manifest: "m.jsonc", Prune: true, Confirm: true}

	if _, eerr := runApplyRealizer(flags, jqManifest(), fr, noopEmitter(), "noop", nil, nil, nil, nil, nil); eerr != nil {
		t.Fatalf("unexpected envelope error: %v", eerr)
	}
	if fr.removeCalls != 0 {
		t.Fatalf("Remove called %d times with no drift, want 0", fr.removeCalls)
	}
	if gens, _ := provision.List(); len(gens) != 0 {
		t.Fatalf("no-op convergence must record no generation, got %d", len(gens))
	}
}

// --prune --dry-run: the prune set is previewed in result.Pruned WITHOUT calling
// Remove, requires no --confirm, and records no generation.
func TestRunApplyRealizer_PruneDryRun_PreviewsWithoutRemoving(t *testing.T) {
	t.Setenv("ENDSTATE_ROOT", t.TempDir())

	fr := &fakeRealizer{
		planDiff:   realizer.Diff{ToAdd: []realizer.Installable{{ID: "jq", Ref: "nixpkgs#jq"}}},
		currentSet: driftSet(4),
	}
	flags := ApplyFlags{Manifest: "m.jsonc", Prune: true, DryRun: true} // no Confirm

	raw, eerr := runApplyRealizer(flags, jqManifest(), fr, noopEmitter(), "prune-dry", nil, nil, nil, nil, nil)
	if eerr != nil {
		t.Fatalf("unexpected envelope error: %v", eerr)
	}
	result := raw.(*ApplyResult)
	if !result.DryRun {
		t.Error("result.DryRun = false, want true")
	}
	if len(result.Pruned) != 1 || result.Pruned[0] != "ripgrep" {
		t.Fatalf("result.Pruned = %v, want [ripgrep]", result.Pruned)
	}
	if fr.removeCalls != 0 {
		t.Fatalf("Remove called %d times in dry-run, want 0", fr.removeCalls)
	}
	if gens, _ := provision.List(); len(gens) != 0 {
		t.Fatalf("dry-run must record no generation, got %d", len(gens))
	}
}

// --prune without --confirm (not dry-run): refuses with an envelope error and
// removes nothing; the install phase results stand.
func TestRunApplyRealizer_PruneWithoutConfirm_Refuses(t *testing.T) {
	t.Setenv("ENDSTATE_ROOT", t.TempDir())

	fr := &fakeRealizer{
		planDiff:   realizer.Diff{Present: []realizer.Installable{{ID: "jq", Ref: "nixpkgs#jq"}}},
		currentSet: driftSet(4),
	}
	flags := ApplyFlags{Manifest: "m.jsonc", Prune: true} // Confirm:false, DryRun:false

	_, eerr := runApplyRealizer(flags, jqManifest(), fr, noopEmitter(), "prune-noconfirm", nil, nil, nil, nil, nil)
	if eerr == nil {
		t.Fatal("expected an envelope error refusing --prune without --confirm, got nil")
	}
	if eerr.Code != envelope.ErrInternalError {
		t.Fatalf("refusal code = %q, want INTERNAL_ERROR", eerr.Code)
	}
	if fr.removeCalls != 0 {
		t.Fatalf("Remove called %d times, want 0 (refused)", fr.removeCalls)
	}
}

// Default apply (no --prune): Remove is never called even when drift exists.
func TestRunApplyRealizer_NoPrune_NeverRemoves(t *testing.T) {
	t.Setenv("ENDSTATE_ROOT", t.TempDir())

	fr := &fakeRealizer{
		planDiff:   realizer.Diff{Present: []realizer.Installable{{ID: "jq", Ref: "nixpkgs#jq"}}},
		currentSet: driftSet(4),
	}
	flags := ApplyFlags{Manifest: "m.jsonc"} // no Prune

	if _, eerr := runApplyRealizer(flags, jqManifest(), fr, noopEmitter(), "noprune", nil, nil, nil, nil, nil); eerr != nil {
		t.Fatalf("unexpected envelope error: %v", eerr)
	}
	if fr.removeCalls != 0 {
		t.Fatalf("Remove called %d times without --prune, want 0", fr.removeCalls)
	}
}

// A realizer that does NOT implement Pruner must refuse --prune with
// CONVERGENCE_UNSUPPORTED (the realizer-path counterpart of the driver refusal).
func TestRunApplyRealizer_NonPruner_Unsupported(t *testing.T) {
	t.Setenv("ENDSTATE_ROOT", t.TempDir())

	stub := &realizerOnlyStub{
		planDiff:   realizer.Diff{Present: []realizer.Installable{{ID: "jq", Ref: "nixpkgs#jq"}}},
		currentSet: driftSet(4),
	}
	flags := ApplyFlags{Manifest: "m.jsonc", Prune: true, Confirm: true}

	_, eerr := runApplyRealizer(flags, jqManifest(), stub, noopEmitter(), "nonpruner", nil, nil, nil, nil, nil)
	if eerr == nil {
		t.Fatal("expected CONVERGENCE_UNSUPPORTED for a non-Pruner realizer, got nil")
	}
	if eerr.Code != envelope.ErrConvergenceUnsupported {
		t.Fatalf("error code = %q, want CONVERGENCE_UNSUPPORTED", eerr.Code)
	}
}

// Driver (winget) path: --prune refuses with CONVERGENCE_UNSUPPORTED and changes
// nothing. withMockDriver overrides BOTH newRealizerFn (-> ErrNoRealizer) and
// newDriverFn so the driver fork is taken even on linux/darwin/windows CI.
func TestRunApply_DriverPath_PruneUnsupported(t *testing.T) {
	t.Setenv("ENDSTATE_ROOT", t.TempDir())

	md := &mockDriver{installed: map[string]bool{}}
	manifestContent := `{
		"version": 1,
		"name": "prune-driver-test",
		"apps": [
			{ "id": "a", "refs": { "windows": "Vendor.A" } }
		]
	}`
	manifestPath := filepath.Join(t.TempDir(), "m.jsonc")
	if err := os.WriteFile(manifestPath, []byte(manifestContent), 0644); err != nil {
		t.Fatal(err)
	}

	var eerr *envelope.Error
	withMockDriver(md, func() {
		_, eerr = RunApply(ApplyFlags{Manifest: manifestPath, Prune: true})
	})
	if eerr == nil {
		t.Fatal("expected CONVERGENCE_UNSUPPORTED on the driver path, got nil")
	}
	if eerr.Code != envelope.ErrConvergenceUnsupported {
		t.Fatalf("error code = %q, want CONVERGENCE_UNSUPPORTED", eerr.Code)
	}
	// Nothing installed by the refused run.
	if gens, _ := provision.List(); len(gens) != 0 {
		t.Fatalf("refused --prune must record no generation, got %d", len(gens))
	}
}

// --- separation of concerns guard --------------------------------------------

// TestPruneStaysPackageOnly: the realizer apply path (which owns convergence /
// --prune) must not import the config/restore layer — pruning packages is
// distinct from reverting configs.
func TestPruneStaysPackageOnly(t *testing.T) {
	fset := token.NewFileSet()
	af, err := parser.ParseFile(fset, "apply_realizer.go", nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("parse apply_realizer.go: %v", err)
	}
	for _, imp := range af.Imports {
		if strings.Contains(imp.Path.Value, "internal/restore") {
			t.Fatalf("apply_realizer.go imports %s; the prune path must stay package-stage only", imp.Path.Value)
		}
	}
}
