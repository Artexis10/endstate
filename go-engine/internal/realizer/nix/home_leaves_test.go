// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package nix

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

// builtActivation writes a fake activation package: a store directory that holds
// the files tree, and an output that links it with the given format version.
func builtActivation(t *testing.T, version string, build func(files string)) (output, store string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("a built activation package is a POSIX symlink layout")
	}
	root := t.TempDir()
	store = filepath.Join(root, "store")
	files := filepath.Join(store, "hash-home-manager-files")
	output = filepath.Join(root, "output")
	for _, dir := range []string{files, output} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	build(files)
	if err := os.WriteFile(filepath.Join(output, "gen-version"), []byte(version+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(files, filepath.Join(output, "home-files")); err != nil {
		t.Fatal(err)
	}
	return output, store
}

func writeLeaf(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
}

func leafPaths(dirs []HomeFilesDir) []string {
	var paths []string
	for _, dir := range dirs {
		for _, name := range dir.Names {
			paths = append(paths, strings.TrimPrefix(dir.Dir+"/"+name, "/"))
		}
	}
	sort.Strings(paths)
	return paths
}

func TestReadHomeFilesListsLeavesAndTreatsLinksAsSingleLeaves(t *testing.T) {
	var elsewhere string
	output, store := builtActivation(t, "1", func(files string) {
		writeLeaf(t, filepath.Join(files, ".bashrc"))
		writeLeaf(t, filepath.Join(files, ".config", "app", "a.conf"))
		// A directory that a non-recursive declaration links is one leaf: its referent
		// holds children that must never become claims of their own.
		elsewhere = filepath.Join(filepath.Dir(files), "linked-source")
		writeLeaf(t, filepath.Join(elsewhere, "child"))
		if err := os.Symlink(elsewhere, filepath.Join(files, ".config", "themes")); err != nil {
			t.Fatal(err)
		}
		// A link back to an ancestor must not be followed.
		if err := os.Symlink(files, filepath.Join(files, ".config", "app", "loop")); err != nil {
			t.Fatal(err)
		}
	})
	dirs, err := ReadHomeFiles(output, store, 100)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{".bashrc", ".config/app/a.conf", ".config/app/loop", ".config/themes"}
	if got := leafPaths(dirs); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("leaves = %v, want %v", got, want)
	}
}

func TestReadHomeFilesRefusesUnknownLayouts(t *testing.T) {
	for _, test := range []struct {
		name  string
		build func(t *testing.T) (output, store string)
	}{
		{"newer package format", func(t *testing.T) (string, string) {
			return builtActivation(t, "2", func(files string) { writeLeaf(t, filepath.Join(files, "x")) })
		}},
		{"files link outside the store", func(t *testing.T) (string, string) {
			output, _ := builtActivation(t, "1", func(files string) { writeLeaf(t, filepath.Join(files, "x")) })
			return output, t.TempDir()
		}},
		{"no files link", func(t *testing.T) (string, string) {
			output, store := builtActivation(t, "1", func(files string) { writeLeaf(t, filepath.Join(files, "x")) })
			if err := os.Remove(filepath.Join(output, "home-files")); err != nil {
				t.Fatal(err)
			}
			return output, store
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			output, store := test.build(t)
			if _, err := ReadHomeFiles(output, store, 100); err == nil {
				t.Fatal("unreadable layout was accepted")
			}
		})
	}
}

func TestReadHomeFilesBoundNamesTheDirectoryThatHoldsMostEntries(t *testing.T) {
	// A recursive root splits into variant directories: no single variant holds most
	// entries, so the message must name the root that holds them all.
	output, store := builtActivation(t, "1", func(files string) {
		for _, variant := range []string{"a", "b", "c", "d"} {
			for i := 0; i < 50; i++ {
				writeLeaf(t, filepath.Join(files, ".vim", variant, fmt.Sprintf("f%d", i)))
			}
		}
	})
	_, err := ReadHomeFiles(output, store, 120)
	var tooLarge *ErrHomeFilesTooLarge
	if !errors.As(err, &tooLarge) || tooLarge.Root != "~/.vim" {
		t.Fatalf("bound error does not name the root that holds most entries: %v", err)
	}
}
