// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/Artexis10/endstate/go-engine/internal/config"
	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/events"
)

// ExportFlags holds the parsed CLI flags for the export-config command.
type ExportFlags struct {
	// Manifest is the path to the .jsonc manifest file.
	Manifest string
	// Export is the output directory path for the export.
	Export string
	// DryRun previews the export without copying files.
	DryRun bool
	// Events controls streaming event output. "jsonl" enables it; "" disables.
	Events string
}

// ExportData is the data payload for the export-config command JSON envelope.
type ExportData struct {
	ExportPath  string   `json:"exportPath"`
	ExportCount int      `json:"exportCount"`
	SkipCount   int      `json:"skipCount"`
	WarnCount   int      `json:"warnCount"`
	Warnings    []string `json:"warnings,omitempty"`
}

// RunExport executes the export-config command: reads manifest restore entries
// and copies from system target paths to the export directory at the source
// relative path (inverse of restore). Also copies the manifest as
// manifest.snapshot.jsonc.
func RunExport(flags ExportFlags) (interface{}, *envelope.Error) {
	runID := buildRunID("export")
	emitter := events.NewEmitter(runID, flags.Events == "jsonl")

	// --- 1. Load manifest ---
	mf, envelopeErr := loadManifest(flags.Manifest)
	if envelopeErr != nil {
		return nil, envelopeErr
	}

	emitter.EmitPhase("export")

	// Resolve export directory.
	manifestDir := filepath.Dir(flags.Manifest)
	absManifestDir, _ := filepath.Abs(manifestDir)

	exportDir := flags.Export
	if exportDir == "" {
		exportDir = filepath.Join(absManifestDir, "export")
	}
	exportDir, _ = filepath.Abs(exportDir)

	exportCount := 0
	skipCount := 0
	var warnings []string

	// --- 2. Process restore entries (inverse: target -> source) ---
	for _, entry := range mf.Restore {
		// Expand target path (system path).
		target := config.ExpandWindowsEnvVars(entry.Target)
		target = os.ExpandEnv(target)

		// Check if system target exists.
		if _, err := os.Stat(target); os.IsNotExist(err) {
			skipCount++
			emitter.EmitItem(entry.Source, "export", "skipped", "missing_target", "System path not found: "+target)
			continue
		}

		// Resolve export destination: exportDir + source relative path.
		exportDest := filepath.Join(exportDir, entry.Source)

		if flags.DryRun {
			emitter.EmitItem(entry.Source, "export", "would_export", "", fmt.Sprintf("Would copy %s -> %s", target, exportDest))
			exportCount++
			continue
		}

		// Create parent directory.
		if err := os.MkdirAll(filepath.Dir(exportDest), 0755); err != nil {
			warnings = append(warnings, fmt.Sprintf("Cannot create directory for %s: %v", exportDest, err))
			skipCount++
			continue
		}

		// Copy target to export destination.
		info, err := os.Stat(target)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("Cannot stat %s: %v", target, err))
			skipCount++
			continue
		}

		if info.IsDir() {
			if err := exportCopyDir(target, exportDest); err != nil {
				warnings = append(warnings, fmt.Sprintf("Failed to copy directory %s: %v", target, err))
				skipCount++
				continue
			}
		} else {
			if err := exportCopyFile(target, exportDest); err != nil {
				warnings = append(warnings, fmt.Sprintf("Failed to copy file %s: %v", target, err))
				skipCount++
				continue
			}
		}

		emitter.EmitItem(entry.Source, "export", "exported", "", "Exported "+target)
		exportCount++
	}

	// --- 3. Copy manifest as snapshot ---
	if !flags.DryRun {
		absManifest, _ := filepath.Abs(flags.Manifest)
		snapshotDest := filepath.Join(exportDir, "manifest.snapshot.jsonc")
		if err := exportCopyFile(absManifest, snapshotDest); err != nil {
			warnings = append(warnings, fmt.Sprintf("Failed to copy manifest snapshot: %v", err))
		}
	}

	emitter.EmitSummary("export", exportCount+skipCount, exportCount, skipCount, 0)

	return &ExportData{
		ExportPath:  exportDir,
		ExportCount: exportCount,
		SkipCount:   skipCount,
		WarnCount:   len(warnings),
		Warnings:    warnings,
	}, nil
}

// exportCopyFile copies a single file from src to dst.
func exportCopyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}

	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	_, err = io.Copy(dstFile, srcFile)
	return err
}

// exportCopyDir copies a directory tree from src to dst.
func exportCopyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}

		destPath := filepath.Join(dst, relPath)

		if info.IsDir() {
			return os.MkdirAll(destPath, info.Mode())
		}

		return exportCopyFile(path, destPath)
	})
}
