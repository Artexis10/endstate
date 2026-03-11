// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package events

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// captureEmitter returns an Emitter that writes to a buffer and the buffer
// itself, so tests can inspect emitted output.
func captureEmitter(runID string) (*Emitter, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	return NewEmitterWithWriter(runID, true, buf), buf
}

// lastLine extracts the last non-empty line from buf. Events are always written
// one per line so this reliably returns the most-recently emitted event.
func lastLine(buf *bytes.Buffer) string {
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(lines[i]) != "" {
			return lines[i]
		}
	}
	return ""
}

// parseEvent parses a single NDJSON line into a generic map.
func parseEvent(t *testing.T, line string) map[string]interface{} {
	t.Helper()
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(line), &m); err != nil {
		t.Fatalf("event is not valid JSON: %v\nraw: %q", err, line)
	}
	return m
}

// assertBaseFields checks that all required base fields are present and correct.
func assertBaseFields(t *testing.T, ev map[string]interface{}, wantRunID, wantEventType string) {
	t.Helper()

	v, ok := ev["version"]
	if !ok {
		t.Error("missing required field: version")
	} else if v != float64(1) {
		t.Errorf("version = %v, want 1", v)
	}

	runID, ok := ev["runId"]
	if !ok {
		t.Error("missing required field: runId")
	} else if runID != wantRunID {
		t.Errorf("runId = %v, want %q", runID, wantRunID)
	}

	ts, ok := ev["timestamp"]
	if !ok {
		t.Error("missing required field: timestamp")
	} else {
		tsStr, _ := ts.(string)
		if _, err := time.Parse(time.RFC3339Nano, tsStr); err != nil {
			// Try plain RFC3339 as well (some implementations omit sub-seconds)
			if _, err2 := time.Parse(time.RFC3339, tsStr); err2 != nil {
				t.Errorf("timestamp %q is not valid RFC3339: %v", tsStr, err)
			}
		}
	}

	eventType, ok := ev["event"]
	if !ok {
		t.Error("missing required field: event")
	} else if eventType != wantEventType {
		t.Errorf("event = %v, want %q", eventType, wantEventType)
	}
}

// ---------------------------------------------------------------------------
// Phase event
// ---------------------------------------------------------------------------

func TestEmitPhase(t *testing.T) {
	runID := "apply-20250101-120000-TEST"
	em, buf := captureEmitter(runID)
	em.EmitPhase("apply")

	line := lastLine(buf)
	if line == "" {
		t.Fatal("no output written")
	}
	ev := parseEvent(t, line)
	assertBaseFields(t, ev, runID, "phase")

	if ev["phase"] != "apply" {
		t.Errorf("phase = %v, want %q", ev["phase"], "apply")
	}
}

func TestEmitPhaseIsNDJSON(t *testing.T) {
	em, buf := captureEmitter("test-run")
	em.EmitPhase("plan")
	em.EmitPhase("apply")

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d: %q", len(lines), buf.String())
	}
	for i, l := range lines {
		var m map[string]interface{}
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			t.Errorf("line %d is not valid JSON: %v", i, err)
		}
	}
}

// ---------------------------------------------------------------------------
// Item event
// ---------------------------------------------------------------------------

func TestEmitItem(t *testing.T) {
	runID := "apply-test"
	em, buf := captureEmitter(runID)
	em.EmitItem("Microsoft.VSCode", "winget", "installed", "", "Installed successfully")

	line := lastLine(buf)
	ev := parseEvent(t, line)
	assertBaseFields(t, ev, runID, "item")

	tests := []struct {
		field string
		want  interface{}
	}{
		{"id", "Microsoft.VSCode"},
		{"driver", "winget"},
		{"status", "installed"},
		{"message", "Installed successfully"},
	}
	for _, tc := range tests {
		if ev[tc.field] != tc.want {
			t.Errorf("field %q = %v, want %v", tc.field, ev[tc.field], tc.want)
		}
	}
	// reason field must be present (even if empty)
	if _, ok := ev["reason"]; !ok {
		t.Error("required field 'reason' missing from item event")
	}
}

