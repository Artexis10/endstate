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
