// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/events"
	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/restore"
	"github.com/Artexis10/endstate/go-engine/internal/state"
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
	// RestoreTargets contains repeatable capture-to-target mappings. Command
	// orchestration validates these against generation-aware capture IDs.
	RestoreTargets []string
}

// RestoreData is the data payload for the restore command JSON envelope.
type RestoreData struct {
	Results     []restore.RestoreResult `json:"results"`
	JournalPath string                  `json:"journalPath,omitempty"`
	DryRun      bool                    `json:"dryRun"`
	*ConfigResultFields
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
	repoRoot := resolveRepoRootFn()
	configRuntime, configInputErr := newConfigRestoreRuntime(configRestoreBuildRequest{
		Manifest:       mf,
		ManifestPath:   flags.Manifest,
		RepoRoot:       repoRoot,
		RestoreFilter:  flags.RestoreFilter,
		RestoreTargets: flags.RestoreTargets,
	})
	if configInputErr != nil {
		return nil, configInputErr
	}

	if !configRuntime.inputs.hasConfigPayloads && len(mf.Restore) == 0 {
		return &RestoreData{
			Results: []restore.RestoreResult{},
			DryRun:  flags.DryRun,
		}, nil
	}

	// Check opt-in.
	if !flags.EnableRestore && !configRuntime.inputs.hasConfigPayloads {
		return nil, envelope.NewError(
			envelope.ErrRestoreFailed,
			"Restore entries found but --enable-restore not specified.",
		).WithRemediation("Add --enable-restore to enable restore operations.")
	}

	evidence := newStandaloneConfigRestoreEvidenceSource(mf.Apps)
	session := newConfigRestoreExecutionSession(configRuntime, evidence)
	if _, previewErr := session.Preview(context.Background()); previewErr != nil {
		return nil, configRestoreInternalError(previewErr.Error())
	}
	options := restoreConfigRestoreExecutionOptions(flags, runID, repoRoot, emitter)
	execution, executeErr := session.Execute(context.Background(), options)
	if executeErr != nil {
		return nil, executeErr
	}
	var configFields *ConfigResultFields
	if configRuntime.inputs.hasConfigPayloads {
		configFields = NewConfigResultFields(execution.Plan.Sets, execution.RestoreItems)
	}

	return &RestoreData{
		Results:            execution.Results,
		JournalPath:        execution.JournalPath,
		DryRun:             flags.DryRun,
		ConfigResultFields: configFields,
	}, nil
}

func restoreConfigRestoreExecutionOptions(
	flags RestoreFlags,
	runID string,
	repoRoot string,
	emitter *events.Emitter,
) configRestoreExecutionOptions {
	manifestDir, _ := filepath.Abs(filepath.Dir(flags.Manifest))
	exportRoot := ""
	if flags.Export != "" {
		exportRoot, _ = filepath.Abs(flags.Export)
	}
	stateDir := state.StateDir()
	if repoRoot != "" {
		stateDir = filepath.Join(repoRoot, "state")
	}
	stateDir, _ = filepath.Abs(stateDir)
	logsDir := ""
	if repoRoot != "" {
		logsDir = filepath.Join(repoRoot, "logs")
	}
	options := configRestoreExecutionOptions{
		RestoreEnabled: flags.EnableRestore, DryRun: flags.DryRun,
		RunID: runID, StateDir: stateDir, ManifestPath: flags.Manifest,
		ManifestDir: manifestDir, ExportRoot: exportRoot,
		BackupDir: filepath.Join(stateDir, "backups", runID), JournalLogsDir: logsDir,
		Emitter: emitter,
	}
	options.Registry, options.ProcessObserver = newConfigRestorePlatformAdapters()
	return options
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
		var id string
		switch restoreType {
		case "delete-glob":
			id = fmt.Sprintf("delete-glob:%s/%s", filepath.ToSlash(e.Target), e.Pattern)
		case "registry-set":
			id = fmt.Sprintf("registry-set:%s\\%s", e.Key, e.ValueName)
		default:
			id = fmt.Sprintf("%s:%s->%s", restoreType, filepath.ToSlash(e.Source), filepath.ToSlash(e.Target))
		}

		action := restore.RestoreAction{
			Type:       e.Type,
			Source:     e.Source,
			Target:     e.Target,
			Pattern:    e.Pattern,
			Reason:     e.Reason,
			Backup:     e.Backup,
			Optional:   e.Optional,
			Exclude:    e.Exclude,
			ID:         id,
			FromModule: e.FromModule,
			Key:        e.Key,
			ValueName:  e.ValueName,
			ValueType:  e.ValueType,
			Data:       e.Data,
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
