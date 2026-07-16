// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package bundle

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
)

var (
	errUnsafeBundlePath = errors.New("unsafe bundle path")
	errBundleLink       = errors.New("links and reparse points are unsupported")
)

func normalizePortablePath(value string) (string, error) {
	if value == "" || strings.TrimSpace(value) != value || strings.ContainsRune(value, '\x00') {
		return "", fmt.Errorf("%w: %q is empty or contains invalid whitespace", errUnsafeBundlePath, value)
	}
	normalized := strings.ReplaceAll(value, `\`, "/")
	if strings.HasPrefix(normalized, "/") || strings.HasPrefix(normalized, "//") || hasPortableVolume(normalized) {
		return "", fmt.Errorf("%w: %q is absolute or volume-qualified", errUnsafeBundlePath, value)
	}
	if strings.HasPrefix(normalized, "~") || strings.ContainsAny(normalized, "$%:") {
		return "", fmt.Errorf("%w: %q contains host expansion or volume syntax", errUnsafeBundlePath, value)
	}
	for _, component := range strings.Split(normalized, "/") {
		if component == ".." {
			return "", fmt.Errorf("%w: %q contains parent traversal", errUnsafeBundlePath, value)
		}
		for _, character := range component {
			if character < 0x20 {
				return "", fmt.Errorf("%w: %q contains a control character", errUnsafeBundlePath, value)
			}
		}
	}
	normalized = path.Clean(normalized)
	if normalized == "." || normalized == "" || normalized == ".." || strings.HasPrefix(normalized, "../") {
		return "", fmt.Errorf("%w: %q is not a contained relative path", errUnsafeBundlePath, value)
	}
	return normalized, nil
}

func hasPortableVolume(value string) bool {
	return len(value) >= 2 && ((value[0] >= 'A' && value[0] <= 'Z') || (value[0] >= 'a' && value[0] <= 'z')) && value[1] == ':'
}

func containedHostPath(root, portableRelative string) (string, error) {
	normalized, err := normalizePortablePath(portableRelative)
	if err != nil {
		return "", err
	}
	rootAbs, err := filepath.Abs(filepath.Clean(root))
	if err != nil {
		return "", fmt.Errorf("canonicalize root %q: %w", root, err)
	}
	candidate := filepath.Join(rootAbs, filepath.FromSlash(normalized))
	relative, err := filepath.Rel(rootAbs, candidate)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return "", fmt.Errorf("%w: %q escapes root %q", errUnsafeBundlePath, portableRelative, root)
	}
	return candidate, nil
}

func ensureNoLinksInExistingPath(candidate string) error {
	abs, err := filepath.Abs(filepath.Clean(candidate))
	if err != nil {
		return err
	}
	var chain []string
	for current := abs; ; current = filepath.Dir(current) {
		chain = append(chain, current)
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
	}
	for left, right := 0, len(chain)-1; left < right; left, right = left+1, right-1 {
		chain[left], chain[right] = chain[right], chain[left]
	}
	for _, current := range chain {
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if isLinkOrReparse(info) {
			return fmt.Errorf("%w: %q", errBundleLink, current)
		}
	}
	return nil
}
