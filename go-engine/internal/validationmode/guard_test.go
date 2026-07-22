// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationmode

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
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
		} else if strings.Contains(strings.ToLower(err.Error()), strings.ToLower(base)) || strings.Contains(strings.ToLower(err.Error()), strings.ToLower(outside)) {
			t.Fatalf("guard error leaked absolute path: %v", err)
		}
	} else if runtime.GOOS != "windows" {
		t.Fatal(err)
	}
}

func TestWriteGuardProtectsIncrementallyAndCompactsDescendants(t *testing.T) {
	base := t.TempDir()
	allowed := filepath.Join(base, "allowed")
	protected := filepath.Join(base, "protected")
	child := filepath.Join(protected, "child")
	if err := os.MkdirAll(allowed, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(child, 0o700); err != nil {
		t.Fatal(err)
	}
	guard, err := NewWriteGuard(allowed, []string{protected})
	if err != nil {
		t.Fatal(err)
	}
	if err := guard.Protect([]ProtectedPath{{Path: child, Label: "child"}, {Path: protected, Label: "host-target"}}); err != nil {
		t.Fatal(err)
	}
	if got := guard.ProtectedCount(); got != 1 {
		t.Fatalf("protected count = %d, want 1", got)
	}
	if err := os.WriteFile(filepath.Join(child, "changed.txt"), []byte("changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	changes, err := guard.Check()
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) == 0 || guard.Label(changes[0].Path) != "host-target" {
		t.Fatalf("changes = %#v, want safe ancestor label", changes)
	}
}

func TestWriteGuardIncrementalProtectPreservesEarlierBaseline(t *testing.T) {
	base := t.TempDir()
	allowed := filepath.Join(base, "allowed")
	first := filepath.Join(base, "first")
	second := filepath.Join(base, "second")
	for _, dir := range []string{allowed, first, second} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	firstFile := filepath.Join(first, "value.txt")
	if err := os.WriteFile(firstFile, []byte("before"), 0o600); err != nil {
		t.Fatal(err)
	}
	guard, err := NewWriteGuard(allowed, []string{first})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(firstFile, []byte("after"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := guard.Protect([]ProtectedPath{{Path: second, Label: "second"}}); err != nil {
		t.Fatal(err)
	}
	guard.Seal()
	changes, err := guard.Check()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, change := range changes {
		if change.Path == firstFile && change.Kind == ChangeContent {
			found = true
		}
	}
	if !found {
		t.Fatalf("incremental registration erased first mutation: %#v", changes)
	}
}

func TestWriteGuardAncestorCompactionPreservesDescendantEvidence(t *testing.T) {
	base := t.TempDir()
	allowed := filepath.Join(base, "allowed")
	parent := filepath.Join(base, "protected")
	child := filepath.Join(parent, "child")
	for _, dir := range []string{allowed, child} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	file := filepath.Join(child, "value.txt")
	if err := os.WriteFile(file, []byte("before"), 0o600); err != nil {
		t.Fatal(err)
	}
	guard, err := NewWriteGuard(allowed, []string{child})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("after"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := guard.Protect([]ProtectedPath{{Path: parent, Label: "parent"}}); err != nil {
		t.Fatal(err)
	}
	if guard.ProtectedCount() != 1 {
		t.Fatalf("protected count = %d, want compacted ancestor", guard.ProtectedCount())
	}
	guard.Seal()
	changes, err := guard.Check()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, change := range changes {
		if change.Path == file && change.Kind == ChangeContent {
			found = true
		}
	}
	if !found {
		t.Fatalf("ancestor compaction erased descendant mutation: %#v", changes)
	}
}

func TestWriteGuardFailedIncrementalProtectIsAtomic(t *testing.T) {
	base := t.TempDir()
	allowed := filepath.Join(base, "allowed")
	first := filepath.Join(base, "first")
	second := filepath.Join(base, "second")
	for _, dir := range []string{allowed, first, second} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	firstFile := filepath.Join(first, "value.txt")
	if err := os.WriteFile(firstFile, []byte("before"), 0o600); err != nil {
		t.Fatal(err)
	}
	guard, err := newWriteGuardWithLimits(allowed, []string{first}, guardLimits{roots: 3, entries: 3, bytes: 100})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(firstFile, []byte("after!"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(second, "extra.txt"), []byte("extra"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := guard.Protect([]ProtectedPath{{Path: second, Label: "second"}}); !errors.Is(err, ErrGuardBudget) {
		t.Fatalf("Protect error = %v, want ErrGuardBudget", err)
	}
	if guard.ProtectedCount() != 1 {
		t.Fatalf("protected count = %d, want failed registration to be atomic", guard.ProtectedCount())
	}
	changes, err := guard.Check()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, change := range changes {
		if change.Path == firstFile && change.Kind == ChangeContent {
			found = true
		}
	}
	if !found {
		t.Fatalf("failed registration changed earlier baseline: %#v", changes)
	}
}

func TestWriteGuardRejectsProtectedRootBudget(t *testing.T) {
	base := t.TempDir()
	allowed := filepath.Join(base, "allowed")
	if err := os.MkdirAll(allowed, 0o700); err != nil {
		t.Fatal(err)
	}
	guard, err := NewWriteGuard(allowed, []string{filepath.Join(base, "seed")})
	if err != nil {
		t.Fatal(err)
	}
	paths := make([]ProtectedPath, 0, 256)
	for index := 0; index < 256; index++ {
		paths = append(paths, ProtectedPath{Path: filepath.Join(base, "p", fmt.Sprintf("%03d", index)), Label: "budget"})
	}
	if err := guard.Protect(paths); !errors.Is(err, ErrGuardBudget) {
		t.Fatalf("Protect error = %v, want ErrGuardBudget", err)
	}
}

func TestWriteGuardSealRejectsLateSnapshots(t *testing.T) {
	base := t.TempDir()
	allowed := filepath.Join(base, "allowed")
	if err := os.MkdirAll(allowed, 0o700); err != nil {
		t.Fatal(err)
	}
	guard, err := NewWriteGuard(allowed, []string{filepath.Join(base, "protected")})
	if err != nil {
		t.Fatal(err)
	}
	guard.Seal()
	if err := guard.Protect([]ProtectedPath{{Path: filepath.Join(base, "late"), Label: "late"}}); !errors.Is(err, ErrUnsafeGuardPath) {
		t.Fatalf("late Protect error = %v, want ErrUnsafeGuardPath", err)
	}
}

func TestWriteGuardRejectsFileByMetadataBeforeHashingPastBudget(t *testing.T) {
	base := t.TempDir()
	allowed := filepath.Join(base, "allowed")
	protected := filepath.Join(base, "protected")
	if err := os.MkdirAll(allowed, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(protected, 0o700); err != nil {
		t.Fatal(err)
	}
	large := filepath.Join(protected, "large.bin")
	if err := os.WriteFile(large, []byte("1234"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := newWriteGuardWithLimits(allowed, []string{protected}, guardLimits{roots: 256, entries: 100000, bytes: 3}); !errors.Is(err, ErrGuardBudget) {
		t.Fatalf("NewWriteGuard error = %v, want ErrGuardBudget", err)
	}
}
