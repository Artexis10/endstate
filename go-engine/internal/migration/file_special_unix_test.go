// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package migration

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/modules"
)

func TestEngineRejectsSpecialFilesWithoutCreatingPartialCopy(t *testing.T) {
	root := safeMigrationTestRoot(t)
	source := filepath.Join(root, "tree")
	if err := os.Mkdir(source, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(source, "pipe"), 0o600); err != nil {
		t.Skipf("cannot create FIFO on this filesystem: %v", err)
	}

	err := NewEngine().Apply(root, []modules.MigrationOperationDef{{
		Type: "file-copy", Source: "tree", Target: "copy",
	}})
	if CodeOf(err) != CodeUnsupportedFileType {
		t.Fatalf("Apply error = %v, code = %q", err, CodeOf(err))
	}
	if _, statErr := os.Lstat(filepath.Join(root, "copy")); !os.IsNotExist(statErr) {
		t.Fatalf("partial copy exists or stat failed: %v", statErr)
	}
}

func TestEngineDirectoryCopyPreservesRootMode(t *testing.T) {
	root := safeMigrationTestRoot(t)
	source := filepath.Join(root, "source")
	if err := os.Mkdir(source, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(source, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := NewEngine().Apply(root, []modules.MigrationOperationDef{{
		Type: "file-copy", Source: "source", Target: "target",
	}}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(root, "target"))
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o750 {
		t.Fatalf("copied root mode = %o, want 750", got)
	}
}
