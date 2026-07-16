// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package configrestore

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

type filesystemEntry struct {
	Path        string
	Kind        StateKind
	Mode        os.FileMode
	Size        int64
	ContentHash string
}

type filesystemState struct {
	Kind    StateKind
	Mode    os.FileMode
	Digest  string
	Entries map[string]filesystemEntry
}

func scanFilesystemState(ctx context.Context, root string) (filesystemState, error) {
	if err := checkSnapshotContext(ctx); err != nil {
		return filesystemState{}, err
	}
	if err := rejectExistingTargetLinks(root); err != nil {
		return filesystemState{}, err
	}
	info, err := os.Lstat(root)
	if os.IsNotExist(err) {
		return absentFilesystemState(), nil
	}
	if err != nil {
		return filesystemState{}, err
	}
	entries := make(map[string]filesystemEntry)
	if err := scanFilesystemNode(ctx, root, root, ".", info, entries); err != nil {
		return filesystemState{}, err
	}
	rootEntry := entries["."]
	state := filesystemState{Kind: rootEntry.Kind, Mode: rootEntry.Mode, Entries: entries}
	state.Digest = digestFilesystemState(state)
	return state, nil
}

func scanFilesystemNode(
	ctx context.Context,
	root string,
	hostPath string,
	relative string,
	info os.FileInfo,
	entries map[string]filesystemEntry,
) error {
	if err := checkSnapshotContext(ctx); err != nil {
		return err
	}
	if isLinkOrReparse(info) {
		return fmt.Errorf("filesystem state contains link or reparse point %q", hostPath)
	}
	portable := filepath.ToSlash(relative)
	switch {
	case info.Mode().IsRegular():
		size, contentHash, err := hashStableRegularFile(ctx, hostPath, info)
		if err != nil {
			return err
		}
		entries[portable] = filesystemEntry{
			Path: portable, Kind: StateFile, Mode: info.Mode().Perm(), Size: size, ContentHash: contentHash,
		}
		return nil
	case info.IsDir():
		entries[portable] = filesystemEntry{Path: portable, Kind: StateDirectory, Mode: info.Mode().Perm()}
		directoryEntries, err := os.ReadDir(hostPath)
		if err != nil {
			return err
		}
		for _, directoryEntry := range directoryEntries {
			if err := checkSnapshotContext(ctx); err != nil {
				return err
			}
			childPath := filepath.Join(hostPath, directoryEntry.Name())
			childInfo, err := os.Lstat(childPath)
			if err != nil {
				return err
			}
			childRelative, err := filepath.Rel(root, childPath)
			if err != nil {
				return err
			}
			if err := scanFilesystemNode(ctx, root, childPath, childRelative, childInfo, entries); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("filesystem state contains unsupported special file %q", hostPath)
	}
}

func hashStableRegularFile(ctx context.Context, path string, expected os.FileInfo) (int64, string, error) {
	if isLinkOrReparse(expected) || !expected.Mode().IsRegular() {
		return 0, "", fmt.Errorf("file %q is not a safe regular file", path)
	}
	file, err := os.Open(path)
	if err != nil {
		return 0, "", err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil {
		return 0, "", err
	}
	if !opened.Mode().IsRegular() || !os.SameFile(expected, opened) {
		return 0, "", fmt.Errorf("file %q changed before hashing", path)
	}
	hasher := sha256.New()
	buffer := make([]byte, 64*1024)
	var size int64
	for {
		if err := checkSnapshotContext(ctx); err != nil {
			return 0, "", err
		}
		count, readErr := file.Read(buffer)
		if count > 0 {
			if _, err := hasher.Write(buffer[:count]); err != nil {
				return 0, "", err
			}
			size += int64(count)
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return 0, "", readErr
		}
	}
	current, err := os.Lstat(path)
	if err != nil {
		return 0, "", err
	}
	if isLinkOrReparse(current) || !current.Mode().IsRegular() || !os.SameFile(opened, current) ||
		current.Size() != size || opened.Size() != size || current.Mode().Perm() != opened.Mode().Perm() {
		return 0, "", fmt.Errorf("file %q changed while hashing", path)
	}
	return size, hex.EncodeToString(hasher.Sum(nil)), nil
}

func absentFilesystemState() filesystemState {
	return filesystemState{Kind: StateAbsent, Digest: digestAbsent("filesystem"), Entries: map[string]filesystemEntry{}}
}

func digestFilesystemState(state filesystemState) string {
	hasher := sha256.New()
	writeDigestString(hasher, "endstate-filesystem-state-v1")
	writeDigestString(hasher, string(state.Kind))
	paths := make([]string, 0, len(state.Entries))
	for path := range state.Entries {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	writeDigestUint64(hasher, uint64(len(paths)))
	for _, path := range paths {
		entry := state.Entries[path]
		writeDigestString(hasher, entry.Path)
		writeDigestString(hasher, string(entry.Kind))
		writeDigestUint64(hasher, uint64(entry.Mode.Perm()))
		writeDigestUint64(hasher, uint64(entry.Size))
		writeDigestString(hasher, entry.ContentHash)
	}
	return hex.EncodeToString(hasher.Sum(nil))
}

func digestAbsent(domain string) string {
	hasher := sha256.New()
	writeDigestString(hasher, "endstate-absent-state-v1")
	writeDigestString(hasher, domain)
	return hex.EncodeToString(hasher.Sum(nil))
}

func writeDigestString(hasher hash.Hash, value string) {
	writeDigestUint64(hasher, uint64(len(value)))
	_, _ = hasher.Write([]byte(value))
}

func writeDigestUint64(hasher hash.Hash, value uint64) {
	var buffer [8]byte
	binary.BigEndian.PutUint64(buffer[:], value)
	_, _ = hasher.Write(buffer[:])
}

func copyFilesystemSnapshot(ctx context.Context, source, destination string, expected filesystemState) error {
	if expected.Kind == StateAbsent {
		return nil
	}
	if err := checkSnapshotContext(ctx); err != nil {
		return err
	}
	if err := rejectExistingTargetLinks(source); err != nil {
		return err
	}
	switch expected.Kind {
	case StateFile:
		return copySnapshotFile(ctx, source, destination, expected.Mode)
	case StateDirectory:
		return copySnapshotDirectory(ctx, source, destination, ".", expected)
	default:
		return fmt.Errorf("unsupported filesystem snapshot kind %q", expected.Kind)
	}
}

func copySnapshotDirectory(ctx context.Context, source, destination, relative string, expected filesystemState) error {
	if err := checkSnapshotContext(ctx); err != nil {
		return err
	}
	entry, exists := expected.Entries[filepath.ToSlash(relative)]
	if !exists || entry.Kind != StateDirectory {
		return fmt.Errorf("directory %q changed before snapshot copy", source)
	}
	info, err := os.Lstat(source)
	if err != nil || !info.IsDir() || isLinkOrReparse(info) || info.Mode().Perm() != entry.Mode.Perm() {
		return fmt.Errorf("directory %q changed before snapshot copy", source)
	}
	if err := os.Mkdir(destination, 0o700); err != nil {
		return err
	}
	directoryEntries, err := os.ReadDir(source)
	if err != nil {
		return err
	}
	for _, directoryEntry := range directoryEntries {
		childSource := filepath.Join(source, directoryEntry.Name())
		childDestination := filepath.Join(destination, directoryEntry.Name())
		childRelative := directoryEntry.Name()
		if relative != "." {
			childRelative = filepath.Join(relative, directoryEntry.Name())
		}
		expectedEntry, exists := expected.Entries[filepath.ToSlash(childRelative)]
		if !exists {
			return fmt.Errorf("filesystem tree %q changed before snapshot copy", source)
		}
		childInfo, err := os.Lstat(childSource)
		if err != nil || isLinkOrReparse(childInfo) {
			return fmt.Errorf("filesystem tree %q changed before snapshot copy", childSource)
		}
		switch expectedEntry.Kind {
		case StateDirectory:
			if err := copySnapshotDirectory(ctx, childSource, childDestination, childRelative, expected); err != nil {
				return err
			}
		case StateFile:
			if !childInfo.Mode().IsRegular() || childInfo.Mode().Perm() != expectedEntry.Mode.Perm() {
				return fmt.Errorf("file %q changed before snapshot copy", childSource)
			}
			if err := copySnapshotFile(ctx, childSource, childDestination, expectedEntry.Mode); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unsupported snapshot entry kind %q", expectedEntry.Kind)
		}
	}
	return os.Chmod(destination, entry.Mode.Perm())
}

func copySnapshotFile(ctx context.Context, source, destination string, mode os.FileMode) (resultErr error) {
	if err := checkSnapshotContext(ctx); err != nil {
		return err
	}
	info, err := os.Lstat(source)
	if err != nil || !info.Mode().IsRegular() || isLinkOrReparse(info) {
		return fmt.Errorf("snapshot source %q is not a safe regular file", source)
	}
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	openedInfo, err := input.Stat()
	if err != nil || !os.SameFile(info, openedInfo) {
		return fmt.Errorf("snapshot source %q changed before copy", source)
	}
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer func() {
		_ = output.Close()
		if resultErr != nil {
			_ = os.Remove(destination)
		}
	}()
	buffer := make([]byte, 64*1024)
	for {
		if err := checkSnapshotContext(ctx); err != nil {
			return err
		}
		count, readErr := input.Read(buffer)
		if count > 0 {
			if _, err := output.Write(buffer[:count]); err != nil {
				return err
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return readErr
		}
	}
	current, err := os.Lstat(source)
	if err != nil || isLinkOrReparse(current) || !os.SameFile(openedInfo, current) ||
		current.Mode().Perm() != openedInfo.Mode().Perm() || current.Size() != openedInfo.Size() {
		return fmt.Errorf("snapshot source %q changed during copy", source)
	}
	if err := output.Sync(); err != nil {
		return err
	}
	if err := output.Close(); err != nil {
		return err
	}
	return os.Chmod(destination, mode.Perm())
}

func desiredCopyState(prior, source filesystemState, exclude []string) (filesystemState, error) {
	switch source.Kind {
	case StateFile:
		if prior.Kind == StateDirectory {
			return filesystemState{}, fmt.Errorf("copy file cannot replace an existing directory")
		}
		entry := source.Entries["."]
		if prior.Kind == StateFile {
			entry.Mode = prior.Mode.Perm()
		}
		state := filesystemState{Kind: StateFile, Mode: entry.Mode, Entries: map[string]filesystemEntry{".": entry}}
		state.Digest = digestFilesystemState(state)
		return state, nil
	case StateDirectory:
		if prior.Kind == StateFile {
			return filesystemState{}, fmt.Errorf("copy directory cannot overlay an existing file")
		}
		entries := cloneFilesystemEntries(prior.Entries)
		if prior.Kind == StateAbsent {
			entries = map[string]filesystemEntry{
				".": {Path: ".", Kind: StateDirectory, Mode: 0o755},
			}
		}
		paths := sortedFilesystemPaths(source.Entries)
		excludedDirectories := make([]string, 0)
		for _, path := range paths {
			if path == "." || hasExcludedAncestor(path, excludedDirectories) {
				continue
			}
			entry := source.Entries[path]
			if copyPathExcluded(path, exclude) {
				if entry.Kind == StateDirectory {
					excludedDirectories = append(excludedDirectories, path)
				}
				continue
			}
			existing, exists := entries[path]
			switch entry.Kind {
			case StateDirectory:
				if exists && existing.Kind != StateDirectory {
					return filesystemState{}, fmt.Errorf("copy directory collides with existing file %q", path)
				}
				if !exists {
					entries[path] = entry
				}
			case StateFile:
				if exists && existing.Kind != StateFile {
					return filesystemState{}, fmt.Errorf("copy file collides with existing directory %q", path)
				}
				if exists {
					entry.Mode = existing.Mode.Perm()
				}
				entries[path] = entry
			default:
				return filesystemState{}, fmt.Errorf("unsupported source entry kind %q", entry.Kind)
			}
		}
		root := entries["."]
		state := filesystemState{Kind: StateDirectory, Mode: root.Mode, Entries: entries}
		state.Digest = digestFilesystemState(state)
		return state, nil
	default:
		return filesystemState{}, fmt.Errorf("copy source is absent or unsupported")
	}
}

func desiredWriteState(prior filesystemState, content []byte) filesystemState {
	mode := os.FileMode(0o644)
	if prior.Kind == StateFile {
		mode = prior.Mode.Perm()
	}
	sum := sha256.Sum256(content)
	entry := filesystemEntry{
		Path: ".", Kind: StateFile, Mode: mode, Size: int64(len(content)), ContentHash: hex.EncodeToString(sum[:]),
	}
	state := filesystemState{Kind: StateFile, Mode: entry.Mode, Entries: map[string]filesystemEntry{".": entry}}
	state.Digest = digestFilesystemState(state)
	return state
}

func cloneFilesystemEntries(entries map[string]filesystemEntry) map[string]filesystemEntry {
	copy := make(map[string]filesystemEntry, len(entries))
	for path, entry := range entries {
		copy[path] = entry
	}
	return copy
}

func sortedFilesystemPaths(entries map[string]filesystemEntry) []string {
	paths := make([]string, 0, len(entries))
	for path := range entries {
		paths = append(paths, path)
	}
	sort.Slice(paths, func(left, right int) bool {
		leftDepth := strings.Count(paths[left], "/")
		rightDepth := strings.Count(paths[right], "/")
		if leftDepth != rightDepth {
			return leftDepth < rightDepth
		}
		return paths[left] < paths[right]
	})
	return paths
}

func hasExcludedAncestor(path string, excluded []string) bool {
	for _, ancestor := range excluded {
		if strings.HasPrefix(path, ancestor+"/") {
			return true
		}
	}
	return false
}

// copyPathExcluded deliberately matches the existing copy driver's declared
// exclusion semantics so desired digests describe the action commit will run.
func copyPathExcluded(relative string, patterns []string) bool {
	normalizedPath := filepath.ToSlash(relative)
	for _, pattern := range patterns {
		search := filepath.ToSlash(pattern)
		search = strings.TrimPrefix(search, "**/")
		search = strings.TrimPrefix(search, "**")
		search = strings.TrimSuffix(search, "/**")
		search = strings.TrimSuffix(search, "**")
		if search != "" && strings.Contains(normalizedPath, search) {
			return true
		}
	}
	return false
}

func statesEqual(left, right filesystemState) bool {
	return left.Kind == right.Kind && left.Mode.Perm() == right.Mode.Perm() && left.Digest == right.Digest
}

func stateRecord(state filesystemState, backupPath string) StateRecord {
	paths := make([]string, 0, len(state.Entries))
	for path := range state.Entries {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	entries := make([]StateEntry, 0, len(paths))
	for _, path := range paths {
		entry := state.Entries[path]
		entries = append(entries, StateEntry{
			Path: entry.Path, Kind: entry.Kind, Mode: entry.Mode.Perm(), Size: entry.Size, ContentHash: entry.ContentHash,
		})
	}
	return StateRecord{
		Kind: state.Kind, Digest: state.Digest, Mode: state.Mode.Perm(), BackupPath: backupPath, entries: entries,
	}
}

func filesystemStateFromRecord(record StateRecord) (filesystemState, error) {
	switch record.Kind {
	case StateAbsent:
		if len(record.entries) != 0 || record.Mode.Perm() != 0 {
			return filesystemState{}, fmt.Errorf("absent filesystem state has mode or entries")
		}
		return absentFilesystemState(), nil
	case StateFile, StateDirectory:
	default:
		return filesystemState{}, fmt.Errorf("state %q is not a filesystem state", record.Kind)
	}
	entries := make(map[string]filesystemEntry, len(record.entries))
	previous := ""
	for index, entry := range record.entries {
		if entry.Path == "" || containsControl(entry.Path) || path.IsAbs(entry.Path) || path.Clean(entry.Path) != entry.Path || entry.Path == ".." ||
			(entry.Path != "." && strings.HasPrefix(entry.Path, "../")) || strings.Contains(entry.Path, `\`) {
			return filesystemState{}, fmt.Errorf("filesystem manifest entry[%d] has unsafe path %q", index, entry.Path)
		}
		if index > 0 && entry.Path <= previous {
			return filesystemState{}, fmt.Errorf("filesystem manifest entries are not strictly ordered")
		}
		previous = entry.Path
		if entry.Mode != entry.Mode.Perm() {
			return filesystemState{}, fmt.Errorf("filesystem manifest entry %q has non-permission mode bits", entry.Path)
		}
		switch entry.Kind {
		case StateFile:
			if entry.Size < 0 || !isLowerHexDigest(entry.ContentHash) {
				return filesystemState{}, fmt.Errorf("filesystem file entry %q has invalid size or content hash", entry.Path)
			}
		case StateDirectory:
			if entry.Size != 0 || entry.ContentHash != "" {
				return filesystemState{}, fmt.Errorf("filesystem directory entry %q has file content", entry.Path)
			}
		default:
			return filesystemState{}, fmt.Errorf("filesystem manifest entry %q has kind %q", entry.Path, entry.Kind)
		}
		entries[entry.Path] = filesystemEntry{
			Path: entry.Path, Kind: entry.Kind, Mode: entry.Mode.Perm(), Size: entry.Size, ContentHash: entry.ContentHash,
		}
	}
	root, exists := entries["."]
	if !exists || root.Kind != record.Kind || root.Mode.Perm() != record.Mode.Perm() {
		return filesystemState{}, fmt.Errorf("filesystem manifest root does not match state summary")
	}
	if record.Kind == StateFile && len(entries) != 1 {
		return filesystemState{}, fmt.Errorf("file manifest contains descendants")
	}
	for entryPath := range entries {
		if entryPath == "." {
			continue
		}
		parentPath := path.Dir(entryPath)
		parent, exists := entries[parentPath]
		if !exists || parent.Kind != StateDirectory {
			return filesystemState{}, fmt.Errorf("filesystem manifest entry %q has no directory parent", entryPath)
		}
	}
	state := filesystemState{Kind: record.Kind, Mode: record.Mode.Perm(), Entries: entries}
	state.Digest = digestFilesystemState(state)
	return state, nil
}

func checkSnapshotContext(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("context is nil")
	}
	return ctx.Err()
}
