// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package nix

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/manifest"
)

// TestNixValue covers the JSON→Nix value encoder the raw `programs` passthrough
// depends on: bools, integer/float numbers, escaped strings, lists, and
// deterministic (sorted-key) attrsets.
func TestNixValue(t *testing.T) {
	cases := []struct {
		name string
		in   any
		want string
	}{
		{"bool-true", true, "true"},
		{"bool-false", false, "false"},
		{"int", float64(42), "42"},
		{"float", float64(1.5), "1.5"},
		{"string", "hi", `"hi"`},
		{"string-escaped", `a"b\c`, `"a\"b\\c"`},
		{"list", []any{"a", float64(1), true}, `[ "a" 1 true ]`},
		{"attrset-sorted", map[string]any{"b": float64(1), "a": "x"}, `{ "a" = "x"; "b" = 1; }`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := nixValue(c.in); got != c.want {
				t.Errorf("nixValue(%v) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

// TestNixValue_EscapesInterpolation: a literal "${" must be escaped to "\${" so it
// is not treated as Nix antiquotation in the generated config.
func TestNixValue_EscapesInterpolation(t *testing.T) {
	got := nixValue("a${b}")
	if got != `"a\${b}"` {
		t.Errorf("nixValue did not escape ${ interpolation: got %q, want %q", got, `"a\${b}"`)
	}
}

// TestCompileHomeNix_CuratedAndRaw: curated concepts map to home-manager options
// (git via the stable extraConfig, shell to home.*, direnv toggle) and the raw
// programs block is forwarded verbatim.
func TestCompileHomeNix_CuratedAndRaw(t *testing.T) {
	s := &manifest.HomeManagerSettings{
		Git:      &manifest.GitSettings{UserName: "Hugo", UserEmail: "h@x.com", DefaultBranch: "main"},
		Shell:    &manifest.ShellSettings{Aliases: map[string]string{"ll": "ls -la"}, SessionVariables: map[string]string{"EDITOR": "nvim"}},
		Direnv:   &manifest.ProgramToggle{Enable: true},
		Programs: map[string]any{"bat": map[string]any{"enable": true}},
	}
	home, staged, err := CompileHomeNix(s, t.TempDir())
	if err != nil {
		t.Fatalf("CompileHomeNix: %v", err)
	}
	if len(staged) != 0 {
		t.Errorf("no files declared, want no staged files, got %v", staged)
	}
	out := string(home)
	for _, w := range []string{
		"programs.git.enable = true;",
		`"name" = "Hugo"`,
		`"email" = "h@x.com"`,
		`"defaultBranch" = "main"`,
		"home.shellAliases",
		`"ll" = "ls -la";`,
		"home.sessionVariables",
		`"EDITOR" = "nvim";`,
		"programs.direnv.enable = true;",
		"programs.bat = ", // raw passthrough, verbatim program name
		`"enable" = true`,
	} {
		if !strings.Contains(out, w) {
			t.Errorf("compiled home.nix missing %q\n---\n%s", w, out)
		}
	}
	// It is a home-manager module.
	if !strings.HasPrefix(strings.TrimSpace(out), "{") {
		t.Errorf("compiled home.nix is not a module:\n%s", out)
	}
}

// TestCompileHomeNix_Deterministic: identical settings → identical output.
func TestCompileHomeNix_Deterministic(t *testing.T) {
	s := &manifest.HomeManagerSettings{
		Programs: map[string]any{"bat": map[string]any{"enable": true}, "fzf": map[string]any{"enable": true}},
	}
	d := t.TempDir()
	a, _, err := CompileHomeNix(s, d)
	if err != nil {
		t.Fatal(err)
	}
	b, _, err := CompileHomeNix(s, d)
	if err != nil {
		t.Fatal(err)
	}
	if string(a) != string(b) {
		t.Error("CompileHomeNix is not deterministic for identical input")
	}
}

// TestCompileHomeNix_RawProgramOverlapErrors: a raw programs key that collides
// with a curated concept (e.g. raw programs.git + curated git) is a clear error,
// not a double-definition Nix would choke on.
func TestCompileHomeNix_RawProgramOverlapErrors(t *testing.T) {
	s := &manifest.HomeManagerSettings{
		Git:      &manifest.GitSettings{UserName: "Hugo"},
		Programs: map[string]any{"git": map[string]any{"enable": true}},
	}
	if _, _, err := CompileHomeNix(s, t.TempDir()); err == nil {
		t.Fatal("expected an error for raw programs.git overlapping the curated git concept, got nil")
	}
}

// TestCompileHomeNix_StagesFiles: a declared file is staged (content captured) and
// referenced via home.file with a relative source path.
func TestCompileHomeNix_StagesFiles(t *testing.T) {
	manDir := t.TempDir()
	src := filepath.Join(manDir, "payload", "bar.conf")
	if err := os.MkdirAll(filepath.Dir(src), 0o755); err != nil {
		t.Fatal(err)
	}
	body := []byte("opaque\x00binary\xffcontent") // binary, not text
	if err := os.WriteFile(src, body, 0o644); err != nil {
		t.Fatal(err)
	}
	s := &manifest.HomeManagerSettings{
		Files: map[string]string{"~/.config/foo/bar.conf": "./payload/bar.conf"},
	}
	home, staged, err := CompileHomeNix(s, manDir)
	if err != nil {
		t.Fatalf("CompileHomeNix: %v", err)
	}
	if len(staged) != 1 {
		t.Fatalf("want 1 staged file, got %d (%v)", len(staged), staged)
	}
	// The staged content is the verbatim (binary-safe) source.
	var stagedRel string
	for rel, content := range staged {
		stagedRel = rel
		if string(content) != string(body) {
			t.Errorf("staged content mismatch (binary not preserved)")
		}
		if !strings.HasPrefix(rel, "files/") {
			t.Errorf("staged path %q should be under files/", rel)
		}
	}
	out := string(home)
	if !strings.Contains(out, `home.file.".config/foo/bar.conf".source = ./`+stagedRel) {
		t.Errorf("compiled home.nix missing home.file entry for the staged source (%s):\n%s", stagedRel, out)
	}
}

// TestCompileHomeNix_MissingFileSourceErrors: a declared source that does not
// exist fails clearly rather than silently dropping the file.
func TestCompileHomeNix_MissingFileSourceErrors(t *testing.T) {
	s := &manifest.HomeManagerSettings{Files: map[string]string{"~/.x": "./does-not-exist"}}
	if _, _, err := CompileHomeNix(s, t.TempDir()); err == nil {
		t.Fatal("expected an error for a missing file source, got nil")
	}
}

// TestGenerateHomeFlakeFromSettings_WritesSelfContainedFlake: the settings entry
// compiles the home.nix, stages files, reuses the wrapper's flake generation, and
// returns the <dir>#<name> flakeref — a self-contained, inspectable flake.
func TestGenerateHomeFlakeFromSettings_WritesSelfContainedFlake(t *testing.T) {
	orig := homeIdentityFn
	homeIdentityFn = func() (HomeIdentity, error) {
		return HomeIdentity{Username: "tester", HomeDir: "/home/tester", StateVersion: "24.05"}, nil
	}
	defer func() { homeIdentityFn = orig }()

	manDir := t.TempDir()
	src := filepath.Join(manDir, "bar.conf")
	if err := os.WriteFile(src, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := &manifest.HomeManagerSettings{
		Git:   &manifest.GitSettings{UserName: "Hugo"},
		Files: map[string]string{"~/.config/bar.conf": "./bar.conf"},
	}

	stateDir := t.TempDir()
	ref, err := GenerateHomeFlakeFromSettings(stateDir, s, manDir)
	if err != nil {
		t.Fatalf("GenerateHomeFlakeFromSettings: %v", err)
	}
	wantDir := filepath.Join(stateDir, "home-manager", "tester")
	if ref != wantDir+"#tester" {
		t.Errorf("ref = %q, want %q", ref, wantDir+"#tester")
	}
	// flake.nix + compiled home.nix + staged file all present and readable.
	for _, rel := range []string{"flake.nix", "home.nix", "files/.config_bar.conf"} {
		if _, err := os.Stat(filepath.Join(wantDir, rel)); err != nil {
			t.Errorf("expected generated artifact %q to exist: %v", rel, err)
		}
	}
	hn, _ := os.ReadFile(filepath.Join(wantDir, "home.nix"))
	if !strings.Contains(string(hn), `"name" = "Hugo"`) {
		t.Errorf("generated home.nix missing the git mapping:\n%s", hn)
	}
}
