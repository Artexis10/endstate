// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package discovery

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

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
