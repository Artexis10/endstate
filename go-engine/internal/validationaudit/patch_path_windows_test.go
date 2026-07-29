// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package validationaudit

import (
	"os"
	"path/filepath"
	"syscall"
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

func TestUnsafePathInfoRejectsWindowsJunctionWhenAvailable(t *testing.T) {
	junction := filepath.Join(os.Getenv("SystemDrive"), "Users", "All Users")
	attributes, err := syscall.GetFileAttributes(syscall.StringToUTF16Ptr(junction))
	if err != nil || attributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT == 0 {
		t.Skip("Windows junction unavailable")
	}
	info, err := os.Lstat(junction)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Skip("host junction is reported as a symbolic link")
	}
	if !unsafePathInfo(junction, info) {
		t.Fatal("unsafePathInfo() accepted Windows junction")
	}
}

func TestOpenWindowsSafePathHoldsLeafAgainstReplacement(t *testing.T) {
	path := filepath.Join(t.TempDir(), "candidate.patch")
	if err := os.WriteFile(path, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := openWindowsSafePath(path, false)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err := os.Rename(path, filepath.Join(filepath.Dir(path), "replacement.patch")); err == nil {
		t.Fatal("held patch leaf allowed replacement")
	}
}
