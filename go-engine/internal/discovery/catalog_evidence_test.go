// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package discovery

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/config"
	"github.com/Artexis10/endstate/go-engine/internal/packagecatalog"
)

func TestCrossSourceFixtureReconcilesNativeDesktopAndConfig(t *testing.T) {
	root := t.TempDir()
	executable := "sample-editor"
	if runtime.GOOS == "windows" {
		executable += ".cmd"
	}
	entry := map[string]any{
		"schemaVersion": 1, "id": "sample-editor", "displayName": "Sample Editor",
		"nix": map[string]string{"attribute": "sample-editor"},
		"aliases": map[string][]string{
			"debian": {"sample-editor"}, "flatpak": {"org.example.Editor"},
			"desktop": {"sample-editor.desktop"}, "executable": {executable},
			"config": {"${home}/editor.json"},
		},
	}
	body, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "editor.json"), body, 0600); err != nil {
		t.Fatal(err)
	}
	catalog, err := packagecatalog.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	bin, home, desktop := t.TempDir(), t.TempDir(), t.TempDir()
	for path, contents := range map[string]string{
		filepath.Join(bin, executable):                  "Never execute this inventory evidence.",
		filepath.Join(home, "editor.json"):              "private settings",
		filepath.Join(desktop, "sample-editor.desktop"): "[Desktop Entry]\nType=Application\nName=Sample Editor\n",
	} {
		if err := os.WriteFile(path, []byte(contents), 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin)
	orchestrator := Orchestrator{
		Catalog: catalog, NixpkgsInput: "github:NixOS/nixpkgs/1111111111111111111111111111111111111111",
		Adapters: []Adapter{
			DebianAdapter{Runner: &fixtureRunner{outputs: map[string][]byte{
				"apt-mark":   []byte("sample-editor\n"),
				"dpkg-query": []byte("sample-editor\t1.2-debian\tii \tno\toptional\teditors\n"),
			}}},
			FlatpakAdapter{Runner: &sequenceRunner{results: []sequenceResult{
				{output: []byte("org.example.Editor\t1.3-flatpak\norg.example.editor\t1.0\n")}, {output: []byte("org.example.Platform\n")},
			}}},
			DesktopAdapter{Roots: []string{desktop}}, CatalogExecutableAdapter{Catalog: catalog},
			CatalogConfigAdapter{Catalog: catalog, Environment: config.PathEnvironment{GOOS: runtime.GOOS, Home: home}},
			fakeAdapter{id: "unreadable", err: errors.New("private diagnostic: " + home)},
		},
	}
	result, err := orchestrator.Discover(context.Background(), Request{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Resolved) != 1 || result.Resolved[0].ID != "sample-editor" || len(result.Resolved[0].Evidence) != 5 {
		t.Fatalf("one app must retain all source-qualified evidence: %+v", result)
	}
	if len(result.Settings) != 1 || result.Settings[0].Portability != PortabilityResolved || len(result.Settings[0].Actions) != 0 {
		t.Fatalf("config evidence changed settings authority: %+v", result.Settings)
	}
	if len(result.Unresolved) != 1 || result.Unresolved[0].Evidence[0].Ref != "org.example.editor" {
		t.Fatalf("case-sensitive source identity was guessed: %+v", result.Unresolved)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{home, bin, desktop, "private settings", "private diagnostic"} {
		if strings.Contains(string(encoded), private) {
			t.Fatalf("source evidence leaked private contents or paths: %s", encoded)
		}
	}
	for _, evidence := range result.Resolved[0].Evidence {
		if evidence.Source == "debian" && evidence.Version != "1.2-debian" || evidence.Source == "flatpak" && evidence.Version != "1.3-flatpak" {
			t.Fatalf("source versions were conflated: %+v", result.Resolved[0].Evidence)
		}
	}
}

func TestExecutableAndConfigEvidenceUsesBoundedCoordinates(t *testing.T) {
	root := t.TempDir()
	catalogDir := filepath.Join(root, "catalog", "packages")
	if err := os.MkdirAll(catalogDir, 0700); err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	name := "example-editor"
	if runtime.GOOS == "windows" {
		name += ".cmd"
	}
	// This invalid program is inventory evidence only; discovery must not run it.
	if err := os.WriteFile(filepath.Join(bin, name), []byte("This program must never be executed."), 0700); err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	configRoot := filepath.Join(home, "config")
	if err := os.MkdirAll(configRoot, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configRoot, "editor.json"), []byte("private configuration contents"), 0600); err != nil {
		t.Fatal(err)
	}
	entry := map[string]any{
		"schemaVersion": 1, "id": "example-editor", "displayName": "Example Editor",
		"nix":          map[string]string{"attribute": "example-editor"},
		"aliases":      map[string][]string{"executable": {name}, "config": {"${home}/config/editor.json"}},
		"configModule": "apps.example-editor",
	}
	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(catalogDir, "editor.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ENDSTATE_ROOT", root)
	t.Setenv("PATH", bin)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	result, err := Discover(context.Background(), Request{SelectedSources: []string{"catalog-executables", "catalog-config-paths"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sources) != 2 || len(result.Resolved) != 1 || result.Resolved[0].ID != "example-editor" {
		t.Fatalf("catalog evidence = %+v", result)
	}
	if len(result.Settings) != 1 || len(result.Settings[0].Actions) != 0 || result.Counts.Settings != 0 {
		t.Fatalf("path presence granted settings authority: %+v", result)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), home) || strings.Contains(string(encoded), bin) || strings.Contains(string(encoded), "private configuration contents") {
		t.Fatalf("evidence leaked machine paths or file contents: %s", encoded)
	}
	if err := os.Remove(filepath.Join(bin, name)); err != nil {
		t.Fatal(err)
	}
	configOnly, err := Discover(context.Background(), Request{SelectedSources: []string{"catalog-executables", "catalog-config-paths"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(configOnly.Resolved) != 0 || len(configOnly.Settings) != 1 || configOnly.Settings[0].Portability != PortabilityConfigOnly {
		t.Fatalf("config presence fabricated a package: %+v", configOnly)
	}
}
