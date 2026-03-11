// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package verifier

import (
	"fmt"
	"os/exec"

	"github.com/Artexis10/endstate/go-engine/internal/manifest"
)

// CheckCommandExists uses exec.LookPath to determine whether the command
// specified in the entry is resolvable on the system PATH. Returns a pass
// result when found and a fail result with "Command not found" otherwise.
func CheckCommandExists(entry manifest.VerifyEntry) VerifyResult {
	path, err := exec.LookPath(entry.Command)
	if err == nil {
		return VerifyResult{
			Type:    entry.Type,
			Command: entry.Command,
			Pass:    true,
			Message: fmt.Sprintf("Command exists: %s", path),
		}
	}

	return VerifyResult{
		Type:    entry.Type,
		Command: entry.Command,
		Pass:    false,
		Message: fmt.Sprintf("Command not found: %s", entry.Command),
	}
}
