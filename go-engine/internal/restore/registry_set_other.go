// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package restore

import "fmt"

// RestoreRegistrySet is the non-Windows entry point for the value-level
// registry-set restore strategy. It still runs the cross-platform validation
// (HKCU-only, value name, value type) so a non-HKCU target is rejected with the
// same error on any platform, then reports that registry ops require Windows.
func RestoreRegistrySet(entry RestoreAction, opts RestoreOptions) (*RestoreResult, error) {
	result := &RestoreResult{
		Target:      registrySetTarget(entry),
		RestoreType: "registry-set",
	}
	if err := validateRegistrySet(entry); err != nil {
		result.Status = "failed"
		result.Error = err.Error()
		return result, nil
	}
	result.Status = "failed"
	result.Error = "registry-set is only supported on Windows"
	return result, nil
}

// revertRegistrySet is a non-Windows stub; registry revert is Windows-only.
func revertRegistrySet(backup *registrySetBackup) error {
	return fmt.Errorf("registry-set revert is only supported on Windows")
}
