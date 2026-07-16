// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/configrestore"
	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/events"
	"github.com/Artexis10/endstate/go-engine/internal/restore"
	"github.com/Artexis10/endstate/go-engine/internal/state"
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

type configRestoreRevertMember struct {
	member     *configrestore.StoreMember
	legacyPath string
	synthetic  bool
	ordinal    uint64
}

// RunRevert reverts the newest active restore run in exact reverse mutation
// order. Generation transactions and registered legacy journals share one
// store ordering; old unregistered journals remain supported.
func RunRevert(flags RevertFlags) (interface{}, *envelope.Error) {
	runID := buildRunID("revert")
	emitter := events.NewEmitter(runID, flags.Events == "jsonl")
	emitter.EmitPhase("revert")

	repoRoot := resolveRepoRootFn()
	logsDir := "logs"
	stateDir := state.StateDir()
	backupDir := ""
	if repoRoot != "" {
		logsDir = filepath.Join(repoRoot, "logs")
		stateDir = filepath.Join(repoRoot, "state")
		backupDir = filepath.Join(stateDir, "backups")
	}
	logsDir, _ = filepath.Abs(logsDir)
	stateDir, _ = filepath.Abs(stateDir)

	latestPath, latestFindErr := restore.FindLatestJournal(logsDir)
	var latestJournal *restore.Journal
	var latestReadErr error
	if latestFindErr == nil {
		latestJournal, latestReadErr = restore.ReadJournal(latestPath)
	}

	registry, _ := newConfigRestorePlatformAdapters()
	guard, beginErr := configrestore.BeginLive(context.Background(), filepath.Clean(stateDir), runID, registry)
	if beginErr != nil {
		detail := map[string]string{"reason": "recovery_failed", "diagnostic": beginErr.Error()}
		if errors.Is(beginErr, configrestore.ErrRecoveryRequired) {
			detail["reason"] = "recovery_required"
		}
		return nil, envelope.NewError(envelope.ErrRestoreFailed, "Configuration revert recovery failed.").
			WithDetail(detail).
			WithRemediation("Resolve the pending configuration recovery failure, then retry.")
	}
	defer guard.Close()

	runs, activeErr := guard.ActiveStoreRuns(context.Background())
	if activeErr != nil {
		return nil, envelope.NewError(envelope.ErrRestoreFailed, "Failed to inspect configuration restore history.").
			WithDetail(map[string]string{"reason": activeErr.Error()})
	}

	results := []restore.RevertResult{}
	journalUsed := ""
	newerStandalone := false
	if len(runs) > 0 {
		if latestFindErr != nil {
			absent, inspectionErr := configRestoreJournalAbsenceProven(logsDir)
			if !absent {
				return nil, configRestoreHistoryOrderError(errors.Join(latestFindErr, inspectionErr))
			}
		} else if latestReadErr != nil {
			return nil, configRestoreHistoryOrderError(latestReadErr)
		}
		var chronologyErr error
		newerStandalone, chronologyErr = newerStandaloneLegacyJournal(runs[0], latestJournal)
		if chronologyErr != nil {
			return nil, configRestoreHistoryOrderError(chronologyErr)
		}
	}
	useStoreRun := len(runs) > 0 && !newerStandalone
	if useStoreRun {
		members := configRestoreRevertMembers(runs[0], latestPath, latestJournal)
		for index := len(members) - 1; index >= 0; index-- {
			member := members[index]
			if member.legacyPath != "" {
				journal, readErr := restore.ReadJournal(member.legacyPath)
				if readErr != nil {
					return nil, envelope.NewError(envelope.ErrRestoreFailed, "Failed to read restore journal: "+readErr.Error())
				}
				legacyResults, revertErr := restore.RunRevert(journal, backupDir)
				if revertErr != nil {
					return nil, envelope.NewError(envelope.ErrRestoreFailed, revertErr.Error())
				}
				if !member.synthetic {
					if markErr := guard.MarkLegacyMemberReverted(context.Background(), member.member); markErr != nil {
						return nil, envelope.NewError(envelope.ErrRestoreFailed, "Failed to record legacy configuration revert.").
							WithDetail(map[string]string{"reason": markErr.Error()})
					}
				}
				results = append(results, legacyResults...)
				journalUsed = member.legacyPath
				continue
			}

			generation, revertErr := guard.RevertGenerationMember(context.Background(), member.member)
			if revertErr != nil {
				return nil, envelope.NewError(envelope.ErrRestoreFailed, "Failed to revert generation-aware configuration.").
					WithDetail(map[string]string{"reason": revertErr.Error()})
			}
			for _, action := range generation.Actions {
				revertAction := "deleted"
				if action.BackupUsed {
					revertAction = "reverted"
				}
				results = append(results, restore.RevertResult{Target: action.Target, Action: revertAction})
			}
		}
	} else {
		if latestFindErr != nil {
			return nil, envelope.NewError(
				envelope.ErrRestoreFailed,
				"No restore journal found. Nothing to revert.",
			).WithRemediation("Run a restore operation first before attempting to revert.")
		}
		if latestJournal == nil {
			var readErr error
			latestJournal, readErr = restore.ReadJournal(latestPath)
			if readErr != nil {
				return nil, envelope.NewError(envelope.ErrRestoreFailed, "Failed to read restore journal: "+readErr.Error())
			}
		}
		legacyResults, revertErr := restore.RunRevert(latestJournal, backupDir)
		if revertErr != nil {
			return nil, envelope.NewError(envelope.ErrRestoreFailed, revertErr.Error())
		}
		results, journalUsed = append(results, legacyResults...), latestPath
	}

	emitConfigRevertResults(emitter, results)
	return &RevertData{Results: results, JournalUsed: journalUsed}, nil
}

