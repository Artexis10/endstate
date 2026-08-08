// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package modules

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
)

func TestRepositoryCurationSeedDeclarationsMatchFiles(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", "..", ".."))
	appsRoot := filepath.Join(repoRoot, "modules", "apps")
	catalog, diagnostics, err := LoadCatalogWithDiagnostics(appsRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(diagnostics) != 0 {
		t.Fatalf("repository module diagnostics = %+v", diagnostics)
	}

	entries, err := os.ReadDir(appsRoot)
	if err != nil {
		t.Fatal(err)
	}
	mismatches := make([]string, 0)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		moduleID := "apps." + entry.Name()
		mod := catalog[moduleID]
		if mod == nil {
			continue
		}
		seedPath := filepath.Join(appsRoot, entry.Name(), "seed.ps1")
		_, statErr := os.Stat(seedPath)
		hasFile := statErr == nil
		if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("stat %s: %v", moduleID, statErr)
		}
		seed := (*CurationSeedDef)(nil)
		if mod.Curation != nil {
			seed = mod.Curation.Seed
		}
		hasCanonicalDeclaration := seed != nil && seed.Type == "script" && seed.Script == "seed.ps1"
		if hasFile != hasCanonicalDeclaration || seed != nil && !hasCanonicalDeclaration {
			mismatches = append(mismatches, fmt.Sprintf("%s(file=%t declaration=%v)", moduleID, hasFile, seed))
		}
	}
	sort.Strings(mismatches)
	if len(mismatches) != 0 {
		t.Fatalf("curation seed mismatches:\n%s", strings.Join(mismatches, "\n"))
	}
}
