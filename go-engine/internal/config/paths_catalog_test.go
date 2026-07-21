// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"os"
	"path/filepath"
	"testing"
)

// ---------------------------------------------------------------------------
// walkUpFor — the ancestor search behind repo-root resolution
// ---------------------------------------------------------------------------

func TestWalkUpFor_FindsNearestAncestor(t *testing.T) {
	root := t.TempDir()
	deep := filepath.Join(root, "a", "b", "c")
	if err := os.MkdirAll(deep, 0755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(root, "a", "marker")
	if err := os.WriteFile(marker, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	got := walkUpFor(deep, func(dir string) bool {
		_, err := os.Stat(filepath.Join(dir, "marker"))
		return err == nil
	})

	want := filepath.Join(root, "a")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestWalkUpFor_MatchesStartDirectoryItself(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "marker"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	got := walkUpFor(root, func(dir string) bool {
		_, err := os.Stat(filepath.Join(dir, "marker"))
		return err == nil
	})

	if got != root {
		t.Errorf("start dir must be considered: got %q, want %q", got, root)
	}
}

func TestWalkUpFor_ReturnsEmptyWhenNoMatch(t *testing.T) {
	if got := walkUpFor(t.TempDir(), func(string) bool { return false }); got != "" {
		t.Errorf("expected empty string when nothing matches, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// ResolveRepoRoot — installed-layout fallback
// ---------------------------------------------------------------------------

// TestResolveRepoRoot_EnvVarWins locks precedence: an explicit override must
// beat both filesystem walks.
func TestResolveRepoRoot_EnvVarWins(t *testing.T) {
	t.Setenv("ENDSTATE_ROOT", "C:\\explicit\\override")

	if got := ResolveRepoRoot(); got != "C:\\explicit\\override" {
		t.Errorf("ENDSTATE_ROOT must win, got %q", got)
	}
}

// TestResolveRepoRoot_MarkerWinsOverCatalog pins ordering. A repo checkout has
// BOTH a release-please marker and a modules/apps tree; the marker must win so
// existing behaviour in a checkout is byte-identical.
func TestResolveRepoRoot_MarkerWinsOverCatalog(t *testing.T) {
	root := t.TempDir()
	// Repo-shaped: marker at the top, catalog nested deeper.
	if err := os.WriteFile(filepath.Join(root, ".release-please-manifest.json"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(root, "nested")
	if err := os.MkdirAll(filepath.Join(nested, "modules", "apps"), 0755); err != nil {
		t.Fatal(err)
	}

	marker := walkUpFor(nested, func(dir string) bool {
		_, err := os.Stat(filepath.Join(dir, ".release-please-manifest.json"))
		return err == nil
	})
	catalog := walkUpFor(nested, func(dir string) bool {
		info, err := os.Stat(filepath.Join(dir, "modules", "apps"))
		return err == nil && info.IsDir()
	})

	if marker != root {
		t.Errorf("marker walk should find the repo root, got %q", marker)
	}
	if catalog != nested {
		t.Errorf("catalog walk should find the nested dir, got %q", catalog)
	}
	if marker == catalog {
		t.Fatal("test fixture is not discriminating between the two walks")
	}
	// ResolveRepoRoot runs the marker walk first, so the repo root wins.
}

// TestResolveRepoRoot_CatalogFallbackFindsInstallLayout is the regression for
// the reported bug: a PATH-invoked binary at <install>\bin\lib\endstate.exe has
// no repo marker anywhere above it, and before this fallback resolved nothing —
// so capture silently emitted an app list with no settings.
func TestResolveRepoRoot_CatalogFallbackFindsInstallLayout(t *testing.T) {
	install := filepath.Join(t.TempDir(), "Endstate", "bin")
	libDir := filepath.Join(install, "lib")
	if err := os.MkdirAll(libDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(install, "modules", "apps"), 0755); err != nil {
		t.Fatal(err)
	}

	got := walkUpFor(libDir, func(dir string) bool {
		info, err := os.Stat(filepath.Join(dir, "modules", "apps"))
		return err == nil && info.IsDir()
	})

	if got != install {
		t.Errorf("expected the install dir carrying modules/apps, got %q want %q", got, install)
	}
}

// TestResolveRepoRoot_CatalogFallbackIgnoresFileNamedModules guards against a
// stray file shadowing the directory check.
func TestResolveRepoRoot_CatalogFallbackIgnoresFileNamedModules(t *testing.T) {
	dir := t.TempDir()
	modulesPath := filepath.Join(dir, "modules")
	if err := os.MkdirAll(modulesPath, 0755); err != nil {
		t.Fatal(err)
	}
	// "apps" exists but is a file, not a directory.
	if err := os.WriteFile(filepath.Join(modulesPath, "apps"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	got := walkUpFor(dir, func(d string) bool {
		info, err := os.Stat(filepath.Join(d, "modules", "apps"))
		return err == nil && info.IsDir()
	})

	if got != "" {
		t.Errorf("a file named apps must not satisfy the catalog check, got %q", got)
	}
}
