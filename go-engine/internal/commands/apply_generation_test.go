// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/events"
	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/provision"
	"github.com/Artexis10/endstate/go-engine/internal/realizer"
)

// --- writeProvisioningGeneration: record-building logic (backend-agnostic) ---

func TestWriteProvisioningGeneration_RecordsInstalledOnly(t *testing.T) {
	t.Setenv("ENDSTATE_ROOT", t.TempDir())

	actions := []ApplyAction{
		{ID: "ripgrep", Ref: stringPtr("nixpkgs#ripgrep"), Status: "installed"},
		{ID: "jq", Ref: stringPtr("nixpkgs#jq"), Status: "present"},
		{ID: "bad", Ref: stringPtr("nixpkgs#bad"), Status: "failed"},
	}
	writeProvisioningGeneration("apply-x", "nix", actions, nil, "7", false, nil)

	gens, err := provision.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(gens) != 1 {
		t.Fatalf("want 1 generation, got %d", len(gens))
	}
	g := gens[0]
	if g.Backend != "nix" || g.Native != "7" || g.Partial {
		t.Fatalf("unexpected header: %+v", g)
	}
	if len(g.AddedRefs) != 1 || g.AddedRefs[0] != "nixpkgs#ripgrep" {
		t.Fatalf("addedRefs should be installed-only: %v", g.AddedRefs)
	}
	if len(g.Items) != 2 { // installed + present, NOT failed
		t.Fatalf("items should be installed+present only: %+v", g.Items)
	}
}

func TestWriteProvisioningGeneration_NoInstallsNoGeneration(t *testing.T) {
	t.Setenv("ENDSTATE_ROOT", t.TempDir())

	actions := []ApplyAction{{ID: "jq", Ref: stringPtr("nixpkgs#jq"), Status: "present"}}
	writeProvisioningGeneration("apply-x", "nix", actions, nil, "", false, nil)

	gens, _ := provision.List()
	if len(gens) != 0 {
		t.Fatalf("idempotent run must write no generation, got %d", len(gens))
	}
}

func TestWriteProvisioningGeneration_WingetPartialSubset(t *testing.T) {
	t.Setenv("ENDSTATE_ROOT", t.TempDir())

	actions := []ApplyAction{
		{ID: "a", Ref: stringPtr("A.A"), Status: "installed"},
		{ID: "b", Ref: stringPtr("B.B"), Status: "failed"},
	}
	writeProvisioningGeneration("apply-x", "winget", actions, nil, "", true, nil)

	gens, _ := provision.List()
	if len(gens) != 1 {
		t.Fatalf("want 1 generation, got %d", len(gens))
	}
	g := gens[0]
	if g.Backend != "winget" || g.Native != "" || !g.Partial {
		t.Fatalf("unexpected winget header: %+v", g)
	}
	if len(g.AddedRefs) != 1 || g.AddedRefs[0] != "A.A" {
		t.Fatalf("addedRefs should be the installed subset: %v", g.AddedRefs)
	}
}

// --- Realizer (nix) apply path wiring ---

func TestRunApplyRealizer_WritesGenerationOnFullSuccess(t *testing.T) {
	t.Setenv("ENDSTATE_ROOT", t.TempDir())

	r := &fakeRealizer{
		planDiff:      realizer.Diff{ToAdd: []realizer.Installable{{ID: "ripgrep", Ref: "nixpkgs#ripgrep"}}},
		realizeResult: realizer.Result{Advanced: true, ToGeneration: 3},
	}
	mf := &manifest.Manifest{Apps: []manifest.App{nixApp("ripgrep", "nixpkgs#ripgrep")}}
	emitter := events.NewEmitter("apply-test", false)

	_, eerr := runApplyRealizer(ApplyFlags{Manifest: "m.jsonc"}, mf, r, emitter, "apply-test", nil, nil, nil, nil)
	if eerr != nil {
		t.Fatalf("unexpected envelope error: %v", eerr)
	}

	gens, _ := provision.List()
	if len(gens) != 1 {
		t.Fatalf("want 1 generation, got %d", len(gens))
	}
	g := gens[0]
	if g.Backend != "nix" || g.Native != "3" || g.Partial {
		t.Fatalf("unexpected nix generation: %+v", g)
	}
	if len(g.AddedRefs) != 1 || g.AddedRefs[0] != "nixpkgs#ripgrep" {
		t.Fatalf("addedRefs: %v", g.AddedRefs)
	}
}

func TestRunApplyRealizer_NoGenerationWhenGenerationDidNotAdvance(t *testing.T) {
	t.Setenv("ENDSTATE_ROOT", t.TempDir())

	// Realize returns success but the profile generation did not advance: an
	// atomic backend must NOT record a generation.
	r := &fakeRealizer{
		planDiff:      realizer.Diff{ToAdd: []realizer.Installable{{ID: "ripgrep", Ref: "nixpkgs#ripgrep"}}},
		realizeResult: realizer.Result{Advanced: false},
	}
	mf := &manifest.Manifest{Apps: []manifest.App{nixApp("ripgrep", "nixpkgs#ripgrep")}}
	emitter := events.NewEmitter("apply-test", false)

	_, _ = runApplyRealizer(ApplyFlags{Manifest: "m.jsonc"}, mf, r, emitter, "apply-test", nil, nil, nil, nil)

	gens, _ := provision.List()
	if len(gens) != 0 {
		t.Fatalf("no generation expected when not advanced, got %d", len(gens))
	}
}

// --- Driver (winget) apply path wiring ---

func TestRunApply_DriverPathWritesGeneration(t *testing.T) {
	t.Setenv("ENDSTATE_ROOT", t.TempDir())

	// Nothing pre-installed: the mock driver installs the ref this run.
	md := &mockDriver{installed: map[string]bool{}}

	manifestContent := `{
		"version": 1,
		"name": "gen-driver-test",
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
		_, eerr = RunApply(ApplyFlags{Manifest: manifestPath})
	})
	if eerr != nil {
		t.Fatalf("unexpected envelope error: %v", eerr)
	}

	gens, _ := provision.List()
	if len(gens) != 1 {
		t.Fatalf("want 1 generation, got %d", len(gens))
	}
	g := gens[0]
	// Backend is taken from the driver's Name() (the mock reports "mock"),
	// proving the wiring is backend-agnostic. Native is empty (non-atomic).
	if g.Backend != "mock" || g.Native != "" || g.Partial {
		t.Fatalf("unexpected driver generation: %+v", g)
	}
	if len(g.AddedRefs) != 1 || g.AddedRefs[0] != "Vendor.A" {
		t.Fatalf("addedRefs: %v", g.AddedRefs)
	}
}
