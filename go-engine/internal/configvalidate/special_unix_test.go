// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package configvalidate

import (
	"path/filepath"
	"syscall"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/modules"
)

func TestValidateRejectsSpecialFiles(t *testing.T) {
	root := safeValidationTestRoot(t)
	if err := syscall.Mkfifo(filepath.Join(root, "pipe"), 0o600); err != nil {
		t.Skipf("cannot create FIFO on this filesystem: %v", err)
	}
	err := ValidateStaging(root, []modules.ValidationDef{{Type: "file-exists", Path: "pipe"}})
	if CodeOf(err) != CodeUnsupportedFileType {
		t.Fatalf("Validate FIFO error = %v, code = %q", err, CodeOf(err))
	}
	err = ValidateResolved([]ResolvedValidation{{
		Definition: modules.ValidationDef{Type: "file-exists", Path: "logical"},
		HostPath:   filepath.Join(root, "pipe"),
	}})
	if CodeOf(err) != CodeUnsupportedFileType {
		t.Fatalf("ValidateResolved FIFO error = %v, code = %q", err, CodeOf(err))
	}
}

func TestValidateResolvedDoesNotReinterpretBackslashInHostFilename(t *testing.T) {
	root := safeValidationTestRoot(t)
	writeValidationFile(t, root, "directory/target", "value")
	err := ValidateResolved([]ResolvedValidation{{
		Definition: modules.ValidationDef{Type: "file-exists", Path: "logical"},
		HostPath:   filepath.Join(root, `directory\target`),
	}})
	if CodeOf(err) != CodeUnsafePath {
		t.Fatalf("ValidateResolved error = %v, code = %q, want %q", err, CodeOf(err), CodeUnsafePath)
	}
}
