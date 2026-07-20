// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package importer

import (
	"reflect"
	"testing"
)

// Winget-source packages map to app entries: Id → Ref, Name → DisplayName,
// slugged last-segment → ID. Imported ordering is canonical (by Id,
// case-insensitive), so lookups are by Ref rather than input position.
func TestMapBundle_WingetMapping(t *testing.T) {
	b := &Bundle{Packages: []Package{
		{ID: "Microsoft.VisualStudioCode", Name: "Microsoft Visual Studio Code", Source: "winget", ManagerName: "WinGet"},
		{ID: "Git.Git", Name: "Git", Source: "winget", ManagerName: "WinGet"},
	}}

	res := MapBundle(b, MapOptions{})
	if len(res.Imported) != 2 {
		t.Fatalf("imported = %d, want 2", len(res.Imported))
	}
	byRef := map[string]ImportedApp{}
	for _, a := range res.Imported {
		byRef[a.Ref] = a
	}
	vsc := byRef["Microsoft.VisualStudioCode"]
	if vsc.ID != "visualstudiocode" || vsc.DisplayName != "Microsoft Visual Studio Code" {
		t.Errorf("vscode imported = %+v", vsc)
	}
	if git := byRef["Git.Git"]; git.ID != "git" {
		t.Errorf("git imported = %+v", git)
	}
	if vsc.Version != "" {
		t.Errorf("no --pin: version should be empty, got %q", vsc.Version)
	}

	// Canonical order: Git.Git sorts before Microsoft.VisualStudioCode.
	if res.Imported[0].Ref != "Git.Git" || res.Imported[1].Ref != "Microsoft.VisualStudioCode" {
		t.Errorf("imported order = [%s, %s], want [Git.Git, Microsoft.VisualStudioCode]",
			res.Imported[0].Ref, res.Imported[1].Ref)
	}
}

// A repeated winget Id is imported once; the second occurrence is reported in
// skipped[] as a duplicate of the first slug — never silently dropped.
func TestMapBundle_DuplicateWingetId(t *testing.T) {
	b := &Bundle{Packages: []Package{
		{ID: "Git.Git", Name: "Git", Source: "winget"},
		{ID: "Git.Git", Name: "Git (again)", Source: "winget"},
	}}
	res := MapBundle(b, MapOptions{})

	if len(res.Imported) != 1 {
		t.Fatalf("a doubled winget Id must import once, got %d: %+v", len(res.Imported), res.Imported)
	}
	if res.Imported[0].Ref != "Git.Git" || res.Imported[0].ID != "git" {
		t.Errorf("imported = %+v, want ref Git.Git / id git", res.Imported[0])
	}

	// refs.windows are unique across imported apps.
	refs := map[string]int{}
	for _, a := range res.Imported {
		refs[a.Ref]++
	}
	for ref, n := range refs {
		if n != 1 {
			t.Errorf("ref %q appears %d times in imported; refs.windows must be unique", ref, n)
		}
	}

	// The second occurrence surfaces as a skip with the duplicate reason.
	var dup *SkippedPackage
	for i := range res.Skipped {
		if res.Skipped[i].ID == "Git.Git" {
			dup = &res.Skipped[i]
		}
	}
	if dup == nil {
		t.Fatalf("expected the duplicate reported in skipped, got %+v", res.Skipped)
	}
	if dup.Reason != "duplicate of git" {
		t.Errorf("duplicate skip reason = %q, want %q", dup.Reason, "duplicate of git")
	}
	if dup.Manager != "winget" {
		t.Errorf("duplicate skip manager = %q, want winget", dup.Manager)
	}
}

