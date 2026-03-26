// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package restore

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// RestoreCopy implements the copy restore strategy. It handles both file and
// directory copy, with exclude glob matching, up-to-date detection, backup,
// and locked file handling.
func RestoreCopy(entry RestoreAction, source, target string, opts RestoreOptions) (*RestoreResult, error) {
	result := &RestoreResult{
		Source: source,
		Target: target,
	}

	// Check source exists.
	srcInfo, err := os.Stat(source)
	if err != nil {
		if os.IsNotExist(err) {
			result.Status = "failed"
			result.Error = fmt.Sprintf("source not found: %s", source)
			return result, nil
		}
		return nil, err
	}

	if srcInfo.IsDir() {
		return restoreCopyDir(entry, source, target, opts)
	}
	return restoreCopyFile(entry, source, target, opts)
}

// restoreCopyFile copies a single file from source to target.
func restoreCopyFile(entry RestoreAction, source, target string, opts RestoreOptions) (*RestoreResult, error) {
	result := &RestoreResult{
		Source: source,
		Target: target,
	}

	// Up-to-date detection via hash comparison.
	upToDate, err := IsUpToDate(source, target)
	if err == nil && upToDate {
		result.Status = "skipped_up_to_date"
		return result, nil
	}

	// Dry-run: report what would happen.
	if opts.DryRun {
		result.Status = "restored"
		return result, nil
	}

	// Backup target if it exists and backup is requested.
	if entry.Backup {
		if _, statErr := os.Stat(target); statErr == nil {
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
	}

	// Ensure target directory exists.
	targetDir := filepath.Dir(target)
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		result.Status = "failed"
		result.Error = fmt.Sprintf("cannot create target directory: %v", err)
		return result, nil
	}

	// Copy file.
	if err := copyFile(source, target); err != nil {
		// Check for sharing violation (locked file).
		if isSharingViolation(err) {
			result.Status = "restored"
			result.Warnings = append(result.Warnings, fmt.Sprintf("WARN: Skipped locked file (sharing violation): %s", target))
			return result, nil
		}
		result.Status = "failed"
		result.Error = fmt.Sprintf("copy failed: %v", err)
		return result, nil
	}

	result.Status = "restored"
	return result, nil
}

// restoreCopyDir copies a directory tree from source to target, supporting
// exclude patterns and locked file handling.
func restoreCopyDir(entry RestoreAction, source, target string, opts RestoreOptions) (*RestoreResult, error) {
	result := &RestoreResult{
		Source: source,
		Target: target,
	}

	// Dry-run: report what would happen.
	if opts.DryRun {
		result.Status = "restored"
		return result, nil
	}

	// Backup target if it exists and backup is requested.
	if entry.Backup {
		if _, statErr := os.Stat(target); statErr == nil {
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
	}

	// Ensure target directory exists.
	if err := os.MkdirAll(target, 0755); err != nil {
		result.Status = "failed"
		result.Error = fmt.Sprintf("cannot create target directory: %v", err)
		return result, nil
	}

	// Build exclude checker.
	excludePatterns := entry.Exclude
	excludeFunc := func(relPath string) bool {
		return isPathExcluded(relPath, excludePatterns)
	}

	// Walk source and copy.
	var warnings []string
	err := filepath.Walk(source, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		relPath, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}

		if relPath == "." {
			return nil
		}

		// Check excludes.
		if len(excludePatterns) > 0 && excludeFunc(relPath) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		destPath := filepath.Join(target, relPath)

		if info.IsDir() {
			return os.MkdirAll(destPath, info.Mode())
		}

		// Copy file, handle locked files.
		copyErr := copyFile(path, destPath)
		if copyErr != nil {
			if isSharingViolation(copyErr) {
				warnings = append(warnings, fmt.Sprintf("WARN: Skipped locked file (sharing violation): %s", relPath))
				return nil
			}
			return copyErr
		}

		return nil
	})

	if err != nil {
		result.Status = "failed"
		result.Error = fmt.Sprintf("directory copy failed: %v", err)
		return result, nil
	}

	result.Status = "restored"
	result.Warnings = warnings
	return result, nil
}

// isPathExcluded checks whether a relative path matches any of the exclude
// patterns. Patterns use doublestar-style matching: ** matches any path
// segment. The implementation strips leading/trailing ** and checks if the
// remaining pattern is contained in the path.
func isPathExcluded(relPath string, patterns []string) bool {
	// Normalise to forward-slash for consistent matching.
	normalizedPath := filepath.ToSlash(relPath)

	for _, pattern := range patterns {
		normalizedPattern := filepath.ToSlash(pattern)

		// Strip leading and trailing ** segments.
		searchPattern := normalizedPattern
		searchPattern = strings.TrimPrefix(searchPattern, "**/")
		searchPattern = strings.TrimPrefix(searchPattern, "**\\")
		searchPattern = strings.TrimPrefix(searchPattern, "**")
		searchPattern = strings.TrimSuffix(searchPattern, "/**")
		searchPattern = strings.TrimSuffix(searchPattern, "\\**")
		searchPattern = strings.TrimSuffix(searchPattern, "**")

		if searchPattern == "" {
			continue
		}

		// Check if the normalised path contains the search pattern.
		if strings.Contains(normalizedPath, searchPattern) {
			return true
		}
	}

	return false
}

// isSharingViolation checks if an error is a Windows sharing violation.
// On non-Windows platforms this always returns false.
func isSharingViolation(err error) bool {
	if err == nil {
		return false
	}
	// Windows sharing violation errors contain specific HRESULT text.
	msg := err.Error()
	return strings.Contains(msg, "sharing violation") ||
		strings.Contains(msg, "being used by another process") ||
		strings.Contains(msg, "locked")
}
