// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package bundle

import (
	"path"
	"path/filepath"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

// ResolvedPathMatchesSecrets checks resolved paths without host expansion.
// Platform case policy is explicit; basename patterns and directory prefixes
// retain the capture boundary's established meaning.
func ResolvedPathMatchesSecrets(filePath string, patterns []string, caseSensitive bool) (bool, error) {
	target := filepath.ToSlash(filePath)
	if !caseSensitive {
		target = strings.ToLower(target)
	}
	for _, pattern := range patterns {
		pattern = strings.TrimSuffix(filepath.ToSlash(pattern), "/")
		if !caseSensitive {
			pattern = strings.ToLower(pattern)
		}
		if pattern == "" {
			continue
		}
		matched, err := doublestar.Match(pattern, target)
		if err != nil {
			return false, err
		}
		if matched {
			return true, nil
		}
		if !strings.Contains(pattern, "/") {
			matched, err = doublestar.Match(pattern, path.Base(target))
			if err != nil {
				return false, err
			}
			if matched {
				return true, nil
			}
		}
		// A declared secret directory excludes its entire subtree, including globbed directories.
		for parent := path.Dir(target); parent != "." && parent != "/"; parent = path.Dir(parent) {
			matched, err = doublestar.Match(pattern, parent)
			if err != nil {
				return false, err
			}
			if matched {
				return true, nil
			}
		}
	}
	return false, nil
}
