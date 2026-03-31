// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// RunRecord is the structure persisted to state/runs/<runId>.json after each
// command execution.
type RunRecord struct {
	RunID     string      `json:"runId"`
	Timestamp string      `json:"timestamp"`
	Command   string      `json:"command"`
	DryRun    bool        `json:"dryRun"`
	Machine   string      `json:"machine,omitempty"`
	Manifest  ManifestRef `json:"manifest"`
	Summary   RunSummary  `json:"summary"`
	Actions   interface{} `json:"actions,omitempty"`
}

// ManifestRef identifies the manifest used for a run.
type ManifestRef struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Hash string `json:"hash"`
}

// RunSummary aggregates outcome counts for a run.
type RunSummary struct {
	Success int `json:"success"`
	Skipped int `json:"skipped"`
	Failed  int `json:"failed"`
}

// SaveRunHistory saves a run record to state/runs/<runId>.json using the
// atomic temp+rename pattern. The runDir is created if it does not exist.
func SaveRunHistory(runDir string, runID string, record *RunRecord) error {
	if err := os.MkdirAll(runDir, 0755); err != nil {
		return err
	}

	path := filepath.Join(runDir, runID+".json")
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}

	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

// ListRunHistory lists run history files sorted by name descending (newest
// first), limited to limit entries. If limit <= 0 all entries are returned.
// If the directory does not exist an empty slice is returned with no error.
func ListRunHistory(runDir string, limit int) ([]*RunRecord, error) {
	entries, err := os.ReadDir(runDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []*RunRecord{}, nil
		}
		return nil, err
	}

	// Collect only .json files, excluding .tmp files.
	var jsonFiles []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() {
			continue
		}
		if strings.HasSuffix(name, ".json") && !strings.HasSuffix(name, ".tmp") {
			jsonFiles = append(jsonFiles, name)
		}
	}

	// Sort descending by name (newest first, since names contain timestamps).
	sort.Sort(sort.Reverse(sort.StringSlice(jsonFiles)))

	// Apply limit.
	if limit > 0 && len(jsonFiles) > limit {
		jsonFiles = jsonFiles[:limit]
	}

	records := make([]*RunRecord, 0, len(jsonFiles))
	for _, name := range jsonFiles {
		path := filepath.Join(runDir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		var record RunRecord
		if err := json.Unmarshal(data, &record); err != nil {
			return nil, err
		}
		records = append(records, &record)
	}

	return records, nil
}

// GetRunHistory reads a specific run record by its ID from the runs directory.
func GetRunHistory(runDir string, runID string) (*RunRecord, error) {
	path := filepath.Join(runDir, runID+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var record RunRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return nil, err
	}
	return &record, nil
}
