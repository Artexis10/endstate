// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package restore

import (
	"fmt"
	"os"
	"path/filepath"
)

// RestoreDeleteGlob implements the delete-glob restore strategy. It deletes
// files matching a glob pattern inside the target directory. Each deleted file
// is backed up first (when BackupDir is set) so that revert can undo the
// deletions. In dry-run mode, matching files are reported but not deleted.
func RestoreDeleteGlob(entry RestoreAction, target string, opts RestoreOptions) ([]RestoreResult, error) {
	pattern := filepath.Join(target, entry.Pattern)
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid glob pattern %q: %w", entry.Pattern, err)
	}

	// Filter to files only (skip directories).
	var files []string
	for _, m := range matches {
		info, statErr := os.Stat(m)
		if statErr != nil || info.IsDir() {
			continue
		}
		files = append(files, m)
	}

	if len(files) == 0 {
		return []RestoreResult{{
			Target: target,
			Status: "skipped_up_to_date",
		}}, nil
	}

	var results []RestoreResult

	for _, f := range files {
		r := RestoreResult{
			Target:              f,
			TargetExistedBefore: true,
		}

		if opts.DryRun {
			r.Status = "restored"
			results = append(results, r)
			continue
		}

		// Back up before deleting so revert can restore the file.
		if opts.BackupDir != "" {
			backupPath, backupErr := CreateBackup(f, opts.BackupDir)
			if backupErr != nil {
				r.Status = "failed"
				r.Error = fmt.Sprintf("backup before delete failed: %v", backupErr)
				results = append(results, r)
				continue
			}
			r.BackupCreated = true
			r.BackupPath = backupPath
		}

		if err := os.Remove(f); err != nil {
			r.Status = "failed"
			r.Error = fmt.Sprintf("delete failed: %v", err)
			results = append(results, r)
			continue
		}

		r.Status = "restored"
		results = append(results, r)
	}

	return results, nil
}
