// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

// Package schedule provides the task-registration layer for the Endstate
// scheduled drift-check feature, shared by Windows Task Scheduler and systemd
// user timers. Native operations sit behind the Registrar interface.
package schedule

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/safepath"
)

// TaskName is the Windows Task Scheduler task name used by Endstate.
const TaskName = `Endstate\DriftCheck`

// ---------------------------------------------------------------------------
// Config state types
// ---------------------------------------------------------------------------

// Config is the persisted schedule configuration stored at
// state/schedule/config.json. Written atomically on enable/disable.
type Config struct {
	SchemaVersion string        `json:"schemaVersion"`
	Enabled       bool          `json:"enabled"`
	Manifest      string        `json:"manifest"`
	Interval      string        `json:"interval"`
	Time          string        `json:"time"`
	AutoPush      bool          `json:"autoPush"`
	BackupID      string        `json:"backupId,omitempty"`
	TaskName      string        `json:"taskName"`
	Root          string        `json:"root"`
	RegisteredAt  string        `json:"registeredAt,omitempty"`
	Registration  *Registration `json:"registration,omitempty"`
	// PendingArtifact is a locally published fresh capture awaiting a cloud
	// push. It is never the configured verification manifest.
	PendingArtifact string `json:"pendingArtifact,omitempty"`
	PendingSHA256   string `json:"pendingSha256,omitempty"`
	// PendingUploads is an ordered, durable upload queue. Newer builds keep
	// the legacy singular fields in sync with its first item so an older build
	// can still retry work it understands during the compatibility window.
	PendingUploads []PendingUpload `json:"pendingUploads,omitempty"`
	// FallbackManifest is the last known-good local baseline retained while a
	// newly captured baseline is awaiting upload. It lets a corrupt pending
	// artifact recover without leaving the scheduler pinned to an invalid path.
	FallbackManifest  string   `json:"fallbackManifest,omitempty"`
	FallbackManifests []string `json:"fallbackManifests,omitempty"`
}

// PendingUpload is one locally complete artifact awaiting a Cloud push.
// Queue order is capture order and must be preserved across restarts.
type PendingUpload struct {
	Artifact   string `json:"artifact"`
	SHA256     string `json:"sha256"`
	CapturedAt string `json:"capturedAt,omitempty"`
	// CreateVersion state is written ahead of the first create POST. A spool is
	// the exact encrypted request/body bytes needed to replay safely.
	BackupID            string `json:"backupId,omitempty"`
	OperationID         string `json:"operationId,omitempty"`
	CreateSpool         string `json:"createSpool,omitempty"`
	LegacyCreateStarted bool   `json:"legacyCreateStarted,omitempty"`
}

// LastRun is the persisted outcome of the most recent schedule run, stored at
// state/schedule/last-run.json. Written atomically after each schedule run.
type LastRun struct {
	SchemaVersion string         `json:"schemaVersion"`
	RunID         string         `json:"runId"`
	TimestampUTC  string         `json:"timestampUtc"`
	Status        string         `json:"status,omitempty"`
	Verify        *LastRunVerify `json:"verify,omitempty"`
	AutoBackup    *LastRunBackup `json:"autoBackup,omitempty"`
	Error         *LastRunError  `json:"error,omitempty"`
}

// LastRunVerify holds the verify summary and drifted items from a schedule run.
type LastRunVerify struct {
	Summary LastRunVerifySummary `json:"summary"`
	Drifted []LastRunDriftItem   `json:"drifted,omitempty"`
}

// LastRunVerifySummary aggregates counts from the verify run.
type LastRunVerifySummary struct {
	Total int `json:"total"`
	Pass  int `json:"pass"`
	Fail  int `json:"fail"`
}

// LastRunDriftItem records a single drifted item from the verify run.
type LastRunDriftItem struct {
	ID     string `json:"id"`
	Name   string `json:"name,omitempty"`
	Status string `json:"status"`
	Reason string `json:"reason,omitempty"`
}

// LastRunBackup holds the outcome of the optional auto-backup step.
type LastRunBackup struct {
	Outcome   string `json:"outcome"` // "pushed", "skipped", "auth_required", "subscription_required", "offline", "error"
	BackupID  string `json:"backupId,omitempty"`
	VersionID string `json:"versionId,omitempty"`
}

// LastRunError records a hard failure that prevented the run from completing.
type LastRunError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// ---------------------------------------------------------------------------
// State file helpers
// ---------------------------------------------------------------------------

// ConfigPath returns the path to state/schedule/config.json under stateDir.
func ConfigPath(stateDir string) string {
	return filepath.Join(stateDir, "schedule", "config.json")
}

// LastRunPath returns the path to state/schedule/last-run.json under stateDir.
func LastRunPath(stateDir string) string {
	return filepath.Join(stateDir, "schedule", "last-run.json")
}

// ReadConfig reads the schedule config from path. Returns a default disabled
// Config with SchemaVersion "1.0" if the file does not exist.
func ReadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Config{SchemaVersion: "1.0"}, nil
		}
		return nil, err
	}
	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, err
	}
	c.NormalizePendingUploads()
	return &c, nil
}

// WriteConfig writes cfg to path using the atomic temp+rename pattern.
func WriteConfig(path string, cfg *Config) error {
	cfg.NormalizePendingUploads()
	return writeAtomic(path, cfg)
}

// ReplacePendingUploads replaces the durable queue and synchronizes the
// compatibility fields without re-running legacy migration. Queue drains must
// use this instead of assigning PendingUploads directly so stale singular
// fields cannot resurrect a completed upload on the next WriteConfig.
func (c *Config) ReplacePendingUploads(pending []PendingUpload) {
	c.PendingUploads = pending
	if len(c.PendingUploads) == 0 {
		c.PendingArtifact, c.PendingSHA256 = "", ""
		return
	}
	c.PendingArtifact = c.PendingUploads[0].Artifact
	c.PendingSHA256 = c.PendingUploads[0].SHA256
}

// NormalizePendingUploads migrates the singular pending capture persisted by
// earlier builds into the ordered queue and mirrors the queue head back into
// the legacy fields. It is deliberately idempotent because ReadConfig and
// WriteConfig both call it.
func (c *Config) NormalizePendingUploads() {
	if len(c.FallbackManifests) == 0 && c.FallbackManifest != "" {
		c.FallbackManifests = []string{c.FallbackManifest}
	}
	if len(c.PendingUploads) == 0 && c.PendingArtifact != "" && c.PendingSHA256 != "" {
		c.ReplacePendingUploads([]PendingUpload{{Artifact: c.PendingArtifact, SHA256: c.PendingSHA256}})
		return
	}
	c.ReplacePendingUploads(c.PendingUploads)
}

// ReadLastRun reads last-run.json from path. Returns nil, nil when the file
// does not exist (never-run state).
func ReadLastRun(path string) (*LastRun, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var lr LastRun
	if err := json.Unmarshal(data, &lr); err != nil {
		return nil, err
	}
	return &lr, nil
}

// WriteLastRun writes lr to path using the atomic temp+rename pattern.
func WriteLastRun(path string, lr *LastRun) error {
	return writeAtomic(path, lr)
}

// writeAtomic marshals v as indented JSON and writes it to path via a
// temp file + rename, matching the pattern in internal/state/state.go.
func writeAtomic(path string, v interface{}) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return safepath.AtomicWriteFile(path, data, 0644)
}

// NowUTC returns the current time in RFC3339 UTC format.
func NowUTC() string {
	return time.Now().UTC().Format(time.RFC3339)
}
