// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package bundle

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
)

// ModuleSnapshot identifies the immutable canonical module bytes stored for
// inspection. It deliberately does not expose a parsed executable authority.
type ModuleSnapshot struct {
	Path        string
	ContentHash string
}

// WriteModuleSnapshot writes the catalog-load-pinned canonical bytes without
// rereading Module.FilePath. Identical snapshots deduplicate; conflicts never
// overwrite existing bytes.
func WriteModuleSnapshot(stagingRoot string, mod *modules.Module) (ModuleSnapshot, error) {
	if mod == nil {
		return ModuleSnapshot{}, integrityError(IntegritySnapshotConflict, "", "module is nil")
	}
	canonical := mod.CanonicalSnapshot()
	if len(canonical) == 0 || !payloadHashPattern.MatchString(mod.Revision) || hashBytes(canonical) != mod.Revision {
		return ModuleSnapshot{}, integrityError(IntegritySnapshotConflict, mod.ID, "module has no valid pinned canonical snapshot/revision")
	}
	safeID := safeSnapshotID(mod.ID)
	portablePath := path.Join("provenance", "modules", safeID+"-"+mod.Revision+".json")
	if err := ensureNoLinksInExistingPath(stagingRoot); err != nil {
		return ModuleSnapshot{}, integrityPathError(err, stagingRoot)
	}
	destination, err := containedHostPath(stagingRoot, portablePath)
	if err != nil {
		return ModuleSnapshot{}, integrityPathError(err, portablePath)
	}
	if err := ensureNoLinksInExistingPath(filepath.Dir(destination)); err != nil {
		return ModuleSnapshot{}, integrityPathError(err, filepath.Dir(destination))
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return ModuleSnapshot{}, integrityError(IntegritySnapshotConflict, portablePath, "create snapshot directory: %v", err)
	}
	if err := ensureNoLinksInExistingPath(filepath.Dir(destination)); err != nil {
		return ModuleSnapshot{}, integrityPathError(err, filepath.Dir(destination))
	}

	if info, err := os.Lstat(destination); err == nil {
		if isLinkOrReparse(info) {
			return ModuleSnapshot{}, integrityError(IntegrityLinkUnsupported, portablePath, "snapshot destination is a link or reparse point")
		}
		if !info.Mode().IsRegular() {
			return ModuleSnapshot{}, integrityError(IntegritySnapshotConflict, portablePath, "snapshot destination is not a regular file")
		}
		existing, err := os.ReadFile(destination)
		if err != nil {
			return ModuleSnapshot{}, integrityError(IntegritySnapshotConflict, portablePath, "read existing snapshot: %v", err)
		}
		if string(existing) != string(canonical) || hashBytes(existing) != mod.Revision {
			return ModuleSnapshot{}, integrityError(IntegritySnapshotConflict, portablePath, "existing snapshot bytes conflict")
		}
		return ModuleSnapshot{Path: portablePath, ContentHash: mod.Revision}, nil
	} else if !os.IsNotExist(err) {
		return ModuleSnapshot{}, integrityError(IntegritySnapshotConflict, portablePath, "inspect snapshot destination: %v", err)
	}

	file, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return ModuleSnapshot{}, integrityError(IntegritySnapshotConflict, portablePath, "create snapshot: %v", err)
	}
	removeOnError := true
	defer func() {
		_ = file.Close()
		if removeOnError {
			_ = os.Remove(destination)
		}
	}()
	if _, err := file.Write(canonical); err != nil {
		return ModuleSnapshot{}, integrityError(IntegritySnapshotConflict, portablePath, "write snapshot: %v", err)
	}
	if err := file.Sync(); err != nil {
		return ModuleSnapshot{}, integrityError(IntegritySnapshotConflict, portablePath, "sync snapshot: %v", err)
	}
	if err := file.Close(); err != nil {
		return ModuleSnapshot{}, integrityError(IntegritySnapshotConflict, portablePath, "close snapshot: %v", err)
	}
	removeOnError = false
	return ModuleSnapshot{Path: portablePath, ContentHash: mod.Revision}, nil
}

// VerifyModuleSnapshot verifies only containment, regular-file status, and the
// recorded content hash. It never parses or returns a Module and therefore
// cannot make the bundle snapshot target authority.
func VerifyModuleSnapshot(bundleRoot string, capture manifest.ConfigCapture) error {
	recordedPath := capture.CaptureModule.SnapshotPath
	portablePath, err := normalizePortablePath(recordedPath)
	if err != nil || portablePath != recordedPath || !strings.HasPrefix(portablePath, "provenance/modules/") {
		return integrityError(IntegrityUnsafePath, recordedPath, "module snapshot must be a canonical path under provenance/modules/")
	}
	hostPath, err := containedHostPath(bundleRoot, portablePath)
	if err != nil {
		return integrityPathError(err, recordedPath)
	}
	if err := ensureNoLinksInExistingPath(hostPath); err != nil {
		if os.IsNotExist(err) {
			return integrityError(IntegritySnapshotMissing, recordedPath, "module snapshot is missing")
		}
		return integrityPathError(err, recordedPath)
	}
	info, err := os.Lstat(hostPath)
	if err != nil {
		if os.IsNotExist(err) {
			return integrityError(IntegritySnapshotMissing, recordedPath, "module snapshot is missing")
		}
		return integrityError(IntegritySnapshotMissing, recordedPath, "inspect module snapshot: %v", err)
	}
	if isLinkOrReparse(info) {
		return integrityError(IntegrityLinkUnsupported, recordedPath, "module snapshot is a link or reparse point")
	}
	if !info.Mode().IsRegular() {
		return integrityError(IntegrityNotRegular, recordedPath, "module snapshot is not a regular file")
	}
	_, actualHash, err := hashRegularFile(hostPath)
	if err != nil {
		return err
	}
	if !payloadHashPattern.MatchString(capture.CaptureModule.ContentHash) || actualHash != capture.CaptureModule.ContentHash {
		return integrityError(IntegritySnapshotHashMismatch, recordedPath, "module snapshot SHA-256 does not match captureModule.contentHash")
	}
	return nil
}

func safeSnapshotID(moduleID string) string {
	var builder strings.Builder
	var pendingSeparator rune
	for _, character := range strings.ToLower(moduleID) {
		alphanumeric := (character >= '0' && character <= '9') || (character >= 'a' && character <= 'z')
		if alphanumeric {
			if pendingSeparator != 0 && builder.Len() > 0 {
				builder.WriteRune(pendingSeparator)
			}
			pendingSeparator = 0
			builder.WriteRune(character)
			continue
		}
		if character == '.' || character == '_' || character == '-' {
			if pendingSeparator == 0 {
				pendingSeparator = character
			} else {
				pendingSeparator = '-'
			}
		} else {
			pendingSeparator = '-'
		}
	}
	safe := builder.String()
	if safe == "" {
		return "module"
	}
	return safe
}

func hashBytes(data []byte) string {
	return fmt.Sprintf("%x", sha256.Sum256(data))
}
