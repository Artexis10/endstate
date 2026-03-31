// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// ---------------------------------------------------------------------------
// State tests
// ---------------------------------------------------------------------------

func TestWriteState_AtomicWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	s := &State{
		SchemaVersion: "1.0",
		LastRunID:     "apply-20241220-143052",
		LastCommand:   "apply",
		LastTimestamp:  "2024-12-20T14:30:52Z",
		RunCount:      5,
	}

	if err := WriteState(path, s); err != nil {
		t.Fatalf("WriteState failed: %v", err)
	}

	// Verify the file exists and has correct content.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read state file: %v", err)
	}

	var got State
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("failed to unmarshal state: %v", err)
	}

	if got.SchemaVersion != "1.0" {
		t.Errorf("expected schemaVersion=1.0, got %q", got.SchemaVersion)
	}
	if got.LastRunID != "apply-20241220-143052" {
		t.Errorf("expected lastRunId=apply-20241220-143052, got %q", got.LastRunID)
	}
	if got.LastCommand != "apply" {
		t.Errorf("expected lastCommand=apply, got %q", got.LastCommand)
	}
	if got.RunCount != 5 {
		t.Errorf("expected runCount=5, got %d", got.RunCount)
	}

	// Verify no .tmp file remains.
	tmpPath := path + ".tmp"
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Error("expected .tmp file to be removed after atomic write")
	}
}

func TestReadState_MissingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nonexistent.json")

	s, err := ReadState(path)
	if err != nil {
		t.Fatalf("ReadState returned unexpected error for missing file: %v", err)
	}
	if s.SchemaVersion != "1.0" {
		t.Errorf("expected default schemaVersion=1.0, got %q", s.SchemaVersion)
	}
	if s.LastRunID != "" {
		t.Errorf("expected empty lastRunId, got %q", s.LastRunID)
	}
	if s.RunCount != 0 {
		t.Errorf("expected runCount=0, got %d", s.RunCount)
	}
}

func TestReadState_ValidFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	original := &State{
		SchemaVersion: "1.0",
		LastRunID:     "verify-20241221-100000",
		LastCommand:   "verify",
		LastTimestamp:  "2024-12-21T10:00:00Z",
		RunCount:      3,
	}

	if err := WriteState(path, original); err != nil {
		t.Fatalf("WriteState failed: %v", err)
	}

	got, err := ReadState(path)
	if err != nil {
		t.Fatalf("ReadState failed: %v", err)
	}

	if got.SchemaVersion != original.SchemaVersion {
		t.Errorf("schemaVersion mismatch: got %q, want %q", got.SchemaVersion, original.SchemaVersion)
	}
	if got.LastRunID != original.LastRunID {
		t.Errorf("lastRunId mismatch: got %q, want %q", got.LastRunID, original.LastRunID)
	}
	if got.LastCommand != original.LastCommand {
		t.Errorf("lastCommand mismatch: got %q, want %q", got.LastCommand, original.LastCommand)
	}
	if got.LastTimestamp != original.LastTimestamp {
		t.Errorf("lastTimestamp mismatch: got %q, want %q", got.LastTimestamp, original.LastTimestamp)
	}
	if got.RunCount != original.RunCount {
		t.Errorf("runCount mismatch: got %d, want %d", got.RunCount, original.RunCount)
	}
}

// ---------------------------------------------------------------------------
// History tests
// ---------------------------------------------------------------------------

func TestSaveRunHistory(t *testing.T) {
	dir := t.TempDir()
	runDir := filepath.Join(dir, "runs")

	record := &RunRecord{
		RunID:     "apply-20241220-143052",
		Timestamp: "2024-12-20T14:30:52Z",
		Command:   "apply",
		DryRun:    false,
		Manifest: ManifestRef{
			Name: "test-manifest",
			Path: "/path/to/manifest.jsonc",
			Hash: "abc123",
		},
		Summary: RunSummary{
			Success: 10,
			Skipped: 2,
			Failed:  1,
		},
	}

	if err := SaveRunHistory(runDir, record.RunID, record); err != nil {
		t.Fatalf("SaveRunHistory failed: %v", err)
	}

	// Verify file exists with correct content.
	path := filepath.Join(runDir, record.RunID+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read run history file: %v", err)
	}

	var got RunRecord
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("failed to unmarshal run record: %v", err)
	}

	if got.RunID != record.RunID {
		t.Errorf("runId mismatch: got %q, want %q", got.RunID, record.RunID)
	}
	if got.Command != "apply" {
		t.Errorf("command mismatch: got %q, want %q", got.Command, "apply")
	}
	if got.Summary.Success != 10 {
		t.Errorf("summary.success mismatch: got %d, want %d", got.Summary.Success, 10)
	}
	if got.Summary.Skipped != 2 {
		t.Errorf("summary.skipped mismatch: got %d, want %d", got.Summary.Skipped, 2)
	}
	if got.Summary.Failed != 1 {
		t.Errorf("summary.failed mismatch: got %d, want %d", got.Summary.Failed, 1)
	}

	// Verify no .tmp file remains.
	tmpPath := path + ".tmp"
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Error("expected .tmp file to be removed after atomic write")
	}
}

