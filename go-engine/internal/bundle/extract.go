// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package bundle

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// ExtractBundle extracts a zip bundle to a temporary directory and returns the
// path to manifest.jsonc within the extracted directory. The caller is
// responsible for cleaning up the returned directory.
func ExtractBundle(zipPath string) (string, error) {
	extractDir, err := os.MkdirTemp("", "endstate-apply-")
	if err != nil {
		return "", fmt.Errorf("failed to create extraction directory: %w", err)
	}

	if err := extractZipToDir(zipPath, extractDir); err != nil {
		os.RemoveAll(extractDir)
		return "", fmt.Errorf("failed to extract zip %s: %w", zipPath, err)
	}

	manifestPath := filepath.Join(extractDir, "manifest.jsonc")
	if _, err := os.Stat(manifestPath); err != nil {
		os.RemoveAll(extractDir)
		return "", fmt.Errorf("zip does not contain manifest.jsonc")
	}

	return manifestPath, nil
}

// IsBundle checks if the given path has a .zip extension, indicating it is a
// bundle file.
func IsBundle(path string) bool {
	return strings.EqualFold(filepath.Ext(path), ".zip")
}

// extractZipToDir extracts all entries from a zip file into destDir.
func extractZipToDir(zipPath, destDir string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer r.Close()

	for _, f := range r.File {
		destPath := filepath.Join(destDir, f.Name)

		// Security: prevent zip slip by ensuring the resolved path stays
		// within destDir.
		if !strings.HasPrefix(filepath.Clean(destPath)+string(os.PathSeparator), filepath.Clean(destDir)+string(os.PathSeparator)) {
			// Also allow exact match (destPath == destDir for root entries).
			if filepath.Clean(destPath) != filepath.Clean(destDir) {
				return fmt.Errorf("illegal file path in zip: %s", f.Name)
			}
		}

		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(destPath, 0755); err != nil {
				return err
			}
			continue
		}

		// Ensure parent directory exists.
		if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
			return err
		}

		if err := extractZipFile(f, destPath); err != nil {
			return err
		}
	}

	return nil
}

// extractZipFile extracts a single zip file entry to destPath.
func extractZipFile(f *zip.File, destPath string) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()

	out, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, rc); err != nil {
		return err
	}

	return out.Close()
}
