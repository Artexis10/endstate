// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package nix

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/manifest"
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
		"./" + HomeImportModule,
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

	wantDir, name, _ := strings.Cut(ref, "#")
	if name != "tester" || filepath.Dir(wantDir) != filepath.Join(stateDir, "home-manager", "tester") {
		t.Fatalf("flakeref = %q, want a generated directory for tester", ref)
	}

	// flake.nix is written and references ./home.nix with the injected identity.
	flakeBytes, err := os.ReadFile(filepath.Join(wantDir, "flake.nix"))
	if err != nil {
		t.Fatalf("generated flake.nix not readable: %v", err)
	}
	flake := string(flakeBytes)
	for _, w := range []string{"./" + HomeImportModule, `home.username = "tester"`, `home.homeDirectory = "/home/tester"`, `homeConfigurations."tester"`} {
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

func TestGeneratedHomeImportSurvivesLaterGeneration(t *testing.T) {
	defer fakeIdentity(t)()
	stateDir := t.TempDir()
	parent := filepath.Join(stateDir, "home-manager", "tester")
	if err := os.MkdirAll(parent, 0700); err != nil {
		t.Fatal(err)
	}
	legacy := filepath.Join(parent, "home.nix")
	legacyBody := []byte("{ ... }: { programs.example.enable = true; }\n")
	if err := os.WriteFile(legacy, legacyBody, 0600); err != nil {
		t.Fatal(err)
	}
	ref, err := writeHomeFlake(stateDir, []byte("{ ... }: {}\n"), map[string][]byte{"files/payload": {0, 1, 255}}, []manifest.HomeManagerSecret{{Name: "token", Path: "/run/secrets/example"}})
	if err != nil {
		t.Fatal(err)
	}
	firstDir, _, _ := strings.Cut(ref, "#")
	firstFiles := readGeneratedDir(t, firstDir)
	nextRef, err := writeHomeFlake(stateDir, []byte("{ ... }: { programs.example.enable = false; }\n"), map[string][]byte{"files/payload": []byte("later")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	nextDir, _, _ := strings.Cut(nextRef, "#")
	if ref == nextRef || !reflect.DeepEqual(firstFiles, readGeneratedDir(t, firstDir)) {
		t.Fatal("later generation changed an earlier import or payload")
	}
	if _, err := os.Stat(filepath.Join(nextDir, "secrets.nix")); !os.IsNotExist(err) {
		t.Fatalf("secret-free generation inherited secret references: %v", err)
	}
	if strings.Contains(string(readGeneratedDir(t, nextDir)[HomeImportModule]), "secrets.nix") {
		t.Fatal("secret-free import retained an earlier secret module")
	}
	if body, err := os.ReadFile(legacy); err != nil || !bytes.Equal(body, legacyBody) {
		t.Fatalf("generation changed a legacy import: %q %v", body, err)
	}
}

func TestConcurrentHomeGenerationKeepsPayloadsSeparate(t *testing.T) {
	defer fakeIdentity(t)()
	stateDir := t.TempDir()
	type outcome struct {
		ref     string
		payload []byte
		err     error
	}
	start, completed := make(chan struct{}), make(chan outcome, 2)
	for _, payload := range [][]byte{[]byte("first"), []byte("second")} {
		go func() {
			<-start
			ref, err := writeHomeFlake(stateDir, []byte("{ ... }: {}\n"), map[string][]byte{"files/payload": payload}, nil)
			completed <- outcome{ref: ref, payload: payload, err: err}
		}()
	}
	close(start)
	seen := map[string]bool{}
	for range 2 {
		result := <-completed
		if result.err != nil || seen[result.ref] {
			t.Fatalf("generation was not independent: %q %v", result.ref, result.err)
		}
		seen[result.ref] = true
		dir, _, _ := strings.Cut(result.ref, "#")
		got, err := os.ReadFile(filepath.Join(dir, "files", "payload"))
		if err != nil || !bytes.Equal(got, result.payload) {
			t.Fatalf("generation consumed another run's payload: %q %v", got, err)
		}
	}
}

func TestFailedHomeGenerationRemovesOnlyItsIncompleteArtifact(t *testing.T) {
	defer fakeIdentity(t)()
	stateDir := t.TempDir()
	ref, err := writeHomeFlake(stateDir, []byte("{ ... }: {}\n"), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	dir, _, _ := strings.Cut(ref, "#")
	before := readGeneratedDir(t, dir)
	original := writeGeneratedHomeFile
	t.Cleanup(func() { writeGeneratedHomeFile = original })
	writeErr := errors.New("generated file storage unavailable")
	writeGeneratedHomeFile = func(path string, data []byte, mode os.FileMode) error {
		if filepath.Base(path) == HomeImportModule {
			return writeErr
		}
		return original(path, data, mode)
	}
	failedRef, err := writeHomeFlake(stateDir, []byte("{ ... }: {}\n"), map[string][]byte{"files/payload": []byte("incomplete")}, nil)
	if failedRef != "" || !errors.Is(err, writeErr) {
		t.Fatalf("failed generation published an artifact: %q %v", failedRef, err)
	}
	entries, err := os.ReadDir(filepath.Dir(dir))
	if err != nil || len(entries) != 1 || entries[0].Name() != filepath.Base(dir) {
		t.Fatalf("incomplete artifact remains or prior artifact was removed: %v %v", entries, err)
	}
	if !reflect.DeepEqual(before, readGeneratedDir(t, dir)) {
		t.Fatal("failed generation changed a prior import")
	}
}
