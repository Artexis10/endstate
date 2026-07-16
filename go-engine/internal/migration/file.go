// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package migration

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/safepath"
)

type sourceTreeEntry struct {
	relative string
	mode     fs.FileMode
}

func applyFileCopy(root string, operation modules.MigrationOperationDef) error {
	source, err := resolveOperationPath(root, operation.Source)
	if err != nil {
		return err
	}
	target, err := resolveOperationPath(root, operation.Target)
	if err != nil {
		return err
	}
	if targetIsSourceOrDescendant(source, target) {
		return localError(CodeSourceDescendant, fmt.Errorf("target is the source or its descendant"))
	}
	entries, info, err := inspectSourceTree(root, source)
	if err != nil {
		return err
	}
	if err := safepath.MkdirParent(root, operation.Target, 0o755); err != nil {
		return mapPathError(err)
	}
	if _, err := resolveOperationPath(root, operation.Target); err != nil {
		return err
	}

	targetInfo, statErr := os.Lstat(target)
	if statErr != nil && !os.IsNotExist(statErr) {
		return localError(CodeIO, statErr)
	}
	if info.IsDir() {
		if statErr == nil {
			return localError(CodeDestinationExists, fmt.Errorf("directory destination already exists"))
		}
		return copyDirectoryTree(source, target, entries)
	}
	if statErr == nil && !targetInfo.Mode().IsRegular() {
		return localError(CodeUnsupportedFileType, fmt.Errorf("destination is not a regular file"))
	}
	if err := safepath.AtomicCopyFile(source, target, info.Mode()); err != nil {
		return mapPathError(err)
	}
	return nil
}

func applyFileMove(root string, operation modules.MigrationOperationDef) error {
	source, err := resolveOperationPath(root, operation.Source)
	if err != nil {
		return err
	}
	target, err := resolveOperationPath(root, operation.Target)
	if err != nil {
		return err
	}
	if targetIsSourceOrDescendant(source, target) {
		return localError(CodeSourceDescendant, fmt.Errorf("target is the source or its descendant"))
	}
	_, sourceInfo, err := inspectSourceTree(root, source)
	if err != nil {
		return err
	}
	if err := safepath.MkdirParent(root, operation.Target, 0o755); err != nil {
		return mapPathError(err)
	}
	if _, err := resolveOperationPath(root, operation.Target); err != nil {
		return err
	}
	targetInfo, statErr := os.Lstat(target)
	if statErr != nil && !os.IsNotExist(statErr) {
		return localError(CodeIO, statErr)
	}
	if sourceInfo.IsDir() && statErr == nil {
		return localError(CodeDestinationExists, fmt.Errorf("directory destination already exists"))
	}
	if statErr == nil && !targetInfo.Mode().IsRegular() {
		return localError(CodeUnsupportedFileType, fmt.Errorf("destination is not a regular file"))
	}
	if err := safepath.AtomicRename(source, target); err != nil {
		return localError(CodeIO, err)
	}
	return nil
}

func applyFileDelete(root string, operation modules.MigrationOperationDef) error {
	source, err := resolveOperationPath(root, operation.Path)
	if err != nil {
		return err
	}
	if _, _, err := inspectSourceTree(root, source); err != nil {
		return err
	}
	tombstone, err := os.MkdirTemp(filepath.Dir(source), ".endstate-delete-*")
	if err != nil {
		return localError(CodeIO, err)
	}
	if err := os.Remove(tombstone); err != nil {
		return localError(CodeIO, err)
	}
	if err := safepath.AtomicRename(source, tombstone); err != nil {
		return localError(CodeIO, err)
	}
	if err := os.RemoveAll(tombstone); err != nil {
		return localError(CodeIO, err)
	}
	return nil
}

func resolveOperationPath(root, portable string) (string, error) {
	resolved, err := safepath.Resolve(root, portable)
	if err != nil {
		return "", mapPathError(err)
	}
	return resolved, nil
}

