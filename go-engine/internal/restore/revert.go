// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package restore

import (
	"os"
)

// RevertResult records the outcome of a single revert action.
type RevertResult struct {
	Target     string `json:"target"`
	Action     string `json:"action"` // reverted, deleted, skipped
	BackupUsed string `json:"backupUsed,omitempty"`
}

// RunRevert processes journal entries in REVERSE order to undo a restore run.
// For each entry:
//   - If a backup exists -> restore the backup to target.
//   - Else if the target didn't exist before and was restored -> delete the
//     created target.
//   - Else -> skip (nothing to revert).
func RunRevert(journal *Journal, backupDir string) ([]RevertResult, error) {
	workRoot, err := os.MkdirTemp("", "endstate-legacy-revert-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(workRoot)
	return RunRevertDurable(journal, backupDir, workRoot)
}
