// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/state"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// createTestRunFile writes a run record JSON file into dir.
func createTestRunFile(t *testing.T, dir string, record *state.RunRecord) {
	t.Helper()
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, record.RunID+".json")
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
}

// ---------------------------------------------------------------------------
// Report.Command.FileSelection (mirrors Pester Report.Command.FileSelection)
// ---------------------------------------------------------------------------

func TestRunReport_Latest_SelectsNewest(t *testing.T) {
	tmpDir := t.TempDir()
	runDir := filepath.Join(tmpDir, "runs")

	createTestRunFile(t, runDir, &state.RunRecord{
		RunID: "20251201-100000", Timestamp: "2025-12-01T10:00:00Z",
		Command: "apply", Manifest: state.ManifestRef{Path: "test.jsonc", Hash: "ABC"},
		Summary: state.RunSummary{Success: 5, Skipped: 10},
	})
	createTestRunFile(t, runDir, &state.RunRecord{
		RunID: "20251215-120000", Timestamp: "2025-12-15T12:00:00Z",
		Command: "apply", DryRun: true, Manifest: state.ManifestRef{Path: "test.jsonc", Hash: "DEF"},
		Summary: state.RunSummary{Success: 3, Skipped: 7, Failed: 1},
	})
	createTestRunFile(t, runDir, &state.RunRecord{
		RunID: "20251219-080000", Timestamp: "2025-12-19T08:00:00Z",
		Command: "apply", Manifest: state.ManifestRef{Path: "prod.jsonc", Hash: "GHI"},
		Summary: state.RunSummary{Success: 10, Skipped: 20},
	})

	records, err := state.ListRunHistory(runDir, 1)
	if err != nil {
		t.Fatalf("ListRunHistory failed: %v", err)
	}

	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].RunID != "20251219-080000" {
		t.Errorf("expected most recent runId=20251219-080000, got %q", records[0].RunID)
	}
}

func TestRunReport_RunID_SelectsCorrectFile(t *testing.T) {
	tmpDir := t.TempDir()
	runDir := filepath.Join(tmpDir, "runs")

	createTestRunFile(t, runDir, &state.RunRecord{
		RunID: "20251201-100000", Timestamp: "2025-12-01T10:00:00Z",
		Command: "apply",
	})
	createTestRunFile(t, runDir, &state.RunRecord{
		RunID: "20251215-120000", Timestamp: "2025-12-15T12:00:00Z",
		Command: "apply", DryRun: true,
	})

	record, err := state.GetRunHistory(runDir, "20251215-120000")
	if err != nil {
		t.Fatalf("GetRunHistory failed: %v", err)
	}
	if record.RunID != "20251215-120000" {
		t.Errorf("expected runId=20251215-120000, got %q", record.RunID)
	}
	if !record.DryRun {
		t.Error("expected dryRun=true")
	}
}

func TestRunReport_RunID_NonExistent_ReturnsError(t *testing.T) {
	tmpDir := t.TempDir()
	runDir := filepath.Join(tmpDir, "runs")
	if err := os.MkdirAll(runDir, 0755); err != nil {
		t.Fatal(err)
	}

	_, err := state.GetRunHistory(runDir, "99999999-999999")
	if err == nil {
		t.Error("expected error for non-existent runId, got nil")
	}
}

func TestRunReport_Last2_Returns2MostRecent(t *testing.T) {
	tmpDir := t.TempDir()
	runDir := filepath.Join(tmpDir, "runs")

	for _, rid := range []string{"20251201-100000", "20251215-120000", "20251219-080000"} {
		createTestRunFile(t, runDir, &state.RunRecord{
			RunID: rid, Timestamp: rid, Command: "apply",
		})
	}

	records, err := state.ListRunHistory(runDir, 2)
	if err != nil {
		t.Fatalf("ListRunHistory failed: %v", err)
	}

	if len(records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(records))
	}
	if records[0].RunID != "20251219-080000" {
		t.Errorf("expected first record to be newest, got %q", records[0].RunID)
	}
	if records[1].RunID != "20251215-120000" {
		t.Errorf("expected second record to be second-newest, got %q", records[1].RunID)
	}
}

func TestRunReport_LastExceedsCount_ReturnsAll(t *testing.T) {
	tmpDir := t.TempDir()
	runDir := filepath.Join(tmpDir, "runs")

	for _, rid := range []string{"20251201-100000", "20251215-120000", "20251219-080000"} {
		createTestRunFile(t, runDir, &state.RunRecord{
			RunID: rid, Command: "apply",
		})
	}

	records, err := state.ListRunHistory(runDir, 100)
	if err != nil {
		t.Fatalf("ListRunHistory failed: %v", err)
	}

	if len(records) != 3 {
		t.Errorf("expected 3 records when limit exceeds count, got %d", len(records))
	}
}

// ---------------------------------------------------------------------------
// Report.Command.EmptyState (mirrors Pester Report.Command.EmptyState)
// ---------------------------------------------------------------------------

