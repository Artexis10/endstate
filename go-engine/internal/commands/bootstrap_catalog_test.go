// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"os"
	"path/filepath"
	"testing"
)

// seedCatalog builds a minimal source tree shaped like the repo: modules/apps
// with one module, plus a payload tree.
func seedCatalog(t *testing.T, root string, moduleIDs ...string) {
	t.Helper()
	for _, id := range moduleIDs {
		dir := filepath.Join(root, "modules", "apps", id)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "module.jsonc"), []byte(`{"id":"apps.`+id+`"}`), 0644); err != nil {
			t.Fatal(err)
		}
	}
	payload := filepath.Join(root, "payload", "apps")
	if err := os.MkdirAll(payload, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(payload, "seed.txt"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
}

// TestInstallCatalog_CopiesModulesAndPayload is the core of the fix: an install
// that carries no catalog makes capture silently record apps without settings.
func TestInstallCatalog_CopiesModulesAndPayload(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	seedCatalog(t, src, "vscode", "git")

	installed, err := installCatalog(src, dst)
	if err != nil {
		t.Fatalf("installCatalog: %v", err)
	}

	if len(installed) != 2 {
		t.Errorf("expected both trees installed, got %v", installed)
	}
	for _, rel := range []string{
		filepath.Join("modules", "apps", "vscode", "module.jsonc"),
		filepath.Join("modules", "apps", "git", "module.jsonc"),
		filepath.Join("payload", "apps", "seed.txt"),
	} {
		if _, statErr := os.Stat(filepath.Join(dst, rel)); statErr != nil {
			t.Errorf("expected %s in the install, got %v", rel, statErr)
		}
	}
}

// TestInstallCatalog_RefreshDropsRemovedModules verifies a refresh replaces
// rather than unions. A stale catalog is worse than none: capture would match
// against module definitions that no longer exist upstream.
func TestInstallCatalog_RefreshDropsRemovedModules(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()

	// A previous install left a module that upstream has since deleted.
	stale := filepath.Join(dst, "modules", "apps", "retired-app")
	if err := os.MkdirAll(stale, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stale, "module.jsonc"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}

	seedCatalog(t, src, "vscode")

	if _, err := installCatalog(src, dst); err != nil {
		t.Fatalf("installCatalog: %v", err)
	}

	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Error("a refresh must drop modules removed upstream, not union with them")
	}
	if _, err := os.Stat(filepath.Join(dst, "modules", "apps", "vscode", "module.jsonc")); err != nil {
		t.Errorf("current module missing after refresh: %v", err)
	}
}

// TestInstallCatalog_SamePathIsNotDestructive covers re-bootstrapping from an
// already-installed binary, where the resolved source IS the install. Without
// the guard the remove-then-copy would delete the catalog it is copying.
func TestInstallCatalog_SamePathIsNotDestructive(t *testing.T) {
	dir := t.TempDir()
	seedCatalog(t, dir, "vscode")

	installed, err := installCatalog(dir, dir)
	if err != nil {
		t.Fatalf("installCatalog: %v", err)
	}

	if len(installed) != 2 {
		t.Errorf("expected both trees reported as present, got %v", installed)
	}
	if _, err := os.Stat(filepath.Join(dir, "modules", "apps", "vscode", "module.jsonc")); err != nil {
		t.Errorf("copying a tree onto itself destroyed it: %v", err)
	}
}

// TestInstallCatalog_MissingSourceIsNotAnError covers a bare binary downloaded
// outside any repo or GUI layout: nothing to copy, but the shim and PATH entry
// are still worth installing, so bootstrap must not fail.
func TestInstallCatalog_MissingSourceIsNotAnError(t *testing.T) {
	installed, err := installCatalog(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatalf("absent source trees must not fail bootstrap: %v", err)
	}
	if len(installed) != 0 {
		t.Errorf("expected nothing installed, got %v", installed)
	}
}

// TestInstallCatalog_EmptySourceRootIsNoOp covers an unresolvable root.
func TestInstallCatalog_EmptySourceRootIsNoOp(t *testing.T) {
	installed, err := installCatalog("", t.TempDir())
	if err != nil {
		t.Fatalf("empty source root must be a no-op, got %v", err)
	}
	if installed != nil {
		t.Errorf("expected nil, got %v", installed)
	}
}
