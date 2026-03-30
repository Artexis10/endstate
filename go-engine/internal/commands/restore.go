// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/config"
	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/events"
	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/restore"
)

// RestoreFlags holds the parsed CLI flags for the restore command.
type RestoreFlags struct {
	// Manifest is the path to the .jsonc manifest file.
	Manifest string
	// EnableRestore must be true for restore to proceed (opt-in safety).
	EnableRestore bool
	// DryRun previews restore operations without making changes.
	DryRun bool
	// Export is the path to the export directory for Model B source resolution.
	Export string
	// Events controls streaming event output. "jsonl" enables it; "" disables.
	Events string
	// RestoreFilter limits restore to entries matching specific module IDs
	// (comma-separated).
	RestoreFilter string
}

// RestoreData is the data payload for the restore command JSON envelope.
type RestoreData struct {
	Results     []restore.RestoreResult `json:"results"`
	JournalPath string                  `json:"journalPath,omitempty"`
	DryRun      bool                    `json:"dryRun"`
}

// RunRestore executes the restore command: loads the manifest, converts
// RestoreEntries to RestoreActions, runs restore, writes the journal, and
// returns an envelope.
func RunRestore(flags RestoreFlags) (interface{}, *envelope.Error) {
	runID := buildRunID("restore")
	emitter := events.NewEmitter(runID, flags.Events == "jsonl")

	// --- 1. Load manifest ---
	mf, envelopeErr := loadManifest(flags.Manifest)
	if envelopeErr != nil {
		return nil, envelopeErr
	}

	if len(mf.Restore) == 0 {
		return &RestoreData{
			Results: []restore.RestoreResult{},
			DryRun:  flags.DryRun,
		}, nil
	}

	// Check opt-in.
	if !flags.EnableRestore {
		return nil, envelope.NewError(
			envelope.ErrRestoreFailed,
			"Restore entries found but --enable-restore not specified.",
		).WithRemediation("Add --enable-restore to enable restore operations.")
	}

	emitter.EmitPhase("restore")

	// --- 2. Convert manifest entries to restore actions ---
	manifestDir := filepath.Dir(flags.Manifest)
	absManifestDir, _ := filepath.Abs(manifestDir)

	actions := convertToActions(mf.Restore, flags.RestoreFilter)

	// Resolve export path.
	exportRoot := ""
	if flags.Export != "" {
		exportRoot, _ = filepath.Abs(flags.Export)
	}

	// Resolve backup directory.
	repoRoot := config.ResolveRepoRoot()
	backupDir := ""
	if repoRoot != "" {
		backupDir = filepath.Join(repoRoot, "state", "backups", runID)
	}

	// --- 3. Run restore ---
	opts := restore.RestoreOptions{
		DryRun:      flags.DryRun,
		BackupDir:   backupDir,
		ManifestDir: absManifestDir,
		ExportRoot:  exportRoot,
		RunID:       runID,
	}

	results, err := restore.RunRestore(actions, opts, emitter)
	if err != nil {
		return nil, envelope.NewError(envelope.ErrRestoreFailed, err.Error())
	}

	// --- 4. Tally results for summary event (item events emitted inside RunRestore) ---
	restoredCount := 0
	skippedCount := 0
	failedCount := 0
	for _, r := range results {
		switch r.Status {
		case "restored":
			restoredCount++
		case "skipped_up_to_date", "skipped_missing_source":
			skippedCount++
		case "failed":
			failedCount++
		default:
			skippedCount++
		}
	}

	emitter.EmitSummary("restore", len(results), restoredCount, skippedCount, failedCount)

	// --- 5. Write journal (non-dry-run only) ---
	journalPath := ""
	if !flags.DryRun {
		logsDir := ""
		if repoRoot != "" {
			logsDir = filepath.Join(repoRoot, "logs")
		} else {
			logsDir = "logs"
		}

		absManifest, _ := filepath.Abs(flags.Manifest)
		journalErr := restore.WriteJournal(logsDir, runID, absManifest, absManifestDir, exportRoot, results)
		if journalErr == nil {
			latest, findErr := restore.FindLatestJournal(logsDir)
			if findErr == nil {
				journalPath = latest
			}
		}
	}

	return &RestoreData{
		Results:     results,
		JournalPath: journalPath,
		DryRun:      flags.DryRun,
	}, nil
}

// convertToActions converts manifest RestoreEntry items to
// restore.RestoreAction items, applying the optional filter.
func convertToActions(entries []manifest.RestoreEntry, filter string) []restore.RestoreAction {
	var filterList []string
	if filter != "" {
		for _, f := range strings.Split(filter, ",") {
			trimmed := strings.TrimSpace(f)
			if trimmed != "" {
				filterList = append(filterList, trimmed)
			}
		}
	}

	var actions []restore.RestoreAction
	for _, e := range entries {
		// Generate a deterministic ID.
		restoreType := e.Type
		if restoreType == "" {
			restoreType = "copy"
		}
		id := fmt.Sprintf("%s:%s->%s", restoreType, filepath.ToSlash(e.Source), filepath.ToSlash(e.Target))

		action := restore.RestoreAction{
			Type:       e.Type,
			Source:     e.Source,
			Target:     e.Target,
			Backup:     e.Backup,
			Optional:   e.Optional,
			Exclude:    e.Exclude,
			ID:         id,
			FromModule: e.FromModule,
		}

		// Apply filter: if filter is set, skip entries that don't match.
		// Inline entries (no FromModule) always pass the filter.
		if len(filterList) > 0 && action.FromModule != "" {
			matched := false
			for _, f := range filterList {
				if f == action.FromModule || "apps."+f == action.FromModule {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}

		actions = append(actions, action)
	}

	return actions
}
