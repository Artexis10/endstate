// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package restore

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
	var results []RevertResult

	// Process entries in reverse order.
	for i := len(journal.Entries) - 1; i >= 0; i-- {
		entry := journal.Entries[i]

		// Only revert entries that were actually restored.
		if entry.Action != "restored" {
			results = append(results, RevertResult{
				Target: entry.TargetPath,
				Action: "skipped",
			})
			continue
		}

		// CASE 1: Backup exists — restore it.
		if entry.BackupCreated && entry.BackupPath != "" {
			if _, err := os.Stat(entry.BackupPath); err == nil {
				// Registry-import: re-import the backup .reg file instead of
				// copying a file to the target path.
				if entry.RestoreType == "registry-import" {
					cmd := exec.Command("reg", "import", entry.BackupPath)
					if err := cmd.Run(); err != nil {
						return nil, fmt.Errorf("cannot revert registry import from %s: %w", entry.BackupPath, err)
					}
					results = append(results, RevertResult{
						Target:     entry.TargetPath,
						Action:     "reverted",
						BackupUsed: entry.BackupPath,
					})
					continue
				}

				// File-based restore: copy backup back to target path.
				targetDir := filepath.Dir(entry.TargetPath)
				if err := os.MkdirAll(targetDir, 0755); err != nil {
					return nil, fmt.Errorf("cannot create directory for revert target %s: %w", entry.TargetPath, err)
				}

				// Check if backup is a directory.
				info, err := os.Stat(entry.BackupPath)
				if err != nil {
					return nil, fmt.Errorf("cannot stat backup %s: %w", entry.BackupPath, err)
				}

				if info.IsDir() {
					// Remove existing target directory.
					if err := os.RemoveAll(entry.TargetPath); err != nil {
						return nil, fmt.Errorf("cannot remove target for revert %s: %w", entry.TargetPath, err)
					}
					if err := copyDirRecursive(entry.BackupPath, entry.TargetPath, nil); err != nil {
						return nil, fmt.Errorf("cannot restore backup dir %s: %w", entry.BackupPath, err)
					}
				} else {
					if err := copyFile(entry.BackupPath, entry.TargetPath); err != nil {
						return nil, fmt.Errorf("cannot restore backup file %s: %w", entry.BackupPath, err)
					}
				}

				results = append(results, RevertResult{
					Target:     entry.TargetPath,
					Action:     "reverted",
					BackupUsed: entry.BackupPath,
				})
				continue
			}
		}

		// CASE 2: Target was created by restore (didn't exist before) — delete it.
		if !entry.TargetExistedBefore {
			// Registry-import: delete the registry key that was created.
			if entry.RestoreType == "registry-import" {
				cmd := exec.Command("reg", "delete", entry.TargetPath, "/f")
				if err := cmd.Run(); err != nil {
					// Key may already be gone — treat as success.
					results = append(results, RevertResult{
						Target: entry.TargetPath,
						Action: "skipped",
					})
					continue
				}
				results = append(results, RevertResult{
					Target: entry.TargetPath,
					Action: "deleted",
				})
				continue
			}

			if _, err := os.Stat(entry.TargetPath); err == nil {
				if err := os.RemoveAll(entry.TargetPath); err != nil {
					return nil, fmt.Errorf("cannot delete reverted target %s: %w", entry.TargetPath, err)
				}
				results = append(results, RevertResult{
					Target: entry.TargetPath,
					Action: "deleted",
				})
				continue
			}
			// Target already gone — skip.
			results = append(results, RevertResult{
				Target: entry.TargetPath,
				Action: "skipped",
			})
			continue
		}

		// CASE 3: No backup and target existed before — nothing to revert.
		results = append(results, RevertResult{
			Target: entry.TargetPath,
			Action: "skipped",
		})
	}

	return results, nil
}
