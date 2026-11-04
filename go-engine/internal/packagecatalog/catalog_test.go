// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package packagecatalog

import (
	"encoding/json"
	"fmt"
	"github.com/Artexis10/endstate/go-engine/internal/config"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const validRipgrep = `{
  "schemaVersion": 1,
  "id": "ripgrep",
  "displayName": "ripgrep",
  "nix": {"attribute": "ripgrep"},
  "aliases": {
    "debian": ["ripgrep"],
    "rpm": ["ripgrep"],
    "arch": ["ripgrep"],
    "desktop": ["ripgrep.desktop"],
    "executable": ["rg"]
  },
  "configModule": "apps.ripgrep"
}`

func TestParseEntryStrictAndDeterministic(t *testing.T) {
	entry, err := ParseEntry([]byte(validRipgrep), "ripgrep.jsonc")
	if err != nil {
		t.Fatal(err)
	}
	if entry.ID != "ripgrep" || entry.Nix.Attribute != "ripgrep" || entry.ConfigModule != "apps.ripgrep" {
		t.Fatalf("entry = %+v", entry)
	}
	if len(entry.Revision) != 64 || strings.Trim(entry.Revision, "0123456789abcdef") != "" {
		t.Fatalf("revision = %q, want SHA-256", entry.Revision)
	}

	reordered := strings.NewReplacer(
		`"schemaVersion": 1,`, `"displayName": "ripgrep",`,
		`"displayName": "ripgrep",`, `"schemaVersion": 1,`,
	).Replace(validRipgrep)
	other, err := ParseEntry([]byte(reordered), "ripgrep.jsonc")
	if err != nil {
		t.Fatal(err)
	}
	if other.Revision != entry.Revision {
		t.Fatalf("property order changed revision: %s != %s", other.Revision, entry.Revision)
	}
}

func TestParseEntryRejectsUnknownFieldsAndUnsafeTargets(t *testing.T) {
	tests := map[string]string{
		"unknown field":    strings.Replace(validRipgrep, `"configModule":`, `"surprise": true, "configModule":`, 1),
		"flake injection":  strings.Replace(validRipgrep, `"attribute": "ripgrep"`, `"attribute": "nixpkgs#ripgrep"`, 1),
		"attr traversal":   strings.Replace(validRipgrep, `"attribute": "ripgrep"`, `"attribute": "../ripgrep"`, 1),
		"unknown source":   strings.Replace(validRipgrep, `"debian":`, `"mystery":`, 1),
		"executable path":  strings.Replace(validRipgrep, `"executable": ["rg"]`, `"executable": ["../rg"]`, 1),
		"unbounded config": strings.Replace(validRipgrep, `"executable": ["rg"]`, `"config": ["/etc/editor/config"]`, 1),
		"config traversal": strings.Replace(validRipgrep, `"executable": ["rg"]`, `"config": ["${home}/../editor/config"]`, 1),
	}
	for name, data := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseEntry([]byte(data), "ripgrep.jsonc"); err == nil {
				t.Fatal("invalid catalog entry was accepted")
			}
		})
	}
}

func TestLoadRejectsDuplicatePortableIDsAndSourceAliases(t *testing.T) {
	for name, second := range map[string]string{
		"portable id": validRipgrep,
		"qualified alias": strings.NewReplacer(
			`"id": "ripgrep"`, `"id": "other"`,
			`"displayName": "ripgrep"`, `"displayName": "Other"`,
			`"attribute": "ripgrep"`, `"attribute": "other"`,
			`"configModule": "apps.ripgrep"`, `"configModule": "apps.other"`,
		).Replace(validRipgrep),
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "a.jsonc"), []byte(validRipgrep), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "b.jsonc"), []byte(second), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(dir); err == nil {
				t.Fatalf("Load accepted duplicate %s", name)
			}
		})
	}
}

func TestCatalogResolvesOnlyExactSourceQualifiedAlias(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ripgrep.jsonc"), []byte(validRipgrep), 0o600); err != nil {
		t.Fatal(err)
	}
	catalog, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	entry, ok := catalog.Resolve("debian", "ripgrep")
	if !ok || entry.ID != "ripgrep" {
		t.Fatalf("Resolve(debian,ripgrep) = %+v, %v", entry, ok)
	}
	if _, ok := catalog.Resolve("rpm", "RIPGREP"); ok {
		t.Fatal("Resolve guessed a case-insensitive alias")
	}
	if _, ok := catalog.Resolve("debian", "rip-grep"); ok {
		t.Fatal("Resolve guessed a similar display name")
	}
}