func TestListRunHistory_SortOrder(t *testing.T) {
	dir := t.TempDir()
	runDir := filepath.Join(dir, "runs")

	// Save 3 records with different timestamps (names sort lexicographically).
	records := []*RunRecord{
		{RunID: "apply-20241218-100000", Timestamp: "2024-12-18T10:00:00Z", Command: "apply"},
		{RunID: "apply-20241220-143052", Timestamp: "2024-12-20T14:30:52Z", Command: "apply"},
		{RunID: "apply-20241219-120000", Timestamp: "2024-12-19T12:00:00Z", Command: "apply"},
	}

	for _, r := range records {
		if err := SaveRunHistory(runDir, r.RunID, r); err != nil {
			t.Fatalf("SaveRunHistory failed for %s: %v", r.RunID, err)
		}
	}

	got, err := ListRunHistory(runDir, 0)
	if err != nil {
		t.Fatalf("ListRunHistory failed: %v", err)
	}

	if len(got) != 3 {
		t.Fatalf("expected 3 records, got %d", len(got))
	}

	// Verify newest-first order.
	if got[0].RunID != "apply-20241220-143052" {
		t.Errorf("expected first record to be newest, got %q", got[0].RunID)
	}
	if got[1].RunID != "apply-20241219-120000" {
		t.Errorf("expected second record to be middle, got %q", got[1].RunID)
	}
	if got[2].RunID != "apply-20241218-100000" {
		t.Errorf("expected third record to be oldest, got %q", got[2].RunID)
	}
}

func TestListRunHistory_Limit(t *testing.T) {
	dir := t.TempDir()
	runDir := filepath.Join(dir, "runs")

	// Save 5 records.
	for i := 1; i <= 5; i++ {
		r := &RunRecord{
			RunID:   "apply-2024121" + string(rune('0'+i)) + "-100000",
			Command: "apply",
		}
		if err := SaveRunHistory(runDir, r.RunID, r); err != nil {
			t.Fatalf("SaveRunHistory failed: %v", err)
		}
	}

	got, err := ListRunHistory(runDir, 2)
	if err != nil {
		t.Fatalf("ListRunHistory failed: %v", err)
	}

	if len(got) != 2 {
		t.Errorf("expected 2 records with limit=2, got %d", len(got))
	}
}

func TestGetRunHistory(t *testing.T) {
	dir := t.TempDir()
	runDir := filepath.Join(dir, "runs")

	record := &RunRecord{
		RunID:     "verify-20241221-090000",
		Timestamp: "2024-12-21T09:00:00Z",
		Command:   "verify",
		DryRun:    false,
		Manifest: ManifestRef{
			Name: "my-manifest",
			Path: "/some/path.jsonc",
			Hash: "def456",
		},
		Summary: RunSummary{
			Success: 5,
			Skipped: 0,
			Failed:  0,
		},
	}

	if err := SaveRunHistory(runDir, record.RunID, record); err != nil {
		t.Fatalf("SaveRunHistory failed: %v", err)
	}

	got, err := GetRunHistory(runDir, record.RunID)
	if err != nil {
		t.Fatalf("GetRunHistory failed: %v", err)
	}

	if got.RunID != record.RunID {
		t.Errorf("runId mismatch: got %q, want %q", got.RunID, record.RunID)
	}
	if got.Command != "verify" {
		t.Errorf("command mismatch: got %q, want %q", got.Command, "verify")
	}
	if got.Summary.Success != 5 {
		t.Errorf("summary.success mismatch: got %d, want %d", got.Summary.Success, 5)
	}
	if got.Manifest.Name != "my-manifest" {
		t.Errorf("manifest.name mismatch: got %q, want %q", got.Manifest.Name, "my-manifest")
	}
}

func TestListRunHistory_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	runDir := filepath.Join(dir, "runs")

	// Directory does not exist — should return empty slice, no error.
	got, err := ListRunHistory(runDir, 0)
	if err != nil {
		t.Fatalf("ListRunHistory returned unexpected error for missing dir: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil slice for empty dir")
	}
	if len(got) != 0 {
		t.Errorf("expected 0 records for empty dir, got %d", len(got))
	}
}
