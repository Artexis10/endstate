// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package bundle

import (
	"archive/zip"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/validationmode"
)

const (
	validationExtractionMaxEntries = 100_000
	validationExtractionMaxBytes   = uint64(100 * 1024 * 1024)
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

// ExtractBundleWithValidation extracts a bundle beneath validation-owned state
// and authorizes the input and every created member at its immediate I/O
// boundary. The ordinary ExtractBundle path remains unchanged.
func ExtractBundleWithValidation(zipPath string, context *validationmode.Context) (string, error) {
	if context == nil {
		return ExtractBundle(zipPath)
	}
	zipPath = filepath.Clean(zipPath)
	if err := context.ValidateSandboxPath(zipPath); err != nil {
		return "", fmt.Errorf("authorize validation bundle input: %w", err)
	}
	extractDir, err := createValidationExtractionRoot(context)
	if err != nil {
		return "", err
	}
	cleanup := func() { _ = RemoveExtractedBundleWithValidation(extractDir, context) }
	if err := extractZipToDirWithValidation(zipPath, extractDir, context); err != nil {
		cleanup()
		return "", fmt.Errorf("failed to extract validation bundle: %w", err)
	}
	manifestPath, err := validationmode.ResolvePortablePath(extractDir, "manifest.jsonc")
	if err != nil {
		cleanup()
		return "", fmt.Errorf("resolve extracted manifest: %w", err)
	}
	if err := context.ValidateSandboxPath(manifestPath); err != nil {
		cleanup()
		return "", fmt.Errorf("authorize extracted manifest: %w", err)
	}
	if _, err := os.Stat(manifestPath); err != nil {
		cleanup()
		return "", fmt.Errorf("zip does not contain manifest.jsonc")
	}
	return manifestPath, nil
}

func createValidationExtractionRoot(context *validationmode.Context) (string, error) {
	parent := filepath.Join(context.Root(), "state", "extractions")
	if err := context.ValidateSandboxPath(parent); err != nil {
		return "", fmt.Errorf("authorize extraction parent: %w", err)
	}
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return "", fmt.Errorf("create extraction parent: %w", err)
	}
	for attempt := 0; attempt < 8; attempt++ {
		var random [16]byte
		if _, err := rand.Read(random[:]); err != nil {
			return "", fmt.Errorf("generate extraction identity: %w", err)
		}
		candidate := filepath.Join(parent, "bundle-"+hex.EncodeToString(random[:]))
		if err := context.ValidateSandboxPath(candidate); err != nil {
			return "", fmt.Errorf("authorize extraction root: %w", err)
		}
		if err := os.Mkdir(candidate, 0o700); err == nil {
			return candidate, nil
		} else if !os.IsExist(err) {
			return "", fmt.Errorf("create extraction root: %w", err)
		}
	}
	return "", fmt.Errorf("create extraction root: identity collision budget exhausted")
}

func extractZipToDirWithValidation(zipPath, destDir string, context *validationmode.Context) error {
	if err := context.ValidateSandboxPath(zipPath); err != nil {
		return err
	}
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer r.Close()
	if err := validateValidationExtractionBudget(r.File); err != nil {
		return err
	}

	var extractedBytes uint64
	for _, file := range r.File {
		if file.FileInfo().Mode()&os.ModeType != 0 && !file.FileInfo().IsDir() {
			return fmt.Errorf("unsupported special entry in zip")
		}
		portable := strings.TrimSuffix(strings.ReplaceAll(file.Name, `\`, "/"), "/")
		if portable == "" {
			return fmt.Errorf("illegal empty path in zip")
		}
		destPath, err := validationmode.ResolvePortablePath(destDir, portable)
		if err != nil {
			return fmt.Errorf("illegal file path in zip: %w", err)
		}
		if err := context.ValidateSandboxPath(destPath); err != nil {
			return err
		}
		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(destPath, 0o755); err != nil {
				return err
			}
			continue
		}
		parent := filepath.Dir(destPath)
		if err := context.ValidateSandboxPath(parent); err != nil {
			return err
		}
		if err := os.MkdirAll(parent, 0o755); err != nil {
			return err
		}
		if err := context.ValidateSandboxPath(destPath); err != nil {
			return err
		}
		written, err := extractValidationZipFile(file, destPath, validationExtractionMaxBytes-extractedBytes)
		if err != nil {
			return err
		}
		extractedBytes += written
	}
	return nil
}

func validateValidationExtractionBudget(files []*zip.File) error {
	if len(files) > validationExtractionMaxEntries {
		return fmt.Errorf("%w: validation bundle has too many entries", validationmode.ErrGuardBudget)
	}
	var total uint64
	for _, file := range files {
		if file == nil || file.FileInfo().IsDir() {
			continue
		}
		if file.UncompressedSize64 > validationExtractionMaxBytes-total {
			return fmt.Errorf("%w: validation bundle exceeds expanded byte limit", validationmode.ErrGuardBudget)
		}
		total += file.UncompressedSize64
	}
	return nil
}

func extractValidationZipFile(file *zip.File, destination string, remaining uint64) (uint64, error) {
	reader, err := file.Open()
	if err != nil {
		return 0, err
	}
	defer reader.Close()
	out, err := os.Create(destination)
	if err != nil {
		return 0, err
	}
	written, copyErr := copyValidationArchiveMember(out, reader, remaining)
	closeErr := out.Close()
	if copyErr != nil {
		return written, copyErr
	}
	return written, closeErr
}

func copyValidationArchiveMember(destination io.Writer, source io.Reader, remaining uint64) (uint64, error) {
	written, err := io.Copy(destination, io.LimitReader(source, int64(remaining)+1))
	if err != nil {
		return uint64(written), err
	}
	if uint64(written) > remaining {
		return uint64(written), fmt.Errorf("%w: validation bundle exceeds expanded byte limit", validationmode.ErrGuardBudget)
	}
	return uint64(written), nil
}

// RemoveExtractedBundleWithValidation removes only an owned extraction root.
func RemoveExtractedBundleWithValidation(extractDir string, context *validationmode.Context) error {
	if context == nil {
		return os.RemoveAll(extractDir)
	}
	extractDir = filepath.Clean(extractDir)
	if err := context.ValidateSandboxPath(extractDir); err != nil {
		return err
	}
	parent := filepath.Join(context.Root(), "state", "extractions")
	relative, err := filepath.Rel(parent, extractDir)
	if err != nil || relative == "." || relative == "" || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) || relative == ".." {
		return fmt.Errorf("refuse removal outside validation extraction roots")
	}
	return os.RemoveAll(extractDir)
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
