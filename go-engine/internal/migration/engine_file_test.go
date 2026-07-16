// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package migration

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/modules"
)

func TestSupportedOperationTypesIsClosedAndStable(t *testing.T) {
	want := []string{
		"file-copy", "file-delete", "file-move",
		"ini-delete", "ini-move", "ini-set",
		"json-delete", "json-move", "json-set",
	}
	got := SupportedOperationTypes()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("SupportedOperationTypes = %v, want %v", got, want)
	}
	got[0] = "mutated"
	if again := SupportedOperationTypes(); !reflect.DeepEqual(again, want) {
		t.Fatalf("caller mutation changed registry: %v", again)
	}
}

func TestEngineRejectsArbitraryCodeAndUnknownOperations(t *testing.T) {
	root := t.TempDir()
	unsupported := []string{
		"shell", "powershell", "batch", "command", "executable",
		"plugin", "regex-replace", "File-Copy", "file-copy ", "",
	}
	for _, operationType := range unsupported {
		t.Run(operationType, func(t *testing.T) {
			err := NewEngine().Apply(root, []modules.MigrationOperationDef{{Type: operationType}})
			if CodeOf(err) != CodeUnsupportedOperation {
				t.Fatalf("Apply(%q) error = %v, code = %q", operationType, err, CodeOf(err))
			}
		})
	}
}

func TestEngineRequiresSafeRootEvenWithoutOperations(t *testing.T) {
	err := NewEngine().Apply("relative-root", nil)
	if CodeOf(err) != CodeUnsafeRoot {
		t.Fatalf("Apply error = %v, code = %q", err, CodeOf(err))
	}
}

func TestEngineFileOperationsSupportFilesAndContainedTrees(t *testing.T) {
	root := t.TempDir()
	writeMigrationFile(t, root, "source.txt", "source")
	writeMigrationFile(t, root, "tree/z.txt", "z")
	writeMigrationFile(t, root, "tree/a.txt", "a")
	writeMigrationFile(t, root, "move-tree/nested.txt", "move")
	writeMigrationFile(t, root, "delete-tree/nested.txt", "delete")

	operations := []modules.MigrationOperationDef{
		{Type: "file-copy", Source: "source.txt", Target: "copies/copied.txt"},
		{Type: "file-copy", Source: "tree", Target: "tree-copy"},
		{Type: "file-move", Source: "copies/copied.txt", Target: "moved.txt"},
		{Type: "file-move", Source: "move-tree", Target: "moved-tree"},
		{Type: "file-delete", Path: "source.txt"},
		{Type: "file-delete", Path: "delete-tree"},
	}
	if err := NewEngine().Apply(root, operations); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	assertMigrationFile(t, root, "moved.txt", "source")
	assertMigrationFile(t, root, "tree-copy/a.txt", "a")
	assertMigrationFile(t, root, "tree-copy/z.txt", "z")
	assertMigrationFile(t, root, "moved-tree/nested.txt", "move")
	for _, absent := range []string{"source.txt", "copies/copied.txt", "move-tree", "delete-tree"} {
		if _, err := os.Lstat(filepath.Join(root, filepath.FromSlash(absent))); !os.IsNotExist(err) {
			t.Fatalf("%s still exists or stat failed: %v", absent, err)
		}
	}
	assertNoMigrationTemps(t, root)
}

func TestEngineStopsOnFirstErrorWithoutRollingBackEarlierStagingOperations(t *testing.T) {
	root := t.TempDir()
	writeMigrationFile(t, root, "source.txt", "source")
	writeMigrationFile(t, root, "must-remain.txt", "remain")
	operations := []modules.MigrationOperationDef{
		{Type: "file-copy", Source: "source.txt", Target: "copied.txt"},
		{Type: "command"},
		{Type: "file-delete", Path: "must-remain.txt"},
	}

	err := NewEngine().Apply(root, operations)
	if CodeOf(err) != CodeUnsupportedOperation {
		t.Fatalf("Apply error = %v, code = %q", err, CodeOf(err))
	}
	assertMigrationFile(t, root, "copied.txt", "source")
	assertMigrationFile(t, root, "must-remain.txt", "remain")
}

