// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"io"
	"os"
	"path/filepath"
	"strings"
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
	// CatalogInstalled names the catalog trees copied into the install ("modules",
	// "payload"). Empty means the install carries no config-module catalog, so
	// capture from this install will record apps without their settings.
	CatalogInstalled []string `json:"catalogInstalled"`
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

// catalogDirs are the source trees an install needs in order to resolve config
// modules. Without them a PATH-invoked binary resolves no catalog and capture
// degrades to an app list with no settings — the thing that makes Endstate more
// than a package-list exporter.
var catalogDirs = []string{"modules", "payload"}

// installCatalog refreshes the module catalog inside an install directory from
// sourceRoot. Trees absent at the source are skipped, not an error: a bare
// binary downloaded outside a repo or GUI layout has nothing to copy, and
// bootstrap still succeeds so the shim and PATH entry land.
//
// Returns the names of the trees actually installed.
func installCatalog(sourceRoot, installDir string) ([]string, error) {
	if sourceRoot == "" {
		return nil, nil
	}
	var installed []string
	for _, name := range catalogDirs {
		src := filepath.Join(sourceRoot, name)
		info, err := os.Stat(src)
		if err != nil || !info.IsDir() {
			continue
		}
		dst := filepath.Join(installDir, name)
		// Re-bootstrapping from an already-installed binary resolves the install
		// itself as the source. Copying a tree onto itself would be destructive,
		// so treat it as already current.
		if samePath(src, dst) {
			installed = append(installed, name)
			continue
		}
		// Remove first so a refresh drops modules deleted upstream rather than
		// leaving a union of old and new — a stale catalog is worse than none,
		// because it silently captures against outdated module definitions.
		if err := os.RemoveAll(dst); err != nil {
			return installed, err
		}
		if err := copyTree(src, dst); err != nil {
			return installed, err
		}
		installed = append(installed, name)
	}
	return installed, nil
}

// samePath reports whether two paths refer to the same location, comparing
// case-insensitively because Windows paths are.
func samePath(a, b string) bool {
	ca, err := filepath.Abs(a)
	if err != nil {
		return false
	}
	cb, err := filepath.Abs(b)
	if err != nil {
		return false
	}
	return strings.EqualFold(filepath.Clean(ca), filepath.Clean(cb))
}

// copyTree recursively copies the directory at src to dst.
func copyTree(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(src, path)
		if relErr != nil {
			return relErr
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		return copyFile(path, target)
	})
}
