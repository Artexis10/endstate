// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package validationaudit

import (
	"os"
	"path/filepath"
	"testing"
)

func TestUnsafePathInfoRejectsWindowsReparsePoint(t *testing.T) {
	target := t.TempDir()
	link := filepath.Join(t.TempDir(), "reparse")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symbolic links unavailable: %v", err)
	}
	info, err := os.Lstat(link)
	if err != nil {
		t.Fatal(err)
	}
	if !unsafePathInfo(link, info) {
		t.Fatal("unsafePathInfo() accepted Windows reparse point")
	}
}
