// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package bundle

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/modules"
)

// TestCollectConfigFiles_ExcludesSecretFile verifies that a top-level capture
// file listed in the module's secrets block is never staged, and is counted.
func TestCollectConfigFiles_ExcludesSecretFile(t *testing.T) {
	src := t.TempDir()
	settings := filepath.Join(src, "settings.json")
	secret := filepath.Join(src, "secret.token")
	if err := os.WriteFile(settings, []byte(`{"a":1}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(secret, []byte("TOKEN"), 0644); err != nil {
		t.Fatal(err)
	}

	mod := &modules.Module{
		ID: "apps.testsecrets",
		Capture: &modules.CaptureDef{
			Files: []modules.CaptureFile{
				{Source: settings, Dest: "apps/testsecrets/settings.json", Optional: true},
				{Source: secret, Dest: "apps/testsecrets/secret.token", Optional: true},
			},
		},
		Secrets: &modules.SecretsDef{Files: []string{secret}},
	}

	staging := t.TempDir()
	collected, n, err := CollectConfigFiles(mod, staging)
	if err != nil {
		t.Fatalf("CollectConfigFiles: %v", err)
	}
	if n != 1 {
		t.Errorf("secretsExcluded = %d, want 1", n)
	}
	if len(collected) != 1 {
		t.Errorf("collected = %v, want 1 entry (settings only)", collected)
	}
	if _, err := os.Stat(filepath.Join(staging, "configs", "testsecrets", "settings.json")); err != nil {
		t.Errorf("settings.json should be staged: %v", err)
	}
	if _, err := os.Stat(filepath.Join(staging, "configs", "testsecrets", "secret.token")); !os.IsNotExist(err) {
		t.Errorf("secret.token must NOT be staged (got err=%v)", err)
	}
}

// TestCollectConfigFiles_ExcludesSecretInDir verifies that a secret file living
// inside a captured directory is excluded during the recursive copy.
func TestCollectConfigFiles_ExcludesSecretInDir(t *testing.T) {
	src := t.TempDir()
	cfgDir := filepath.Join(src, "cfg")
	if err := os.MkdirAll(cfgDir, 0755); err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(cfgDir, "config.json")
	env := filepath.Join(cfgDir, ".env")
	if err := os.WriteFile(keep, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(env, []byte("API_KEY=secret"), 0644); err != nil {
		t.Fatal(err)
	}

	mod := &modules.Module{
		ID: "apps.testsecretsdir",
		Capture: &modules.CaptureDef{
			Files: []modules.CaptureFile{
				{Source: cfgDir, Dest: "apps/testsecretsdir/cfg", Optional: true},
			},
		},
		Secrets: &modules.SecretsDef{Files: []string{env}},
	}

	staging := t.TempDir()
	_, n, err := CollectConfigFiles(mod, staging)
	if err != nil {
		t.Fatalf("CollectConfigFiles: %v", err)
	}
	if n != 1 {
		t.Errorf("secretsExcluded = %d, want 1", n)
	}
	if _, err := os.Stat(filepath.Join(staging, "configs", "testsecretsdir", "cfg", "config.json")); err != nil {
		t.Errorf("config.json should be staged: %v", err)
	}
	if _, err := os.Stat(filepath.Join(staging, "configs", "testsecretsdir", "cfg", ".env")); !os.IsNotExist(err) {
		t.Errorf(".env must NOT be staged (got err=%v)", err)
	}
}

// TestMatchesSecrets covers literal, directory-prefix, glob, and ** patterns.
func TestMatchesSecrets(t *testing.T) {
	cases := []struct {
		name     string
		path     string
		patterns []string
		want     bool
	}{
		{"literal file", "C:/Users/a/AppData/state.vscdb", []string{"C:/Users/a/AppData/state.vscdb"}, true},
		{"case-insensitive", "C:/Users/A/State.VSCDB", []string{"c:/users/a/state.vscdb"}, true},
		{"dir prefix", "C:/Users/a/secure/key.pem", []string{"C:/Users/a/secure"}, true},
		{"doublestar", "C:/Users/a/app/.env", []string{"**/.env"}, true},
		{"glob basename", "C:/Users/a/x.credential.json", []string{"*.credential*"}, true},
		{"no match", "C:/Users/a/settings.json", []string{"**/.env", "*.token", "C:/Users/a/other"}, false},
		{"empty patterns", "C:/Users/a/settings.json", nil, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := matchesSecrets(c.path, c.patterns); got != c.want {
				t.Errorf("matchesSecrets(%q, %v) = %v, want %v", c.path, c.patterns, got, c.want)
			}
		})
	}
}
