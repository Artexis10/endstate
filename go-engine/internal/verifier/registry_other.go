// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package verifier

import (
	"github.com/Artexis10/endstate/go-engine/internal/manifest"
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
