// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build darwin

package safepath

import (
	"os"
	"path/filepath"
	"strings"
)

// CanonicalizePlatformRootAlias resolves only the fixed macOS /var system
// alias. Arbitrary links remain visible to the caller's component walk.
func CanonicalizePlatformRootAlias(value string) (string, error) {
	clean := filepath.Clean(value)
	alias := string(filepath.Separator) + "var"
	if clean != alias && !strings.HasPrefix(clean, alias+string(filepath.Separator)) {
		return clean, nil
	}

	info, err := os.Lstat(alias)
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return clean, nil
	}
	resolved, err := filepath.EvalSymlinks(alias)
	if err != nil {
		return "", err
	}
	expected := filepath.Join(string(filepath.Separator), "private", "var")
	if filepath.Clean(resolved) != expected {
		return "", pathError(CodeLinkUnsupported, alias, ErrLinkUnsupported)
	}

	suffix := strings.TrimPrefix(clean, alias)
	if suffix == "" {
		return expected, nil
	}
	return filepath.Join(expected, strings.TrimPrefix(suffix, string(filepath.Separator))), nil
}
