// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/envelope"
)

// BootstrapFlags holds the parsed CLI flags for the bootstrap command.
type BootstrapFlags struct {
	// RepoRoot optionally specifies the repo root directory. Currently unused
	// by the Go bootstrap implementation but accepted for forward compatibility.
	RepoRoot string
	// Events controls streaming event output. "jsonl" enables it; "" disables.
	Events string
}

// BootstrapData is the data payload returned by the bootstrap command. It
// reports the paths created and whether the user PATH was modified.
type BootstrapData struct {
	InstallPath string `json:"installPath"`
	ShimPath    string `json:"shimPath"`
	AddedToPath bool   `json:"addedToPath"`
}

// shimContent is the endstate.cmd shim that delegates to the Go binary.
const shimContent = `@echo off
set "ENDSTATE_ENTRYPOINT=%~f0"
"%~dp0lib\endstate.exe" %*
`

// RunBootstrap installs the running binary to the user-local Endstate
// directory and creates a .cmd shim on the user PATH.
//
// Steps:
//  1. Determine install dir: %LOCALAPPDATA%\Endstate\bin\
//  2. Create install dir and lib\ subdirectory.
//  3. Copy running binary to lib\endstate.exe.
//  4. Write endstate.cmd shim to install dir.
//  5. Add install dir to user PATH if not already present.
func RunBootstrap(flags BootstrapFlags) (interface{}, *envelope.Error) {
	installDir := os.ExpandEnv("${LOCALAPPDATA}\\Endstate\\bin")
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

	// Copy binary to lib\endstate.exe.
	destBinary := filepath.Join(libDir, "endstate.exe")
	if err := copyFile(exePath, destBinary); err != nil {
		return nil, envelope.NewError(
			envelope.ErrInternalError,
			fmt.Sprintf("Failed to copy binary: %s", err.Error()),
		)
	}

	// Write the .cmd shim.
	shimPath := filepath.Join(installDir, "endstate.cmd")
	if err := os.WriteFile(shimPath, []byte(shimContent), 0755); err != nil {
		return nil, envelope.NewError(
			envelope.ErrInternalError,
			fmt.Sprintf("Failed to write shim: %s", err.Error()),
		)
	}

	// Check if installDir is already in the user PATH and add it if not.
	addedToPath := false
	if !isInUserPath(installDir) {
		if err := addToUserPath(installDir); err != nil {
			// PATH modification failure is not fatal; report it but succeed.
			// The user can add it manually.
			_ = err
		} else {
			addedToPath = true
		}
	}

	return &BootstrapData{
		InstallPath: installDir,
		ShimPath:    shimPath,
		AddedToPath: addedToPath,
	}, nil
}

// copyFile copies src to dst, overwriting dst if it exists.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}

	return out.Close()
}

// isInUserPath checks whether dir is present in the current PATH environment
// variable (case-insensitive on Windows).
func isInUserPath(dir string) bool {
	pathEnv := os.Getenv("PATH")
	normalised := strings.ToLower(filepath.Clean(dir))

	for _, entry := range filepath.SplitList(pathEnv) {
		if strings.ToLower(filepath.Clean(entry)) == normalised {
			return true
		}
	}
	return false
}

// addToUserPath appends dir to the user PATH via setx. This persists the
// change to the registry (HKCU\Environment\Path) and takes effect in new
// shell sessions.
func addToUserPath(dir string) error {
	currentPath := os.Getenv("PATH")
	newPath := currentPath + ";" + dir

	cmd := exec.Command("setx", "PATH", newPath)
	return cmd.Run()
}