func TestEmitItemWithReason(t *testing.T) {
	em, buf := captureEmitter("test")
	em.EmitItem("Pkg.ID", "winget", "skipped", "already_installed", "")
	ev := parseEvent(t, lastLine(buf))
	if ev["reason"] != "already_installed" {
		t.Errorf("reason = %v, want %q", ev["reason"], "already_installed")
	}
}

// ---------------------------------------------------------------------------
// Summary event
// ---------------------------------------------------------------------------

func TestEmitSummary(t *testing.T) {
	runID := "apply-summary-test"
	em, buf := captureEmitter(runID)
	em.EmitSummary("apply", 15, 12, 2, 1)

	line := lastLine(buf)
	ev := parseEvent(t, line)
	assertBaseFields(t, ev, runID, "summary")

	checks := map[string]float64{
		"total":   15,
		"success": 12,
		"skipped": 2,
		"failed":  1,
	}
	for field, want := range checks {
		got, ok := ev[field].(float64)
		if !ok {
			t.Errorf("field %q missing or wrong type: %v", field, ev[field])
			continue
		}
		if got != want {
			t.Errorf("%q = %v, want %v", field, got, want)
		}
	}
	if ev["phase"] != "apply" {
		t.Errorf("phase = %v, want %q", ev["phase"], "apply")
	}
}

// ---------------------------------------------------------------------------
// Error event
// ---------------------------------------------------------------------------

