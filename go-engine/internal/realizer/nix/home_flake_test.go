// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package nix

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRenderHomeFlake_PinsIdentityModule asserts the pure render wraps the user's
// home.nix with pinned nixpkgs + home-manager (nixpkgs follows), the engine-
// injected identity, and the relative module reference — the inspectable contract
// the engine generates so the user writes config, not flake plumbing.
func TestRenderHomeFlake_PinsIdentityModule(t *testing.T) {
	spec := HomeFlakeSpec{
		Name:           "hugo",
		Username:       "hugo",
		HomeDir:        "/home/hugo",
		StateVersion:   "24.05",
		System:         "x86_64-linux",
		NixpkgsPin:     "github:NixOS/nixpkgs/abc123",
		HomeManagerPin: "github:nix-community/home-manager/def456",
	}
	out := renderHomeFlake(spec)

	wants := []string{
		// pinned inputs (nixpkgs + home-manager) with nixpkgs following
		`nixpkgs.url = "github:NixOS/nixpkgs/abc123"`,
		`home-manager.url = "github:nix-community/home-manager/def456"`,
		`home-manager.inputs.nixpkgs.follows = "nixpkgs"`,
		// the user's config, referenced relative to the flake dir (copied in)
		"./home.nix",
		// engine-injected identity
		`home.username = "hugo"`,
		`home.homeDirectory = "/home/hugo"`,
		`home.stateVersion = "24.05"`,
		// the named configuration + host system
		`homeConfigurations."hugo"`,
		`legacyPackages."x86_64-linux"`,
	}
	for _, w := range wants {
		if !strings.Contains(out, w) {
			t.Errorf("rendered flake missing %q\n---\n%s", w, out)
		}
	}

	// Inspectability: the generated flake must NOT embed a raw absolute path to
	// the user's config (pure-eval forbids it; the file is copied in instead).
	if strings.Contains(out, "/home/hugo/home.nix") {
		t.Errorf("rendered flake references an absolute config path; want the copied-in ./home.nix:\n%s", out)
	}
}

// TestRenderHomeFlake_Deterministic: same spec → identical output (pure template).
func TestRenderHomeFlake_Deterministic(t *testing.T) {
	spec := HomeFlakeSpec{Name: "x", Username: "x", HomeDir: "/h", StateVersion: "24.05", System: "x86_64-linux", NixpkgsPin: "nixpkgs", HomeManagerPin: "github:nix-community/home-manager"}
	if renderHomeFlake(spec) != renderHomeFlake(spec) {
		t.Error("renderHomeFlake is not deterministic for identical input")
	}
}

// TestGenerateHomeFlake_WritesSelfContainedFlakeAndReturnsRef: the generator
// writes a self-contained, inspectable flake (flake.nix + a copy of the user's
// home.nix) under <stateDir>/home-manager/<name>/ and returns the <dir>#<name>
// flakeref the existing ActivateHome consumes. Identity is injected (not read
// from the host) so the test is hermetic.
func TestGenerateHomeFlake_WritesSelfContainedFlakeAndReturnsRef(t *testing.T) {
	// Inject deterministic identity.
	orig := homeIdentityFn
	homeIdentityFn = func() (HomeIdentity, error) {
		return HomeIdentity{Username: "tester", HomeDir: "/home/tester", StateVersion: "24.05"}, nil
	}
	defer func() { homeIdentityFn = orig }()

	// The user's home.nix lives next to their manifest.
	srcDir := t.TempDir()
	srcCfg := filepath.Join(srcDir, "home.nix")
	cfgBody := "{ ... }:\n{\n  programs.git.userName = \"smoke\";\n}\n"
	if err := os.WriteFile(srcCfg, []byte(cfgBody), 0644); err != nil {
		t.Fatal(err)
	}

	stateDir := t.TempDir()
	ref, err := GenerateHomeFlake(stateDir, srcCfg, nil)
	if err != nil {
		t.Fatalf("GenerateHomeFlake: %v", err)
	}

	wantDir := filepath.Join(stateDir, "home-manager", "tester")
	wantRef := wantDir + "#tester"
	if ref != wantRef {
		t.Errorf("flakeref = %q, want %q", ref, wantRef)
	}

	// flake.nix is written and references ./home.nix with the injected identity.
	flakeBytes, err := os.ReadFile(filepath.Join(wantDir, "flake.nix"))
	if err != nil {
		t.Fatalf("generated flake.nix not readable: %v", err)
	}
	flake := string(flakeBytes)
	for _, w := range []string{"./home.nix", `home.username = "tester"`, `home.homeDirectory = "/home/tester"`, `homeConfigurations."tester"`} {
		if !strings.Contains(flake, w) {
			t.Errorf("generated flake.nix missing %q", w)
		}
	}

	// The user's home.nix is copied in verbatim (so the flake is self-contained
	// and ejectable — a power user can switch it by hand).
	copied, err := os.ReadFile(filepath.Join(wantDir, "home.nix"))
	if err != nil {
		t.Fatalf("copied home.nix not readable: %v", err)
	}
	if string(copied) != cfgBody {
		t.Errorf("copied home.nix = %q, want verbatim copy %q", string(copied), cfgBody)
	}
}

// TestGenerateHomeFlake_MissingConfigErrors: a config path that does not exist is
// a clear error (not a silent empty flake).
func TestGenerateHomeFlake_MissingConfigErrors(t *testing.T) {
	orig := homeIdentityFn
	homeIdentityFn = func() (HomeIdentity, error) {
		return HomeIdentity{Username: "tester", HomeDir: "/home/tester", StateVersion: "24.05"}, nil
	}
	defer func() { homeIdentityFn = orig }()

	if _, err := GenerateHomeFlake(t.TempDir(), filepath.Join(t.TempDir(), "nope.nix"), nil); err == nil {
		t.Fatal("expected an error for a missing config path, got nil")
	}
}
