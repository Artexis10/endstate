// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package manifest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLoadManifest_HomeManagerSettings verifies the declarative catalog input
// (homeManager.settings) round-trips through JSONC load: curated concepts, the
// raw programs passthrough, and the files map are all retained; absent ⇒ nil.
func TestLoadManifest_HomeManagerSettings(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "m.jsonc")
	content := `{
  "version": 1,
  "name": "hm-settings",
  "apps": [],
  // declarative, Endstate-native home-manager config (the engine writes the home.nix)
  "homeManager": {
    "settings": {
      "git": { "userName": "Hugo", "userEmail": "h@x.com", "defaultBranch": "main" },
      "shell": { "aliases": { "ll": "ls -la" }, "sessionVariables": { "EDITOR": "nvim" } },
      "direnv": { "enable": true },
      "programs": { "bat": { "enable": true } },
      "files": { "~/.config/foo/bar.conf": "./payload/bar.conf" }
    }
  }
}`
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	m, err := LoadManifest(p)
	if err != nil {
		t.Fatalf("unexpected error loading manifest with homeManager.settings: %v", err)
	}
	if m.HomeManager == nil || m.HomeManager.Settings == nil {
		t.Fatal("HomeManager.Settings = nil, want non-nil")
	}
	s := m.HomeManager.Settings
	if s.Git == nil || s.Git.UserName != "Hugo" || s.Git.UserEmail != "h@x.com" || s.Git.DefaultBranch != "main" {
		t.Errorf("git settings = %+v, want Hugo/h@x.com/main", s.Git)
	}
	if s.Shell == nil || s.Shell.Aliases["ll"] != "ls -la" || s.Shell.SessionVariables["EDITOR"] != "nvim" {
		t.Errorf("shell settings = %+v, want ll alias + EDITOR var", s.Shell)
	}
	if s.Direnv == nil || !s.Direnv.Enable {
		t.Errorf("direnv = %+v, want enable=true", s.Direnv)
	}
	if s.Programs["bat"] == nil {
		t.Errorf("raw programs.bat not retained: %+v", s.Programs)
	}
	if s.Files["~/.config/foo/bar.conf"] != "./payload/bar.conf" {
		t.Errorf("files = %+v, want the declared target→source", s.Files)
	}

	// Absent ⇒ nil.
	p2 := filepath.Join(dir, "none.jsonc")
	if err := os.WriteFile(p2, []byte(`{ "version": 1, "name": "n", "apps": [] }`), 0644); err != nil {
		t.Fatal(err)
	}
	m2, err := LoadManifest(p2)
	if err != nil {
		t.Fatal(err)
	}
	if m2.HomeManager != nil {
		t.Errorf("HomeManager = %+v, want nil when absent", m2.HomeManager)
	}
}

// TestLoadManifest_HomeManagerSettings_RejectsUnknownCuratedKey verifies a typo'd
// curated key fails to load (silent-drop would mean a setting mysteriously never
// applies). The raw programs passthrough stays permissive (any key allowed).
func TestLoadManifest_HomeManagerSettings_RejectsUnknownCuratedKey(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "m.jsonc")
	content := `{
  "version": 1, "name": "typo", "apps": [],
  "homeManager": { "settings": { "git": { "usrName": "Hugo" } } }
}`
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadManifest(p); err == nil {
		t.Fatal("expected an error for an unknown curated key (git.usrName), got nil")
	}
}

// TestLoadManifest_HomeManagerSettings_BroadenedCurated verifies the broadened
// curated catalog concepts (fzf, zoxide, bat, tmux, ssh) round-trip through JSONC
// load with their typed fields retained.
func TestLoadManifest_HomeManagerSettings_BroadenedCurated(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "m.jsonc")
	content := `{
  "version": 1,
  "name": "hm-broaden",
  "apps": [],
  "homeManager": {
    "settings": {
      "fzf": { "enable": true },
      "zoxide": { "enable": true },
      "bat": { "enable": true, "config": { "theme": "TwoDark" } },
      "tmux": { "enable": true, "extraConfig": "set -g mouse on" },
      "ssh": { "enable": true, "extraConfig": "Host *\n  ServerAliveInterval 60" }
    }
  }
}`
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	m, err := LoadManifest(p)
	if err != nil {
		t.Fatalf("unexpected error loading broadened catalog: %v", err)
	}
	if m.HomeManager == nil || m.HomeManager.Settings == nil {
		t.Fatal("HomeManager.Settings = nil, want non-nil")
	}
	s := m.HomeManager.Settings
	if s.Fzf == nil || !s.Fzf.Enable {
		t.Errorf("fzf = %+v, want enable=true", s.Fzf)
	}
	if s.Zoxide == nil || !s.Zoxide.Enable {
		t.Errorf("zoxide = %+v, want enable=true", s.Zoxide)
	}
	if s.Bat == nil || !s.Bat.Enable || s.Bat.Config["theme"] != "TwoDark" {
		t.Errorf("bat = %+v, want enable=true + theme=TwoDark", s.Bat)
	}
	if s.Tmux == nil || !s.Tmux.Enable || s.Tmux.ExtraConfig != "set -g mouse on" {
		t.Errorf("tmux = %+v, want enable=true + extraConfig", s.Tmux)
	}
	if s.SSH == nil || !s.SSH.Enable || !strings.Contains(s.SSH.ExtraConfig, "ServerAliveInterval") {
		t.Errorf("ssh = %+v, want enable=true + extraConfig", s.SSH)
	}
}

