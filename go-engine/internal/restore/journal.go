// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package restore

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Journal records the results of a restore run for use by the revert command.
type Journal struct {
	RunID        string         `json:"runId"`
	Timestamp    string         `json:"timestamp"`
	ManifestPath string         `json:"manifestPath"`
	ManifestDir  string         `json:"manifestDir"`
	ExportRoot   string         `json:"exportRoot,omitempty"`
	Entries      []JournalEntry `json:"entries"`
}

// JournalEntry records the outcome of a single restore action in the journal.
type JournalEntry struct {
	ResolvedSourcePath string `json:"resolvedSourcePath"`
	TargetPath         string `json:"targetPath"`
	TargetExistedBefore bool  `json:"targetExistedBefore"`
	BackupRequested    bool   `json:"backupRequested"`
	BackupCreated      bool   `json:"backupCreated"`
	BackupPath         string `json:"backupPath,omitempty"`
	Action             string `json:"action"`
	Error              string `json:"error,omitempty"`
}

// WriteJournal writes a restore journal to logsDir as an atomic temp+rename
// operation. The journal filename is restore-journal-<runID>.json.
func WriteJournal(logsDir, runID, manifestPath, manifestDir, exportRoot string, results []RestoreResult) error {
	if err := os.MkdirAll(logsDir, 0755); err != nil {
		return fmt.Errorf("cannot create logs directory: %w", err)
	}

	entries := make([]JournalEntry, 0, len(results))
	for _, r := range results {
		entry := JournalEntry{
			ResolvedSourcePath:  r.Source,
			TargetPath:          r.Target,
			TargetExistedBefore: r.TargetExistedBefore,
			BackupRequested:     r.BackupCreated || r.BackupPath != "", // infer from result
			BackupCreated:       r.BackupCreated,
			BackupPath:          r.BackupPath,
			Action:              r.Status,
		}
		if r.Status == "failed" {
			entry.Error = r.Error
		}
		entries = append(entries, entry)
	}

	journal := Journal{
		RunID:        runID,
		Timestamp:    time.Now().UTC().Format(time.RFC3339),
		ManifestPath: manifestPath,
		ManifestDir:  manifestDir,
		ExportRoot:   exportRoot,
		Entries:      entries,
	}

	data, err := json.MarshalIndent(journal, "", "  ")
	if err != nil {
		return fmt.Errorf("cannot marshal journal: %w", err)
	}

	journalFile := filepath.Join(logsDir, fmt.Sprintf("restore-journal-%s.json", runID))
	tmpFile := journalFile + ".tmp"

	if err := os.WriteFile(tmpFile, data, 0644); err != nil {
		return fmt.Errorf("cannot write journal temp file: %w", err)
	}

	if err := os.Rename(tmpFile, journalFile); err != nil {
		os.Remove(tmpFile)
		return fmt.Errorf("cannot rename journal file: %w", err)
	}

	return nil
}

// ReadJournal reads and parses a restore journal from the given path.
func ReadJournal(path string) (*Journal, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read journal: %w", err)
	}

	var journal Journal
	if err := json.Unmarshal(data, &journal); err != nil {
		return nil, fmt.Errorf("cannot parse journal: %w", err)
	}

	return &journal, nil
}

// FindLatestJournal finds the most recent restore-journal-*.json file in
// logsDir, sorted by filename (which includes a timestamp in the runId).
// Returns the full path to the journal file, or an error if none exist.
func FindLatestJournal(logsDir string) (string, error) {
	entries, err := os.ReadDir(logsDir)
	if err != nil {
		return "", fmt.Errorf("cannot read logs directory: %w", err)
	}

	var journalFiles []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasPrefix(name, "restore-journal-") && strings.HasSuffix(name, ".json") {
			journalFiles = append(journalFiles, name)
		}
	}

	if len(journalFiles) == 0 {
		return "", fmt.Errorf("no restore journals found in %s", logsDir)
	}

	// Sort by filename descending — the most recent timestamp is last
	// alphabetically.
	sort.Strings(journalFiles)

	latest := journalFiles[len(journalFiles)-1]
	return filepath.Join(logsDir, latest), nil
}
