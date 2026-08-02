// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package restore

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/Artexis10/endstate/go-engine/internal/validationmode"
)

// ComputeFileHash returns the hex-encoded SHA256 hash of the file at path.
func ComputeFileHash(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("cannot open file for hashing: %w", err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("error reading file for hash: %w", err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// IsUpToDate compares SHA256 hashes of source and target files. Returns true
// if both files exist and have identical content.
func IsUpToDate(sourcePath, targetPath string) (bool, error) {
	// If target doesn't exist, it's not up-to-date.
	if _, err := os.Stat(targetPath); os.IsNotExist(err) {
		return false, nil
	}

	sourceHash, err := ComputeFileHash(sourcePath)
	if err != nil {
		return false, err
	}

	targetHash, err := ComputeFileHash(targetPath)
	if err != nil {
		return false, err
	}

	return sourceHash == targetHash, nil
}

// CreateBackup copies the target file or directory to backupDir, organised
// by SHA256 hash of the target path. Returns the path where the backup was
// stored.
func CreateBackup(targetPath, backupDir string) (string, error) {
	// Generate a safe subdirectory from the hash of the target path.
	pathHash := sha256.Sum256([]byte(targetPath))
	subDir := hex.EncodeToString(pathHash[:])

	backupDest := filepath.Join(backupDir, subDir)
	if err := os.MkdirAll(backupDest, 0755); err != nil {
		return "", fmt.Errorf("cannot create backup directory: %w", err)
	}

	info, err := os.Stat(targetPath)
	if err != nil {
		return "", fmt.Errorf("cannot stat target for backup: %w", err)
	}

	baseName := filepath.Base(targetPath)
	dest := filepath.Join(backupDest, baseName)
	if _, err := os.Lstat(dest); err == nil {
		for ordinal := 1; ; ordinal++ {
			actionDir := filepath.Join(backupDest, fmt.Sprintf("action-%06d", ordinal))
			if err := os.Mkdir(actionDir, 0o755); os.IsExist(err) {
				continue
			} else if err != nil {
				return "", fmt.Errorf("allocate unique backup directory: %w", err)
			}
			dest = filepath.Join(actionDir, baseName)
			break
		}
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("inspect backup destination: %w", err)
	}

	if info.IsDir() {
		if err := copyDirRecursive(targetPath, dest, nil); err != nil {
			return "", fmt.Errorf("backup directory copy failed: %w", err)
		}
	} else {
		if err := copyFile(targetPath, dest); err != nil {
			return "", fmt.Errorf("backup file copy failed: %w", err)
		}
	}

	return dest, nil
}

func isUpToDateWithBoundary(sourcePath, targetPath string, boundary legacyValidationBoundary) (bool, error) {
	if boundary.context == nil {
		return IsUpToDate(sourcePath, targetPath)
	}
	if _, err := boundary.stat("target-up-to-date-stat", targetPath); os.IsNotExist(err) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	sourceData, err := boundary.readFile("source-up-to-date-read", sourcePath)
	if err != nil {
		return false, err
	}
	targetData, err := boundary.readFile("target-up-to-date-read", targetPath)
	if err != nil {
		return false, err
	}
	sourceHash := sha256.Sum256(sourceData)
	targetHash := sha256.Sum256(targetData)
	return sourceHash == targetHash, nil
}

// CreateBackupWithValidation preserves CreateBackup exactly for nil authority
// and reauthorizes every target, destination, and directory member operation
// when validation mode is active.
func CreateBackupWithValidation(targetPath, backupDir string, context *validationmode.Context) (string, error) {
	if context == nil {
		return CreateBackup(targetPath, backupDir)
	}
	boundary := legacyValidationBoundary{context: context, backupDir: backupDir}
	if err := boundary.authorizeBackupDir(backupDir); err != nil {
		return "", err
	}
	pathHash := sha256.Sum256([]byte(targetPath))
	subDir := hex.EncodeToString(pathHash[:])
	backupDest := filepath.Join(backupDir, subDir)
	if err := boundary.mkdirAll("backup-destination-mkdir", backupDest, 0o755); err != nil {
		return "", fmt.Errorf("cannot create backup directory: %w", err)
	}
	info, err := boundary.stat("backup-source-stat", targetPath)
	if err != nil {
		return "", fmt.Errorf("cannot stat target for backup: %w", err)
	}
	baseName := filepath.Base(targetPath)
	dest := filepath.Join(backupDest, baseName)
	if _, err := boundary.lstat("backup-destination-lstat", dest); err == nil {
		for ordinal := 1; ; ordinal++ {
			actionDir := filepath.Join(backupDest, fmt.Sprintf("action-%06d", ordinal))
			if err := boundary.mkdir("backup-action-directory-mkdir", actionDir, 0o755); os.IsExist(err) {
				continue
			} else if err != nil {
				return "", fmt.Errorf("allocate unique backup directory: %w", err)
			}
			dest = filepath.Join(actionDir, baseName)
			break
		}
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("inspect backup destination: %w", err)
	}
	if info.IsDir() {
		if err := copyDirRecursiveWithBoundary(targetPath, dest, nil, boundary, "backup"); err != nil {
			return "", fmt.Errorf("backup directory copy failed: %w", err)
		}
	} else if err := boundary.atomicCopy("backup-atomic-copy", targetPath, dest); err != nil {
		return "", fmt.Errorf("backup file copy failed: %w", err)
	}
	return dest, nil
}

// copyFile copies a single file from src to dst, creating parent directories
// as needed.
func copyFile(src, dst string) error {
	return atomicRestoreCopy(src, dst)
}

// copyDirRecursive copies a directory tree from src to dst. If exclude is
// non-nil, paths matching the exclude checker are skipped. Returns a list of
// warnings (e.g. for locked files) and an error if a non-recoverable issue
// occurs.
func copyDirRecursive(src, dst string, exclude func(relPath string) bool) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}

		// Skip root.
		if relPath == "." {
			return os.MkdirAll(dst, 0755)
		}

		if exclude != nil && exclude(relPath) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		destPath := filepath.Join(dst, relPath)

		if info.IsDir() {
			return os.MkdirAll(destPath, info.Mode())
		}

		return copyFile(path, destPath)
	})
}

func copyDirRecursiveWithBoundary(
	src, dst string,
	exclude func(relPath string) bool,
	boundary legacyValidationBoundary,
	operation string,
) error {
	if boundary.context == nil {
		return copyDirRecursive(src, dst, exclude)
	}
	if err := boundary.authorizeIO(operation+"-walk-root", src); err != nil {
		return err
	}
	return walkTreeWithBoundary(src, boundary, operation, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := boundary.authorizeIO(operation+"-source-member", path); err != nil {
			return err
		}
		if isLinkOrReparse(info) {
			return fmt.Errorf("restore source member is a link or reparse point")
		}
		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if relPath == "." {
			return boundary.mkdirAll(operation+"-destination-root-mkdir", dst, 0o755)
		}
		if exclude != nil && exclude(relPath) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		destPath := filepath.Join(dst, relPath)
		if info.IsDir() {
			return boundary.mkdirAll(operation+"-destination-member-mkdir", destPath, info.Mode())
		}
		return boundary.atomicCopy(operation+"-member-atomic-copy", path, destPath)
	})
}
