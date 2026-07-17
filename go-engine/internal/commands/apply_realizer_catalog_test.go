// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/provision"
	"github.com/Artexis10/endstate/go-engine/internal/realizer"
)

// settingsManifest builds a manifest whose home-manager input is a declarative
// catalog (settings) with a curated git concept and one declared file, and returns
// the (notional) manifest path whose dir holds the file source — so files resolve
// relative to the manifest exactly as in production.
func settingsManifest(t *testing.T, app manifest.App) (*manifest.Manifest, string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "bar.conf"), []byte("hello-catalog"), 0644); err != nil {
		t.Fatal(err)
	}
	mf := nixManifest(app)
	mf.HomeManager = &manifest.HomeManagerConfig{
		Settings: &manifest.HomeManagerSettings{
			Git:   &manifest.GitSettings{UserName: "Hugo"},
			Files: map[string]string{"~/.config/bar.conf": "./bar.conf"},
		},
	}
	return mf, filepath.Join(dir, "machine.jsonc")
}

// TestRunApplyRealizer_HomeManagerSettings_GeneratesAndActivates: with
// --enable-restore and a homeManager.settings block, the config stage COMPILES a
// home.nix, stages the declared file, generates the wrapper flake, activates the
// GENERATED flakeref via the existing ActivateHome, and records it.
func TestRunApplyRealizer_HomeManagerSettings_GeneratesAndActivates(t *testing.T) {
	t.Setenv("ENDSTATE_ROOT", t.TempDir())
	app := nixApp("ripgrep", "nixpkgs#ripgrep")
	mf, manifestPath := settingsManifest(t, app)

	fr := &fakeRealizer{
		planDiff:   realizer.Diff{Present: []realizer.Installable{{ID: "ripgrep", Ref: hostRef(app)}}},
		homeGenNum: 7,
	}
	flags := ApplyFlags{Manifest: manifestPath, EnableRestore: true}
	raw, eerr := runApplyRealizer(flags, mf, fr, noopEmitter(), "run-cat1", nil, nil, nil, nil, nil)
	if eerr != nil {
		t.Fatalf("unexpected envelope error: %v", eerr)
	}
	res := raw.(*ApplyResult)

	if fr.activateCalls != 1 {
		t.Fatalf("ActivateHome calls = %d, want 1", fr.activateCalls)
	}
	if !strings.Contains(fr.lastActivateArg, filepath.FromSlash("home-manager")) || !strings.Contains(fr.lastActivateArg, "#") {
		t.Fatalf("ActivateHome arg = %q, want a generated <dir>#<name> flakeref", fr.lastActivateArg)
	}

	// The generated artifacts exist on disk: flake.nix, the compiled home.nix
	// (with the git mapping), and the staged file.
	dir := strings.SplitN(fr.lastActivateArg, "#", 2)[0]
	if _, err := os.Stat(filepath.Join(dir, "flake.nix")); err != nil {
		t.Fatalf("generated flake.nix missing: %v", err)
	}
	hn, err := os.ReadFile(filepath.Join(dir, "home.nix"))
	if err != nil {
		t.Fatalf("compiled home.nix missing: %v", err)
	}
	if !strings.Contains(string(hn), `"name" = "Hugo"`) {
		t.Errorf("compiled home.nix missing the git mapping:\n%s", hn)
	}
	if _, err := os.Stat(filepath.Join(dir, "files", ".config_bar.conf")); err != nil {
		t.Fatalf("declared file not staged into the flake dir: %v", err)
	}

	// Recorded + surfaced as a generated, activated config.
	gens, _ := provision.List()
	if len(gens) != 1 || gens[0].HomeManager == nil || gens[0].HomeManager.Flake != fr.lastActivateArg || gens[0].HomeManager.Generation != 7 {
		t.Fatalf("generation HomeManager = %+v, want flake=%q gen=7", gens, fr.lastActivateArg)
	}
	if res.HomeManager == nil || !res.HomeManager.Generated || !res.HomeManager.Activated {
		t.Fatalf("result HomeManager = %+v, want generated+activated", res.HomeManager)
	}
}

// TestRunApplyRealizer_HomeManagerSettings_DryRunRevealsNoActivate: --dry-run with
// a settings block COMPILES + reveals the generated flake but activates nothing.
func TestRunApplyRealizer_HomeManagerSettings_DryRunRevealsNoActivate(t *testing.T) {
	t.Setenv("ENDSTATE_ROOT", t.TempDir())
	app := nixApp("ripgrep", "nixpkgs#ripgrep")
	mf, manifestPath := settingsManifest(t, app)

	fr := &fakeRealizer{planDiff: realizer.Diff{Present: []realizer.Installable{{ID: "ripgrep", Ref: hostRef(app)}}}}
	flags := ApplyFlags{Manifest: manifestPath, EnableRestore: true, DryRun: true}
	raw, eerr := runApplyRealizer(flags, mf, fr, noopEmitter(), "run-cat2", nil, nil, nil, nil, nil)
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
	if res.HomeManager == nil || res.HomeManager.Activated || !res.HomeManager.Generated || res.HomeManager.Flake == "" {
		t.Fatalf("dry-run result HomeManager = %+v, want generated+revealed, not activated", res.HomeManager)
	}
	// The compiled flake exists for inspection even on dry-run.
	dir := strings.SplitN(res.HomeManager.Flake, "#", 2)[0]
	if _, err := os.Stat(filepath.Join(dir, "home.nix")); err != nil {
		t.Fatalf("dry-run did not compile the inspectable home.nix: %v", err)
	}
}
