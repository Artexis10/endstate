// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package verifier

import (
	"fmt"
	"os"

	"github.com/Artexis10/endstate/go-engine/internal/manifest"
)

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
