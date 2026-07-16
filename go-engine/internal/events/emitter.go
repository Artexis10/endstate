// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package events

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/planner"
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
// when no reason is available — here we use empty string). name is the
// optional human-readable display name; when empty it is omitted from JSON.
func (e *Emitter) EmitItem(id, driver, status, reason, message, name string) {
	if !e.enabled {
		return
	}
	e.emit(ItemEvent{
		BaseEvent: e.base("item"),
		ID:        id,
		Driver:    driver,
		Name:      name,
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

// EmitConfigResolution projects the planner-owned result at the event boundary
// so presentation and portable target scrubbing cannot drift from envelopes.
func (e *Emitter) EmitConfigResolution(set planner.PlanSet) {
	if !e.enabled {
		return
	}
	resolution := planner.ProjectConfigResolution(set)
	targetCandidates := append([]planner.TargetInstance{}, resolution.TargetCandidates...)
	for index := range targetCandidates {
		targetCandidates[index].Root = ""
	}
	e.emit(ConfigResolutionEvent{
		BaseEvent:                   e.base("config-resolution"),
		CaptureID:                   resolution.CaptureID,
		ModuleID:                    resolution.ModuleID,
		ConfigSetID:                 resolution.ConfigSetID,
		SourceInstance:              resolution.SourceInstance,
		SourceInstanceID:            resolution.SourceInstanceID,
		TargetInstanceID:            resolution.TargetInstanceID,
		TargetCandidates:            targetCandidates,
		SourceGeneration:            resolution.SourceGeneration,
		SourceGenerationFingerprint: resolution.SourceGenerationFingerprint,
		TargetGeneration:            resolution.TargetGeneration,
		Resolution:                  resolution.Resolution,
		Reason:                      resolution.Reason,
		MigrationPath:               append([]string{}, resolution.MigrationPath...),
		CaptureModuleRevision:       resolution.CaptureModuleRevision,
		RestoreModuleRevision:       resolution.RestoreModuleRevision,
		Label:                       resolution.Label,
		Message:                     resolution.Message,
		Remediation:                 resolution.Remediation,
	})
}

// ConfigMigrationProgress contains one engine-owned config migration progress
// transition. Reason and Remediation are nil when the wire value is null.
type ConfigMigrationProgress struct {
	CaptureID      string
	ConfigSetID    string
	Stage          ConfigMigrationStage
	FromGeneration string
	ToGeneration   string
	Status         ConfigProgressStatus
	Reason         *string
	Message        string
	Remediation    *string
}

// EmitConfigMigration emits a config-migration event. Invalid enum values are
// refused rather than leaking an open-ended wire vocabulary.
func (e *Emitter) EmitConfigMigration(progress ConfigMigrationProgress) {
	if !e.enabled || !validConfigMigrationStage(progress.Stage) || !validConfigProgressStatus(progress.Status) {
		return
	}
	e.emit(ConfigMigrationEvent{
		BaseEvent:      e.base("config-migration"),
		CaptureID:      progress.CaptureID,
		ConfigSetID:    progress.ConfigSetID,
		Stage:          progress.Stage,
		FromGeneration: progress.FromGeneration,
		ToGeneration:   progress.ToGeneration,
		Status:         progress.Status,
		Reason:         progress.Reason,
		Message:        progress.Message,
		Remediation:    progress.Remediation,
	})
}

// RestoreItemProgress contains one concrete restore action transition.
type RestoreItemProgress struct {
	ID               string
	Module           string
	Restorer         string
	Source           string
	Target           string
	Status           RestoreItemStatus
	Reason           *string
	BackupPath       *string
	TargetExisted    bool
	Message          string
	CaptureID        string
	ConfigSetID      string
	TargetInstanceID string
	SourceGeneration string
	TargetGeneration string
}

// EmitRestoreItem emits a restore-item event and refuses unknown status values.
func (e *Emitter) EmitRestoreItem(progress RestoreItemProgress) {
	if !e.enabled || !validRestoreItemStatus(progress.Status) {
		return
	}
	e.emit(RestoreItemEvent{
		BaseEvent:        e.base("restore-item"),
		ID:               progress.ID,
		Module:           progress.Module,
		Restorer:         progress.Restorer,
		Source:           progress.Source,
		Target:           progress.Target,
		Status:           progress.Status,
		Reason:           progress.Reason,
		BackupPath:       progress.BackupPath,
		TargetExisted:    progress.TargetExisted,
		Message:          progress.Message,
		CaptureID:        progress.CaptureID,
		ConfigSetID:      progress.ConfigSetID,
		TargetInstanceID: progress.TargetInstanceID,
		SourceGeneration: progress.SourceGeneration,
		TargetGeneration: progress.TargetGeneration,
	})
}

func validConfigMigrationStage(stage ConfigMigrationStage) bool {
	switch stage {
	case ConfigMigrationStaging, ConfigMigrationEdge, ConfigMigrationValidation,
		ConfigMigrationCommit, ConfigMigrationRollback:
		return true
	default:
		return false
	}
}

func validConfigProgressStatus(status ConfigProgressStatus) bool {
	switch status {
	case ConfigProgressStarted, ConfigProgressCompleted, ConfigProgressFailed:
		return true
	default:
		return false
	}
}

func validRestoreItemStatus(status RestoreItemStatus) bool {
	switch status {
	case RestoreItemRestoring, RestoreItemRestored, RestoreItemSkippedUpToDate,
		RestoreItemSkippedMissingSource, RestoreItemFailed:
		return true
	default:
		return false
	}
}

// EmitConsent emits a consent-request event for one or more absent backends the
// run needs to bootstrap. message is the plain-language, product-neutral ask;
// details are the exact installer commands (the inspectable "what will run").
// One event covers the combined backend set so the GUI renders a single dialog.
func (e *Emitter) EmitConsent(backends []string, message string, details []string) {
	if !e.enabled {
		return
	}
	e.emit(ConsentEvent{
		BaseEvent: e.base("consent"),
		Backends:  backends,
		Message:   message,
		Details:   details,
	})
}

// BackupChunkProgress carries the per-chunk fields emitted as a
// `backup-chunk` event. Pass into EmitBackupChunk so future fields can be
// added without breaking call sites.
type BackupChunkProgress struct {
	ChunkIndex    int
	TotalChunks   int
	EncryptedSize int
	Status        string // "uploading"|"uploaded"|"downloading"|"verified"|"decrypted"|"retrying"|"failed"
	Message       string
	// Retry-specific. Both set only when Status == "retrying".
	Attempt     int
	MaxAttempts int
}

// EmitBackupChunk emits a hosted-backup per-chunk progress event. Used by
// the upload and download pipelines so the GUI can render chunk-by-chunk
// progress (and, for the upload path, retry attempts).
func (e *Emitter) EmitBackupChunk(p BackupChunkProgress) {
	if !e.enabled {
		return
	}
	// Current/Total are convenience fields for GUI rendering. The manifest
	// (chunkIndex == -1) reports current 0 — the GUI treats <=0 as "manifest"
	// and shows a separate label.
	current := 0
	if p.ChunkIndex >= 0 {
		current = p.ChunkIndex + 1
	}
	e.emit(BackupChunkEvent{
		BaseEvent:     e.base("backup-chunk"),
		ChunkIndex:    p.ChunkIndex,
		TotalChunks:   p.TotalChunks,
		EncryptedSize: p.EncryptedSize,
		Status:        p.Status,
		Message:       p.Message,
		Attempt:       p.Attempt,
		MaxAttempts:   p.MaxAttempts,
		Current:       current,
		Total:         p.TotalChunks,
	})
}
