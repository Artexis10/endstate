// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package commands

import (
	"os"
	"path/filepath"
	"testing"
)

// setupUnixHome redirects HOME and XDG_DATA_HOME to hermetic temp dirs and
// returns (home, xdgDataHome).
func setupUnixHome(t *testing.T) (string, string) {
	t.Helper()
	home := t.TempDir()
	xdg := filepath.Join(t.TempDir(), "share")
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", xdg)
	// Keep the symlink dir off PATH so the hint path is exercised but harmless.
	t.Setenv("PATH", "/usr/bin:/bin")
	return home, xdg
}

func TestRunBootstrap_UnixInstallsBinaryAndSymlink(t *testing.T) {
	home, xdg := setupUnixHome(t)

	data, ierr := RunBootstrap(BootstrapFlags{})
	if ierr != nil {
		t.Fatalf("RunBootstrap returned error: %v", ierr)
	}

	bd, ok := data.(*BootstrapData)
	if !ok {
		t.Fatalf("expected *BootstrapData, got %T", data)
	}

	wantInstallDir := filepath.Join(xdg, "endstate", "bin")
	if bd.InstallPath != wantInstallDir {
		t.Errorf("InstallPath = %q, want %q", bd.InstallPath, wantInstallDir)
	}

	wantShim := filepath.Join(home, ".local", "bin", "endstate")
	if bd.ShimPath != wantShim {
		t.Errorf("ShimPath = %q, want %q", bd.ShimPath, wantShim)
	}

	if bd.AddedToPath {
		t.Error("AddedToPath = true, want false (Unix never edits PATH)")
	}

	// Binary installed and executable (0755).
	destBinary := filepath.Join(wantInstallDir, "lib", "endstate")
	info, err := os.Stat(destBinary)
	if err != nil {
		t.Fatalf("installed binary missing: %v", err)
	}
	if info.Mode().Perm() != 0755 {
		t.Errorf("binary mode = %o, want 0755", info.Mode().Perm())
	}

	// Symlink points at the installed binary.
	target, err := os.Readlink(wantShim)
	if err != nil {
		t.Fatalf("symlink missing or not a symlink: %v", err)
	}
	if target != destBinary {
		t.Errorf("symlink target = %q, want %q", target, destBinary)
	}
}

func TestRunBootstrap_UnixDefaultsToLocalShareWhenXDGUnset(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("PATH", "/usr/bin:/bin")

	data, ierr := RunBootstrap(BootstrapFlags{})
	if ierr != nil {
		t.Fatalf("RunBootstrap returned error: %v", ierr)
	}
	bd := data.(*BootstrapData)

	wantInstallDir := filepath.Join(home, ".local", "share", "endstate", "bin")
	if bd.InstallPath != wantInstallDir {
		t.Errorf("InstallPath = %q, want %q", bd.InstallPath, wantInstallDir)
	}
}

func TestRunBootstrap_UnixIsIdempotent(t *testing.T) {
	home, xdg := setupUnixHome(t)

	first, ierr := RunBootstrap(BootstrapFlags{})
	if ierr != nil {
		t.Fatalf("first RunBootstrap returned error: %v", ierr)
	}
	fb := first.(*BootstrapData)

	// Re-run: must not fail, must not nest, must re-point the symlink.
	second, ierr := RunBootstrap(BootstrapFlags{})
	if ierr != nil {
		t.Fatalf("second RunBootstrap returned error: %v", ierr)
	}
	sb := second.(*BootstrapData)

	if fb.InstallPath != sb.InstallPath || fb.ShimPath != sb.ShimPath {
		t.Errorf("idempotent re-run changed paths: %+v vs %+v", fb, sb)
	}

	destBinary := filepath.Join(xdg, "endstate", "bin", "lib", "endstate")
	wantShim := filepath.Join(home, ".local", "bin", "endstate")
	target, err := os.Readlink(wantShim)
	if err != nil {
		t.Fatalf("symlink missing after re-run: %v", err)
	}
	if target != destBinary {
		t.Errorf("symlink target after re-run = %q, want %q", target, destBinary)
	}
	info, err := os.Stat(destBinary)
	if err != nil {
		t.Fatalf("binary missing after re-run: %v", err)
	}
	if info.Mode().Perm() != 0755 {
		t.Errorf("binary mode after re-run = %o, want 0755", info.Mode().Perm())
	}
}

// TestRunBootstrap_UnixRePointsExistingNonSymlink verifies that a pre-existing
// regular file at the symlink target is replaced (never nested or errored on).
func TestRunBootstrap_UnixRePointsExistingNonSymlink(t *testing.T) {
	home, _ := setupUnixHome(t)

	binDir := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("setup mkdir: %v", err)
	}
	shimPath := filepath.Join(binDir, "endstate")
	if err := os.WriteFile(shimPath, []byte("stale"), 0644); err != nil {
		t.Fatalf("setup stale file: %v", err)
	}

	if _, ierr := RunBootstrap(BootstrapFlags{}); ierr != nil {
		t.Fatalf("RunBootstrap returned error: %v", ierr)
	}

	info, err := os.Lstat(shimPath)
	if err != nil {
		t.Fatalf("shim missing: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Error("expected the stale regular file to be replaced by a symlink")
	}
}
