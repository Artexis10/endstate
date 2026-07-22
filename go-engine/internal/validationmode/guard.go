// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationmode

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"

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

type ProtectedPath struct{ Path, Label string }

type snapshotEntry struct {
	kind string
	size int64
	hash [sha256.Size]byte
}

// WriteGuard records protected filesystem trees without mutating them.
type WriteGuard struct {
	mu        sync.Mutex
	allowed   string
	protected []string
	labels    map[string]string
	before    map[string]snapshotEntry
	sealed    bool
	limits    guardLimits
}

type guardLimits struct {
	roots   int
	entries int
	bytes   int64
}

var defaultGuardLimits = guardLimits{roots: 256, entries: 100000, bytes: 512 << 20}

// NewWriteGuard validates disjoint boundaries and snapshots protected paths.
func NewWriteGuard(allowed string, protected []string) (*WriteGuard, error) {
	return newWriteGuardWithLimits(allowed, protected, defaultGuardLimits)
}

func newWriteGuardWithLimits(allowed string, protected []string, limits guardLimits) (*WriteGuard, error) {
	allowed, err := canonicalGuardPath(allowed, true)
	if err != nil {
		return nil, err
	}
	if len(protected) == 0 {
		return nil, fmt.Errorf("%w: protected set is empty", ErrUnsafeGuardPath)
	}
	if limits.roots <= 0 || limits.entries <= 0 || limits.bytes < 0 {
		return nil, fmt.Errorf("%w: invalid limits", ErrGuardBudget)
	}
	guard := &WriteGuard{allowed: allowed, labels: map[string]string{}, before: map[string]snapshotEntry{}, limits: limits}
	items := make([]ProtectedPath, 0, len(protected))
	for _, path := range protected {
		items = append(items, ProtectedPath{Path: path, Label: "protected"})
	}
	if err := guard.Protect(items); err != nil {
		return nil, err
	}
	return guard, nil
}

// Protect registers and snapshots additional paths before mutation begins.
func (guard *WriteGuard) Protect(values []ProtectedPath) error {
	if guard == nil {
		return fmt.Errorf("%w: nil guard", ErrUnsafeGuardPath)
	}
	guard.mu.Lock()
	defer guard.mu.Unlock()
	if guard.sealed {
		return fmt.Errorf("%w: guard is sealed", ErrUnsafeGuardPath)
	}
	type candidate struct{ path, label string }
	all := make([]candidate, 0, len(guard.protected)+len(values))
	for _, path := range guard.protected {
		all = append(all, candidate{path: path, label: guard.labels[pathComparisonKey(path)]})
	}
	for _, value := range values {
		canonical, err := canonicalGuardPath(value.Path, false)
		if err != nil {
			return err
		}
		if pathsOverlap(guard.allowed, canonical) {
			return fmt.Errorf("%w: protected path overlaps validation root", ErrGuardOverlap)
		}
		label := strings.TrimSpace(value.Label)
		if label == "" {
			label = "protected"
		}
		all = append(all, candidate{path: canonical, label: label})
	}
	sort.SliceStable(all, func(i, j int) bool {
		if len(all[i].path) != len(all[j].path) {
			return len(all[i].path) < len(all[j].path)
		}
		return pathComparisonKey(all[i].path) < pathComparisonKey(all[j].path)
	})
	compacted := make([]candidate, 0, len(all))
	seen := map[string]int{}
	for _, item := range all {
		key := pathComparisonKey(item.path)
		if index, ok := seen[key]; ok {
			compacted[index].label = item.label
			continue
		}
		covered := false
		for _, ancestor := range compacted {
			if isAncestorOrSame(ancestor.path, item.path) {
				covered = true
				break
			}
		}
		if covered {
			continue
		}
		seen[key] = len(compacted)
		compacted = append(compacted, item)
	}
	if len(compacted) > guard.limits.roots {
		return fmt.Errorf("%w: protected root limit", ErrGuardBudget)
	}
	before := map[string]snapshotEntry{}
	labels := map[string]string{}
	protected := make([]string, 0, len(compacted))
	budget := snapshotBudget{entries: guard.limits.entries, bytes: guard.limits.bytes}
	for _, item := range compacted {
		entries, err := snapshot(item.path, &budget)
		if err != nil {
			return err
		}
		for entryPath, entry := range entries {
			before[entryPath] = entry
		}
		protected = append(protected, item.path)
		labels[pathComparisonKey(item.path)] = item.label
	}
	sort.Slice(protected, func(i, j int) bool { return pathComparisonKey(protected[i]) < pathComparisonKey(protected[j]) })
	guard.protected, guard.labels, guard.before = protected, labels, before
	return nil
}

func (guard *WriteGuard) ProtectedCount() int {
	if guard == nil {
		return 0
	}
	guard.mu.Lock()
	defer guard.mu.Unlock()
	return len(guard.protected)
}

// Seal closes registration. Protect after Seal is rejected so a late snapshot
// can never erase evidence of a mutation.
func (guard *WriteGuard) Seal() {
	if guard != nil {
		guard.mu.Lock()
		defer guard.mu.Unlock()
		guard.sealed = true
	}
}

