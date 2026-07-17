// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build !darwin

package safepath

import "path/filepath"

// CanonicalizePlatformRootAlias is a no-op on hosts without a supported fixed
// root alias.
func CanonicalizePlatformRootAlias(value string) (string, error) {
	return filepath.Clean(value), nil
}
