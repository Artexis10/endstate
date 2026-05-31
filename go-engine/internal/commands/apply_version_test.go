// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/driver"
	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/provision"
	"github.com/Artexis10/endstate/go-engine/internal/realizer"
)

// writeManifest writes content to a temp manifest file and returns its path.
func writeManifest(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "m.jsonc")
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return p
}

// provItem finds a generation item by app ID.
func provItem(t *testing.T, items []provision.ProvItem, id string) provision.ProvItem {
	t.Helper()
	for _, it := range items {
		if it.ID == id {
			return it
		}
	}
	t.Fatalf("no generation item for id %q in %+v", id, items)
	return provision.ProvItem{}
}

// Capture: a present package's installed version is recorded in the generation.
// (A second app is installed so a generation is actually written — a present-only
// run records none.)
func TestRunApply_DriverPath_CapturesVersion(t *testing.T) {
	t.Setenv("ENDSTATE_ROOT", t.TempDir())

	md := &mockDriver{
		installed: map[string]bool{"Vendor.A": true},      // present
		versions:  map[string]string{"Vendor.A": "3.4.5"}, // captured version
	}
	mPath := writeManifest(t, `{
		"name": "capture-test",
		"apps": [
			{ "id": "a", "refs": { "windows": "Vendor.A" } },
			{ "id": "b", "refs": { "windows": "Vendor.B" } }
		]
	}`)

	var eerr *envelope.Error
	withMockDriver(md, func() { _, eerr = RunApply(ApplyFlags{Manifest: mPath}) })
	if eerr != nil {
		t.Fatalf("unexpected envelope error: %v", eerr)
	}

	gens, _ := provision.List()
	if len(gens) != 1 {
		t.Fatalf("want 1 generation, got %d", len(gens))
	}
	if v := provItem(t, gens[0].Items, "a").Version; v != "3.4.5" {
		t.Fatalf("present item version = %q, want 3.4.5 (captured)", v)
	}
}

// Pinning: a declared App.Version installs that exact version via InstallVersion
// and the generation records it.
func TestRunApply_DriverPath_PinsDeclaredVersion(t *testing.T) {
	t.Setenv("ENDSTATE_ROOT", t.TempDir())

	md := &mockDriver{installed: map[string]bool{}}
	mPath := writeManifest(t, `{
		"name": "pin-test",
		"apps": [
			{ "id": "a", "version": "1.2.0", "refs": { "windows": "Vendor.A" } }
		]
	}`)

	var eerr *envelope.Error
	withMockDriver(md, func() { _, eerr = RunApply(ApplyFlags{Manifest: mPath}) })
	if eerr != nil {
		t.Fatalf("unexpected envelope error: %v", eerr)
	}

	if md.installVersionCalls != 1 || md.lastInstallVersion != "1.2.0" {
		t.Fatalf("InstallVersion calls=%d lastVersion=%q, want 1 / 1.2.0", md.installVersionCalls, md.lastInstallVersion)
	}
	if md.installCalls != 0 {
		t.Fatalf("plain Install called %d times, want 0 (pinned)", md.installCalls)
	}
	gens, _ := provision.List()
	if len(gens) != 1 {
		t.Fatalf("want 1 generation, got %d", len(gens))
	}
	if v := provItem(t, gens[0].Items, "a").Version; v != "1.2.0" {
		t.Fatalf("installed item version = %q, want 1.2.0 (pinned)", v)
	}
}

// No declared version installs the latest via plain Install (no pinning).
func TestRunApply_DriverPath_NoVersion_InstallsLatest(t *testing.T) {
	t.Setenv("ENDSTATE_ROOT", t.TempDir())

	md := &mockDriver{installed: map[string]bool{}}
	mPath := writeManifest(t, `{
		"name": "latest-test",
		"apps": [ { "id": "a", "refs": { "windows": "Vendor.A" } } ]
	}`)

	var eerr *envelope.Error
	withMockDriver(md, func() { _, eerr = RunApply(ApplyFlags{Manifest: mPath}) })
	if eerr != nil {
		t.Fatalf("unexpected envelope error: %v", eerr)
	}
	if md.installCalls != 1 || md.installVersionCalls != 0 {
		t.Fatalf("Install=%d InstallVersion=%d, want 1 / 0 (no pin)", md.installCalls, md.installVersionCalls)
	}
}

// An unavailable pinned version fails the item as install_failed; nothing else
// is installed in its place.
func TestRunApply_DriverPath_UnavailablePin_Fails(t *testing.T) {
	t.Setenv("ENDSTATE_ROOT", t.TempDir())

	md := &mockDriver{
		installed: map[string]bool{},
		installVersionResult: &driver.InstallResult{
			Status:  driver.StatusFailed,
			Reason:  driver.ReasonInstallFailed,
			Message: "winget exited with code 1 (requested version 9.9.9)",
		},
	}
	mPath := writeManifest(t, `{
		"name": "pin-unavailable",
		"apps": [ { "id": "a", "version": "9.9.9", "refs": { "windows": "Vendor.A" } } ]
	}`)

	var raw interface{}
	var eerr *envelope.Error
	withMockDriver(md, func() { raw, eerr = RunApply(ApplyFlags{Manifest: mPath}) })
	if eerr != nil {
		t.Fatalf("unexpected envelope error: %v", eerr)
	}
	if md.installVersionCalls != 1 || md.lastInstallVersion != "9.9.9" {
		t.Fatalf("InstallVersion calls=%d version=%q, want 1 / 9.9.9", md.installVersionCalls, md.lastInstallVersion)
	}
	result := raw.(*ApplyResult)
	if result.Summary.Failed != 1 {
		t.Fatalf("Summary.Failed = %d, want 1", result.Summary.Failed)
	}
	// A failed install records no added refs, so no generation is written.
	if gens, _ := provision.List(); len(gens) != 0 {
		t.Fatalf("failed pin must record no generation, got %d", len(gens))
	}
}

// The Nix realizer path ignores App.Version (Nix pins via its ref) and does not
// error.
func TestRunApplyRealizer_IgnoresAppVersion(t *testing.T) {
	t.Setenv("ENDSTATE_ROOT", t.TempDir())

	app := nixApp("ripgrep", "nixpkgs#ripgrep")
	app.Version = "1.2.0" // declared, must be ignored by the realizer path
	mf := &manifest.Manifest{Apps: []manifest.App{app}}
	fr := &fakeRealizer{
		planDiff:      realizer.Diff{ToAdd: []realizer.Installable{{ID: "ripgrep", Ref: "nixpkgs#ripgrep"}}},
		realizeResult: realizer.Result{Advanced: true, ToGeneration: 2},
	}

	if _, eerr := runApplyRealizer(ApplyFlags{Manifest: "m.jsonc"}, mf, fr, noopEmitter(), "rz-ver", nil, nil); eerr != nil {
		t.Fatalf("realizer must ignore App.Version, got error: %v", eerr)
	}
	if fr.realizeCalls != 1 {
		t.Fatalf("Realize calls = %d, want 1 (installed via ref)", fr.realizeCalls)
	}
}