// Label returns the non-sensitive label associated with a changed absolute path.
func (guard *WriteGuard) Label(path string) string {
	if guard == nil {
		return "protected"
	}
	guard.mu.Lock()
	defer guard.mu.Unlock()
	return guard.labelFor(path)
}

// Check returns deterministic protected-path changes since construction.
func (guard *WriteGuard) Check() ([]Change, error) {
	if guard == nil {
		return nil, fmt.Errorf("%w: nil guard", ErrUnsafeGuardPath)
	}
	guard.mu.Lock()
	defer guard.mu.Unlock()
	guard.sealed = true
	after := make(map[string]snapshotEntry)
	budget := snapshotBudget{entries: guard.limits.entries, bytes: guard.limits.bytes}
	for _, path := range guard.protected {
		entries, err := snapshot(path, &budget)
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

func (guard *WriteGuard) labelFor(path string) string {
	best, label := "", "protected"
	for _, root := range guard.protected {
		if isAncestorOrSame(root, path) && len(root) > len(best) {
			best = root
			label = guard.labels[pathComparisonKey(root)]
		}
	}
	return label
}

func canonicalGuardPath(value string, requireDirectory bool) (string, error) {
	if value == "" || !filepath.IsAbs(value) || value != filepath.Clean(value) {
		return "", fmt.Errorf("%w: path must be canonical absolute", ErrUnsafeGuardPath)
	}
	absolute, err := filepath.Abs(value)
	if err != nil {
		return "", fmt.Errorf("%w: path cannot be made absolute", ErrUnsafeGuardPath)
	}
	absolute, err = safepath.CanonicalizePlatformRootAlias(filepath.Clean(absolute))
	if err != nil {
		return "", fmt.Errorf("%w: path has an unsafe platform root", ErrUnsafeGuardPath)
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
					return fmt.Errorf("%w: directory chain is linked or unsafe", ErrUnsafeGuardPath)
				}
			} else {
				parent := filepath.Dir(current)
				if err := safepath.ValidateRoot(parent); err != nil {
					return fmt.Errorf("%w: parent directory is linked or unsafe", ErrUnsafeGuardPath)
				}
				if _, err := safepath.Resolve(parent, filepath.Base(current)); err != nil {
					return fmt.Errorf("%w: file is linked or unsafe", ErrUnsafeGuardPath)
				}
			}
			if current != path && info.IsDir() {
				relative, err := filepath.Rel(current, path)
				if err != nil {
					return fmt.Errorf("%w: path cannot be related to its ancestor", ErrUnsafeGuardPath)
				}
				if _, err := safepath.Resolve(current, filepath.ToSlash(relative)); err != nil {
					return fmt.Errorf("%w: path chain is linked or unsafe", ErrUnsafeGuardPath)
				}
			}
			return nil
		}
		if !os.IsNotExist(err) {
			return fmt.Errorf("%w: path chain is unavailable", ErrUnsafeGuardPath)
		}
		parent := filepath.Dir(current)
		if parent == current {
			return fmt.Errorf("%w: no existing path ancestor", ErrUnsafeGuardPath)
		}
		current = parent
	}
}

type snapshotBudget struct {
	entries int
	bytes   int64
}

func snapshot(root string, budget *snapshotBudget) (map[string]snapshotEntry, error) {
	if err := validateGuardPathChain(root); err != nil {
		return nil, err
	}
	if _, err := os.Lstat(root); os.IsNotExist(err) {
		return map[string]snapshotEntry{}, nil
	} else if err != nil {
		return nil, fmt.Errorf("%w: protected path is unavailable", ErrUnsafeGuardPath)
	}
	result := make(map[string]snapshotEntry)
	err := filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return fmt.Errorf("%w: protected tree is unavailable", ErrUnsafeGuardPath)
		}
		if safepath.IsLinkOrReparse(info) || (!info.IsDir() && !info.Mode().IsRegular()) {
			return fmt.Errorf("%w: protected tree contains a special or linked path", ErrUnsafeGuardPath)
		}
		if budget == nil || budget.entries <= 0 {
			return fmt.Errorf("%w: entry limit", ErrGuardBudget)
		}
		budget.entries--
		entry := snapshotEntry{size: info.Size()}
		if info.IsDir() {
			entry.kind = "directory"
		} else {
			entry.kind = "file"
			if info.Size() < 0 || info.Size() > budget.bytes {
				return fmt.Errorf("%w: byte limit", ErrGuardBudget)
			}
			hash, size, err := safepath.HashRegularFile(path, budget.bytes)
			if err != nil {
				if errors.Is(err, safepath.ErrByteLimit) {
					return fmt.Errorf("%w", ErrGuardBudget)
				}
				return fmt.Errorf("%w: protected file changed or is unsafe", ErrUnsafeGuardPath)
			}
			entry.size = size
			entry.hash = hash
			budget.bytes -= size
		}
		result[filepath.Clean(path)] = entry
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrUnsafeGuardPath, err)
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