// TestLoadManifest_HomeManagerSettings_RejectsUnknownBroadenedKey verifies a typo'd
// sub-key on a broadened curated concept (e.g. bat.confgi) fails to load rather than
// being silently dropped.
func TestLoadManifest_HomeManagerSettings_RejectsUnknownBroadenedKey(t *testing.T) {
	cases := map[string]string{
		"bat-typo":  `"bat": { "confgi": { "theme": "x" } }`,
		"tmux-typo": `"tmux": { "extraConfigg": "x" }`,
		"ssh-typo":  `"ssh": { "extarConfig": "x" }`,
		"fzf-typo":  `"fzf": { "enabel": true }`,
	}
	for name, block := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			p := filepath.Join(dir, "m.jsonc")
			content := `{ "version": 1, "name": "typo", "apps": [], "homeManager": { "settings": { ` + block + ` } } }`
			if err := os.WriteFile(p, []byte(content), 0644); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadManifest(p); err == nil {
				t.Fatalf("%s: expected an error for an unknown sub-key, got nil", name)
			}
		})
	}
}

// TestLoadManifest_HomeManagerSettings_MoreCurated verifies the additional curated
// catalog concepts (eza, gh, lazygit, neovim) round-trip through JSONC load with
// their typed fields retained.
func TestLoadManifest_HomeManagerSettings_MoreCurated(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "m.jsonc")
	content := `{
  "version": 1,
  "name": "hm-more",
  "apps": [],
  "homeManager": {
    "settings": {
      "eza": { "enable": true, "extraOptions": ["--git", "--icons"] },
      "gh": { "enable": true, "settings": { "editor": "nvim" } },
      "lazygit": { "enable": true, "settings": { "gui": { "theme": { "activeBorderColor": ["blue", "bold"] } } } },
      "neovim": { "enable": true, "extraConfig": "set number\nset relativenumber" }
    }
  }
}`
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	m, err := LoadManifest(p)
	if err != nil {
		t.Fatalf("unexpected error loading more curated catalog: %v", err)
	}
	if m.HomeManager == nil || m.HomeManager.Settings == nil {
		t.Fatal("HomeManager.Settings = nil, want non-nil")
	}
	s := m.HomeManager.Settings
	if s.Eza == nil || !s.Eza.Enable || len(s.Eza.ExtraOptions) != 2 || s.Eza.ExtraOptions[0] != "--git" {
		t.Errorf("eza = %+v, want enable=true + extraOptions=[--git --icons]", s.Eza)
	}
	if s.Gh == nil || !s.Gh.Enable || s.Gh.Settings["editor"] != "nvim" {
		t.Errorf("gh = %+v, want enable=true + settings.editor=nvim", s.Gh)
	}
	if s.Lazygit == nil || !s.Lazygit.Enable || s.Lazygit.Settings["gui"] == nil {
		t.Errorf("lazygit = %+v, want enable=true + settings.gui non-nil", s.Lazygit)
	}
	if s.Neovim == nil || !s.Neovim.Enable || !strings.Contains(s.Neovim.ExtraConfig, "set number") {
		t.Errorf("neovim = %+v, want enable=true + extraConfig with 'set number'", s.Neovim)
	}
}

// TestLoadManifest_HomeManagerSettings_RejectsUnknownMoreCuratedKey verifies a typo'd
// sub-key on the more-curated concepts fails to load rather than being silently dropped.
func TestLoadManifest_HomeManagerSettings_RejectsUnknownMoreCuratedKey(t *testing.T) {
	cases := map[string]string{
		"eza-typo":     `"eza": { "enabel": true }`,
		"gh-typo":      `"gh": { "settigns": {} }`,
		"lazygit-typo": `"lazygit": { "settigns": {} }`,
		"neovim-typo":  `"neovim": { "extraCfg": "x" }`,
	}
	for name, block := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			p := filepath.Join(dir, "m.jsonc")
			content := `{ "version": 1, "name": "typo", "apps": [], "homeManager": { "settings": { ` + block + ` } } }`
			if err := os.WriteFile(p, []byte(content), 0644); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadManifest(p); err == nil {
				t.Fatalf("%s: expected an error for an unknown sub-key, got nil", name)
			}
		})
	}
}

// TestLoadManifest_HomeManagerInputsMutuallyExclusive verifies settings / config /
// flake are mutually exclusive — any pair fails to load with a clear error.
func TestLoadManifest_HomeManagerInputsMutuallyExclusive(t *testing.T) {
	cases := map[string]string{
		"settings+flake":  `"homeManager": { "settings": { "direnv": { "enable": true } }, "flake": "/d#me" }`,
		"settings+config": `"homeManager": { "settings": { "direnv": { "enable": true } }, "config": "./home.nix" }`,
		"config+flake":    `"homeManager": { "config": "./home.nix", "flake": "/d#me" }`,
	}
	for name, hm := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			p := filepath.Join(dir, "m.jsonc")
			content := `{ "version": 1, "name": "x", "apps": [], ` + hm + ` }`
			if err := os.WriteFile(p, []byte(content), 0644); err != nil {
				t.Fatal(err)
			}
			_, err := LoadManifest(p)
			if err == nil {
				t.Fatalf("%s: expected a mutual-exclusion error, got nil", name)
			}
			if !strings.Contains(strings.ToLower(err.Error()), "mutually exclusive") {
				t.Errorf("%s: error %q does not explain the mutual exclusion", name, err.Error())
			}
		})
	}
}
