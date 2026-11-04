// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package modules

import (
	"path/filepath"
	"sort"
	"testing"
)

// productionRepoRoot is the repository root relative to this test file.
func productionRepoRoot() string {
	return filepath.Join("..", "..", "..")
}

// TestGetCatalogLoadsEveryShippedModuleFile catches a shipped module file that
// the production catalog never loads: a whole tier directory that GetCatalog
// does not enumerate, or a module that loads with a diagnostic and silently
// drops out. Every modules/<tier>/<dir>/module.jsonc must be in the catalog.
func TestGetCatalogLoadsEveryShippedModuleFile(t *testing.T) {
	repoRoot := productionRepoRoot()
	shipped, err := filepath.Glob(filepath.Join(repoRoot, "modules", "*", "*", "module.jsonc"))
	if err != nil {
		t.Fatal(err)
	}
	if len(shipped) < 300 {
		t.Fatalf("found only %d shipped module files; wrong repository root?", len(shipped))
	}

	catalog, diagnostics, err := GetCatalogWithDiagnostics(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	for _, diagnostic := range diagnostics {
		t.Errorf("catalog diagnostic for %s: %s", diagnostic.FilePath, diagnostic.Message)
	}

	loaded := make([]string, 0, len(catalog))
	for _, mod := range catalog {
		loaded = append(loaded, filepath.Clean(mod.FilePath))
	}
	want := make([]string, 0, len(shipped))
	for _, path := range shipped {
		want = append(want, filepath.Clean(path))
	}
	sort.Strings(loaded)
	sort.Strings(want)
	if len(loaded) != len(want) {
		t.Fatalf("GetCatalog loaded %d modules, %d module files ship", len(loaded), len(want))
	}
	for index := range want {
		if loaded[index] != want[index] {
			t.Fatalf("loaded module file %s, shipped file %s", loaded[index], want[index])
		}
	}
}
