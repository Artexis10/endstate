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

// fakeIdentity installs a deterministic, host-independent identity for the
// generator tests and returns a restore func.
func fakeIdentity(t *testing.T) func() {
	t.Helper()
	orig := homeIdentityFn
	homeIdentityFn = func() (HomeIdentity, error) {
		return HomeIdentity{Username: "tester", HomeDir: "/home/tester", StateVersion: "24.05"}, nil
	}
	return func() { homeIdentityFn = orig }
}

// readGeneratedDir reads every regular file under dir, returning name→content.
func readGeneratedDir(t *testing.T, dir string) map[string][]byte {
	t.Helper()
	out := map[string][]byte{}
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		b, rerr := os.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		rel, _ := filepath.Rel(dir, p)
		out[rel] = b
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// TestCompileSecretsModule_PathEmitsHomeFileSourceReference: a path entry emits a
// home.file.<homeRelTarget>.source REFERENCE to the path.
func TestCompileSecretsModule_PathEmitsHomeFileSourceReference(t *testing.T) {
	mod, ok := compileSecretsModule([]manifest.HomeManagerSecret{
		{Name: "~/.config/secret.conf", Path: "/run/secrets/secret.conf"},
	})
	if !ok {
		t.Fatal("compileSecretsModule returned ok=false for a path entry")
	}
	want := `home.file.".config/secret.conf".source = config.lib.file.mkOutOfStoreSymlink "/run/secrets/secret.conf";`
	if !strings.Contains(mod, want) {
		t.Errorf("secrets module missing %q\n---\n%s", want, mod)
	}
}

// TestCompileSecretsModule_Deterministic: secrets are sorted by name, so the same
// input (in any order) yields identical output.
func TestCompileSecretsModule_Deterministic(t *testing.T) {
	a, _ := compileSecretsModule([]manifest.HomeManagerSecret{
		{Name: "zeta", Path: "/run/z"}, {Name: "alpha", Path: "/run/a"},
	})
	b, _ := compileSecretsModule([]manifest.HomeManagerSecret{
		{Name: "alpha", Path: "/run/a"}, {Name: "zeta", Path: "/run/z"},
	})
	if a != b {
		t.Errorf("non-deterministic output:\n--a--\n%s\n--b--\n%s", a, b)
	}
	// alpha must precede zeta in the stable output.
	if strings.Index(a, "alpha") > strings.Index(a, "zeta") {
		t.Errorf("secrets not sorted by name:\n%s", a)
	}
}

// TestCompileSecretsModule_Empty: no secrets ⇒ no module.
func TestCompileSecretsModule_Empty(t *testing.T) {
	if _, ok := compileSecretsModule(nil); ok {
		t.Error("compileSecretsModule(nil) returned ok=true")
	}
}

// TestCompileSecretsModule_EnvEmitsSessionVariableReference: an env+path entry emits
// a home.sessionVariables.<Env> referencing the PATH (the *_FILE convention), and
// does NOT emit a home.file sink for it (env entries are variables, not file
// targets).
func TestCompileSecretsModule_EnvEmitsSessionVariableReference(t *testing.T) {
	mod, ok := compileSecretsModule([]manifest.HomeManagerSecret{
		{Name: "token", Env: "API_TOKEN", Path: "/run/secrets/api"},
	})
	if !ok {
		t.Fatal("compileSecretsModule returned ok=false for an env+path entry")
	}
	want := `home.sessionVariables.API_TOKEN = "/run/secrets/api";`
	if !strings.Contains(mod, want) {
		t.Errorf("secrets module missing %q\n---\n%s", want, mod)
	}
	// An env entry must NOT also emit a home.file sink — Name is a var identifier,
	// not a file target.
	if strings.Contains(mod, "home.file.") {
		t.Errorf("env entry must not emit a home.file sink:\n%s", mod)
	}
}

// TestCompileSecretsModule_EnvUsesNixStringPath: the referenced path is emitted via
// nixString, so a Windows-style backslash path is escaped (no spurious failure on
// any OS) and never embedded raw.
func TestCompileSecretsModule_EnvUsesNixStringPath(t *testing.T) {
	const winPath = `C:\ProgramData\secrets\api.token`
	mod, ok := compileSecretsModule([]manifest.HomeManagerSecret{
		{Name: "token", Env: "API_TOKEN", Path: winPath},
	})
	if !ok {
		t.Fatal("compileSecretsModule returned ok=false")
	}
	want := "home.sessionVariables.API_TOKEN = " + nixString(winPath) + ";"
	if !strings.Contains(mod, want) {
		t.Errorf("secrets module missing nix-encoded reference %q\n---\n%s", want, mod)
	}
}

// TestCompileSecretsModule_MixedDeterministic: a mix of path-only and env+path
// entries is sorted by name, so any input order yields identical output.
func TestCompileSecretsModule_MixedDeterministic(t *testing.T) {
	a, _ := compileSecretsModule([]manifest.HomeManagerSecret{
		{Name: "zeta", Env: "ZETA", Path: "/run/z"},
		{Name: "alpha", Path: "/run/a"},
	})
	b, _ := compileSecretsModule([]manifest.HomeManagerSecret{
		{Name: "alpha", Path: "/run/a"},
		{Name: "zeta", Env: "ZETA", Path: "/run/z"},
	})
	if a != b {
		t.Errorf("non-deterministic output:\n--a--\n%s\n--b--\n%s", a, b)
	}
	// Sorted by name: alpha (path-only home.file) must precede zeta (env
	// sessionVariable). The env case emits no Name token, so anchor on the emitted
	// statements themselves.
	if strings.Index(a, `home.file."alpha"`) > strings.Index(a, `home.sessionVariables.ZETA`) {
		t.Errorf("secrets not sorted by name (alpha before zeta):\n%s", a)
	}
	// Both kinds of statement present.
	if !strings.Contains(a, `home.file.`) || !strings.Contains(a, `home.sessionVariables.ZETA`) {
		t.Errorf("expected both a home.file (path-only) and a sessionVariable (env+path):\n%s", a)
	}
}

// TestWriteHomeFlake_NoEmbed_EnvSentinelAbsentFromEntireGeneratedTree mirrors the
// path-only no-embed keystone for env+path entries: a sentinel written at the env
// secret's PATH must be absent from every generated byte, while the reference (the
// path) is present in secrets.nix.
func TestWriteHomeFlake_NoEmbed_EnvSentinelAbsentFromEntireGeneratedTree(t *testing.T) {
	defer fakeIdentity(t)()

	const sentinel = "ENV-SECRET-SENTINEL-DO-NOT-LEAK-c4d8e1"
	manDir := t.TempDir()
	secretFile := filepath.Join(manDir, "the-env-secret")
	if err := os.WriteFile(secretFile, []byte(sentinel), 0600); err != nil {
		t.Fatal(err)
	}

	stateDir := t.TempDir()
	ref, err := GenerateHomeFlakeFromSettings(
		stateDir,
		&manifest.HomeManagerSettings{Git: &manifest.GitSettings{UserName: "Hugo"}},
		manDir,
		[]manifest.HomeManagerSecret{
			{Name: "token", Env: "API_TOKEN", Path: secretFile}, // env references the sentinel file
		},
	)
	if err != nil {
		t.Fatalf("GenerateHomeFlakeFromSettings: %v", err)
	}
	dir := strings.SplitN(ref, "#", 2)[0]

	for name, content := range readGeneratedDir(t, dir) {
		if strings.Contains(string(content), sentinel) {
			t.Fatalf("NO-EMBED VIOLATION: sentinel found in generated file %q — an env secret's content was read into the generated tree", name)
		}
	}

	// The path REFERENCE itself must still be present in secrets.nix (nix-encoded so
	// a Windows temp path doesn't spuriously fail the substring check).
	secMod, ok := readGeneratedDir(t, dir)["secrets.nix"]
	if !ok {
		t.Fatal("secrets.nix not staged")
	}
	wantRef := "home.sessionVariables.API_TOKEN = " + nixString(secretFile) + ";"
	if !strings.Contains(string(secMod), wantRef) {
		t.Errorf("secrets.nix must REFERENCE the secret path via the sessionVariable %q:\n%s", wantRef, secMod)
	}
}

// TestWriteHomeFlake_SecretsStagedModuleReferencedNotEmbedded: the generated flake
// references ./secrets.nix as a module, and secrets.nix carries the references.
func TestWriteHomeFlake_SecretsStagedModuleReferencedNotEmbedded(t *testing.T) {
	defer fakeIdentity(t)()
	stateDir := t.TempDir()
	ref, err := GenerateHomeFlakeFromSettings(
		stateDir,
		&manifest.HomeManagerSettings{Git: &manifest.GitSettings{UserName: "Hugo"}},
		t.TempDir(),
		[]manifest.HomeManagerSecret{{Name: "~/.config/tok", Path: "/run/secrets/tok"}},
	)
	if err != nil {
		t.Fatalf("GenerateHomeFlakeFromSettings: %v", err)
	}
	dir := strings.SplitN(ref, "#", 2)[0]
	files := readGeneratedDir(t, dir)
	flake, ok := files["flake.nix"]
	if !ok {
		t.Fatal("flake.nix not generated")
	}
	if !strings.Contains(string(flake), "./secrets.nix") {
		t.Errorf("flake.nix does not reference ./secrets.nix:\n%s", flake)
	}
	secMod, ok := files["secrets.nix"]
	if !ok {
		t.Fatal("secrets.nix not staged")
	}
	if !strings.Contains(string(secMod), `mkOutOfStoreSymlink "/run/secrets/tok"`) {
		t.Errorf("secrets.nix missing the path reference:\n%s", secMod)
	}
}

// TestWriteHomeFlake_NoEmbed_SentinelAbsentFromEntireGeneratedTree is the
// STRUCTURAL keystone test: a secret entry must NEVER enter the staged map and its
// path must NEVER be os.ReadFile'd. We write a SENTINEL into the file the secret
// path points at, generate, and assert the sentinel is ABSENT from EVERY generated
// byte (flake.nix / home.nix / secrets.nix / any staged file).
func TestWriteHomeFlake_NoEmbed_SentinelAbsentFromEntireGeneratedTree(t *testing.T) {
	defer fakeIdentity(t)()

	const sentinel = "SUPER-SECRET-SENTINEL-DO-NOT-LEAK-7f3a9c"
	manDir := t.TempDir()
	secretFile := filepath.Join(manDir, "the-secret")
	if err := os.WriteFile(secretFile, []byte(sentinel), 0600); err != nil {
		t.Fatal(err)
	}

	stateDir := t.TempDir()
	ref, err := GenerateHomeFlakeFromSettings(
		stateDir,
		&manifest.HomeManagerSettings{Git: &manifest.GitSettings{UserName: "Hugo"}},
		manDir,
		[]manifest.HomeManagerSecret{
			{Name: "~/.secret", Path: secretFile}, // path points AT the sentinel file
		},
	)
	if err != nil {
		t.Fatalf("GenerateHomeFlakeFromSettings: %v", err)
	}
	dir := strings.SplitN(ref, "#", 2)[0]

	for name, content := range readGeneratedDir(t, dir) {
		if strings.Contains(string(content), sentinel) {
			t.Fatalf("NO-EMBED VIOLATION: sentinel found in generated file %q — a secret's content was read into the generated tree", name)
		}
	}

	// The path REFERENCE itself must still be present (referenced, never embedded).
	secMod, ok := readGeneratedDir(t, dir)["secrets.nix"]
	if !ok {
		t.Fatal("secrets.nix not staged")
	}
	// Assert against the nix-ENCODED path (nixString escapes backslashes, so a raw
	// Windows temp path C:\... appears as "C:\\..." in the module — checking the raw
	// path would spuriously fail on Windows even though the reference is present).
	if !strings.Contains(string(secMod), nixString(secretFile)) {
		t.Errorf("secrets.nix must REFERENCE the secret path %q:\n%s", secretFile, secMod)
	}
}

// TestGenerateHomeFlake_ConfigMode_ComposesSecrets: secrets also compose with
// config-mode (a user home.nix the engine wraps) — secrets.nix is staged and the
// user's home.nix is untouched.
func TestGenerateHomeFlake_ConfigMode_ComposesSecrets(t *testing.T) {
	defer fakeIdentity(t)()
	manDir := t.TempDir()
	userCfg := filepath.Join(manDir, "home.nix")
	const userMarker = "USER-HOME-NIX-CONTENT"
	if err := os.WriteFile(userCfg, []byte("{ ... }:\n{\n  # "+userMarker+"\n}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	stateDir := t.TempDir()
	ref, err := GenerateHomeFlake(stateDir, userCfg, []manifest.HomeManagerSecret{{Name: "p", Path: "/run/p"}})
	if err != nil {
		t.Fatalf("GenerateHomeFlake: %v", err)
	}
	dir := strings.SplitN(ref, "#", 2)[0]
	files := readGeneratedDir(t, dir)
	if _, ok := files["secrets.nix"]; !ok {
		t.Error("secrets.nix not staged in config mode")
	}
	if !strings.Contains(string(files["home.nix"]), userMarker) {
		t.Error("user's home.nix was not copied in verbatim")
	}
	if strings.Contains(string(files["home.nix"]), "/run/p") {
		t.Error("secret reference leaked into the user's home.nix; it must live in secrets.nix")
	}
}
