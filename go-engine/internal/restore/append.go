// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package restore

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// RestoreAppend implements the append restore strategy. It reads source content
// and appends it to the target file. If the target does not exist, it is
// created. If backup is requested and the target exists, a backup is created
// first.
func RestoreAppend(entry RestoreAction, source, target string, opts RestoreOptions) (*RestoreResult, error) {
	result := &RestoreResult{
		Source: source,
		Target: target,
	}

	// Check source exists.
	if _, err := os.Stat(source); os.IsNotExist(err) {
		result.Status = "failed"
		result.Error = fmt.Sprintf("source not found: %s", source)
		return result, nil
	}

	// Read source content.
	sourceData, err := os.ReadFile(source)
	if err != nil {
		result.Status = "failed"
		result.Error = fmt.Sprintf("cannot read source: %v", err)
		return result, nil
	}
	sourceContent := string(sourceData)

	// Read target content if exists.
	var targetContent string
	targetExists := false
	if _, statErr := os.Stat(target); statErr == nil {
		targetExists = true
		targetData, readErr := os.ReadFile(target)
		if readErr != nil {
			result.Status = "failed"
			result.Error = fmt.Sprintf("cannot read target: %v", readErr)
			return result, nil
		}
		targetContent = string(targetData)
	}

	// Compute the appended content.
	var mergedContent string
	if targetExists {
		// Ensure the existing content ends with a newline before appending.
		base := targetContent
		if len(base) > 0 && !strings.HasSuffix(base, "\n") {
			base += "\n"
		}
		mergedContent = base + sourceContent
	} else {
		mergedContent = sourceContent
	}

	// Ensure trailing newline.
	if len(mergedContent) > 0 && !strings.HasSuffix(mergedContent, "\n") {
		mergedContent += "\n"
	}

	// Check if content would be unchanged (up-to-date).
	if targetExists && targetContent == mergedContent {
		result.Status = "skipped_up_to_date"
		return result, nil
	}

	// Dry-run.
	if opts.DryRun {
		result.Status = "restored"
		return result, nil
	}

	// Backup target if exists and backup requested.
	if entry.Backup && targetExists {
		backupDir := opts.BackupDir
		if backupDir == "" {
			backupDir = filepath.Join("state", "backups", opts.RunID)
		}
		backupPath, backupErr := CreateBackup(target, backupDir)
		if backupErr != nil {
			result.Status = "failed"
			result.Error = fmt.Sprintf("backup failed: %v", backupErr)
			return result, nil
		}
		result.BackupPath = backupPath
		result.BackupCreated = true
	}

	// Ensure target directory exists.
	targetDir := filepath.Dir(target)
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		result.Status = "failed"
		result.Error = fmt.Sprintf("cannot create target directory: %v", err)
		return result, nil
	}

	// Write atomically.
	tmpPath := target + ".tmp"
	if err := os.WriteFile(tmpPath, []byte(mergedContent), 0644); err != nil {
		result.Status = "failed"
		result.Error = fmt.Sprintf("write failed: %v", err)
		return result, nil
	}
	if err := os.Rename(tmpPath, target); err != nil {
		os.Remove(tmpPath)
		result.Status = "failed"
		result.Error = fmt.Sprintf("atomic rename failed: %v", err)
		return result, nil
	}

	result.Status = "restored"
	return result, nil
}
