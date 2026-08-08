// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package verifier

import (
	"fmt"
	"os"

	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/validationmode"
)

var fileStatNative = os.Stat

// CheckFileExists expands environment variables in the entry's Path and checks
// whether the path exists on the filesystem. Both files and directories are
// considered a pass. Returns a fail result with "File not found" when the path
// does not exist.
func CheckFileExists(entry manifest.VerifyEntry) VerifyResult {
	expanded := os.ExpandEnv(entry.Path)

	_, err := os.Stat(expanded)
	if err == nil {
		return VerifyResult{
			Type:    entry.Type,
			Path:    expanded,
			Pass:    true,
			Message: fmt.Sprintf("Path exists: %s", expanded),
		}
	}

	return VerifyResult{
		Type:    entry.Type,
		Path:    expanded,
		Pass:    false,
		Message: fmt.Sprintf("File not found: %s", expanded),
	}
}

func checkFileExistsWithValidation(entry manifest.VerifyEntry, context *validationmode.Context) (VerifyResult, error) {
	base := VerifyResult{Type: entry.Type, Path: entry.Path}
	resolved, err := context.ResolveHostPath(entry.Path, validationmode.HostPathPolicy{})
	if err != nil {
		base.Message = fmt.Sprintf("File check rejected: %s", entry.Path)
		return base, fmt.Errorf("resolve verifier path: %w", err)
	}
	// ResolveHostPath validates the parent chain. Revalidate the exact leaf at
	// the immediate native boundary so a post-resolution link swap fails closed.
	if err := context.ValidateSandboxPath(resolved); err != nil {
		base.Message = fmt.Sprintf("File check rejected: %s", entry.Path)
		return base, fmt.Errorf("authorize verifier path: %w", err)
	}
	if _, err := fileStatNative(resolved); err == nil {
		base.Pass = true
		base.Message = fmt.Sprintf("Path exists: %s", entry.Path)
		return base, nil
	}
	base.Message = fmt.Sprintf("File not found: %s", entry.Path)
	return base, nil
}