func configRestoreHistoryOrderError(err error) *envelope.Error {
	diagnostic := "restore history could not be inspected"
	if err != nil {
		diagnostic = err.Error()
	}
	return envelope.NewError(envelope.ErrRestoreFailed, "Configuration restore history cannot be ordered safely.").
		WithDetail(map[string]string{"reason": "history_order_unknown", "diagnostic": diagnostic}).
		WithRemediation("Inspect the latest restore journal and its timestamp before retrying revert.")
}

func configRestoreJournalAbsenceProven(logsDir string) (bool, error) {
	entries, err := os.ReadDir(logsDir)
	if os.IsNotExist(err) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	for _, entry := range entries {
		name := entry.Name()
		if !entry.IsDir() && strings.HasPrefix(name, "restore-journal-") && strings.HasSuffix(name, ".json") {
			return false, fmt.Errorf("a restore journal exists but latest-journal selection failed")
		}
	}
	return true, nil
}

func configRestoreRevertMembers(
	run *configrestore.StoreRun,
	latestPath string,
	latestJournal *restore.Journal,
) []configRestoreRevertMember {
	if run == nil {
		return []configRestoreRevertMember{}
	}
	members := run.Members()
	result := make([]configRestoreRevertMember, 0, len(members)+1)
	registeredLatest := false
	var lastOrdinal uint64
	for _, member := range members {
		entry := configRestoreRevertMember{member: member, ordinal: member.Ordinal()}
		if member.Kind() == configrestore.StoreMemberLegacy {
			entry.legacyPath = member.LegacyJournalPath()
			registeredLatest = registeredLatest ||
				(latestPath != "" && filepath.Clean(entry.legacyPath) == filepath.Clean(latestPath))
		}
		if entry.ordinal >= lastOrdinal {
			lastOrdinal = entry.ordinal
		}
		result = append(result, entry)
	}
	if latestJournal != nil && latestJournal.RunID == run.RunID() && !registeredLatest {
		result = append(result, configRestoreRevertMember{
			legacyPath: latestPath, synthetic: true, ordinal: lastOrdinal + 1,
		})
	}
	return result
}

func newerStandaloneLegacyJournal(run *configrestore.StoreRun, journal *restore.Journal) (bool, error) {
	if run == nil || journal == nil || journal.RunID == run.RunID() {
		return false, nil
	}
	timestamp, err := time.Parse(time.RFC3339, journal.Timestamp)
	if err != nil {
		return false, fmt.Errorf("latest standalone journal has invalid timestamp: %w", err)
	}
	startedSecond := run.StartedAt().UTC().Truncate(time.Second)
	if timestamp.Equal(startedSecond) {
		return false, fmt.Errorf("latest standalone journal and active store run share an unorderable UTC second")
	}
	return timestamp.After(startedSecond), nil
}

func emitConfigRevertResults(emitter *events.Emitter, results []restore.RevertResult) {
	revertedCount, skippedCount := 0, 0
	for _, result := range results {
		switch result.Action {
		case "reverted":
			emitter.EmitItem(result.Target, "restore", "reverted", "", "Restored from backup", "")
			revertedCount++
		case "deleted":
			emitter.EmitItem(result.Target, "restore", "deleted", "", "Deleted created target", "")
			revertedCount++
		default:
			emitter.EmitItem(result.Target, "restore", "skipped", "", "Nothing to revert", "")
			skippedCount++
		}
	}
	emitter.EmitSummary("revert", len(results), revertedCount, skippedCount, 0)
}
