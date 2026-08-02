// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package safepath

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestIsLinkOrReparseIdentifiesSymlink(t *testing.T) {
	target := t.TempDir()
	link := filepath.Join(t.TempDir(), "linked")
	if err := os.Symlink(target, link); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("symlink unavailable: %v", err)
		}
		t.Fatal(err)
	}
	info, err := os.Lstat(link)
	if err != nil {
		t.Fatal(err)
	}
	if !IsLinkOrReparse(info) {
		t.Fatal("IsLinkOrReparse returned false for a symlink")
	}
}
