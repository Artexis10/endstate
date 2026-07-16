// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package configrestore

import (
	"fmt"
	"os"
	"path/filepath"
)

// rejectExistingTargetLinks verifies every existing component from the volume
// root through target. Missing suffixes are safe to materialize; links and
// reparse points are not, because lexical containment would no longer prove
// the concrete host target identity.
func rejectExistingTargetLinks(target string) error {
	clean := filepath.Clean(target)
	chain := make([]string, 0, 8)
	for current := clean; ; current = filepath.Dir(current) {
		chain = append(chain, current)
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
	}
	for index := len(chain) - 1; index >= 0; index-- {
		current := chain[index]
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if isLinkOrReparse(info) {
			return fmt.Errorf("target path component %q is a link or reparse point", current)
		}
		if index > 0 && !info.IsDir() {
			return fmt.Errorf("target parent %q is not a directory", current)
		}
	}
	return nil
}
