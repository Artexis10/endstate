// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package safepath

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestResolveAcceptsContainedPortablePath(t *testing.T) {
	root := safePathTestRoot(t)
	got, err := Resolve(root, "profiles/v2/settings.json")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	want := filepath.Join(root, "profiles", "v2", "settings.json")
	if got != want {
		t.Fatalf("Resolve = %q, want %q", got, want)
	}
}

func TestValidateRootAcceptsHostTempDirectory(t *testing.T) {
	if err := ValidateRoot(t.TempDir()); err != nil {
		t.Fatalf("ValidateRoot host temp directory: %v", err)
	}
}

func TestResolveRejectsUnsafePortablePaths(t *testing.T) {
	root := safePathTestRoot(t)
	unsafe := []string{
		"",
		".",
		"/absolute/path",
		`C:\absolute\path`,
		`C:relative-volume`,
		`\\server\share\path`,
		`//server/share/path`,
		`settings.json:stream`,
		"../escape",
		"safe/../../escape",
		"$HOME/settings",
		"%APPDATA%/settings",
		"${instance.root}/settings",
		"~/settings",
		" spaced/settings",
		"settings/ ",
		"settings\x00name",
	}
	for _, path := range unsafe {
		t.Run(path, func(t *testing.T) {
			_, err := Resolve(root, path)
			if !errors.Is(err, ErrUnsafePath) {
				t.Fatalf("Resolve(%q) error = %v, want ErrUnsafePath", path, err)
			}
		})
	}
}

func TestResolveRequiresAbsoluteExistingDirectoryRoot(t *testing.T) {
	file := filepath.Join(safePathTestRoot(t), "file")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(safePathTestRoot(t), "missing")
	for _, root := range []string{"relative", missing, file} {
		_, err := Resolve(root, "settings.json")
		if !errors.Is(err, ErrUnsafeRoot) {
			t.Fatalf("Resolve root %q error = %v, want ErrUnsafeRoot", root, err)
		}
	}
}

func TestResolveRejectsLinkInExistingPath(t *testing.T) {
	root := safePathTestRoot(t)
	outside := t.TempDir()
	link := filepath.Join(root, "linked")
	if err := os.Symlink(outside, link); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("creating Windows reparse-point symlink requires local privilege: %v", err)
		}
		t.Fatal(err)
	}

	_, err := Resolve(root, "linked/settings.json")
	if !errors.Is(err, ErrLinkUnsupported) {
		t.Fatalf("Resolve through link error = %v, want ErrLinkUnsupported", err)
	}
}

func TestResolveRejectsLinkedRoot(t *testing.T) {
	parent := t.TempDir()
	realRoot := t.TempDir()
	linkedRoot := filepath.Join(parent, "linked-root")
	if err := os.Symlink(realRoot, linkedRoot); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("creating Windows reparse-point symlink requires local privilege: %v", err)
		}
		t.Fatal(err)
	}

	_, err := Resolve(linkedRoot, "settings.json")
	if !errors.Is(err, ErrLinkUnsupported) {
		t.Fatalf("Resolve linked root error = %v, want ErrLinkUnsupported", err)
	}
}

func TestResolveRejectsLinkInRootParentChain(t *testing.T) {
	realParent := t.TempDir()
	if err := os.Mkdir(filepath.Join(realParent, "staging"), 0o755); err != nil {
		t.Fatal(err)
	}
	linkParent := filepath.Join(t.TempDir(), "linked-parent")
	if err := os.Symlink(realParent, linkParent); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("creating Windows reparse-point symlink requires local privilege: %v", err)
		}
		t.Fatal(err)
	}
	root := filepath.Join(linkParent, "staging")

	_, err := Resolve(root, "settings.json")
	if !errors.Is(err, ErrLinkUnsupported) {
		t.Fatalf("Resolve beneath linked parent error = %v, want ErrLinkUnsupported", err)
	}
}

func safePathTestRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return root
}
