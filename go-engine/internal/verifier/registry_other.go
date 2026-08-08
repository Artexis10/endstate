// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package verifier

import (
	"fmt"

	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/validationmode"
)

// CheckRegistryKeyExists is a stub for non-Windows platforms. Registry
// verification is only supported on Windows.
func CheckRegistryKeyExists(entry manifest.VerifyEntry) VerifyResult {
	return VerifyResult{
		Type:    entry.Type,
		Path:    entry.Path,
		Pass:    false,
		Message: "Registry checks only supported on Windows",
	}
}

func checkRegistryKeyExistsWithValidation(entry manifest.VerifyEntry, context *validationmode.Context) (VerifyResult, error) {
	if _, err := context.MapHKCU(entry.Path); err != nil {
		return VerifyResult{Type: entry.Type, Path: entry.Path, ValueName: entry.ValueName, Message: fmt.Sprintf("Registry check rejected: %s", entry.Path)}, err
	}
	return CheckRegistryKeyExists(entry), nil
}

func checkRegistryValueEqualsWithValidation(entry manifest.VerifyEntry, context *validationmode.Context) (VerifyResult, error) {
	if _, err := context.MapHKCU(entry.Path); err != nil {
		return VerifyResult{Type: entry.Type, Path: entry.Path, ValueName: entry.ValueName, Message: fmt.Sprintf("Registry check rejected: %s", entry.Path)}, err
	}
	return CheckRegistryValueEquals(entry), nil
}

// CheckRegistryValueEquals is a stub for non-Windows platforms. Registry
// value-data verification is only supported on Windows.
func CheckRegistryValueEquals(entry manifest.VerifyEntry) VerifyResult {
	return VerifyResult{
		Type:      entry.Type,
		Path:      entry.Path,
		ValueName: entry.ValueName,
		Pass:      false,
		Message:   "Registry checks only supported on Windows",
	}
}
