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

// TestLoadManifest_HomeManagerSecrets_RejectsEnv: env-exposed secrets are deferred
// in Phase 1 (path-only) — declaring one fails to load with a clear message.
func TestLoadManifest_HomeManagerSecrets_RejectsEnv(t *testing.T) {
	p := writeSecretsManifest(t, `{
  "version": 1, "name": "secrets", "apps": [],
  "homeManager": { "settings": { "git": { "userName": "Hugo" } },
    "secrets": [ { "name": "token", "env": "API_TOKEN" } ] }
}`)
	_, err := LoadManifest(p)
	if err == nil {
		t.Fatal("expected an error for an env secret (Phase 1 is path-only), got nil")
	}
	if !strings.Contains(err.Error(), "not yet supported") {
		t.Errorf("error = %q, want it to mention env is not yet supported", err)
	}
}

// TestLoadManifest_HomeManagerSecrets_RejectsBothPathAndEnv: a single entry must
// declare exactly one of path/env, never both.
func TestLoadManifest_HomeManagerSecrets_RejectsBothPathAndEnv(t *testing.T) {
	p := writeSecretsManifest(t, `{
  "version": 1, "name": "secrets", "apps": [],
  "homeManager": { "settings": { "git": { "userName": "Hugo" } },
    "secrets": [ { "name": "x", "path": "/run/x", "env": "X" } ] }
}`)
	if _, err := LoadManifest(p); err == nil {
		t.Fatal("expected an error for both path and env, got nil")
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
