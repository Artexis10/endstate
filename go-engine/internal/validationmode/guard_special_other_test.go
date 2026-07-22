// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package validationmode

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestWriteGuardRejectsSpecialFiles(t *testing.T) {
	base := t.TempDir()
	allowed := filepath.Join(base, "allowed")
	if err := os.MkdirAll(allowed, 0o700); err != nil {
		t.Fatal(err)
	}
	special := filepath.Join(base, "special")
	if err := syscall.Mkfifo(special, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewWriteGuard(allowed, []string{special}); !errors.Is(err, ErrUnsafeGuardPath) {
		t.Fatalf("special error = %v, want ErrUnsafeGuardPath", err)
	}
}
