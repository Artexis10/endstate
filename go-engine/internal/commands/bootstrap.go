// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"io"
	"os"
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
//
// On Windows, ShimPath is the path of the generated endstate.cmd shim. On
// Unix (linux/darwin), ShimPath is the path of the symlink that points at the
// installed binary, and AddedToPath is always false (the engine never edits
// shell rc files).
type BootstrapData struct {
	InstallPath string `json:"installPath"`
	ShimPath    string `json:"shimPath"`
	AddedToPath bool   `json:"addedToPath"`
}

// copyFile copies src to dst, overwriting dst if it exists. The destination is
// created with the default os.Create mode (0666 before umask); callers that
// need the destination to be executable adjust its mode after the copy.
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
