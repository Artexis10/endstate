// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package events

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"
)

// Emitter writes NDJSON events to an io.Writer (default: os.Stderr).
// When enabled is false all Emit* methods are no-ops, so callers never need
// an extra guard.
type Emitter struct {
	runID   string
	enabled bool
	writer  io.Writer
}

// NewEmitter creates an Emitter that writes to os.Stderr.
func NewEmitter(runID string, enabled bool) *Emitter {
	return &Emitter{runID: runID, enabled: enabled, writer: os.Stderr}
}

// NewEmitterWithWriter creates an Emitter that writes to w. Intended for
// tests where capturing output via bytes.Buffer is required.
func NewEmitterWithWriter(runID string, enabled bool, w io.Writer) *Emitter {
	return &Emitter{runID: runID, enabled: enabled, writer: w}
}

// now returns the current UTC instant formatted as RFC3339Nano, matching the
// millisecond precision used by the PowerShell engine.
func now() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

// emit marshals v to compact JSON and writes it as a single line followed by
// a newline, as required by the NDJSON format. Marshalling errors are silently
// discarded to avoid disrupting the caller on an informational write path.
func (e *Emitter) emit(v interface{}) {
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	fmt.Fprintln(e.writer, string(b))
}

// base constructs a BaseEvent with the emitter's runID and a fresh timestamp.
func (e *Emitter) base(eventType string) BaseEvent {
	return BaseEvent{
		Version:   1,
		RunID:     e.runID,
		Timestamp: now(),
		Event:     eventType,
	}
}

// EmitPhase emits a phase event. Valid phase values are:
// "plan" | "apply" | "verify" | "capture" | "restore"
func (e *Emitter) EmitPhase(phase string) {
	if !e.enabled {
		return
	}
	e.emit(PhaseEvent{
		BaseEvent: e.base("phase"),
		Phase:     phase,
	})
}

// EmitItem emits an item progress event. reason and message may be empty
// strings; reason is always serialised (including as "") to satisfy the
// contract requirement that the field is present (set to null in PS engine
// when no reason is available — here we use empty string).
func (e *Emitter) EmitItem(id, driver, status, reason, message string) {
	if !e.enabled {
		return
	}
	e.emit(ItemEvent{
		BaseEvent: e.base("item"),
		ID:        id,
		Driver:    driver,
		Status:    status,
		Reason:    reason,
		Message:   message,
	})
}

// EmitSummary emits a summary event at the end of a phase.
func (e *Emitter) EmitSummary(phase string, total, success, skipped, failed int) {
	if !e.enabled {
		return
	}
	e.emit(SummaryEvent{
		BaseEvent: e.base("summary"),
		Phase:     phase,
		Total:     total,
		Success:   success,
		Skipped:   skipped,
		Failed:    failed,
	})
}

// EmitError emits an error event. scope must be "item" or "engine". id is
// optional and is omitted from the JSON output when empty.
func (e *Emitter) EmitError(scope, message, id string) {
	if !e.enabled {
		return
	}
	e.emit(ErrorEvent{
		BaseEvent: e.base("error"),
		Scope:     scope,
		Message:   message,
		ID:        id,
	})
}

// EmitArtifact emits an artifact event. phase is always "capture" per the
// contract; kind is always "manifest".
func (e *Emitter) EmitArtifact(phase, kind, path string) {
	if !e.enabled {
		return
	}
	e.emit(ArtifactEvent{
		BaseEvent: e.base("artifact"),
		Phase:     phase,
		Kind:      kind,
		Path:      path,
	})
}
