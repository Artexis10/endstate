// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package nix

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func typedGenerationIdentity(t *testing.T) {
	t.Helper()
	orig := homeIdentityFn
	homeIdentityFn = func() (HomeIdentity, error) {
		return HomeIdentity{Username: "tester", HomeDir: "/home/tester", StateVersion: "24.05"}, nil
	}
	t.Cleanup(func() { homeIdentityFn = orig })
}

func TestGenerateHomeFlakeWithPlacementsWritesTypedSettingsAsASeparateModule(t *testing.T) {
	typedGenerationIdentity(t)
	manifestDir, stateDir := t.TempDir(), t.TempDir()
	explicit := []byte("{ ... }: { programs.git.enable = true; }\n")
	if err := os.WriteFile(filepath.Join(manifestDir, "home.nix"), explicit, 0o644); err != nil {
		t.Fatal(err)
	}
	canonical := []byte("[virtualenvs]\nin-project = true\n")
	typed := TypedSetting{Program: "poetry", Option: "programs.poetry.settings", NullPackage: true, Canonical: canonical}

	ref, err := GenerateHomeFlakeWithPlacements(stateDir, nil, manifestDir, nil, "home.nix", nil, []TypedSetting{typed})
	if err != nil {
		t.Fatal(err)
	}
	directory, _, _ := strings.Cut(ref, "#")
	read := func(name string) string {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(directory, name))
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	sum := sha256.Sum256(canonical)
	staged := "typed/" + hex.EncodeToString(sum[:]) + ".toml"
	module := read("typed.nix")
	for _, want := range []string{
		`programs."poetry".enable = true;`,
		`programs."poetry".package = null;`,
		`programs."poetry".settings = builtins.fromTOML (builtins.readFile ./` + staged + `);`,
	} {
		if !strings.Contains(module, want) {
			t.Errorf("typed module lacks %q:\n%s", want, module)
		}
	}
	if read(staged) != string(canonical) {
		t.Errorf("staged payload is not the canonical bytes: %q", read(staged))
	}
	if read("explicit.nix") != string(explicit) {
		t.Error("the explicit configuration was edited")
	}
	// The payload reaches Home Manager through the option, never as a placed file.
	if placements := read("placements.nix"); strings.Contains(placements, "pypoetry") || strings.Contains(placements, "poetry") {
		t.Errorf("typed settings were also placed as a file:\n%s", placements)
	}
	if entry := read("home.nix"); entry != "{ ... }: { imports = [ ./explicit.nix ./placements.nix ./typed.nix ]; }\n" {
		t.Errorf("typed module is not imported beside the others: %q", entry)
	}
}

func TestGenerateHomeFlakeWithPlacementsLeavesThePackageAloneWhenItCannotBeNull(t *testing.T) {
	typedGenerationIdentity(t)
	ref, err := GenerateHomeFlakeWithPlacements(t.TempDir(), nil, t.TempDir(), nil, "", nil, []TypedSetting{
		{Program: "poetry", Option: "programs.poetry.settings", Canonical: []byte("[virtualenvs]\nin-project = true\n")},
	})
	if err != nil {
		t.Fatal(err)
	}
	directory, _, _ := strings.Cut(ref, "#")
	module, err := os.ReadFile(filepath.Join(directory, "typed.nix"))
	if err != nil || strings.Contains(string(module), "package") {
		t.Fatalf("package option was set although it cannot be null: %s %v", module, err)
	}
	entry, _ := os.ReadFile(filepath.Join(directory, "home.nix"))
	if string(entry) != "{ ... }: { imports = [ ./placements.nix ./typed.nix ]; }\n" {
		t.Errorf("typed module is not imported without an explicit configuration: %q", entry)
	}
}

func TestGenerateHomeFlakeWithPlacementsRefusesTypedSettingsOutsideTheSettingsOption(t *testing.T) {
	typedGenerationIdentity(t)
	for _, setting := range []TypedSetting{
		{Program: "poetry", Option: "programs.poetry.extraConfig", Canonical: []byte("x = 1\n")},
		{Program: "poetry", Option: "programs.git.settings", Canonical: []byte("x = 1\n")},
		{Program: "", Option: "programs..settings", Canonical: []byte("x = 1\n")},
	} {
		if _, err := GenerateHomeFlakeWithPlacements(t.TempDir(), nil, t.TempDir(), nil, "", nil, []TypedSetting{setting}); err == nil {
			t.Errorf("typed setting for %q was rendered into %q", setting.Program, setting.Option)
		}
	}
}

func TestTypedModuleKeepsAHostileProgramNameInsideOneQuotedAttribute(t *testing.T) {
	module, _, err := compileTypedModule([]TypedSetting{{Program: `a"; evil = "`, Option: `programs.a"; evil = ".settings`, Canonical: []byte("x = 1\n")}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(module), `programs."a\"; evil = \"".enable = true;`) || !strings.Contains(string(module), `programs."a\"; evil = \"".settings = `) {
		t.Fatalf("program name escaped its attribute:\n%s", module)
	}
}
