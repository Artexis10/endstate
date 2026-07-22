// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationmode

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
)

func TestWriteGuardReportsDeterministicProtectedChanges(t *testing.T) {
	base := t.TempDir()
	allowed := filepath.Join(base, "allowed")
	protected := filepath.Join(base, "protected")
	deleted := filepath.Join(protected, "a-deleted.txt")
	changed := filepath.Join(protected, "b-changed.txt")
	typeChanged := filepath.Join(protected, "c-type")
	if err := os.MkdirAll(typeChanged, 0o700); err != nil {
		t.Fatal(err)
	}
	for path, content := range map[string]string{deleted: "delete", changed: "before"} {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(allowed, 0o700); err != nil {
		t.Fatal(err)
	}
	createdRoot := filepath.Join(base, "created-protected")
	guard, err := NewWriteGuard(allowed, []string{protected, createdRoot})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(allowed, "ignored.txt"), []byte("allowed"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(deleted); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(changed, []byte("after"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(typeChanged); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(typeChanged, []byte("now-file"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(createdRoot, "tree"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(createdRoot, "tree", "new.txt"), []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}

	changes, err := guard.Check()
	if err != nil {
		t.Fatal(err)
	}
	want := []Change{
		{Path: createdRoot, Kind: ChangeCreated},
		{Path: filepath.Join(createdRoot, "tree"), Kind: ChangeCreated},
		{Path: filepath.Join(createdRoot, "tree", "new.txt"), Kind: ChangeCreated},
		{Path: deleted, Kind: ChangeDeleted},
		{Path: changed, Kind: ChangeContent},
		{Path: typeChanged, Kind: ChangeType},
	}
	if !reflect.DeepEqual(changes, want) {
		t.Fatalf("changes = %#v, want %#v", changes, want)
	}
}

func TestWriteGuardDetectsDirectoryTreeChange(t *testing.T) {
	base := t.TempDir()
	allowed := filepath.Join(base, "allowed")
	protected := filepath.Join(base, "protected")
	if err := os.MkdirAll(filepath.Join(protected, "old"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(allowed, 0o700); err != nil {
		t.Fatal(err)
	}
	guard, err := NewWriteGuard(allowed, []string{protected})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(protected, "old")); err != nil {
		t.Fatal(err)
	}
	changes, err := guard.Check()
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0] != (Change{Path: filepath.Join(protected, "old"), Kind: ChangeDeleted}) {
		t.Fatalf("tree changes = %#v", changes)
	}
}

func TestWriteGuardRejectsOverlap(t *testing.T) {
	base := t.TempDir()
	allowed := filepath.Join(base, "allowed")
	if err := os.MkdirAll(filepath.Join(allowed, "child"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, protected := range []string{allowed, base, filepath.Join(allowed, "child")} {
		if _, err := NewWriteGuard(allowed, []string{protected}); !errors.Is(err, ErrGuardOverlap) {
			t.Errorf("protected %q error = %v, want ErrGuardOverlap", protected, err)
		}
	}
}

func TestWriteGuardRejectsLinks(t *testing.T) {
	base := t.TempDir()
	allowed := filepath.Join(base, "allowed")
	protected := filepath.Join(base, "protected")
	outside := t.TempDir()
	if err := os.MkdirAll(protected, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(allowed, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(protected, "linked")
	if err := os.Symlink(outside, link); err == nil {
		if _, err := NewWriteGuard(allowed, []string{protected}); !errors.Is(err, ErrUnsafeGuardPath) {
			t.Fatalf("link error = %v, want ErrUnsafeGuardPath", err)
		}
	} else if runtime.GOOS != "windows" {
		t.Fatal(err)
	}
}
