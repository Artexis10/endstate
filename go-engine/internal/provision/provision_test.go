// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package provision

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteTo_AssignsMonotonicNumbersAndDefaultsSchema(t *testing.T) {
	dir := t.TempDir()

	g1 := &Generation{RunID: "apply-1", Backend: "nix", AddedRefs: []string{"nixpkgs#ripgrep"}}
	if err := WriteTo(dir, g1); err != nil {
		t.Fatalf("write g1: %v", err)
	}
	if g1.Number != 1 {
		t.Fatalf("g1.Number = %d, want 1", g1.Number)
	}
	if g1.SchemaVersion != SchemaVersion {
		t.Fatalf("g1.SchemaVersion = %q, want %q", g1.SchemaVersion, SchemaVersion)
	}

	g2 := &Generation{RunID: "apply-2", Backend: "winget"}
	if err := WriteTo(dir, g2); err != nil {
		t.Fatalf("write g2: %v", err)
	}
	if g2.Number != 2 {
		t.Fatalf("g2.Number = %d, want 2", g2.Number)
	}

	if _, err := os.Stat(filepath.Join(dir, "000001.json")); err != nil {
		t.Fatalf("expected 000001.json: %v", err)
	}
	// No temp file should survive a successful write.
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Fatalf("leftover temp file: %s", e.Name())
		}
	}
}

// TestWriteTo_RecordsHomeManager verifies the optional HomeManager config record
// (the home-manager flakeref + its resulting generation number) round-trips
// through write/read, so an activated config joins the same audit trail as
// packages.
func TestWriteTo_RecordsHomeManager(t *testing.T) {
	dir := t.TempDir()
	g := &Generation{
		RunID:       "apply-hm",
		Backend:     "nix",
		HomeManager: &HomeGenRef{Flake: "/home/me/dotfiles#hugo", Generation: 7},
	}
	if err := WriteTo(dir, g); err != nil {
		t.Fatalf("write: %v", err)
	}
	gens, err := ListFrom(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(gens) != 1 {
		t.Fatalf("len = %d, want 1", len(gens))
	}
	hm := gens[0].HomeManager
	if hm == nil {
		t.Fatal("HomeManager = nil after round-trip, want non-nil")
	}
	if hm.Flake != "/home/me/dotfiles#hugo" {
		t.Errorf("HomeManager.Flake = %q, want %q", hm.Flake, "/home/me/dotfiles#hugo")
	}
	if hm.Generation != 7 {
		t.Errorf("HomeManager.Generation = %d, want 7", hm.Generation)
	}
}

func TestListFrom_NewestFirstAndIgnoresNonRecords(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 3; i++ {
		if err := WriteTo(dir, &Generation{Backend: "nix"}); err != nil {
			t.Fatal(err)
		}
	}
	// Stray files that must be ignored.
	if err := os.WriteFile(filepath.Join(dir, "000099.json.tmp"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	gens, err := ListFrom(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(gens) != 3 {
		t.Fatalf("len = %d, want 3", len(gens))
	}
	if gens[0].Number != 3 || gens[1].Number != 2 || gens[2].Number != 1 {
		t.Fatalf("not newest-first: %d, %d, %d", gens[0].Number, gens[1].Number, gens[2].Number)
	}
}

func TestListFrom_MissingDirIsEmptyNoError(t *testing.T) {
	gens, err := ListFrom(filepath.Join(t.TempDir(), "absent"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(gens) != 0 {
		t.Fatalf("len = %d, want 0", len(gens))
	}
}

func TestNextNumber_EmptyDirIsOne(t *testing.T) {
	n, err := nextNumber(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("nextNumber = %d, want 1", n)
	}
}

func TestDir_ResolvesUnderStateDirNeverHardcoded(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ENDSTATE_ROOT", root)
	want := filepath.Join(root, "state", "generations")
	if got := Dir(); got != want {
		t.Fatalf("Dir() = %q, want %q", got, want)
	}
}

// TestPackageStaysInstallOnly enforces the separation-of-concerns invariant in
// code: the provision package must never import the config/restore layer.
func TestPackageStaysInstallOnly(t *testing.T) {
	fset := token.NewFileSet()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		af, err := parser.ParseFile(fset, f, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", f, err)
		}
		for _, imp := range af.Imports {
			if strings.Contains(imp.Path.Value, "internal/restore") {
				t.Fatalf("%s imports %s; the provisioning generation must stay install-only", f, imp.Path.Value)
			}
		}
	}
}