func TestEmitError(t *testing.T) {
	tests := []struct {
		name    string
		scope   string
		message string
		id      string
		wantID  bool
	}{
		{
			name:    "engine error without id",
			scope:   "engine",
			message: "fatal: something went wrong",
			id:      "",
			wantID:  false,
		},
		{
			name:    "item error with id",
			scope:   "item",
			message: "failed to install",
			id:      "Some.Package",
			wantID:  true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			em, buf := captureEmitter("error-test")
			em.EmitError(tc.scope, tc.message, tc.id)

			ev := parseEvent(t, lastLine(buf))
			assertBaseFields(t, ev, "error-test", "error")

			if ev["scope"] != tc.scope {
				t.Errorf("scope = %v, want %q", ev["scope"], tc.scope)
			}
			if ev["message"] != tc.message {
				t.Errorf("message = %v, want %q", ev["message"], tc.message)
			}
			_, hasID := ev["id"]
			if tc.wantID && !hasID {
				t.Errorf("expected 'id' field for item-scope error")
			}
			if !tc.wantID && hasID && ev["id"] != "" {
				t.Errorf("expected no 'id' field for engine-scope error, got %v", ev["id"])
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Artifact event
// ---------------------------------------------------------------------------

func TestEmitArtifact(t *testing.T) {
	runID := "capture-test"
	em, buf := captureEmitter(runID)
	em.EmitArtifact("capture", "manifest", `C:\manifests\captured.jsonc`)

	line := lastLine(buf)
	ev := parseEvent(t, line)
	assertBaseFields(t, ev, runID, "artifact")

	if ev["phase"] != "capture" {
		t.Errorf("phase = %v, want %q", ev["phase"], "capture")
	}
	if ev["kind"] != "manifest" {
		t.Errorf("kind = %v, want %q", ev["kind"], "manifest")
	}
	wantPath := `C:\manifests\captured.jsonc`
	if ev["path"] != wantPath {
		t.Errorf("path = %v, want %q", ev["path"], wantPath)
	}
}

// ---------------------------------------------------------------------------
// Disabled emitter
// ---------------------------------------------------------------------------

func TestDisabledEmitterProducesNoOutput(t *testing.T) {
	buf := &bytes.Buffer{}
	em := NewEmitterWithWriter("test-run", false, buf)

	em.EmitPhase("apply")
	em.EmitItem("Pkg", "winget", "installed", "", "")
	em.EmitSummary("apply", 1, 1, 0, 0)
	em.EmitError("engine", "oops", "")
	em.EmitArtifact("capture", "manifest", "/path")

	if buf.Len() != 0 {
		t.Errorf("disabled emitter produced output: %q", buf.String())
	}
}

// ---------------------------------------------------------------------------
// Timestamps are valid RFC3339
// ---------------------------------------------------------------------------

func TestTimestampsAreValidRFC3339(t *testing.T) {
	em, buf := captureEmitter("ts-test")
	em.EmitPhase("apply")
	em.EmitSummary("apply", 0, 0, 0, 0)

	for _, line := range strings.Split(strings.TrimRight(buf.String(), "\n"), "\n") {
		if line == "" {
			continue
		}
		ev := parseEvent(t, line)
		ts, _ := ev["timestamp"].(string)
		if ts == "" {
			t.Errorf("empty timestamp in event: %v", ev)
			continue
		}
		_, errNano := time.Parse(time.RFC3339Nano, ts)
		_, errPlain := time.Parse(time.RFC3339, ts)
		if errNano != nil && errPlain != nil {
			t.Errorf("timestamp %q is not valid RFC3339: %v", ts, errNano)
		}
	}
}

// ---------------------------------------------------------------------------
// Gap tests ported from Pester: Events module
// (Events.Tests.ps1)
// ---------------------------------------------------------------------------

// TestEmitPhase_AllPhases verifies that all valid phase values emit correctly.
// Pester: "Should accept plan phase", "Should accept verify phase", "Should accept capture phase"
func TestEmitPhase_AllPhases(t *testing.T) {
	phases := []string{"plan", "apply", "verify", "capture", "restore"}
	for _, phase := range phases {
		t.Run(phase, func(t *testing.T) {
			em, buf := captureEmitter("phase-test")
			em.EmitPhase(phase)
			ev := parseEvent(t, lastLine(buf))
			assertBaseFields(t, ev, "phase-test", "phase")
			if ev["phase"] != phase {
				t.Errorf("phase = %v, want %q", ev["phase"], phase)
			}
		})
	}
}

// TestEmitItem_AllValidStatuses verifies all valid item status values.
// Pester: "Should accept all valid status values"
func TestEmitItem_AllValidStatuses(t *testing.T) {
	statuses := []string{"to_install", "installing", "installed", "present", "skipped", "failed"}
	for _, status := range statuses {
		t.Run(status, func(t *testing.T) {
			em, buf := captureEmitter("status-test")
			em.EmitItem("App.Id", "winget", status, "", "")
			ev := parseEvent(t, lastLine(buf))
			if ev["status"] != status {
				t.Errorf("status = %v, want %q", ev["status"], status)
			}
		})
	}
}

// TestEmitItem_ReasonAlwaysPresent verifies that reason is always present in item events.
// Pester: "Should set reason to null when not provided" (PS uses null, Go uses empty string)
func TestEmitItem_ReasonAlwaysPresent(t *testing.T) {
	em, buf := captureEmitter("reason-test")
	em.EmitItem("App.Id", "winget", "installed", "", "")
	ev := parseEvent(t, lastLine(buf))
	if _, ok := ev["reason"]; !ok {
		t.Error("required field 'reason' missing from item event (should be present even when empty)")
	}
}

// TestEmitSummary_VerifyPhase tests summary emission with verify phase.
// Pester: "Should accept verify phase" in Write-SummaryEvent context
func TestEmitSummary_VerifyPhase(t *testing.T) {
	em, buf := captureEmitter("verify-summary")
	em.EmitSummary("verify", 5, 4, 0, 1)
	ev := parseEvent(t, lastLine(buf))
	assertBaseFields(t, ev, "verify-summary", "summary")
	if ev["phase"] != "verify" {
		t.Errorf("phase = %v, want %q", ev["phase"], "verify")
	}
	if ev["total"] != float64(5) {
		t.Errorf("total = %v, want 5", ev["total"])
	}
}

// TestEmitSummary_CapturePhase tests summary emission with capture phase.
// Pester: "Should accept capture phase" in Write-SummaryEvent context
func TestEmitSummary_CapturePhase(t *testing.T) {
	em, buf := captureEmitter("capture-summary")
	em.EmitSummary("capture", 15, 12, 3, 0)
	ev := parseEvent(t, lastLine(buf))
	assertBaseFields(t, ev, "capture-summary", "summary")
	if ev["phase"] != "capture" {
		t.Errorf("phase = %v, want %q", ev["phase"], "capture")
	}
	if ev["total"] != float64(15) {
		t.Errorf("total = %v, want 15", ev["total"])
	}
	if ev["success"] != float64(12) {
		t.Errorf("success = %v, want 12", ev["success"])
	}
	if ev["skipped"] != float64(3) {
		t.Errorf("skipped = %v, want 3", ev["skipped"])
	}
}

// TestNDJSONStream_FullApplyPipeline verifies a full apply pipeline stream.
// Pester: "Should produce parseable NDJSON stream" with phase->item->phase->item->item->summary
func TestNDJSONStream_FullApplyPipeline(t *testing.T) {
	em, buf := captureEmitter("pipeline-test")

	em.EmitPhase("plan")
	em.EmitItem("App.Id", "winget", "to_install", "", "")
	em.EmitPhase("apply")
	em.EmitItem("App.Id", "winget", "installing", "", "")
	em.EmitItem("App.Id", "winget", "installed", "", "Installed successfully")
	em.EmitSummary("apply", 1, 1, 0, 0)

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 6 {
		t.Fatalf("expected 6 NDJSON lines, got %d: %q", len(lines), buf.String())
	}

	// Parse each line and verify event types in order
	events := make([]map[string]interface{}, len(lines))
	for i, line := range lines {
		events[i] = parseEvent(t, line)
	}

	expectedTypes := []string{"phase", "item", "phase", "item", "item", "summary"}
	for i, want := range expectedTypes {
		if events[i]["event"] != want {
			t.Errorf("event[%d].event = %v, want %q", i, events[i]["event"], want)
		}
	}

	// Verify specific field values
	if events[0]["phase"] != "plan" {
		t.Errorf("event[0].phase = %v, want %q", events[0]["phase"], "plan")
	}
	if events[1]["status"] != "to_install" {
		t.Errorf("event[1].status = %v, want %q", events[1]["status"], "to_install")
	}
	if events[2]["phase"] != "apply" {
		t.Errorf("event[2].phase = %v, want %q", events[2]["phase"], "apply")
	}
	if events[4]["status"] != "installed" {
		t.Errorf("event[4].status = %v, want %q", events[4]["status"], "installed")
	}
}

// TestNDJSONStream_CapturePipeline verifies a capture NDJSON stream.
// Pester: "Should produce parseable capture NDJSON stream"
func TestNDJSONStream_CapturePipeline(t *testing.T) {
	em, buf := captureEmitter("capture-pipeline")

	em.EmitPhase("capture")
	em.EmitItem("Git.Git", "winget", "present", "detected", "Detected")
	em.EmitItem("MS.VCRedist", "winget", "skipped", "filtered_runtime", "Excluded (runtime)")
	em.EmitArtifact("capture", "manifest", `C:\profiles\test.jsonc`)
	em.EmitSummary("capture", 2, 1, 1, 0)

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 5 {
		t.Fatalf("expected 5 NDJSON lines, got %d", len(lines))
	}

	events := make([]map[string]interface{}, len(lines))
	for i, line := range lines {
		events[i] = parseEvent(t, line)
	}

	// Verify event types in order
	expectedTypes := []string{"phase", "item", "item", "artifact", "summary"}
	for i, want := range expectedTypes {
		if events[i]["event"] != want {
			t.Errorf("event[%d].event = %v, want %q", i, events[i]["event"], want)
		}
	}

	// Verify capture-specific fields
	if events[0]["phase"] != "capture" {
		t.Errorf("event[0].phase = %v, want %q", events[0]["phase"], "capture")
	}
	if events[1]["status"] != "present" {
		t.Errorf("event[1].status = %v, want %q", events[1]["status"], "present")
	}
	if events[1]["reason"] != "detected" {
		t.Errorf("event[1].reason = %v, want %q", events[1]["reason"], "detected")
	}
	if events[2]["reason"] != "filtered_runtime" {
		t.Errorf("event[2].reason = %v, want %q", events[2]["reason"], "filtered_runtime")
	}
	if events[3]["kind"] != "manifest" {
		t.Errorf("event[3].kind = %v, want %q", events[3]["kind"], "manifest")
	}
	if events[4]["phase"] != "capture" {
		t.Errorf("event[4].phase = %v, want %q", events[4]["phase"], "capture")
	}
}

// TestEmitItem_MessageIncludedWhenProvided verifies the message field is present.
// Pester: "Should include message when provided"
func TestEmitItem_MessageIncludedWhenProvided(t *testing.T) {
	em, buf := captureEmitter("msg-test")
	em.EmitItem("App.Id", "winget", "failed", "", "Connection timeout")
	ev := parseEvent(t, lastLine(buf))
	if ev["message"] != "Connection timeout" {
		t.Errorf("message = %v, want %q", ev["message"], "Connection timeout")
	}
}

// TestEmitError_EngineScope verifies error event with engine scope.
// Pester: "Should emit error event with correct structure"
func TestEmitError_EngineScope(t *testing.T) {
	em, buf := captureEmitter("err-test")
	em.EmitError("engine", "Failed to connect", "")
	ev := parseEvent(t, lastLine(buf))
	assertBaseFields(t, ev, "err-test", "error")
	if ev["scope"] != "engine" {
		t.Errorf("scope = %v, want %q", ev["scope"], "engine")
	}
	if ev["message"] != "Failed to connect" {
		t.Errorf("message = %v, want %q", ev["message"], "Failed to connect")
	}
}

// TestEmitError_ItemScopeWithID verifies error event with item scope includes ID.
// Pester: "Should include item ID when provided for item scope"
func TestEmitError_ItemScopeWithID(t *testing.T) {
	em, buf := captureEmitter("err-item-test")
	em.EmitError("item", "Install failed", "App.Id")
	ev := parseEvent(t, lastLine(buf))
	if ev["scope"] != "item" {
		t.Errorf("scope = %v, want %q", ev["scope"], "item")
	}
	if ev["id"] != "App.Id" {
		t.Errorf("id = %v, want %q", ev["id"], "App.Id")
	}
}

// TestEmitPhase_SingleLineJSON verifies each event is a single line (NDJSON).
// Pester: "Should output single-line JSON (NDJSON format)"
func TestEmitPhase_SingleLineJSON(t *testing.T) {
	em, buf := captureEmitter("ndjson-test")
	em.EmitItem("App.Id", "winget", "installed", "", "")
	output := buf.String()
	lines := strings.Split(strings.TrimRight(output, "\n"), "\n")
	nonEmpty := 0
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			nonEmpty++
		}
	}
	if nonEmpty != 1 {
		t.Errorf("expected exactly 1 non-empty line, got %d: %q", nonEmpty, output)
	}
}

// TestEmitMultipleEvents_SeparateLines verifies multiple events are separate lines.
// Pester: "Should emit multiple events as separate lines"
func TestEmitMultipleEvents_SeparateLines(t *testing.T) {
	em, buf := captureEmitter("multi-test")
	em.EmitPhase("apply")
	em.EmitItem("App1", "winget", "installing", "", "")
	em.EmitItem("App2", "winget", "installed", "", "")
	em.EmitSummary("apply", 2, 2, 0, 0)

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 4 {
		t.Fatalf("expected 4 lines, got %d", len(lines))
	}
	for i, l := range lines {
		var m map[string]interface{}
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			t.Errorf("line %d is not valid JSON: %v", i, err)
		}
	}
}
