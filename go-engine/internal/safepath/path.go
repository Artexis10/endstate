// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

// Package safepath resolves strict portable paths beneath an engine-owned root.
package safepath

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
)

type Code string

const (
	CodeUnsafePath      Code = "unsafe_path"
	CodeUnsafeRoot      Code = "unsafe_root"
	CodeLinkUnsupported Code = "link_unsupported"
	CodeSourceChanged   Code = "source_changed"
)

var (
	ErrUnsafePath      = errors.New("unsafe portable path")
	ErrUnsafeRoot      = errors.New("unsafe staging root")
	ErrLinkUnsupported = errors.New("links and reparse points are unsupported")
	ErrSourceChanged   = errors.New("source changed during copy")
)

type Error struct {
	Code Code
	Path string
	Err  error
}

func (e *Error) Error() string {
	return fmt.Sprintf("safepath %s for %q: %v", e.Code, e.Path, e.Err)
}

func (e *Error) Unwrap() error { return e.Err }

func ValidateRoot(root string) error {
	_, err := validateRoot(root)
	return err
}

func Resolve(root, portableRelative string) (string, error) {
	rootPath, err := validateRoot(root)
	if err != nil {
		return "", err
	}
	normalized, err := normalizePortable(portableRelative)
	if err != nil {
		return "", err
	}
	candidate := filepath.Join(rootPath, filepath.FromSlash(normalized))
	if !contained(rootPath, candidate) {
		return "", pathError(CodeUnsafePath, portableRelative, ErrUnsafePath)
	}
	if err := rejectExistingLinks(rootPath, normalized); err != nil {
		return "", err
	}
	return candidate, nil
}

func MkdirParent(root, portableRelative string, mode os.FileMode) error {
	rootPath, err := validateRoot(root)
	if err != nil {
		return err
	}
	normalized, err := normalizePortable(portableRelative)
	if err != nil {
		return err
	}
	components := strings.Split(normalized, "/")
	current := rootPath
	for _, component := range components[:len(components)-1] {
		current = filepath.Join(current, filepath.FromSlash(component))
		info, statErr := os.Lstat(current)
		if os.IsNotExist(statErr) {
			if mkdirErr := os.Mkdir(current, mode); mkdirErr != nil {
				return mkdirErr
			}
			info, statErr = os.Lstat(current)
		}
		if statErr != nil {
			return statErr
		}
		if isLinkOrReparse(info) {
			return pathError(CodeLinkUnsupported, current, ErrLinkUnsupported)
		}
		if !info.IsDir() {
			return pathError(CodeUnsafePath, current, ErrUnsafePath)
		}
	}
	return nil
}

func validateRoot(root string) (string, error) {
	if root == "" || !filepath.IsAbs(root) {
		return "", pathError(CodeUnsafeRoot, root, ErrUnsafeRoot)
	}
	clean := filepath.Clean(root)
	info, err := rejectRootChainLinks(clean)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", pathError(CodeUnsafeRoot, root, ErrUnsafeRoot)
	}
	return clean, nil
}

func rejectRootChainLinks(root string) (os.FileInfo, error) {
	chain := make([]string, 0)
	for current := root; ; current = filepath.Dir(current) {
		chain = append(chain, current)
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
	}
	var rootInfo os.FileInfo
	for index := len(chain) - 1; index >= 0; index-- {
		current := chain[index]
		info, err := os.Lstat(current)
		if err != nil {
			return nil, pathError(CodeUnsafeRoot, root, errors.Join(ErrUnsafeRoot, err))
		}
		if isLinkOrReparse(info) {
			return nil, pathError(CodeLinkUnsupported, current, ErrLinkUnsupported)
		}
		if current == root {
			rootInfo = info
		}
	}
	return rootInfo, nil
}

func normalizePortable(value string) (string, error) {
	if value == "" || value != strings.TrimSpace(value) || strings.ContainsRune(value, '\x00') {
		return "", pathError(CodeUnsafePath, value, ErrUnsafePath)
	}
	normalized := strings.ReplaceAll(value, `\`, "/")
	if strings.HasPrefix(normalized, "/") || hasDrivePrefix(normalized) || strings.Contains(normalized, ":") ||
		strings.ContainsAny(normalized, "$%~") {
		return "", pathError(CodeUnsafePath, value, ErrUnsafePath)
	}
	components := strings.Split(normalized, "/")
	for _, component := range components {
		if component == "" || component == "." || component == ".." || component != strings.TrimSpace(component) {
			return "", pathError(CodeUnsafePath, value, ErrUnsafePath)
		}
		for _, character := range component {
			if character < 0x20 {
				return "", pathError(CodeUnsafePath, value, ErrUnsafePath)
			}
		}
	}
	normalized = path.Clean(normalized)
	if normalized == "." || normalized == ".." || strings.HasPrefix(normalized, "../") {
		return "", pathError(CodeUnsafePath, value, ErrUnsafePath)
	}
	return normalized, nil
}

func hasDrivePrefix(value string) bool {
	return len(value) >= 2 && ((value[0] >= 'A' && value[0] <= 'Z') ||
		(value[0] >= 'a' && value[0] <= 'z')) && value[1] == ':'
}

func contained(root, candidate string) bool {
	relative, err := filepath.Rel(root, candidate)
	return err == nil && relative != ".." &&
		!strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}

func rejectExistingLinks(root, normalized string) error {
	current := root
	for _, component := range strings.Split(normalized, "/") {
		current = filepath.Join(current, filepath.FromSlash(component))
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			break
		}
		if err != nil {
			return err
		}
		if isLinkOrReparse(info) {
			return pathError(CodeLinkUnsupported, current, ErrLinkUnsupported)
		}
	}
	return nil
}

func pathError(code Code, value string, err error) error {
	return &Error{Code: code, Path: value, Err: err}
}