func TestRunReport_EmptyDirectory_ReturnsEmpty(t *testing.T) {
	tmpDir := t.TempDir()
	runDir := filepath.Join(tmpDir, "runs")
	if err := os.MkdirAll(runDir, 0755); err != nil {
		t.Fatal(err)
	}

	records, err := state.ListRunHistory(runDir, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(records) != 0 {
		t.Errorf("expected 0 records for empty dir, got %d", len(records))
	}
}

func TestRunReport_NonExistentDirectory_ReturnsEmpty(t *testing.T) {
	records, err := state.ListRunHistory("/nonexistent/path/runs", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(records) != 0 {
		t.Errorf("expected 0 records for nonexistent dir, got %d", len(records))
	}
}

// ---------------------------------------------------------------------------
// Report.Command.JsonOutput structure (mirrors Pester Report.Command.JsonOutput)
// ---------------------------------------------------------------------------

func TestRunReport_ReportResult_IncludesCorrectFields(t *testing.T) {
	tmpDir := t.TempDir()
	runDir := filepath.Join(tmpDir, "runs")

	createTestRunFile(t, runDir, &state.RunRecord{
		RunID: "20251219-090000", Timestamp: "2025-12-19T09:00:00Z",
		Command: "apply",
		Manifest: state.ManifestRef{
			Path: ".\\manifests\\test.jsonc",
			Hash: "XYZ999",
		},
		Summary: state.RunSummary{Success: 5, Skipped: 10, Failed: 2},
	})

	records, err := state.ListRunHistory(runDir, 1)
	if err != nil {
		t.Fatalf("ListRunHistory failed: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}

	r := records[0]
	if r.RunID != "20251219-090000" {
		t.Errorf("expected runId=20251219-090000, got %q", r.RunID)
	}
	if r.Summary.Success != 5 {
		t.Errorf("expected summary.success=5, got %d", r.Summary.Success)
	}
	if r.Summary.Skipped != 10 {
		t.Errorf("expected summary.skipped=10, got %d", r.Summary.Skipped)
	}
	if r.Summary.Failed != 2 {
		t.Errorf("expected summary.failed=2, got %d", r.Summary.Failed)
	}
	if r.Manifest.Hash != "XYZ999" {
		t.Errorf("expected manifest.hash=XYZ999, got %q", r.Manifest.Hash)
	}
	if r.Manifest.Path != ".\\manifests\\test.jsonc" {
		t.Errorf("expected manifest.path, got %q", r.Manifest.Path)
	}
}

// ---------------------------------------------------------------------------
// RunReport command (the actual command function)
// ---------------------------------------------------------------------------

func TestRunReport_DefaultReturnsReportResult(t *testing.T) {
	// RunReport reads from the real state dir, which may or may not have data.
	// We just verify it returns a non-nil result and no error.
	result, err := RunReport(ReportFlags{Latest: true})
	if err != nil {
		t.Fatalf("RunReport returned unexpected error: %v", err)
	}

	rr, ok := result.(*ReportResult)
	if !ok {
		t.Fatalf("expected *ReportResult, got %T", result)
	}

	if rr.Reports == nil {
		t.Error("expected reports to be non-nil (empty slice)")
	}
}

// ---------------------------------------------------------------------------
// Report.Command.MutualExclusion (mirrors Pester parameter validation)
// ---------------------------------------------------------------------------

func TestRunReport_ReportResultShape(t *testing.T) {
	// Verify the JSON shape of ReportResult serializes correctly.
	rr := &ReportResult{Reports: []*state.RunRecord{}}
	data, err := json.Marshal(rr)
	if err != nil {
		t.Fatalf("failed to marshal ReportResult: %v", err)
	}

	var parsed map[string]interface{}
	if jsonErr := json.Unmarshal(data, &parsed); jsonErr != nil {
		t.Fatalf("ReportResult is not valid JSON: %v", jsonErr)
	}

	if _, ok := parsed["reports"]; !ok {
		t.Error("expected 'reports' key in serialized ReportResult")
	}
}

// ---------------------------------------------------------------------------
// SaveRunHistory + ListRunHistory round-trip
// ---------------------------------------------------------------------------

func TestRunReport_SaveAndListRoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	runDir := filepath.Join(tmpDir, "runs")

	record := &state.RunRecord{
		RunID:     "20260101-120000",
		Timestamp: "2026-01-01T12:00:00Z",
		Command:   "apply",
		Manifest:  state.ManifestRef{Path: "test.jsonc", Hash: "hash123", Name: "test"},
		Summary:   state.RunSummary{Success: 3, Skipped: 1, Failed: 0},
	}

	if err := state.SaveRunHistory(runDir, record.RunID, record); err != nil {
		t.Fatalf("SaveRunHistory failed: %v", err)
	}

	records, err := state.ListRunHistory(runDir, 10)
	if err != nil {
		t.Fatalf("ListRunHistory failed: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record after save, got %d", len(records))
	}
	if records[0].RunID != "20260101-120000" {
		t.Errorf("expected runId=20260101-120000, got %q", records[0].RunID)
	}
	if records[0].Manifest.Name != "test" {
		t.Errorf("expected manifest.name=test, got %q", records[0].Manifest.Name)
	}
}
