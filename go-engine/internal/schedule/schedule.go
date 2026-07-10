// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

// Package schedule provides the task-registration layer for the Endstate
// scheduled drift-check feature. It wraps schtasks.exe behind a Registrar
// interface so command handlers are unit-testable without touching the real
// Windows Task Scheduler.
package schedule

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// TaskName is the Windows Task Scheduler task name used by Endstate.
const TaskName = `Endstate\DriftCheck`

// ---------------------------------------------------------------------------
// Registrar interface (test seam)
// ---------------------------------------------------------------------------

// Registrar abstracts the OS task-scheduler operations so command handlers
// can be tested without shelling out to schtasks.exe.
type Registrar interface {
	// Register creates or updates (idempotent /F) the scheduled task with the
	// given command line, schedule interval, and time-of-day.
	Register(taskName, exePath, args, interval, timeOfDay string) error
	// Unregister removes the scheduled task (/F, no error if absent).
	Unregister(taskName string) error
}

// ---------------------------------------------------------------------------
// Real schtasks.exe implementation
// ---------------------------------------------------------------------------

// SchtasksRegistrar is the production Registrar that shells out to schtasks.exe.
// It is used on Windows only; on other platforms schedule enable returns
// NOT_SUPPORTED before ever calling the registrar.
type SchtasksRegistrar struct{}

// Register creates or updates the Endstate drift-check task via schtasks.exe.
// The task runs as the current interactive user (LOGONTYPE interactive-only so
// the Credential Manager keychain is accessible).
//
// schtasks command:
//
//	schtasks /Create /F /SC DAILY|WEEKLY /ST HH:MM /TN <name> /TR "<exe> <args>" /RL LIMITED
func (r *SchtasksRegistrar) Register(taskName, exePath, args, interval, timeOfDay string) error {
	sc := "DAILY"
	if interval == "weekly" {
		sc = "WEEKLY"
	}

	tr := fmt.Sprintf(`"%s" %s`, exePath, args)

	cmd := exec.Command("schtasks",
		"/Create",
		"/F",
		"/SC", sc,
		"/ST", timeOfDay,
		"/TN", taskName,
		"/TR", tr,
		"/RL", "LIMITED",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("schtasks /Create failed: %w (output: %s)", err, string(out))
	}
	return nil
}

// Unregister deletes the scheduled task via schtasks.exe /Delete /F.
// Returns nil if the task does not exist.
func (r *SchtasksRegistrar) Unregister(taskName string) error {
	cmd := exec.Command("schtasks", "/Delete", "/F", "/TN", taskName)
	out, err := cmd.CombinedOutput()
	if err != nil {
		// schtasks exits non-zero if the task does not exist; treat that as success.
		msg := string(out)
		if isNotFoundOutput(msg) {
			return nil
		}
		return fmt.Errorf("schtasks /Delete failed: %w (output: %s)", err, msg)
	}
	return nil
}

// isNotFoundOutput returns true when schtasks output indicates the task did
// not exist (so Unregister can be idempotent).
func isNotFoundOutput(output string) bool {
	// schtasks prints "ERROR: The system cannot find the file specified." or
	// "ERROR: The specified task name ... does not exist" on deletion of a
	// missing task. A simple substring check is sufficient.
	return len(output) == 0 ||
		containsAny(output, "does not exist", "cannot find", "not found")
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if len(s) >= len(sub) {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Config state types
// ---------------------------------------------------------------------------

// Config is the persisted schedule configuration stored at
// state/schedule/config.json. Written atomically on enable/disable.
type Config struct {
	SchemaVersion string `json:"schemaVersion"`
	Enabled       bool   `json:"enabled"`
	Manifest      string `json:"manifest"`
	Interval      string `json:"interval"`
	Time          string `json:"time"`
	AutoPush      bool   `json:"autoPush"`
	TaskName      string `json:"taskName"`
	Root          string `json:"root"`
	RegisteredAt  string `json:"registeredAt,omitempty"`
}

// LastRun is the persisted outcome of the most recent schedule run, stored at
// state/schedule/last-run.json. Written atomically after each schedule run.
type LastRun struct {
	SchemaVersion string          `json:"schemaVersion"`
	RunID         string          `json:"runId"`
	TimestampUTC  string          `json:"timestampUtc"`
	Verify        *LastRunVerify  `json:"verify,omitempty"`
	AutoBackup    *LastRunBackup  `json:"autoBackup,omitempty"`
	Error         *LastRunError   `json:"error,omitempty"`
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
	Outcome   string `json:"outcome"` // "pushed", "skipped", "auth_required", "error"
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
	return &c, nil
}

// WriteConfig writes cfg to path using the atomic temp+rename pattern.
func WriteConfig(path string, cfg *Config) error {
	return writeAtomic(path, cfg)
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
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

// NowUTC returns the current time in RFC3339 UTC format.
func NowUTC() string {
	return time.Now().UTC().Format(time.RFC3339)
}
