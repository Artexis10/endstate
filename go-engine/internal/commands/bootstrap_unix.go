// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package commands

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/Artexis10/endstate/go-engine/internal/envelope"
)

// RunBootstrap installs the running binary to the user-local Endstate
// directory and creates a symlink on the user PATH (linux/darwin).
//
// Steps:
//  1. Determine install dir: ${XDG_DATA_HOME:-$HOME/.local/share}/endstate/bin
//  2. Create install dir and lib/ subdirectory.
//  3. Copy running binary to lib/endstate and chmod it 0755 (executable).
//  4. Create/re-point a symlink $HOME/.local/bin/endstate -> lib/endstate.
//  5. Never edit the user PATH or any shell rc file; print a one-line hint if
//     the symlink directory is not already on PATH.
func RunBootstrap(flags BootstrapFlags) (interface{}, *envelope.Error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, envelope.NewError(
			envelope.ErrInternalError,
			fmt.Sprintf("Failed to determine home directory: %s", err.Error()),
		)
	}

	dataHome := os.Getenv("XDG_DATA_HOME")
	if dataHome == "" {
		dataHome = filepath.Join(home, ".local", "share")
	}
	installDir := filepath.Join(dataHome, "endstate", "bin")
	libDir := filepath.Join(installDir, "lib")

	// Create directories.
	if err := os.MkdirAll(libDir, 0755); err != nil {
		return nil, envelope.NewError(
			envelope.ErrInternalError,
			fmt.Sprintf("Failed to create install directory: %s", err.Error()),
		)
	}

	// Get the path of the currently running binary.
	exePath, err := os.Executable()
	if err != nil {
		return nil, envelope.NewError(
			envelope.ErrInternalError,
			fmt.Sprintf("Failed to determine executable path: %s", err.Error()),
		)
	}

	// Resolve symlinks to get the real path.
	exePath, err = filepath.EvalSymlinks(exePath)
	if err != nil {
		return nil, envelope.NewError(
			envelope.ErrInternalError,
			fmt.Sprintf("Failed to resolve executable path: %s", err.Error()),
		)
	}

	// Copy binary to lib/endstate. If the running binary already resolves to
	// the install target (a re-bootstrap of the installed copy), skip the copy
	// rather than truncating the binary we are executing.
	destBinary := filepath.Join(libDir, "endstate")
	resolvedDest, derr := filepath.EvalSymlinks(destBinary)
	selfCopy := derr == nil && resolvedDest == exePath
	if !selfCopy {
		if err := copyFile(exePath, destBinary); err != nil {
			return nil, envelope.NewError(
				envelope.ErrInternalError,
				fmt.Sprintf("Failed to copy binary: %s", err.Error()),
			)
		}
	}

	// Ensure the installed binary is executable (copyFile creates it 0666).
	if err := os.Chmod(destBinary, 0755); err != nil {
		return nil, envelope.NewError(
			envelope.ErrInternalError,
			fmt.Sprintf("Failed to mark binary executable: %s", err.Error()),
		)
	}

	// Create/re-point the symlink at $HOME/.local/bin/endstate.
	binDir := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		return nil, envelope.NewError(
			envelope.ErrInternalError,
			fmt.Sprintf("Failed to create bin directory: %s", err.Error()),
		)
	}
	shimPath := filepath.Join(binDir, "endstate")

	// Remove any existing symlink/file at the target so the link is re-pointed
	// idempotently rather than nesting or failing on exists.
	if _, lerr := os.Lstat(shimPath); lerr == nil {
		if err := os.Remove(shimPath); err != nil {
			return nil, envelope.NewError(
				envelope.ErrInternalError,
				fmt.Sprintf("Failed to remove existing symlink: %s", err.Error()),
			)
		}
	}
	if err := os.Symlink(destBinary, shimPath); err != nil {
		return nil, envelope.NewError(
			envelope.ErrInternalError,
			fmt.Sprintf("Failed to create symlink: %s", err.Error()),
		)
	}

	// Never edit PATH or shell rc files on Unix. If the symlink directory is
	// not already on PATH, surface a one-line hint to stderr so the human
	// message helps without changing the JSON payload shape.
	if !isInUserPath(binDir) {
		fmt.Fprintf(os.Stderr, "Hint: add %s to your PATH to run `endstate` directly.\n", binDir)
	}

	return &BootstrapData{
		InstallPath: installDir,
		ShimPath:    shimPath,
		AddedToPath: false,
	}, nil
}

// isInUserPath checks whether dir is present in the current PATH environment
// variable.
func isInUserPath(dir string) bool {
	pathEnv := os.Getenv("PATH")
	normalised := filepath.Clean(dir)

	for _, entry := range filepath.SplitList(pathEnv) {
		if filepath.Clean(entry) == normalised {
			return true
		}
	}
	return false
}
