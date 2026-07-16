// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package bundle

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
)

// BundleMetadata is the metadata.json content written into the zip bundle.
type BundleMetadata struct {
	SchemaVersion          string   `json:"schemaVersion"`
	ManifestVersion        int      `json:"manifestVersion,omitempty"`
	CapturedAt             string   `json:"capturedAt"`
	MachineName            string   `json:"machineName"`
	EndstateVersion        string   `json:"endstateVersion"`
	ConfigCapturesIncluded []string `json:"configCapturesIncluded,omitempty"`
	ConfigModulesIncluded  []string `json:"configModulesIncluded"`
	ConfigModulesSkipped   []string `json:"configModulesSkipped"`
	CaptureWarnings        []string `json:"captureWarnings"`
}

// payloadPathPattern matches ./payload/apps/<id>/ style source paths in
// restore entries for rewriting to the zip layout.
var payloadPathPattern = regexp.MustCompile(`^\./payload/apps/([^/]+)/(.+)$`)

// CreateBundle creates a zip bundle containing the manifest, collected config
// files, and metadata.
//
// The algorithm:
//  1. Stage manifest as manifest.jsonc in a temp directory
//  2. For each matched module with capture.files, collect config files into
//     staging under configs/<module-dir-name>/
//  3. Inject module restore entries into the staged manifest with path
//     rewriting: ./payload/apps/<id>/ becomes ./configs/<module-dir-name>/
//  4. Write metadata.json with timestamp, machine name, version info
//  5. Create zip atomically (write to temp file, rename to final path)
func CreateBundle(manifestPath string, matchedModules []*modules.Module, outputPath string, version string) error {
	stagingDir, err := os.MkdirTemp("", "endstate-bundle-")
	if err != nil {
		return fmt.Errorf("failed to create staging directory: %w", err)
	}
	defer os.RemoveAll(stagingDir)

	// --- Stage 1: Copy manifest ---
	stagedManifest := filepath.Join(stagingDir, "manifest.jsonc")
	manifestData, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("failed to read manifest %s: %w", manifestPath, err)
	}
	if err := os.WriteFile(stagedManifest, manifestData, 0644); err != nil {
		return fmt.Errorf("failed to stage manifest: %w", err)
	}

	// --- Stage 2a: Collect config files ---
	var included []string
	var skipped []string
	var captureWarnings []string

	for _, mod := range matchedModules {
		moduleDirName := mod.ID
		if strings.HasPrefix(moduleDirName, "apps.") {
			moduleDirName = moduleDirName[5:]
		}

		fileCollected, _, err := CollectConfigFiles(mod, stagingDir)
		if err != nil {
			captureWarnings = append(captureWarnings, fmt.Sprintf("module %s: %v", mod.ID, err))
			skipped = append(skipped, moduleDirName)
			continue
		}

		regCollected, regErr := CollectRegistryKeys(mod, stagingDir)
		if regErr != nil {
			captureWarnings = append(captureWarnings, fmt.Sprintf("module %s registry: %v", mod.ID, regErr))
			// Don't skip the whole module — file collection may have succeeded.
		}

		regValuesCollected, regValErr := CollectRegistryValues(mod, stagingDir)
		if regValErr != nil {
			captureWarnings = append(captureWarnings, fmt.Sprintf("module %s registry values: %v", mod.ID, regValErr))
			// Don't skip the whole module — other collection may have succeeded.
		}

		collected := append(fileCollected, regCollected...)
		collected = append(collected, regValuesCollected...)

		if len(collected) > 0 {
			included = append(included, moduleDirName)
		} else {
			skipped = append(skipped, moduleDirName)
		}
	}

	// --- Stage 2b: Inject configModules + restore entries with path rewriting ---
	if len(included) > 0 {
		m, err := manifest.LoadManifest(stagedManifest)
		if err != nil {
			return fmt.Errorf("failed to reload staged manifest: %w", err)
		}

		// Build set of included modules for filtering.
		includedSet := make(map[string]bool)
		for _, inc := range included {
			includedSet[inc] = true
		}

		// Build configModules list and rewritten restore entries.
		var configModuleIDs []string
		var rewrittenRestore []manifest.RestoreEntry

		for _, mod := range matchedModules {
			moduleDirName := mod.ID
			if strings.HasPrefix(moduleDirName, "apps.") {
				moduleDirName = moduleDirName[5:]
			}
			if !includedSet[moduleDirName] {
				continue
			}

			configModuleIDs = append(configModuleIDs, mod.ID)

			for _, r := range mod.Restore {
				entry := manifest.RestoreEntry{
					Type:     r.Type,
					Source:   rewriteSourcePath(r.Source, moduleDirName),
					Target:   r.Target,
					Backup:   r.Backup,
					Optional: r.Optional,
					Exclude:  r.Exclude,
					// Value-level registry-set fields have no payload source to
					// rewrite; carry them through verbatim so the bundle round-trips.
					Key:       r.Key,
					ValueName: r.ValueName,
					ValueType: r.ValueType,
					Data:      r.Data,
				}
				rewrittenRestore = append(rewrittenRestore, entry)
			}
		}

		// Update manifest.
		m.ConfigModules = configModuleIDs
		if len(rewrittenRestore) > 0 {
			m.Restore = rewrittenRestore
		}

		// Write updated manifest back.
		updatedData, err := json.MarshalIndent(m, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal updated manifest: %w", err)
		}
		if err := os.WriteFile(stagedManifest, updatedData, 0644); err != nil {
			return fmt.Errorf("failed to write updated manifest: %w", err)
		}
	}

	// --- Stage 3: Write metadata.json ---
	hostname, _ := os.Hostname()

	metadata := BundleMetadata{
		SchemaVersion:         "1.0",
		CapturedAt:            time.Now().UTC().Format(time.RFC3339),
		MachineName:           hostname,
		EndstateVersion:       version,
		ConfigModulesIncluded: included,
		ConfigModulesSkipped:  skipped,
		CaptureWarnings:       captureWarnings,
	}
	// Ensure empty slices serialize as [] not null.
	if metadata.ConfigModulesIncluded == nil {
		metadata.ConfigModulesIncluded = []string{}
	}
	if metadata.ConfigModulesSkipped == nil {
		metadata.ConfigModulesSkipped = []string{}
	}
	if metadata.CaptureWarnings == nil {
		metadata.CaptureWarnings = []string{}
	}

	metadataBytes, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}
	metadataPath := filepath.Join(stagingDir, "metadata.json")
	if err := os.WriteFile(metadataPath, metadataBytes, 0644); err != nil {
		return fmt.Errorf("failed to write metadata: %w", err)
	}

	// --- Stage 4: Create zip atomically ---
	if err := writeCaptureZipAtomically(stagingDir, outputPath); err != nil {
		return fmt.Errorf("failed to create zip: %w", err)
	}

	return nil
}

