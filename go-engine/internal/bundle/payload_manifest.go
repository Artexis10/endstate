// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package bundle

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/manifest"
)

const (
	IntegrityUnsafePath           = "INTEGRITY_UNSAFE_PATH"
	IntegrityLinkUnsupported      = "INTEGRITY_LINK_UNSUPPORTED"
	IntegrityNotRegular           = "INTEGRITY_NOT_REGULAR"
	IntegrityPayloadMissing       = "PAYLOAD_MISSING"
	IntegrityPayloadExtra         = "PAYLOAD_EXTRA"
	IntegrityPayloadSizeMismatch  = "PAYLOAD_SIZE_MISMATCH"
	IntegrityPayloadHashMismatch  = "PAYLOAD_HASH_MISMATCH"
	IntegrityPayloadDuplicate     = "PAYLOAD_DUPLICATE"
	IntegrityPayloadInvalidEntry  = "PAYLOAD_INVALID_ENTRY"
	IntegritySnapshotMissing      = "MODULE_SNAPSHOT_MISSING"
	IntegritySnapshotHashMismatch = "MODULE_SNAPSHOT_HASH_MISMATCH"
	IntegritySnapshotConflict     = "MODULE_SNAPSHOT_CONFLICT"
)

var payloadHashPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// IntegrityError is a stable typed bundle-integrity failure. Later planning
// maps payload codes to payload_integrity_failed without parsing error text.
type IntegrityError struct {
	Code   string
	Path   string
	Detail string
}

func (e *IntegrityError) Error() string {
	if e.Path == "" {
		return "bundle integrity: " + e.Detail
	}
	return fmt.Sprintf("bundle integrity at %q: %s", e.Path, e.Detail)
}

// IntegrityDiagnosticCode extracts a stable bundle-integrity diagnostic.
func IntegrityDiagnosticCode(err error) string {
	var integrityErr *IntegrityError
	if errors.As(err, &integrityErr) {
		return integrityErr.Code
	}
	return ""
}

// BuildPayloadManifest returns a closed-world, deterministic index of every
// regular file under payloadRoot using exact on-disk bytes.
func BuildPayloadManifest(payloadRoot string) ([]manifest.PayloadManifestEntry, error) {
	if strings.TrimSpace(payloadRoot) == "" {
		return nil, integrityError(IntegrityUnsafePath, payloadRoot, "payload root is empty")
	}
	if !filepath.IsAbs(payloadRoot) {
		return nil, integrityError(IntegrityUnsafePath, payloadRoot, "payload root must be an absolute host path")
	}
	if err := ensureNoLinksInExistingPath(payloadRoot); err != nil {
		return nil, integrityPathError(err, payloadRoot)
	}
	rootInfo, err := os.Lstat(payloadRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, integrityError(IntegrityPayloadMissing, payloadRoot, "payload root is missing")
		}
		return nil, integrityError(IntegrityNotRegular, payloadRoot, "inspect payload root: %v", err)
	}
	if isLinkOrReparse(rootInfo) {
		return nil, integrityError(IntegrityLinkUnsupported, payloadRoot, "payload root is a link or reparse point")
	}
	if !rootInfo.IsDir() {
		return nil, integrityError(IntegrityNotRegular, payloadRoot, "payload root is not a directory")
	}

	entries := make([]manifest.PayloadManifestEntry, 0)
	err = filepath.WalkDir(payloadRoot, func(hostPath string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return integrityError(IntegrityNotRegular, hostPath, "walk payload: %v", walkErr)
		}
		if hostPath == payloadRoot {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return integrityError(IntegrityNotRegular, hostPath, "inspect payload entry: %v", err)
		}
		if isLinkOrReparse(info) {
			return integrityError(IntegrityLinkUnsupported, hostPath, "payload contains a link or reparse point")
		}
		if entry.IsDir() {
			return nil
		}
		if !info.Mode().IsRegular() {
			return integrityError(IntegrityNotRegular, hostPath, "payload entry is not a regular file")
		}
		relative, err := filepath.Rel(payloadRoot, hostPath)
		if err != nil {
			return integrityError(IntegrityUnsafePath, hostPath, "derive relative payload path: %v", err)
		}
		portable, err := normalizePortablePath(filepath.ToSlash(relative))
		if err != nil {
			return integrityError(IntegrityUnsafePath, filepath.ToSlash(relative), "%v", err)
		}
		size, hash, err := hashRegularFile(hostPath)
		if err != nil {
			return err
		}
		entries = append(entries, manifest.PayloadManifestEntry{RelativePath: portable, Size: size, SHA256: hash})
		return nil
	})
	if err != nil {
		return nil, err
	}
	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		paths = append(paths, entry.RelativePath)
	}
	if err := validatePortablePayloadPathSet(paths); err != nil {
		return nil, err
	}
	sort.Slice(entries, func(left, right int) bool { return entries[left].RelativePath < entries[right].RelativePath })
	return entries, nil
}

