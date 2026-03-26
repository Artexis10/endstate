// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"path/filepath"

	"github.com/Artexis10/endstate/go-engine/internal/config"
	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/events"
	"github.com/Artexis10/endstate/go-engine/internal/restore"
)

// RevertFlags holds the parsed CLI flags for the revert command.
type RevertFlags struct {
	// Events controls streaming event output. "jsonl" enables it; "" disables.
	Events string
}

// RevertData is the data payload for the revert command JSON envelope.
type RevertData struct {
	Results     []restore.RevertResult `json:"results"`
	JournalUsed string                 `json:"journalUsed"`
}

// RunRevert executes the revert command: finds the latest restore journal,
// reads it, runs revert, and returns an envelope.
func RunRevert(flags RevertFlags) (interface{}, *envelope.Error) {
	runID := buildRunID("revert")
	emitter := events.NewEmitter(runID, flags.Events == "jsonl")

	emitter.EmitPhase("revert")

	// --- 1. Find latest journal ---
	repoRoot := config.ResolveRepoRoot()
	logsDir := "logs"
	if repoRoot != "" {
		logsDir = filepath.Join(repoRoot, "logs")
	}

	journalPath, findErr := restore.FindLatestJournal(logsDir)
	if findErr != nil {
		return nil, envelope.NewError(
			envelope.ErrRestoreFailed,
			"No restore journal found. Nothing to revert.",
		).WithRemediation("Run a restore operation first before attempting to revert.")
	}

	// --- 2. Read journal ---
	journal, readErr := restore.ReadJournal(journalPath)
	if readErr != nil {
		return nil, envelope.NewError(
			envelope.ErrRestoreFailed,
			"Failed to read restore journal: "+readErr.Error(),
		)
	}

	// --- 3. Run revert ---
	backupDir := ""
	if repoRoot != "" {
		backupDir = filepath.Join(repoRoot, "state", "backups")
	}

	results, revertErr := restore.RunRevert(journal, backupDir)
	if revertErr != nil {
		return nil, envelope.NewError(envelope.ErrRestoreFailed, revertErr.Error())
	}

	// --- 4. Emit events ---
	revertedCount := 0
	skippedCount := 0
	for _, r := range results {
		switch r.Action {
		case "reverted":
			emitter.EmitItem(r.Target, "restore", "reverted", "", "Restored from backup", "")
			revertedCount++
		case "deleted":
			emitter.EmitItem(r.Target, "restore", "deleted", "", "Deleted created target", "")
			revertedCount++
		default:
			emitter.EmitItem(r.Target, "restore", "skipped", "", "Nothing to revert", "")
			skippedCount++
		}
	}

	emitter.EmitSummary("revert", len(results), revertedCount, skippedCount, 0)

	return &RevertData{
		Results:     results,
		JournalUsed: journalPath,
	}, nil
}