// rewriteSourcePath converts ./payload/apps/<id>/<file> to
// ./configs/<moduleDirName>/<file> for the bundle layout.
func rewriteSourcePath(source string, moduleDirName string) string {
	// Normalize backslashes to forward slashes for matching.
	normalized := strings.ReplaceAll(source, "\\", "/")

	matches := payloadPathPattern.FindStringSubmatch(normalized)
	if matches != nil {
		// matches[2] is the filename/path after the module ID directory.
		leaf := filepath.Base(matches[2])
		return "./configs/" + moduleDirName + "/" + leaf
	}

	return source
}

// createZipFromDir creates a zip file from the contents of a directory.
func createZipFromDir(srcDir, zipPath string) error {
	zipFile, err := os.Create(zipPath)
	if err != nil {
		return err
	}
	return createZipFromDirFile(srcDir, zipFile)
}

// createZipFromDirFile writes a zip to an already-created file and owns the
// file from entry through Sync and Close. Every finalization error participates
// in the returned error so callers never publish a partially finalized zip.
func createZipFromDirFile(srcDir string, zipFile *os.File) error {
	w := zip.NewWriter(zipFile)

	walkErr := filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if isLinkOrReparse(info) {
			return fmt.Errorf("zip entry %q is a link or reparse point", path)
		}

		// Skip the root directory itself.
		if path == srcDir {
			if !info.IsDir() {
				return fmt.Errorf("zip source root %q is not a directory", path)
			}
			return nil
		}

		relPath, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}

		// Use forward slashes in zip entries.
		zipEntryName := filepath.ToSlash(relPath)

		if info.IsDir() {
			// Add trailing slash for directories in zip.
			_, err := w.Create(zipEntryName + "/")
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("zip entry %q is not a regular file", path)
		}

		// Create file entry in zip.
		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		header.Name = zipEntryName
		header.Method = zip.Deflate

		writer, err := w.CreateHeader(header)
		if err != nil {
			return err
		}

		file, err := os.Open(path)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(writer, file)
		closeErr := file.Close()
		return errors.Join(copyErr, closeErr)
	})
	writerCloseErr := w.Close()
	syncErr := zipFile.Sync()
	fileCloseErr := zipFile.Close()
	return errors.Join(walkErr, writerCloseErr, syncErr, fileCloseErr)
}
