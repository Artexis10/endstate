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
	Name    string `json:"name,omitempty"`
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

// ConsentEvent requests the user's consent to bootstrap one or more absent
// package backends (the engine installs its own backend when it is missing on
// macOS/Linux). It is a non-breaking addition under the event-contract
// extensibility rules (new event type, no version bump).
//
// One event covers the COMBINED set of backends a run needs and lacks, so the
// GUI renders a single plain-language dialog. The Message is product-neutral
// (it never names "Nix"/"Homebrew") to keep the concepts invisible; the Details
// carry the exact, inspectable installer commands for anyone who looks. Backends
// are the internal lane identifiers the GUI maps back to the run.
type ConsentEvent struct {
	BaseEvent
	// Backends are the internal identifiers of the absent backends needing
	// consent (e.g. "brew", "nix"). Structured metadata, not user-facing copy.
	Backends []string `json:"backends"`
	// Message is the plain-language, product-neutral consent ask.
	Message string `json:"message"`
	// Details are the exact installer commands the privileged step would run,
	// one per backend, surfaced as the inspectable "what will run" affordance.
	Details []string `json:"details,omitempty"`
}

// BackupChunkEvent tracks per-chunk progress of a hosted-backup push or pull.
//
// Status values:
//   - Push (backup-push phase): "uploading" → "uploaded" (terminal-success),
//     with "retrying" between attempts and "failed" on terminal error.
//   - Pull (backup-pull phase): "downloading" → "verified" → "decrypted"
//     (per-chunk pipeline), with "failed" on error. The pull path does not
//     currently retry at the chunk level so "retrying" is push-only today.
//
// Retry fields (Attempt / MaxAttempts) are present when Status == "retrying".
// Current / Total mirror ChunkIndex+1 / TotalChunks for forward-compat with
// non-chunk-indexed progress sources; they are always populated.
type BackupChunkEvent struct {
	BaseEvent
	// ChunkIndex is the 0-based chunk index, or storage.ManifestChunkIndex (-1)
	// for the manifest blob itself.
	ChunkIndex int `json:"chunkIndex"`
	// TotalChunks is the count of data chunks (excluding the manifest).
	TotalChunks int `json:"totalChunks"`
	// EncryptedSize is the on-the-wire size of the chunk in bytes.
	EncryptedSize int    `json:"encryptedSize"`
	Status        string `json:"status"`
	// Message carries a non-fatal hint (e.g. error message before retry).
	// Omitted when empty.
	Message string `json:"message,omitempty"`
	// Attempt is the 1-based current attempt number; only set when
	// Status == "retrying".
	Attempt int `json:"attempt,omitempty"`
	// MaxAttempts is the inclusive upper bound on attempts; only set when
	// Status == "retrying".
	MaxAttempts int `json:"maxAttempts,omitempty"`
	// Current is the 1-based chunk-of-total position (mirrors ChunkIndex+1
	// for data chunks). Always set.
	Current int `json:"current,omitempty"`
	// Total mirrors TotalChunks for forward-compat. Always set.
	Total int `json:"total,omitempty"`
}