// VerifyPayloadManifest verifies the declared manifest and the actual payload
// in both directions before any compatibility planning can lead to mutation.
func VerifyPayloadManifest(payloadRoot string, entries []manifest.PayloadManifestEntry) error {
	declaredEntries := make([]manifest.PayloadManifestEntry, 0, len(entries))
	for index, entry := range entries {
		portable, err := normalizePortablePath(entry.RelativePath)
		if err != nil {
			return integrityError(IntegrityUnsafePath, entry.RelativePath, "payloadManifest[%d]: %v", index, err)
		}
		if entry.Size < 0 || !payloadHashPattern.MatchString(entry.SHA256) {
			return integrityError(IntegrityPayloadInvalidEntry, portable, "payloadManifest[%d] has invalid size or SHA-256", index)
		}
		entry.RelativePath = portable
		declaredEntries = append(declaredEntries, entry)
	}
	paths := make([]string, 0, len(declaredEntries))
	for _, entry := range declaredEntries {
		paths = append(paths, entry.RelativePath)
	}
	if err := validatePortablePayloadPathSet(paths); err != nil {
		return err
	}
	sort.Slice(declaredEntries, func(left, right int) bool {
		return declaredEntries[left].RelativePath < declaredEntries[right].RelativePath
	})
	declared := make(map[string]manifest.PayloadManifestEntry, len(declaredEntries))
	for _, entry := range declaredEntries {
		declared[strings.ToLower(entry.RelativePath)] = entry
	}

	actualEntries, err := BuildPayloadManifest(payloadRoot)
	if err != nil {
		return err
	}
	actual := make(map[string]manifest.PayloadManifestEntry, len(actualEntries))
	for _, entry := range actualEntries {
		actual[strings.ToLower(entry.RelativePath)] = entry
	}
	for _, entry := range declaredEntries {
		key := strings.ToLower(entry.RelativePath)
		found, exists := actual[key]
		if !exists {
			return integrityError(IntegrityPayloadMissing, entry.RelativePath, "declared payload file is missing")
		}
		if found.Size != entry.Size {
			return integrityError(IntegrityPayloadSizeMismatch, entry.RelativePath, "size is %d, expected %d", found.Size, entry.Size)
		}
		if found.SHA256 != entry.SHA256 {
			return integrityError(IntegrityPayloadHashMismatch, entry.RelativePath, "SHA-256 does not match")
		}
	}
	for _, entry := range actualEntries {
		if _, exists := declared[strings.ToLower(entry.RelativePath)]; !exists {
			return integrityError(IntegrityPayloadExtra, entry.RelativePath, "payload contains an undeclared regular file")
		}
	}
	return nil
}

func validatePortablePayloadPathSet(paths []string) error {
	for index, current := range paths {
		currentKey := strings.ToLower(current)
		for _, existing := range paths[:index] {
			existingKey := strings.ToLower(existing)
			if currentKey == existingKey || strings.HasPrefix(currentKey, existingKey+"/") || strings.HasPrefix(existingKey, currentKey+"/") {
				return integrityError(IntegrityPayloadDuplicate, current, "duplicates or overlaps payload path %q", existing)
			}
		}
	}
	return nil
}

func hashRegularFile(hostPath string) (int64, string, error) {
	if err := ensureNoLinksInExistingPath(hostPath); err != nil {
		return 0, "", integrityPathError(err, hostPath)
	}
	file, err := os.Open(hostPath)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, "", integrityError(IntegrityPayloadMissing, hostPath, "file is missing")
		}
		return 0, "", integrityError(IntegrityNotRegular, hostPath, "open regular file: %v", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return 0, "", integrityError(IntegrityNotRegular, hostPath, "stat regular file: %v", err)
	}
	if !info.Mode().IsRegular() {
		return 0, "", integrityError(IntegrityNotRegular, hostPath, "entry is not a regular file")
	}
	hash := sha256.New()
	written, err := io.Copy(hash, file)
	if err != nil {
		return 0, "", integrityError(IntegrityNotRegular, hostPath, "hash file: %v", err)
	}
	if written != info.Size() {
		return 0, "", integrityError(IntegrityPayloadSizeMismatch, hostPath, "file changed while hashing")
	}
	return written, hex.EncodeToString(hash.Sum(nil)), nil
}

func integrityError(code, path, format string, args ...any) error {
	return &IntegrityError{Code: code, Path: path, Detail: fmt.Sprintf(format, args...)}
}

func integrityPathError(err error, hostPath string) error {
	if errors.Is(err, errBundleLink) {
		return integrityError(IntegrityLinkUnsupported, hostPath, "%v", err)
	}
	if errors.Is(err, errUnsafeBundlePath) {
		return integrityError(IntegrityUnsafePath, hostPath, "%v", err)
	}
	return integrityError(IntegrityNotRegular, hostPath, "%v", err)
}
