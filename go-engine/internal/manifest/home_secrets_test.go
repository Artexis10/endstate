// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package manifest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeManifest writes content to a temp .jsonc file and returns its path.
func writeSecretsManifest(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "m.jsonc")
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestLoadManifest_HomeManagerSecrets_PathUnmarshals: a path-based secret entry
// round-trips through JSONC load into HomeManagerConfig.Secrets (a sibling of
// settings/config/flake). Composes with settings.
func TestLoadManifest_HomeManagerSecrets_PathUnmarshals(t *testing.T) {
	p := writeSecretsManifest(t, `{
  "version": 1, "name": "secrets", "apps": [],
  "homeManager": {
    "settings": { "git": { "userName": "Hugo" } },
    "secrets": [ { "name": "~/.npmrc", "path": "/run/secrets/npmrc" } ]
  }
}`)
	m, err := LoadManifest(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.HomeManager == nil || len(m.HomeManager.Secrets) != 1 {
		t.Fatalf("HomeManager.Secrets = %+v, want one entry", m.HomeManager)
	}
	s := m.HomeManager.Secrets[0]
	if s.Name != "~/.npmrc" || s.Path != "/run/secrets/npmrc" || s.Env != "" {
		t.Errorf("secret = %+v, want name=~/.npmrc path=/run/secrets/npmrc env=\"\"", s)
	}
	if m.HomeManager.Settings == nil {
		t.Error("secrets must compose with settings; settings was dropped")
	}
}

// TestLoadManifest_HomeManagerSecrets_AcceptsEnvWithPath: Phase 2 — an env entry
// that ALSO carries a path is valid. The engine emits a sessionVariable referencing
// the FILE PATH (the *_FILE path-reference convention), never the value.
func TestLoadManifest_HomeManagerSecrets_AcceptsEnvWithPath(t *testing.T) {
	p := writeSecretsManifest(t, `{
  "version": 1, "name": "secrets", "apps": [],
  "homeManager": { "settings": { "git": { "userName": "Hugo" } },
    "secrets": [ { "name": "token", "env": "API_TOKEN", "path": "/run/secrets/api" } ] }
}`)
	m, err := LoadManifest(p)
	if err != nil {
		t.Fatalf("env+path secret must load (Phase 2 path-reference), got %v", err)
	}
	if m.HomeManager == nil || len(m.HomeManager.Secrets) != 1 {
		t.Fatalf("HomeManager.Secrets = %+v, want one entry", m.HomeManager)
	}
	s := m.HomeManager.Secrets[0]
	if s.Env != "API_TOKEN" || s.Path != "/run/secrets/api" {
		t.Errorf("secret = %+v, want env=API_TOKEN path=/run/secrets/api", s)
	}
}

// TestLoadManifest_HomeManagerSecrets_RejectsEnvWithoutPath: an env entry MUST also
// declare a path (the engine references the file path, never the value) — env alone
// fails to load with a message directing the user to declare the file via "path".
func TestLoadManifest_HomeManagerSecrets_RejectsEnvWithoutPath(t *testing.T) {
	p := writeSecretsManifest(t, `{
  "version": 1, "name": "secrets", "apps": [],
  "homeManager": { "settings": { "git": { "userName": "Hugo" } },
    "secrets": [ { "name": "token", "env": "API_TOKEN" } ] }
}`)
	_, err := LoadManifest(p)
	if err == nil {
		t.Fatal("expected an error for an env secret without a path, got nil")
	}
	if !strings.Contains(err.Error(), "path") {
		t.Errorf("error = %q, want it to direct the user to declare a \"path\"", err)
	}
}

// TestLoadManifest_HomeManagerSecrets_RejectsInvalidEnvName: an env name that is not
// a valid shell/Nix identifier is rejected at load (this also blocks Nix-attr
// injection via a crafted name before any emission).
func TestLoadManifest_HomeManagerSecrets_RejectsInvalidEnvName(t *testing.T) {
	p := writeSecretsManifest(t, `{
  "version": 1, "name": "secrets", "apps": [],
  "homeManager": { "settings": { "git": { "userName": "Hugo" } },
    "secrets": [ { "name": "token", "env": "x = \"evil\"; y", "path": "/run/x" } ] }
}`)
	_, err := LoadManifest(p)
	if err == nil {
		t.Fatal("expected an error for an invalid env name, got nil")
	}
	if !strings.Contains(err.Error(), "env") {
		t.Errorf("error = %q, want it to mention the invalid env name", err)
	}
}

// TestLoadManifest_HomeManagerSecrets_AcceptsBothPathAndEnv: Phase 2 — declaring
// both path and env is the env-exposed-secret shape (env names the variable; path
// names the file referenced). It is valid.
func TestLoadManifest_HomeManagerSecrets_AcceptsBothPathAndEnv(t *testing.T) {
	p := writeSecretsManifest(t, `{
  "version": 1, "name": "secrets", "apps": [],
  "homeManager": { "settings": { "git": { "userName": "Hugo" } },
    "secrets": [ { "name": "x", "path": "/run/x", "env": "X" } ] }
}`)
	if _, err := LoadManifest(p); err != nil {
		t.Fatalf("an entry with both path and env must load (Phase 2), got %v", err)
	}
}

// TestLoadManifest_HomeManagerSecrets_RejectsNeitherPathNorEnv: a single entry
// must declare exactly one of path/env, never neither.
func TestLoadManifest_HomeManagerSecrets_RejectsNeitherPathNorEnv(t *testing.T) {
	p := writeSecretsManifest(t, `{
  "version": 1, "name": "secrets", "apps": [],
  "homeManager": { "settings": { "git": { "userName": "Hugo" } },
    "secrets": [ { "name": "x" } ] }
}`)
	if _, err := LoadManifest(p); err == nil {
		t.Fatal("expected an error for neither path nor env, got nil")
	}
}

// TestLoadManifest_HomeManagerSecrets_RejectsDuplicateName: two entries with the
// same name fail to load (collision in the generated sinks).
func TestLoadManifest_HomeManagerSecrets_RejectsDuplicateName(t *testing.T) {
	p := writeSecretsManifest(t, `{
  "version": 1, "name": "secrets", "apps": [],
  "homeManager": { "settings": { "git": { "userName": "Hugo" } },
    "secrets": [ { "name": "dup", "path": "/run/a" }, { "name": "dup", "path": "/run/b" } ] }
}`)
	if _, err := LoadManifest(p); err == nil {
		t.Fatal("expected an error for a duplicate secret name, got nil")
	}
}

// TestLoadManifest_HomeManagerSecrets_RejectsEmptyName: an entry without a name
// fails to load.
func TestLoadManifest_HomeManagerSecrets_RejectsEmptyName(t *testing.T) {
	p := writeSecretsManifest(t, `{
  "version": 1, "name": "secrets", "apps": [],
  "homeManager": { "settings": { "git": { "userName": "Hugo" } },
    "secrets": [ { "path": "/run/a" } ] }
}`)
	if _, err := LoadManifest(p); err == nil {
		t.Fatal("expected an error for an empty secret name, got nil")
	}
}

// TestLoadManifest_HomeManagerSecrets_RejectsUnsupportedBackend: a backend other
// than "" / "boundary" fails to load with a clear message — the engine never
// silently degrades to embedding.
func TestLoadManifest_HomeManagerSecrets_RejectsUnsupportedBackend(t *testing.T) {
	p := writeSecretsManifest(t, `{
  "version": 1, "name": "secrets", "apps": [],
  "homeManager": { "settings": { "git": { "userName": "Hugo" } },
    "secrets": [ { "name": "x", "path": "/run/x", "backend": "sops" } ] }
}`)
	_, err := LoadManifest(p)
	if err == nil {
		t.Fatal("expected an error for an unsupported backend, got nil")
	}
	if !strings.Contains(err.Error(), "unsupported backend") {
		t.Errorf("error = %q, want it to mention \"unsupported backend\"", err)
	}
}

// TestLoadManifest_HomeManagerSecrets_AllowsExplicitBoundaryBackend: backend
// "boundary" (the explicit default) loads.
func TestLoadManifest_HomeManagerSecrets_AllowsExplicitBoundaryBackend(t *testing.T) {
	p := writeSecretsManifest(t, `{
  "version": 1, "name": "secrets", "apps": [],
  "homeManager": { "settings": { "git": { "userName": "Hugo" } },
    "secrets": [ { "name": "x", "path": "/run/x", "backend": "boundary" } ] }
}`)
	if _, err := LoadManifest(p); err != nil {
		t.Fatalf("backend \"boundary\" must load, got %v", err)
	}
}

// TestLoadManifest_HomeManagerSecrets_RejectsWithFlakeMode: secrets combined with
// a pure flake input are rejected — the user's external flake owns its secrets.
func TestLoadManifest_HomeManagerSecrets_RejectsWithFlakeMode(t *testing.T) {
	p := writeSecretsManifest(t, `{
  "version": 1, "name": "secrets", "apps": [],
  "homeManager": { "flake": "github:me/dots#hugo",
    "secrets": [ { "name": "x", "env": "A" } ] }
}`)
	if _, err := LoadManifest(p); err == nil {
		t.Fatal("expected an error for secrets combined with flake mode, got nil")
	}
}
