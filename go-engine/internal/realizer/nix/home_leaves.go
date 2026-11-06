// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package nix

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// StoreRoot is the directory that holds every built Nix output.
const StoreRoot = "/nix/store"

// MaxHomeEntries bounds only Endstate's own read of a built home-files tree. A
// wrong refusal withholds a whole activation, so it sits well above any realistic
// recursive tree. Measured on a generated fixture of 100,000 leaves in 1,000
// directories: about 0.1 s to read the tree, 0.2 s to map the leaves to claims and
// 0.3 s to check them against 20 other owners, so the bound costs about 3 s.
const MaxHomeEntries = 500_000

// HomeFilesDir lists the leaves that one real directory of a built home-files
// tree holds. Dir is a slash path below the home directory; "" is home itself.
type HomeFilesDir struct {
	Dir   string
	Names []string
}

// ErrHomeFilesTooLarge reports a tree that exceeds the entry bound. Root names
// the home subtree that held the most entries when the bound was crossed.
type ErrHomeFilesTooLarge struct {
	Root  string
	Limit int
}

func (e *ErrHomeFilesTooLarge) Error() string {
	return fmt.Sprintf("the Home Manager files tree holds more than %d entries, most of them below %s", e.Limit, e.Root)
}

var errHomeFilesFormat = errors.New("unsupported Home Manager activation package layout")

// ReadHomeFiles lists the exact leaves Home Manager links for a built activation
// package. `home-files` and `gen-version` are Home Manager's own package-format
// tokens: the package links its home-files tree once and records format version
// 1. The read follows that one link and requires a store path. It descends real
// directories only and counts every other entry, links included, as one leaf, as
// Home Manager's own `find -type f -or -type l` does. It never follows an in-tree
// link, so a link back to an ancestor cannot loop and a link leaf never claims
// its referent.
func ReadHomeFiles(output, storeRoot string, limit int) ([]HomeFilesDir, error) {
	if !filepath.IsAbs(output) {
		return nil, fmt.Errorf("%w: activation output is not absolute", errHomeFilesFormat)
	}
	version, err := os.ReadFile(filepath.Join(output, "gen-version"))
	if err != nil {
		return nil, fmt.Errorf("read Home Manager package format: %w", err)
	}
	if strings.TrimSpace(string(version)) != "1" {
		return nil, fmt.Errorf("%w: package format %q", errHomeFilesFormat, strings.TrimSpace(string(version)))
	}
	link := filepath.Join(output, "home-files")
	target, err := os.Readlink(link)
	if err != nil {
		return nil, fmt.Errorf("read Home Manager files link: %w", err)
	}
	slashTarget := filepath.ToSlash(target)
	if path.Clean(slashTarget) != slashTarget || path.Dir(slashTarget) != filepath.ToSlash(storeRoot) {
		return nil, fmt.Errorf("%w: home-files does not link a store path", errHomeFilesFormat)
	}

	var result []HomeFilesDir
	counts := map[string]int{}
	entries := 0
	pending := []string{""}
	for len(pending) > 0 {
		dir := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		children, err := os.ReadDir(filepath.Join(target, filepath.FromSlash(dir)))
		if err != nil {
			return nil, fmt.Errorf("read Home Manager files tree: %w", err)
		}
		entries += len(children)
		counts[declarationRoot(dir)] += len(children)
		if entries > limit {
			return nil, &ErrHomeFilesTooLarge{Root: largestRoot(counts), Limit: limit}
		}
		names := make([]string, 0, len(children))
		for _, child := range children {
			if child.IsDir() {
				pending = append(pending, path.Join(dir, child.Name()))
				continue
			}
			names = append(names, child.Name())
		}
		if len(names) > 0 {
			result = append(result, HomeFilesDir{Dir: dir, Names: names})
		}
	}
	return result, nil
}

// declarationRoot groups a directory by its first two path components. The built
// tree does not record which declaration created a directory, so this names a
// place the user can find in their configuration.
func declarationRoot(dir string) string {
	parts := strings.SplitN(dir, "/", 3)
	if len(parts) > 2 {
		parts = parts[:2]
	}
	return strings.Join(parts, "/")
}

func largestRoot(counts map[string]int) string {
	root, most := "", -1
	for candidate, count := range counts {
		if count > most || (count == most && candidate < root) {
			root, most = candidate, count
		}
	}
	if root == "" {
		return "the home directory"
	}
	return "~/" + root
}