// Two exports of the same machine that differ only in package order produce an
// identical MapResult (stable ordering across re-exports).
func TestMapBundle_StableAcrossReorder(t *testing.T) {
	pkgs := []Package{
		{ID: "Microsoft.VisualStudioCode", Name: "VS Code", Source: "winget", Version: "1.0"},
		{ID: "Git.Git", Name: "Git", Source: "winget"},
		{ID: "Mozilla.Firefox", Name: "Firefox", Source: "winget"},
		{ID: "nodejs", Name: "Node.js", Source: "chocolatey", ManagerName: "Chocolatey"},
		{ID: "black", Name: "black", Source: "pip", ManagerName: "Pip"},
	}
	reordered := []Package{pkgs[4], pkgs[0], pkgs[3], pkgs[2], pkgs[1]}

	a := MapBundle(&Bundle{Packages: pkgs}, MapOptions{Pin: true})
	c := MapBundle(&Bundle{Packages: reordered}, MapOptions{Pin: true})
	if !reflect.DeepEqual(a, c) {
		t.Errorf("MapBundle is order-sensitive:\n original = %+v\n reordered = %+v", a, c)
	}
}

// The winget discriminator is case-insensitive, and ManagerName is only a
// fallback when Source is empty.
func TestMapBundle_WingetDiscriminator(t *testing.T) {
	b := &Bundle{Packages: []Package{
		{ID: "A.One", Name: "One", Source: "WinGet"},                       // case-insensitive source
		{ID: "B.Two", Name: "Two", Source: "", ManagerName: "winget"},      // fallback via ManagerName
		{ID: "C.Three", Name: "Three", Source: "msstore", ManagerName: "WinGet"}, // non-winget source wins over manager
	}}
	res := MapBundle(b, MapOptions{})
	if len(res.Imported) != 2 {
		t.Fatalf("imported = %d, want 2 (msstore-source must not import)", len(res.Imported))
	}
	if len(res.Skipped) != 1 || res.Skipped[0].ID != "C.Three" {
		t.Fatalf("expected the msstore package skipped, got %+v", res.Skipped)
	}
}

// --pin: InstallationOptions.Version (authored intent) beats the observed Version.
func TestMapBundle_PinPrecedence(t *testing.T) {
	b := &Bundle{Packages: []Package{
		{ID: "A.Pinned", Name: "Pinned", Source: "winget", Version: "1.2.3",
			InstallationOptions: InstallOptions{Version: "1.2.0"}},
		{ID: "B.Observed", Name: "Observed", Source: "winget", Version: "9.9.9"},
		{ID: "C.Versionless", Name: "Versionless", Source: "winget"},
	}}
	res := MapBundle(b, MapOptions{Pin: true})
	if res.Imported[0].Version != "1.2.0" {
		t.Errorf("pin should win: version = %q, want 1.2.0", res.Imported[0].Version)
	}
	if res.Imported[1].Version != "9.9.9" {
		t.Errorf("observed version should be used absent a pin: version = %q, want 9.9.9", res.Imported[1].Version)
	}
	if res.Imported[2].Version != "" {
		t.Errorf("versionless package with --pin should have empty version, got %q", res.Imported[2].Version)
	}
}

// Without --pin, no imported app carries a version.
func TestMapBundle_NoPinDefault(t *testing.T) {
	b := &Bundle{Packages: []Package{
		{ID: "A.One", Name: "One", Source: "winget", Version: "1.0.0",
			InstallationOptions: InstallOptions{Version: "0.9.0"}},
	}}
	res := MapBundle(b, MapOptions{})
	if res.Imported[0].Version != "" {
		t.Errorf("default (no --pin): version must be empty, got %q", res.Imported[0].Version)
	}
}