func TestEngineRejectsEveryUnsafeOperationPath(t *testing.T) {
	unsafe := []string{
		"", "/absolute", `C:\absolute`, `C:volume`, `\\server\share`,
		"../escape", "safe/../../escape", "file:stream", "$HOME/file",
		"%APPDATA%/file", "${instance.root}/file", "~/file",
	}
	for _, path := range unsafe {
		t.Run(path, func(t *testing.T) {
			root := t.TempDir()
			writeMigrationFile(t, root, "source.txt", "source")
			err := NewEngine().Apply(root, []modules.MigrationOperationDef{{
				Type: "file-copy", Source: "source.txt", Target: path,
			}})
			if CodeOf(err) != CodeUnsafePath {
				t.Fatalf("Apply target %q error = %v, code = %q", path, err, CodeOf(err))
			}
		})
	}
}

func TestEngineRejectsCopyAndMoveIntoSourceDescendant(t *testing.T) {
	for _, operationType := range []string{"file-copy", "file-move"} {
		t.Run(operationType, func(t *testing.T) {
			root := t.TempDir()
			writeMigrationFile(t, root, "tree/source.txt", "source")
			err := NewEngine().Apply(root, []modules.MigrationOperationDef{{
				Type: operationType, Source: "tree", Target: "tree/descendant",
			}})
			if CodeOf(err) != CodeSourceDescendant {
				t.Fatalf("Apply error = %v, code = %q", err, CodeOf(err))
			}
			assertMigrationFile(t, root, "tree/source.txt", "source")
			if _, err := os.Lstat(filepath.Join(root, "tree", "descendant")); !os.IsNotExist(err) {
				t.Fatalf("descendant target exists or stat failed: %v", err)
			}
		})
	}
}

func TestEngineRejectsLinksWithoutCreatingPartialCopy(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	writeMigrationFile(t, root, "tree/safe.txt", "safe")
	if err := os.Symlink(filepath.Join(outside, "outside.txt"), filepath.Join(root, "tree", "link")); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("creating Windows reparse-point symlink requires local privilege: %v", err)
		}
		t.Fatal(err)
	}

	err := NewEngine().Apply(root, []modules.MigrationOperationDef{{
		Type: "file-copy", Source: "tree", Target: "copy",
	}})
	if CodeOf(err) != CodeLinkUnsupported {
		t.Fatalf("Apply error = %v, code = %q", err, CodeOf(err))
	}
	if _, statErr := os.Lstat(filepath.Join(root, "copy")); !os.IsNotExist(statErr) {
		t.Fatalf("partial copy exists or stat failed: %v", statErr)
	}
}

func TestEngineRejectsExistingDirectoryDestination(t *testing.T) {
	root := t.TempDir()
	writeMigrationFile(t, root, "source/file.txt", "source")
	writeMigrationFile(t, root, "target/original.txt", "original")
	err := NewEngine().Apply(root, []modules.MigrationOperationDef{{
		Type: "file-copy", Source: "source", Target: "target",
	}})
	if CodeOf(err) != CodeDestinationExists {
		t.Fatalf("Apply error = %v, code = %q", err, CodeOf(err))
	}
	assertMigrationFile(t, root, "target/original.txt", "original")
}

func writeMigrationFile(t *testing.T, root, relative, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func assertMigrationFile(t *testing.T, root, relative, want string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relative)))
	if err != nil {
		t.Fatalf("read %s: %v", relative, err)
	}
	if string(data) != want {
		t.Fatalf("%s = %q, want %q", relative, data, want)
	}
}

func assertNoMigrationTemps(t *testing.T, root string) {
	t.Helper()
	var temps []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if strings.HasPrefix(info.Name(), ".endstate-") {
			temps = append(temps, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(temps)
	if len(temps) != 0 {
		t.Fatalf("temporary paths remain: %v", temps)
	}
}
