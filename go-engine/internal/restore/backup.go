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

// copyFile copies a single file from src to dst, creating parent directories
// as needed.
func copyFile(src, dst string) error {
	dir := filepath.Dir(dst)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	srcInfo, err := srcFile.Stat()
	if err != nil {
		return err
	}

	dstFile, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, srcInfo.Mode())
	if err != nil {
		return err
	}
	defer dstFile.Close()

	_, err = io.Copy(dstFile, srcFile)
	return err
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
