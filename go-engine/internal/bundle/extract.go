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

	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/safepath"
	"github.com/Artexis10/endstate/go-engine/internal/validationmode"
)

const (
	validationExtractionMaxEntries = 100_000
	validationExtractionMaxBytes   = uint64(100 * 1024 * 1024)
)

type validationExtractionMember struct {
	file        *zip.File
	destination string
	directory   bool
}

type validationExtractionRemovalMember struct {
	path      string
	directory bool
}

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
	plan, err := planValidationExtraction(r.File, destDir, context)
	if err != nil {
		return err
	}

	var extractedBytes uint64
	for _, member := range plan {
		if err := context.ValidateSandboxPath(member.destination); err != nil {
			return err
		}
		if member.directory {
			if err := os.MkdirAll(member.destination, 0o755); err != nil {
				return err
			}
			continue
		}
		parent := filepath.Dir(member.destination)
		if err := context.ValidateSandboxPath(parent); err != nil {
			return err
		}
		if err := os.MkdirAll(parent, 0o755); err != nil {
			return err
		}
		if err := context.ValidateSandboxPath(member.destination); err != nil {
			return err
		}
		written, err := extractValidationZipFile(member.file, member.destination, validationExtractionMaxBytes-extractedBytes)
		if err != nil {
			return err
		}
		extractedBytes += written
	}
	return nil
}

// planValidationExtraction authorizes and de-duplicates the entire central
// directory before the first archive member is created. Identity comparison is
// deliberately Windows-like on every platform because validation bundles are
// portable evidence and must not become ambiguous when consumed on Windows.
func planValidationExtraction(files []*zip.File, destDir string, context *validationmode.Context) ([]validationExtractionMember, error) {
	type plannedIdentity struct {
		directory bool
		name      string
	}
	identities := make(map[string]plannedIdentity, len(files))
	plan := make([]validationExtractionMember, 0, len(files))
	for _, file := range files {
		if file == nil {
			return nil, fmt.Errorf("invalid nil entry in zip")
		}
		info := file.FileInfo()
		if info.Mode()&os.ModeType != 0 && !info.IsDir() {
			return nil, fmt.Errorf("unsupported special entry in zip")
		}
		portable := strings.TrimSuffix(strings.ReplaceAll(file.Name, `\`, "/"), "/")
		if portable == "" {
			return nil, fmt.Errorf("illegal empty path in zip")
		}
		destination, err := validationmode.ResolvePortablePath(destDir, portable)
		if err != nil {
			return nil, fmt.Errorf("illegal file path in zip: %w", err)
		}
		if err := context.ValidateSandboxPath(destination); err != nil {
			return nil, err
		}

		identity := strings.ToLower(strings.ReplaceAll(portable, `\`, "/"))
		if previous, exists := identities[identity]; exists {
			return nil, fmt.Errorf("ambiguous zip destination %q conflicts with %q", file.Name, previous.name)
		}
		for slash := strings.IndexByte(identity, '/'); slash >= 0; {
			ancestor := identity[:slash]
			if previous, exists := identities[ancestor]; exists && !previous.directory {
				return nil, fmt.Errorf("zip destination %q is beneath file %q", file.Name, previous.name)
			}
			next := strings.IndexByte(identity[slash+1:], '/')
			if next < 0 {
				break
			}
			slash += next + 1
		}
		if !info.IsDir() {
			prefix := identity + "/"
			for existing, previous := range identities {
				if strings.HasPrefix(existing, prefix) {
					return nil, fmt.Errorf("zip file %q conflicts with child %q", file.Name, previous.name)
				}
			}
		}
		identities[identity] = plannedIdentity{directory: info.IsDir(), name: file.Name}
		plan = append(plan, validationExtractionMember{file: file, destination: destination, directory: info.IsDir()})
	}
	return plan, nil
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
	parent := filepath.Clean(filepath.Join(context.Root(), "state", "extractions"))
	relative, err := filepath.Rel(parent, extractDir)
	if err != nil || relative == "." || relative == "" || filepath.IsAbs(relative) ||
		strings.ContainsRune(relative, os.PathSeparator) || !isValidationExtractionName(relative) {
		return fmt.Errorf("refuse removal outside validation extraction roots")
	}
	if err := context.ValidateSandboxPath(extractDir); err != nil {
		return err
	}
	paths, err := planValidationExtractionRemoval(extractDir, context)
	if err != nil {
		return err
	}
	return removeValidationExtractionPlan(paths, context)
}

func removeValidationExtractionPlan(plan []validationExtractionRemovalMember, context *validationmode.Context) error {
	for _, member := range plan {
		if err := context.ValidateSandboxPath(member.path); err != nil {
			return err
		}
		info, err := os.Lstat(member.path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if safepath.IsLinkOrReparse(info) {
			return fmt.Errorf("refuse linked extraction member %q", member.path)
		}
		if !info.IsDir() && !info.Mode().IsRegular() {
			return fmt.Errorf("refuse special extraction member %q", member.path)
		}
		if info.IsDir() != member.directory {
			return fmt.Errorf("refuse extraction member type change %q", member.path)
		}
		if err := os.Remove(member.path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func isValidationExtractionName(name string) bool {
	if len(name) != len("bundle-")+32 || !strings.HasPrefix(name, "bundle-") {
		return false
	}
	for _, character := range name[len("bundle-"):] {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}

// planValidationExtractionRemoval validates the complete tree before returning
// a post-order removal plan. Cleanup never begins while an unsafe member could
// still be hiding later in the tree.
func planValidationExtractionRemoval(root string, context *validationmode.Context) ([]validationExtractionRemovalMember, error) {
	var paths []validationExtractionRemovalMember
	var visit func(string) error
	visit = func(path string) error {
		if err := context.ValidateSandboxPath(path); err != nil {
			return err
		}
		info, err := os.Lstat(path)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if safepath.IsLinkOrReparse(info) {
			return fmt.Errorf("refuse linked extraction member %q", path)
		}
		if !info.IsDir() && !info.Mode().IsRegular() {
			return fmt.Errorf("refuse special extraction member %q", path)
		}
		if info.IsDir() {
			entries, err := os.ReadDir(path)
			if err != nil {
				return err
			}
			for _, entry := range entries {
				child, err := validationmode.ResolvePortablePath(path, entry.Name())
				if err != nil {
					return err
				}
				if err := visit(child); err != nil {
					return err
				}
			}
		}
		paths = append(paths, validationExtractionRemovalMember{path: path, directory: info.IsDir()})
		return nil
	}
	if err := visit(root); err != nil {
		return nil, err
	}
	return paths, nil
}

// IsBundle checks whether the given path names a capture bundle (.endstate, or
// the legacy .zip). Delegates to manifest.IsBundlePath so the engine has
// exactly one definition of the bundle extension set.
func IsBundle(path string) bool {
	return manifest.IsBundlePath(path)
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
