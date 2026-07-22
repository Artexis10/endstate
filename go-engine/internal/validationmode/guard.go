// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationmode

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/safepath"
)

type ChangeKind string

const (
	ChangeCreated ChangeKind = "created"
	ChangeDeleted ChangeKind = "deleted"
	ChangeType    ChangeKind = "type"
	ChangeContent ChangeKind = "content"
)

type Change struct {
	Path string
	Kind ChangeKind
}

type snapshotEntry struct {
	kind string
	size int64
	hash [sha256.Size]byte
}

// WriteGuard records protected filesystem trees without mutating them.
type WriteGuard struct {
	allowed   string
	protected []string
	before    map[string]snapshotEntry
}

// NewWriteGuard validates disjoint boundaries and snapshots protected paths.
func NewWriteGuard(allowed string, protected []string) (*WriteGuard, error) {
	allowed, err := canonicalGuardPath(allowed, true)
	if err != nil {
		return nil, err
	}
	if len(protected) == 0 {
		return nil, fmt.Errorf("%w: protected set is empty", ErrUnsafeGuardPath)
	}
	canonicalProtected := make([]string, 0, len(protected))
	seen := make(map[string]struct{}, len(protected))
	for _, path := range protected {
		canonical, err := canonicalGuardPath(path, false)
		if err != nil {
			return nil, err
		}
		if pathsOverlap(allowed, canonical) {
			return nil, fmt.Errorf("%w: %q and %q", ErrGuardOverlap, allowed, canonical)
		}
		key := pathComparisonKey(canonical)
		if _, duplicate := seen[key]; duplicate {
			return nil, fmt.Errorf("%w: duplicate protected path", ErrGuardOverlap)
		}
		seen[key] = struct{}{}
		canonicalProtected = append(canonicalProtected, canonical)
	}
	sort.Slice(canonicalProtected, func(left, right int) bool {
		return pathComparisonKey(canonicalProtected[left]) < pathComparisonKey(canonicalProtected[right])
	})
	before := make(map[string]snapshotEntry)
	for _, path := range canonicalProtected {
		entries, err := snapshot(path)
		if err != nil {
			return nil, err
		}
		for entryPath, entry := range entries {
			before[entryPath] = entry
		}
	}
	return &WriteGuard{allowed: allowed, protected: canonicalProtected, before: before}, nil
}

// Check returns deterministic protected-path changes since construction.
func (guard *WriteGuard) Check() ([]Change, error) {
	after := make(map[string]snapshotEntry)
	for _, path := range guard.protected {
		entries, err := snapshot(path)
		if err != nil {
			return nil, err
		}
		for entryPath, entry := range entries {
			after[entryPath] = entry
		}
	}
	keys := make(map[string]struct{}, len(guard.before)+len(after))
	for path := range guard.before {
		keys[path] = struct{}{}
	}
	for path := range after {
		keys[path] = struct{}{}
	}
	ordered := make([]string, 0, len(keys))
	for path := range keys {
		ordered = append(ordered, path)
	}
	sort.Slice(ordered, func(left, right int) bool {
		return pathComparisonKey(ordered[left]) < pathComparisonKey(ordered[right])
	})
	changes := make([]Change, 0)
	for _, path := range ordered {
		before, existedBefore := guard.before[path]
		after, existsAfter := after[path]
		switch {
		case !existedBefore:
			changes = append(changes, Change{Path: path, Kind: ChangeCreated})
		case !existsAfter:
			changes = append(changes, Change{Path: path, Kind: ChangeDeleted})
		case before.kind != after.kind:
			changes = append(changes, Change{Path: path, Kind: ChangeType})
		case before.kind == "file" && (before.size != after.size || before.hash != after.hash):
			changes = append(changes, Change{Path: path, Kind: ChangeContent})
		}
	}
	return changes, nil
}

func canonicalGuardPath(value string, requireDirectory bool) (string, error) {
	if value == "" || !filepath.IsAbs(value) || value != filepath.Clean(value) {
		return "", fmt.Errorf("%w: path must be canonical absolute", ErrUnsafeGuardPath)
	}
	absolute, err := filepath.Abs(value)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrUnsafeGuardPath, err)
	}
	absolute, err = safepath.CanonicalizePlatformRootAlias(filepath.Clean(absolute))
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrUnsafeGuardPath, err)
	}
	if err := validateGuardPathChain(absolute); err != nil {
		return "", err
	}
	if requireDirectory {
		info, err := os.Lstat(absolute)
		if err != nil || !info.IsDir() {
			return "", fmt.Errorf("%w: allowed root is not a directory", ErrUnsafeGuardPath)
		}
	}
	return absolute, nil
}

func validateGuardPathChain(path string) error {
	current := path
	for {
		info, err := os.Lstat(current)
		if err == nil {
			if info.IsDir() {
				if err := safepath.ValidateRoot(current); err != nil {
					return fmt.Errorf("%w: %v", ErrUnsafeGuardPath, err)
				}
			} else {
				parent := filepath.Dir(current)
				if err := safepath.ValidateRoot(parent); err != nil {
					return fmt.Errorf("%w: %v", ErrUnsafeGuardPath, err)
				}
				if _, err := safepath.Resolve(parent, filepath.Base(current)); err != nil {
					return fmt.Errorf("%w: %v", ErrUnsafeGuardPath, err)
				}
			}
			if current != path && info.IsDir() {
				relative, err := filepath.Rel(current, path)
				if err != nil {
					return fmt.Errorf("%w: %v", ErrUnsafeGuardPath, err)
				}
				if _, err := safepath.Resolve(current, filepath.ToSlash(relative)); err != nil {
					return fmt.Errorf("%w: %v", ErrUnsafeGuardPath, err)
				}
			}
			return nil
		}
		if !os.IsNotExist(err) {
			return fmt.Errorf("%w: %v", ErrUnsafeGuardPath, err)
		}
		parent := filepath.Dir(current)
		if parent == current {
			return fmt.Errorf("%w: no existing path ancestor", ErrUnsafeGuardPath)
		}
		current = parent
	}
}

func snapshot(root string) (map[string]snapshotEntry, error) {
	if err := validateGuardPathChain(root); err != nil {
		return nil, err
	}
	if _, err := os.Lstat(root); os.IsNotExist(err) {
		return map[string]snapshotEntry{}, nil
	} else if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnsafeGuardPath, err)
	}
	result := make(map[string]snapshotEntry)
	err := filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if safepath.IsLinkOrReparse(info) || (!info.IsDir() && !info.Mode().IsRegular()) {
			return fmt.Errorf("%w: special or linked path %q", ErrUnsafeGuardPath, path)
		}
		entry := snapshotEntry{size: info.Size()}
		if info.IsDir() {
			entry.kind = "directory"
		} else {
			entry.kind = "file"
			data, _, err := safepath.ReadRegularFile(path)
			if err != nil {
				return err
			}
			entry.size = int64(len(data))
			entry.hash = sha256.Sum256(data)
		}
		result[filepath.Clean(path)] = entry
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnsafeGuardPath, err)
	}
	return result, nil
}

func pathsOverlap(left, right string) bool {
	return isAncestorOrSame(left, right) || isAncestorOrSame(right, left)
}

func isAncestorOrSame(ancestor, candidate string) bool {
	relative, err := filepath.Rel(ancestor, candidate)
	if err != nil {
		return false
	}
	if runtime.GOOS == "windows" {
		relative = strings.ToLower(relative)
	}
	return relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative))
}

func pathComparisonKey(path string) string {
	if runtime.GOOS == "windows" {
		return strings.ToLower(filepath.Clean(path))
	}
	return filepath.Clean(path)
}
