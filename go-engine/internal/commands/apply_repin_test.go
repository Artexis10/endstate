// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/provision"
)

// driftManifest declares app "a" pinned to 1.2.0 with a windows ref, used with a
// mockDriver that reports 1.1.0 installed (drift).
const driftManifest = `{
	"version": 1,
	"name": "repin-test",
	"apps": [ { "id": "a", "version": "1.2.0", "refs": { "windows": "Vendor.A" } } ]
}`

func driftedMock() *mockDriver {
	return &mockDriver{
		installed: map[string]bool{"Vendor.A": true},
		versions:  map[string]string{"Vendor.A": "1.1.0"}, // drifted from declared 1.2.0
	}
}

// --repin --confirm reinstalls the declared version over the drifted one and the
// generation records the converged version.
func TestRunApply_Repin_Confirm_RepinsDrift(t *testing.T) {
	t.Setenv("ENDSTATE_ROOT", t.TempDir())
	md := driftedMock()
	mPath := writeManifest(t, driftManifest)

	var eerr *envelope.Error
	withMockDriver(md, func() { _, eerr = RunApply(ApplyFlags{Manifest: mPath, Repin: true, Confirm: true}) })
	if eerr != nil {
		t.Fatalf("unexpected envelope error: %v", eerr)
	}
	if md.reinstallVersionCalls != 1 || md.lastReinstallVersion != "1.2.0" {
		t.Fatalf("ReinstallVersion calls=%d version=%q, want 1 / 1.2.0", md.reinstallVersionCalls, md.lastReinstallVersion)
	}
	gens, _ := provision.List()
	if len(gens) != 1 {
		t.Fatalf("want 1 generation recording the re-pin, got %d", len(gens))
	}
	if v := provItem(t, gens[0].Items, "a").Version; v != "1.2.0" {
		t.Fatalf("generation item version = %q, want 1.2.0", v)
	}
	if len(gens[0].AddedRefs) != 0 {
		t.Fatalf("repin generation addedRefs = %v, want empty because the package existed before the run", gens[0].AddedRefs)
	}
}

// --repin --dry-run previews the drift without reinstalling or requiring confirm,
// and records no generation.
func TestRunApply_Repin_DryRun_PreviewsNoReinstall(t *testing.T) {
	t.Setenv("ENDSTATE_ROOT", t.TempDir())
	md := driftedMock()
	mPath := writeManifest(t, driftManifest)

	var raw interface{}
	var eerr *envelope.Error
	withMockDriver(md, func() { raw, eerr = RunApply(ApplyFlags{Manifest: mPath, Repin: true, DryRun: true}) })
	if eerr != nil {
		t.Fatalf("unexpected envelope error: %v", eerr)
	}
	if md.reinstallVersionCalls != 0 {
		t.Fatalf("ReinstallVersion called %d times in dry-run, want 0", md.reinstallVersionCalls)
	}
	result := raw.(*ApplyResult)
	var found bool
	for _, a := range result.Actions {
		if a.ID == "a" {
			found = true
			if a.Reason != "version_drift" {
				t.Fatalf("dry-run drift action reason = %q, want version_drift", a.Reason)
			}
		}
	}
	if !found {
		t.Fatal("expected an action for app a in the dry-run preview")
	}
	if gens, _ := provision.List(); len(gens) != 0 {
		t.Fatalf("dry-run must record no generation, got %d", len(gens))
	}
}

// --repin without --confirm (not dry-run) refuses and reinstalls nothing.
func TestRunApply_Repin_WithoutConfirm_Refuses(t *testing.T) {
	t.Setenv("ENDSTATE_ROOT", t.TempDir())
	md := driftedMock()
	mPath := writeManifest(t, driftManifest)

	var eerr *envelope.Error
	withMockDriver(md, func() { _, eerr = RunApply(ApplyFlags{Manifest: mPath, Repin: true}) })
	if eerr == nil {
		t.Fatal("expected a refusal error for --repin without --confirm, got nil")
	}
	if eerr.Code != envelope.ErrInternalError {
		t.Fatalf("refusal code = %q, want INTERNAL_ERROR", eerr.Code)
	}
	if md.reinstallVersionCalls != 0 {
		t.Fatalf("ReinstallVersion called %d times, want 0 (refused)", md.reinstallVersionCalls)
	}
}

// Default apply (no --repin) leaves a drifted version untouched.
func TestRunApply_NoRepin_DriftUntouched(t *testing.T) {
	t.Setenv("ENDSTATE_ROOT", t.TempDir())
	md := driftedMock()
	mPath := writeManifest(t, driftManifest)

	var eerr *envelope.Error
	withMockDriver(md, func() { _, eerr = RunApply(ApplyFlags{Manifest: mPath}) })
	if eerr != nil {
		t.Fatalf("unexpected envelope error: %v", eerr)
	}
	if md.reinstallVersionCalls != 0 {
		t.Fatalf("ReinstallVersion called %d times without --repin, want 0", md.reinstallVersionCalls)
	}
	if md.versions["Vendor.A"] != "1.1.0" {
		t.Fatalf("drifted version changed to %q without --repin", md.versions["Vendor.A"])
	}
}
