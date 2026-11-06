// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package nix

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

// StoreRoot is the directory that holds every built Nix output.
const StoreRoot = "/nix/store"

// MaxHomeEntries bounds only Endstate's own read of a built home-files tree. A
// wrong refusal withholds a whole activation, so it sits about five times above a
// generated tree of 100,000 leaves in 1,000 directories. Measured outside the
// repo, the read, the claim mapping and the check against 20 other owners took
// under one second together for that tree, so the bound costs a few seconds.
const MaxHomeEntries = 500_000

// readChunk caps what one directory read loads, so the bound also stops a single
// flat directory of millions of entries before it is loaded whole.
const readChunk = 4096

// HomeFilesDir lists the leaves that one real directory of a built home-files
// tree holds. Dir is a slash path below the home directory; "" is home itself.
type HomeFilesDir struct {
	Dir   string
	Names []string
}

// ErrHomeFilesTooLarge reports a tree that exceeds the entry bound. Root names
// the deepest home subtree that held more than half of the entries counted when
// the bound was crossed.
type ErrHomeFilesTooLarge struct {
	Root  string
	Limit int
}

func (e *ErrHomeFilesTooLarge) Error() string {
	return fmt.Sprintf("the Home Manager files tree holds more than %d entries; most of those read were below %s", e.Limit, e.Root)
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
	if !storePathIn(filepath.ToSlash(target), filepath.ToSlash(storeRoot)) {
		return nil, fmt.Errorf("%w: home-files does not link a store path", errHomeFilesFormat)
	}

	var result []HomeFilesDir
	counts := map[string]int{}
	entries := 0
	pending := []string{""}
	for len(pending) > 0 {
		dir := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		handle, err := os.Open(filepath.Join(target, filepath.FromSlash(dir)))
		if err != nil {
			return nil, fmt.Errorf("read Home Manager files tree: %w", err)
		}
		var names []string
		for done := false; !done; {
			children, err := handle.ReadDir(readChunk)
			done = err == io.EOF
			if err != nil && !done {
				_ = handle.Close()
				return nil, fmt.Errorf("read Home Manager files tree: %w", err)
			}
			entries += len(children)
			addEntries(counts, dir, len(children))
			if entries > limit {
				_ = handle.Close()
				return nil, &ErrHomeFilesTooLarge{Root: deepestMajorityRoot(counts, entries), Limit: limit}
			}
			for _, child := range children {
				if child.IsDir() {
					pending = append(pending, path.Join(dir, child.Name()))
					continue
				}
				names = append(names, child.Name())
			}
		}
		_ = handle.Close()
		if len(names) > 0 {
			slices.Sort(names)
			result = append(result, HomeFilesDir{Dir: dir, Names: names})
		}
	}
	return result, nil
}

// addEntries adds a directory's entries to the count of the directory and of each
// of its ancestors, so a count is the number of entries below that directory.
func addEntries(counts map[string]int, dir string, n int) {
	for {
		counts[dir] += n
		if dir == "" {
			return
		}
		dir = dir[:max(strings.LastIndex(dir, "/"), 0)]
	}
}

// deepestMajorityRoot names the deepest directory that holds more than half of the
// counted entries. Those directories nest, so the longest path is the deepest. The
// built tree does not record which declaration created a directory; this names the
// place where most of a runaway tree sits, which is where the user can look first.
func deepestMajorityRoot(counts map[string]int, total int) string {
	root := ""
	for dir, count := range counts {
		if count*2 > total && len(dir) > len(root) {
			root = dir
		}
	}
	if root == "" {
		return "the home directory"
	}
	return "~/" + root
}