func inspectSourceTree(root, source string) ([]sourceTreeEntry, os.FileInfo, error) {
	info, err := os.Lstat(source)
	if os.IsNotExist(err) {
		return nil, nil, localError(CodePathNotFound, err)
	}
	if err != nil {
		return nil, nil, localError(CodeIO, err)
	}
	if !info.IsDir() && !info.Mode().IsRegular() {
		return nil, nil, localError(CodeUnsupportedFileType, fmt.Errorf("source is not a regular file or directory"))
	}
	if info.Mode().IsRegular() {
		return []sourceTreeEntry{{mode: info.Mode()}}, info, nil
	}

	entries := make([]sourceTreeEntry, 0)
	err = filepath.WalkDir(source, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relativeToRoot, err := filepath.Rel(root, current)
		if err != nil {
			return err
		}
		if _, err := safepath.Resolve(root, filepath.ToSlash(relativeToRoot)); err != nil {
			return err
		}
		entryInfo, err := entry.Info()
		if err != nil {
			return err
		}
		if !entryInfo.IsDir() && !entryInfo.Mode().IsRegular() {
			return localError(CodeUnsupportedFileType, fmt.Errorf("source tree contains a special file"))
		}
		relative, err := filepath.Rel(source, current)
		if err != nil {
			return err
		}
		entries = append(entries, sourceTreeEntry{relative: relative, mode: entryInfo.Mode()})
		return nil
	})
	if err != nil {
		return nil, nil, mapPathError(err)
	}
	sort.Slice(entries, func(left, right int) bool { return entries[left].relative < entries[right].relative })
	return entries, info, nil
}

func copyDirectoryTree(source, target string, entries []sourceTreeEntry) (resultErr error) {
	temporary, err := os.MkdirTemp(filepath.Dir(target), ".endstate-copy-*")
	if err != nil {
		return localError(CodeIO, err)
	}
	defer func() {
		if resultErr != nil {
			_ = os.RemoveAll(temporary)
		}
	}()
	if len(entries) > 0 {
		if err := os.Chmod(temporary, entries[0].mode.Perm()); err != nil {
			return localError(CodeIO, err)
		}
	}
	for _, entry := range entries {
		if entry.relative == "." {
			continue
		}
		sourcePath := filepath.Join(source, entry.relative)
		targetPath := filepath.Join(temporary, entry.relative)
		if entry.mode.IsDir() {
			if err := os.Mkdir(targetPath, entry.mode.Perm()); err != nil {
				return localError(CodeIO, err)
			}
			continue
		}
		if err := safepath.AtomicCopyFile(sourcePath, targetPath, entry.mode); err != nil {
			return mapPathError(err)
		}
	}
	if err := safepath.AtomicRename(temporary, target); err != nil {
		return localError(CodeIO, err)
	}
	return nil
}

func targetIsSourceOrDescendant(source, target string) bool {
	source = filepath.Clean(source)
	target = filepath.Clean(target)
	if runtime.GOOS == "windows" {
		source = strings.ToLower(source)
		target = strings.ToLower(target)
	}
	relative, err := filepath.Rel(source, target)
	if err != nil {
		return false
	}
	return relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)))
}

func mapPathError(err error) error {
	var local *Error
	if errors.As(err, &local) {
		return local
	}
	switch {
	case errors.Is(err, safepath.ErrLinkUnsupported):
		return localError(CodeLinkUnsupported, err)
	case errors.Is(err, safepath.ErrUnsafeRoot):
		return localError(CodeUnsafeRoot, err)
	case errors.Is(err, safepath.ErrUnsafePath):
		return localError(CodeUnsafePath, err)
	case errors.Is(err, safepath.ErrSourceChanged):
		return localError(CodeSourceChanged, err)
	default:
		return localError(CodeIO, err)
	}
}

func localError(code ErrorCode, err error) error {
	return &Error{Code: code, Index: -1, Err: err}
}