// Skip transparency: every non-winget manager is reported with a count and a
// reason; incompatible packages pass through; nothing is silently dropped.
func TestMapBundle_SkipTransparency(t *testing.T) {
	b := &Bundle{
		Packages: []Package{
			{ID: "Git.Git", Name: "Git", Source: "winget", ManagerName: "WinGet"},
			{ID: "nodejs", Name: "Node.js", Source: "chocolatey", ManagerName: "Chocolatey"},
			{ID: "neovim", Name: "Neovim", Source: "main", ManagerName: "Scoop"},
			{ID: "black", Name: "black", Source: "pip", ManagerName: "Pip"},
		},
		IncompatiblePackages: []IncompatiblePackage{
			{ID: "Contoso.Local", Name: "Local App", Version: "1.0.0", Source: "Local PC"},
		},
	}
	res := MapBundle(b, MapOptions{})

	// No package is unaccounted for.
	if got := len(res.Imported) + len(res.Skipped); got != len(b.Packages) {
		t.Errorf("imported+skipped = %d, want %d (no silent drops)", got, len(b.Packages))
	}
	if len(res.Skipped) != 3 {
		t.Fatalf("skipped = %d, want 3", len(res.Skipped))
	}

	// Each non-winget manager is named.
	managers := map[string]int{}
	for _, s := range res.Skipped {
		managers[s.Manager]++
		if s.Reason == "" {
			t.Errorf("skipped %q has no reason", s.ID)
		}
	}
	for _, want := range []string{"Chocolatey", "Scoop", "Pip"} {
		if managers[want] != 1 {
			t.Errorf("expected manager %q reported once, counts = %v", want, managers)
		}
	}

	if len(res.Incompatible) != 1 || res.Incompatible[0].ID != "Contoso.Local" {
		t.Errorf("incompatible passthrough failed: %+v", res.Incompatible)
	}
}

// A winget package with no Id is skipped (not imported) with a clear reason.
func TestMapBundle_MissingIdSkipped(t *testing.T) {
	b := &Bundle{Packages: []Package{{ID: "", Name: "Nameless", Source: "winget"}}}
	res := MapBundle(b, MapOptions{})
	if len(res.Imported) != 0 {
		t.Errorf("a winget package with no Id must not import, got %+v", res.Imported)
	}
	if len(res.Skipped) != 1 || res.Skipped[0].Manager != "winget" {
		t.Fatalf("expected one winget skip for the missing Id, got %+v", res.Skipped)
	}
}

// Slug collisions are de-duplicated deterministically and recorded.
func TestMapBundle_SlugCollisionDedup(t *testing.T) {
	b := &Bundle{Packages: []Package{
		{ID: "Foo.Git", Name: "Foo Git", Source: "winget"},
		{ID: "Bar.Git", Name: "Bar Git", Source: "winget"},
		{ID: "Baz.Git", Name: "Baz Git", Source: "winget"},
	}}
	res := MapBundle(b, MapOptions{})
	got := []string{res.Imported[0].ID, res.Imported[1].ID, res.Imported[2].ID}
	want := []string{"git", "git-2", "git-3"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("collision de-dup ids = %v, want %v", got, want)
	}
	if len(res.Collisions) != 2 {
		t.Errorf("expected 2 collision notes, got %d: %v", len(res.Collisions), res.Collisions)
	}
}

// Mapping is deterministic: the same input with the same options yields an
// identical result value.
func TestMapBundle_Deterministic(t *testing.T) {
	b := &Bundle{Packages: []Package{
		{ID: "Microsoft.VisualStudioCode", Name: "VS Code", Source: "winget", Version: "1.0"},
		{ID: "Foo.Git", Name: "Foo Git", Source: "winget"},
		{ID: "Bar.Git", Name: "Bar Git", Source: "winget"},
		{ID: "nodejs", Name: "Node.js", Source: "chocolatey", ManagerName: "Chocolatey"},
	}}
	a := MapBundle(b, MapOptions{Pin: true})
	c := MapBundle(b, MapOptions{Pin: true})
	if !reflect.DeepEqual(a, c) {
		t.Errorf("MapBundle is not deterministic:\n a = %+v\n c = %+v", a, c)
	}
}

func TestSlugForID(t *testing.T) {
	cases := map[string]string{
		"Microsoft.VisualStudioCode": "visualstudiocode",
		"Git.Git":                    "git",
		"Mozilla.Firefox":            "firefox",
		"vim":                        "vim",
		"":                           "app",
		"A.B.C":                      "c",
		"Some.Weird!Id":              "weird-id",
	}
	for in, want := range cases {
		if got := slugForID(in); got != want {
			t.Errorf("slugForID(%q) = %q, want %q", in, got, want)
		}
	}
}