func TestReleaseLinuxCorpusMappings(t *testing.T) {

	catalog, err := Load(filepath.Join("..", "..", "..", "catalog", "packages"))
	if err != nil {
		t.Fatal(err)
	}

	want := map[string]struct {
		attribute    string
		configModule string
	}{
		"openssh":       {attribute: "openssh", configModule: "apps.ssh-config"},
		"tmux":          {attribute: "tmux", configModule: "apps.tmux"},
		"direnv":        {attribute: "direnv"},
		"starship":      {attribute: "starship", configModule: "apps.starship"},
		"fzf":           {attribute: "fzf"},
		"zoxide":        {attribute: "zoxide", configModule: "apps.zoxide"},
		"bat":           {attribute: "bat", configModule: "apps.bat"},
		"btop":          {attribute: "btop", configModule: "apps.btop"},
		"eza":           {attribute: "eza", configModule: "apps.eza"},
		"fastfetch":     {attribute: "fastfetch", configModule: "apps.fastfetch"},
		"neovim":        {attribute: "neovim", configModule: "apps.neovim"},
		"helix":         {attribute: "helix", configModule: "apps.helix"},
		"wezterm":       {attribute: "wezterm", configModule: "apps.wezterm"},
		"kitty":         {attribute: "kitty"},
		"alacritty":     {attribute: "alacritty", configModule: "apps.alacritty"},
		"keepassxc":     {attribute: "keepassxc", configModule: "apps.keepassxc"},
		"lazydocker":    {attribute: "lazydocker", configModule: "apps.lazydocker"},
		"lazygit":       {attribute: "lazygit", configModule: "apps.lazygit"},
		"poetry":        {attribute: "poetry", configModule: "apps.poetry"},
		"jujutsu":       {attribute: "jujutsu"},
		"atuin":         {attribute: "atuin"},
		"yazi":          {attribute: "yazi", configModule: "apps.yazi"},
		"vscode":        {attribute: "vscode", configModule: "apps.vscode"},
		"firefox":       {attribute: "firefox"},
		"google-chrome": {attribute: "google-chrome"},
		"chromium":      {attribute: "chromium"},
		"curl":          {attribute: "curl"},
		"tree":          {attribute: "tree"},
		"unzip":         {attribute: "unzip"},
		"zip":           {attribute: "zip"},
		"wl-clipboard":  {attribute: "wl-clipboard"},
		"xclip":         {attribute: "xclip"},
		"yadm":          {attribute: "yadm"},
		"pipx":          {attribute: "pipx"},
		"nodejs":        {attribute: "nodejs"},
		"powershell":    {attribute: "powershell", configModule: "apps.powershell-profile"},
		"socat":         {attribute: "socat"},
		"imagemagick":   {attribute: "imagemagick"},
	}
	for id, expected := range want {
		entry, ok := catalog.Entry(id)
		if !ok {
			t.Errorf("missing %q", id)
			continue
		}
		if entry.Nix.Attribute != expected.attribute {
			t.Errorf("%s nix attribute = %q, want %q", id, entry.Nix.Attribute, expected.attribute)
		}
		if entry.ConfigModule != expected.configModule {
			t.Errorf("%s configModule = %q, want %q", id, entry.ConfigModule, expected.configModule)
		}
		if len(entry.Aliases) == 0 {
			t.Errorf("%s has no source-qualified aliases", id)
		}
	}
}

// Simultaneous collisions must identify the same source on every catalog load.
func TestCatalogRejectsAmbiguousDesktopAndExecutableAliases(t *testing.T) {
	dir := t.TempDir()
	for _, id := range []string{"alpha", "beta"} {
		body := fmt.Sprintf(`{"schemaVersion":1,"id":%q,"displayName":%q,"nix":{"attribute":%q},"aliases":{"desktop":["editor.desktop"],"executable":["editor"]}}`, id, id, id)
		if err := os.WriteFile(filepath.Join(dir, id+".json"), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for attempt := 0; attempt < 100; attempt++ {
		_, err := Load(dir)
		if err == nil || !strings.Contains(err.Error(), "duplicate package alias desktop:editor.desktop") {
			t.Fatalf("ambiguous source diagnostic = %v, want desktop alias before executable alias", err)
		}
	}
}

// The same executable-root lookup serves installed CLI and GUI engine layouts.
// Run a relocated test executable so neither ENDSTATE_ROOT nor the checkout can
// accidentally supply the catalog under test.
func TestInstalledRuntimeCatalogRoundTrip(t *testing.T) {
	if os.Getenv("ENDSTATE_CATALOG_PROBE") == "1" {
		root := config.ResolveRepoRoot()
		catalog, err := Load(filepath.Join(root, "catalog", "packages"))
		if err != nil {
			t.Fatal(err)
		}
		data, err := json.Marshal(catalog.Entries())
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(os.Getenv("ENDSTATE_CATALOG_RESULT"), data, 0600); err != nil {
			t.Fatal(err)
		}
		return
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	binary, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	for _, layout := range []string{"lib", filepath.Join("resources", "binaries")} {
		t.Run(layout, func(t *testing.T) {
			root := t.TempDir()
			catalogDir := filepath.Join(root, "catalog", "packages")
			if err := os.MkdirAll(catalogDir, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(catalogDir, "ripgrep.jsonc"), []byte(validRipgrep), 0600); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, layout, filepath.Base(executable))
			if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, binary, 0700); err != nil {
				t.Fatal(err)
			}
			result := filepath.Join(root, "result.json")
			cmd := exec.Command(path, "-test.run=^TestInstalledRuntimeCatalogRoundTrip$")
			cmd.Dir = t.TempDir()
			cmd.Env = append(os.Environ(), "ENDSTATE_ROOT=", "ENDSTATE_CATALOG_PROBE=1", "ENDSTATE_CATALOG_RESULT="+result)
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("installed lookup: %v\n%s", err, output)
			}
			data, err := os.ReadFile(result)
			if err != nil {
				t.Fatal(err)
			}
			var entries []Entry
			if err := json.Unmarshal(data, &entries); err != nil {
				t.Fatal(err)
			}
			if len(entries) != 1 || entries[0].ID != "ripgrep" || entries[0].Nix.Attribute != "ripgrep" || len(entries[0].Aliases["desktop"]) != 1 {
				t.Fatalf("installed catalog = %s", data)
			}
		})
	}
}
