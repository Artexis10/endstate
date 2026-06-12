// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunBootstrap_WindowsInstallsBinaryAndShim(t *testing.T) {
	localAppData := t.TempDir()
	t.Setenv("LOCALAPPDATA", localAppData)

	installDir := filepath.Join(localAppData, "Endstate", "bin")
	// Put installDir on PATH so the hermetic test never invokes `setx`.
	t.Setenv("PATH", installDir)

	data, ierr := RunBootstrap(BootstrapFlags{})
	if ierr != nil {
		t.Fatalf("RunBootstrap returned error: %v", ierr)
	}
	bd, ok := data.(*BootstrapData)
	if !ok {
		t.Fatalf("expected *BootstrapData, got %T", data)
	}

	if bd.InstallPath != installDir {
		t.Errorf("InstallPath = %q, want %q", bd.InstallPath, installDir)
	}

	wantShim := filepath.Join(installDir, "endstate.cmd")
	if bd.ShimPath != wantShim {
		t.Errorf("ShimPath = %q, want %q", bd.ShimPath, wantShim)
	}

	// AddedToPath is false because installDir is already on PATH.
	if bd.AddedToPath {
		t.Error("AddedToPath = true, want false (already on PATH)")
	}

	// Binary installed.
	destBinary := filepath.Join(installDir, "lib", "endstate.exe")
	if _, err := os.Stat(destBinary); err != nil {
		t.Fatalf("installed binary missing: %v", err)
	}

	// Shim written and references lib\endstate.exe.
	shimBytes, err := os.ReadFile(wantShim)
	if err != nil {
		t.Fatalf("shim missing: %v", err)
	}
	if !strings.Contains(string(shimBytes), `lib\endstate.exe`) {
		t.Errorf("shim does not reference lib\\endstate.exe: %q", string(shimBytes))
	}
}

func TestRunBootstrap_WindowsIsIdempotent(t *testing.T) {
	localAppData := t.TempDir()
	t.Setenv("LOCALAPPDATA", localAppData)
	installDir := filepath.Join(localAppData, "Endstate", "bin")
	t.Setenv("PATH", installDir)

	first, ierr := RunBootstrap(BootstrapFlags{})
	if ierr != nil {
		t.Fatalf("first RunBootstrap returned error: %v", ierr)
	}
	second, ierr := RunBootstrap(BootstrapFlags{})
	if ierr != nil {
		t.Fatalf("second RunBootstrap returned error: %v", ierr)
	}
	fb := first.(*BootstrapData)
	sb := second.(*BootstrapData)
	if fb.InstallPath != sb.InstallPath || fb.ShimPath != sb.ShimPath {
		t.Errorf("idempotent re-run changed paths: %+v vs %+v", fb, sb)
	}
}
