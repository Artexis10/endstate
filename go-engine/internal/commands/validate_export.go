// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"os"
	"path/filepath"

	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/events"
)

// ValidateExportFlags holds the parsed CLI flags for the validate-export command.
type ValidateExportFlags struct {
	// Manifest is the path to the .jsonc manifest file.
	Manifest string
	// Export is the path to the export directory to validate.
	Export string
	// Events controls streaming event output. "jsonl" enables it; "" disables.
	Events string
}

// ValidateExportData is the data payload for the validate-export command.
type ValidateExportData struct {
	Valid      bool     `json:"valid"`
	ValidCount int      `json:"validCount"`
	WarnCount  int      `json:"warnCount"`
	FailCount  int      `json:"failCount"`
	Warnings   []string `json:"warnings,omitempty"`
	Errors     []string `json:"errors,omitempty"`
}

// RunValidateExport checks whether all restore entry sources exist in the
// export directory.
func RunValidateExport(flags ValidateExportFlags) (interface{}, *envelope.Error) {
	runID := buildRunID("validate-export")
	emitter := events.NewEmitter(runID, flags.Events == "jsonl")

	// --- 1. Load manifest ---
	mf, envelopeErr := loadManifest(flags.Manifest)
	if envelopeErr != nil {
		return nil, envelopeErr
	}

	emitter.EmitPhase("validate-export")

	// Resolve export directory.
	manifestDir := filepath.Dir(flags.Manifest)
	absManifestDir, _ := filepath.Abs(manifestDir)

	exportDir := flags.Export
	if exportDir == "" {
		exportDir = filepath.Join(absManifestDir, "export")
	}
	exportDir, _ = filepath.Abs(exportDir)

	validCount := 0
	var warnings []string
	var errors []string

	// --- 2. Check each restore entry source in export dir ---
	for _, entry := range mf.Restore {
		// Try export dir first (Model B).
		candidate := filepath.Join(exportDir, entry.Source)
		if _, err := os.Stat(candidate); err == nil {
			validCount++
			emitter.EmitItem(entry.Source, "validate", "valid", "", "Found in export")
			continue
		}

		// Not found in export dir.
		if entry.Optional {
			warnings = append(warnings, "Optional source not found in export: "+entry.Source)
			emitter.EmitItem(entry.Source, "validate", "warn", "optional_missing", "Optional source not in export")
		} else {
			errors = append(errors, "Required source not found in export: "+entry.Source)
			emitter.EmitItem(entry.Source, "validate", "fail", "missing", "Required source not in export")
		}
	}

	failCount := len(errors)
	warnCount := len(warnings)
	valid := failCount == 0

	emitter.EmitSummary("validate-export", validCount+failCount+warnCount, validCount, warnCount, failCount)

	return &ValidateExportData{
		Valid:      valid,
		ValidCount: validCount,
		WarnCount:  warnCount,
		FailCount:  failCount,
		Warnings:   warnings,
		Errors:     errors,
	}, nil
}
