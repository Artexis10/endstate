// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package restore

import "fmt"

func queryRegistryImportTarget(string) (bool, error) {
	return false, fmt.Errorf("registry-import is only supported on Windows")
}
