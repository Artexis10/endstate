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
	boundary := legacyValidationBoundary{context: opts.ValidationContext, backupDir: opts.BackupDir}

	// Check source exists.
	srcInfo, err := boundary.stat("source-stat", source)
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
	boundary := legacyValidationBoundary{context: opts.ValidationContext, backupDir: opts.BackupDir}

	// Up-to-date detection via hash comparison.
	upToDate, err := isUpToDateWithBoundary(source, target, boundary)
	if err == nil && upToDate {
		result.Status = "skipped_up_to_date"
		return result, nil
	}
	if err != nil && opts.ValidationContext != nil {
		result.Status = "failed"
		result.Error = fmt.Sprintf("target snapshot failed: %v", err)
		return result, nil
	}

	// Dry-run: report what would happen.
	if opts.DryRun {
		result.Status = "restored"
		return result, nil
	}
	if entry.Backup {
		if err := boundary.authorizeBackupDir(restoreBackupDirectory(opts)); err != nil {
			result.Status = "failed"
			result.Error = fmt.Sprintf("backup failed: %v", err)
			return result, nil
		}
	}

	// Backup target if it exists and backup is requested.
	if entry.Backup {
		if _, statErr := boundary.stat("target-backup-stat", target); statErr == nil {
			backupDir := restoreBackupDirectory(opts)
			backupPath, backupErr := CreateBackupWithValidation(target, backupDir, opts.ValidationContext)
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
	if err := boundary.mkdirAll("target-directory-mkdir", targetDir, 0755); err != nil {
		result.Status = "failed"
		result.Error = fmt.Sprintf("cannot create target directory: %v", err)
		return result, nil
	}

	// Copy file.
	if err := boundary.atomicCopy("target-atomic-copy", source, target); err != nil {
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
	boundary := legacyValidationBoundary{context: opts.ValidationContext, backupDir: opts.BackupDir}
	excludePatterns := entry.Exclude
	excludeFunc := func(relPath string) bool {
		return isPathExcluded(relPath, excludePatterns)
	}
	if err := validateRestoreCopyTreeWithBoundary(source, target, excludeFunc, boundary); err != nil {
		result.Status = "failed"
		result.Error = fmt.Sprintf("unsafe directory copy: %v", err)
		return result, nil
	}

	// Dry-run: report what would happen.
	if opts.DryRun {
		result.Status = "restored"
		return result, nil
	}
	if entry.Backup {
		if err := boundary.authorizeBackupDir(restoreBackupDirectory(opts)); err != nil {
			result.Status = "failed"
			result.Error = fmt.Sprintf("backup failed: %v", err)
			return result, nil
		}
	}

	// Backup target if it exists and backup is requested.
	if entry.Backup {
		if _, statErr := boundary.stat("target-backup-stat", target); statErr == nil {
			backupDir := restoreBackupDirectory(opts)
			backupPath, backupErr := CreateBackupWithValidation(target, backupDir, opts.ValidationContext)
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
	if err := boundary.mkdirAll("target-directory-mkdir", target, 0755); err != nil {
		result.Status = "failed"
		result.Error = fmt.Sprintf("cannot create target directory: %v", err)
		return result, nil
	}

	// Walk source and copy.
	var warnings []string
	err := filepath.Walk(source, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := boundary.authorizeIO("copy-source-member", path); err != nil {
			return err
		}
		if isLinkOrReparse(info) {
			return fmt.Errorf("source path member is a link or reparse point")
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
		if err := ValidateFilesystemTarget(destPath); err != nil {
			return err
		}

		if info.IsDir() {
			return boundary.mkdirAll("copy-target-member-mkdir", destPath, info.Mode())
		}

		// Copy file, handle locked files.
		copyErr := boundary.atomicCopy("copy-target-member", path, destPath)
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

func validateRestoreCopyTree(source, target string, exclude func(string) bool) error {
	if err := ValidateFilesystemTarget(target); err != nil {
		return err
	}
	return filepath.Walk(source, func(sourcePath string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if isLinkOrReparse(info) {
			return fmt.Errorf("source path component %q is a link or reparse point", sourcePath)
		}
		relative, err := filepath.Rel(source, sourcePath)
		if err != nil {
			return err
		}
		if relative == "." {
			return nil
		}
		if exclude != nil && exclude(relative) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		return ValidateFilesystemTarget(filepath.Join(target, relative))
	})
}

func validateRestoreCopyTreeWithBoundary(source, target string, exclude func(string) bool, boundary legacyValidationBoundary) error {
	if boundary.context == nil {
		return validateRestoreCopyTree(source, target, exclude)
	}
	if err := boundary.authorizeIO("copy-tree-target", target); err != nil {
		return err
	}
	if err := boundary.authorizeIO("copy-tree-source-root", source); err != nil {
		return err
	}
	return filepath.Walk(source, func(sourcePath string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := boundary.authorizeIO("copy-tree-source-member", sourcePath); err != nil {
			return err
		}
		if isLinkOrReparse(info) {
			return fmt.Errorf("source path component is a link or reparse point")
		}
		relative, err := filepath.Rel(source, sourcePath)
		if err != nil {
			return err
		}
		if relative == "." {
			return nil
		}
		if exclude != nil && exclude(relative) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		return boundary.authorizeIO("copy-tree-target-member", filepath.Join(target, relative))
	})
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
