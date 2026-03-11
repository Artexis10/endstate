// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

// Package events provides NDJSON streaming event emission for the Endstate
// engine as defined in docs/contracts/event-contract.md.
//
// Events are written to stderr (or a configurable io.Writer) as single-line
// JSON objects. They are informational and ephemeral: they do NOT replace the
// authoritative stdout JSON envelope.
package events

// BaseEvent contains the required fields that every event MUST include per
// schema v1. Concrete event structs embed this type.
type BaseEvent struct {
	Version   int    `json:"version"`   // Always 1
	RunID     string `json:"runId"`
	Timestamp string `json:"timestamp"` // RFC3339 UTC
	Event     string `json:"event"`     // "phase" | "item" | "summary" | "error" | "artifact"
}

// PhaseEvent signals a transition between engine phases.
// Valid phase values: "plan" | "apply" | "verify" | "capture" | "restore"
type PhaseEvent struct {
	BaseEvent
	Phase string `json:"phase"`
}

// ItemEvent tracks progress of a single installable item.
type ItemEvent struct {
	BaseEvent
	ID      string `json:"id"`
	Driver  string `json:"driver"`
	Status  string `json:"status"`
	Reason  string `json:"reason"`
	Message string `json:"message,omitempty"`
}

// SummaryEvent is emitted at the end of each phase with aggregate counts.
type SummaryEvent struct {
	BaseEvent
	Phase   string `json:"phase"`
	Total   int    `json:"total"`
	Success int    `json:"success"`
	Skipped int    `json:"skipped"`
	Failed  int    `json:"failed"`
}

// ErrorEvent reports an error at item or engine scope.
type ErrorEvent struct {
	BaseEvent
	Scope   string `json:"scope"`
	Message string `json:"message"`
	ID      string `json:"id,omitempty"`
}

// ArtifactEvent reports a generated artifact (e.g., a captured manifest).
type ArtifactEvent struct {
	BaseEvent
	Phase string `json:"phase"`
	Kind  string `json:"kind"`
	Path  string `json:"path"`
}
