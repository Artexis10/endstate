// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/gofrs/flock"

	"github.com/Artexis10/endstate/go-engine/internal/backup/upload"
	"github.com/Artexis10/endstate/go-engine/internal/config"
	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/schedule"
)

// scheduleRegistrarFn is the factory that returns the Registrar used by
// schedule enable/disable. It defaults to the real schtasks.exe implementation
// and is replaced in tests to inject a fake.
var scheduleRegistrarFn func() schedule.Registrar = func() schedule.Registrar {
	return &schedule.SchtasksRegistrar{}
}

var (
	scheduleVerifyFn         = RunVerify
	scheduleCaptureFn        = RunCapture
	scheduleBackupFn         = RunBackup
	scheduleRunSupportedFn   = func() bool { return runtime.GOOS == "windows" }
	scheduleWriteLastRunFn   = schedule.WriteLastRun
	scheduleRemoveLastRunFn  = os.Remove
	scheduleWriteConfigFn    = schedule.WriteConfig
	scheduleRemoveArtifactFn = os.Remove
	scheduleRenameArtifactFn = os.Rename
)

// ---------------------------------------------------------------------------
// ScheduleFlags — shared flag bag for all schedule subcommands
// ---------------------------------------------------------------------------

// ScheduleFlags holds the parsed CLI flags for the schedule command family.
type ScheduleFlags struct {
	// Subcommand is one of: enable, disable, status, run.
	Subcommand string
	// Manifest is the path to the manifest file (enable, run).
	Manifest string
	// Interval is "daily" or "weekly" (enable).
	Interval string
	// Time is the time-of-day in HH:MM format (enable).
	Time string
	// AutoPush enables capture+push after verify (enable, propagated to run).
	AutoPush bool
	// BackupID pins scheduled Cloud delivery to an existing backup.
	BackupID string
	// ArtifactSHA256 identifies one pending upload for explicit discard.
	ArtifactSHA256 string
	// Confirm gates destructive queue-state changes.
	Confirm bool
	// Root overrides the engine root (run; baked by enable into the task command line).
	Root string
	// JSON controls --json output (all subcommands).
	JSON bool
}

// ---------------------------------------------------------------------------
// Response types
// ---------------------------------------------------------------------------

// ScheduleEnableData is the data payload for `schedule enable --json`.
type ScheduleEnableData struct {
	Enabled  bool   `json:"enabled"`
	Manifest string `json:"manifest"`
	Interval string `json:"interval"`
	Time     string `json:"time"`
	AutoPush bool   `json:"autoPush"`
	BackupID string `json:"backupId,omitempty"`
	TaskName string `json:"taskName"`
	Root     string `json:"root"`
}

// ScheduleDisableData is the data payload for `schedule disable --json`.
type ScheduleDisableData struct {
	Enabled  bool   `json:"enabled"`
	TaskName string `json:"taskName"`
}

type ScheduleDiscardUploadData struct {
	Discarded      bool   `json:"discarded"`
	ArtifactSHA256 string `json:"artifactSha256"`
}

// ScheduleStatusData is the data payload for `schedule status --json`.
// It composes config.json + last-run.json and is the sole client-facing
// drift-truth source (CLI is source of truth invariant).
type ScheduleStatusData struct {
	Enabled       bool                  `json:"enabled"`
	Manifest      string                `json:"manifest,omitempty"`
	Interval      string                `json:"interval,omitempty"`
	Time          string                `json:"time,omitempty"`
	AutoPush      bool                  `json:"autoPush"`
	BackupID      string                `json:"backupId,omitempty"`
	TaskName      string                `json:"taskName,omitempty"`
	PendingUpload SchedulePendingUpload `json:"pendingUpload"`
	LastRun       *schedule.LastRun     `json:"lastRun,omitempty"`
}

// SchedulePendingUpload exposes cloud-push truth without disclosing the
// local artifact path. GUI consumers can distinguish a fresh local baseline
// awaiting cloud delivery from a healthy fully-uploaded run.
type SchedulePendingUpload struct {
	Pending     bool   `json:"pending"`
	ArtifactSHA string `json:"artifactSha256,omitempty"`
	Count       int    `json:"count"`
	LastOutcome string `json:"lastOutcome,omitempty"`
}

// ScheduleRunData is the data payload for `schedule run --json`.
// It re-serialises the last-run document into the envelope for manual/debug
// invocations; the task itself writes last-run.json and exits 0.
type ScheduleRunData struct {
	RunID        string                  `json:"runId"`
	TimestampUTC string                  `json:"timestampUtc"`
	Verify       *schedule.LastRunVerify `json:"verify,omitempty"`
	AutoBackup   *schedule.LastRunBackup `json:"autoBackup,omitempty"`
	Error        *schedule.LastRunError  `json:"error,omitempty"`
}

// ---------------------------------------------------------------------------
// RunSchedule — top-level dispatcher
// ---------------------------------------------------------------------------

// RunSchedule is the top-level handler for the `schedule` command family.
func RunSchedule(flags ScheduleFlags) (interface{}, *envelope.Error) {
	switch flags.Subcommand {
	case "enable":
		return runScheduleEnable(flags)
	case "disable":
		return runScheduleDisable(flags)
	case "status":
		return runScheduleStatus(flags)
	case "run":
		return runScheduleRun(flags)
	case "discard-upload":
		return runScheduleDiscardUpload(flags)
	default:
		return nil, envelope.NewError(
			envelope.ErrInternalError,
			fmt.Sprintf("unknown schedule subcommand %q; use enable|disable|status|run|discard-upload", flags.Subcommand),
		)
	}
}

// ---------------------------------------------------------------------------
// schedule enable
// ---------------------------------------------------------------------------

func runScheduleEnable(flags ScheduleFlags) (interface{}, *envelope.Error) {
	if !scheduleRunSupportedFn() {
		return nil, envelope.NewError(
			envelope.ErrNotSupported,
			"schedule enable is only supported on Windows.",
		).WithRemediation("Use cron or launchd on Linux/macOS.")
	}

	if flags.Manifest == "" {
		return nil, envelope.NewError(
			envelope.ErrManifestNotFound,
			"--manifest is required for schedule enable.",
		)
	}
	manifestPath, pathErr := canonicalSchedulePath(flags.Manifest)
	if pathErr != nil {
		return nil, envelope.NewError(envelope.ErrManifestNotFound, "Invalid schedule manifest path: "+pathErr.Error())
	}
	if _, err := os.Stat(manifestPath); os.IsNotExist(err) {
		return nil, envelope.NewError(
			envelope.ErrManifestNotFound,
			"The specified manifest file does not exist.",
		).WithDetail(map[string]string{"path": manifestPath})
	}

	interval := flags.Interval
	if interval == "" {
		interval = "daily"
	}
	timeOfDay := flags.Time
	if timeOfDay == "" {
		timeOfDay = "09:00"
	}

	// Resolve the root: explicit --root flag > ENDSTATE_ROOT > exe-walk
	// (mirrors config.ResolveRepoRoot but accepts an explicit override).
	root, rootErr := scheduleRootWithError(flags.Root)
	if rootErr != nil {
		return nil, envelope.NewError(envelope.ErrInternalError, "Invalid schedule root: "+rootErr.Error())
	}
	stateDir := filepath.Join(root, "state")
	if err := os.MkdirAll(filepath.Join(stateDir, "schedule"), 0o755); err != nil {
		return nil, envelope.NewError(envelope.ErrInternalError, "Create schedule state directory: "+err.Error())
	}
	lock := flock.New(filepath.Join(stateDir, "schedule", "run.lock"))
	if err := lock.Lock(); err != nil {
		return nil, envelope.NewError(envelope.ErrInternalError, "Failed to acquire schedule state lock: "+err.Error())
	}
	defer func() { _ = lock.Unlock() }()
	// Validate and normalise existing state before mutating Task Scheduler. A
	// corrupt config is a local safety failure, not a reason to create work that
	// cannot be described or safely resumed.
	previous, previousErr := schedule.ReadConfig(schedule.ConfigPath(stateDir))
	if previousErr != nil {
		return nil, envelope.NewError(envelope.ErrInternalError, "Failed to read existing schedule config: "+previousErr.Error())
	}
	previous.NormalizePendingUploads()

	// Bake the root into the registered task command line so scheduled runs and
	// GUI-spawned runs share one state directory. Task Scheduler cannot set env vars.
	exePath, exeErr := os.Executable()
	if exeErr != nil {
		return nil, envelope.NewError(
			envelope.ErrInternalError,
			"Could not resolve current executable path: "+exeErr.Error(),
		)
	}

	cfg := &schedule.Config{
		SchemaVersion: "1.0",
		Enabled:       true,
		Manifest:      manifestPath,
		Interval:      interval,
		Time:          timeOfDay,
		AutoPush:      flags.AutoPush,
		BackupID:      flags.BackupID,
		TaskName:      schedule.TaskName,
		Root:          root,
		RegisteredAt:  schedule.NowUTC(),
	}
	if previous != nil {
		cfg.ReplacePendingUploads(previous.PendingUploads)
		if cfg.BackupID == "" {
			cfg.BackupID = previous.BackupID
		}
		cfg.FallbackManifest = previous.FallbackManifest
		cfg.FallbackManifests = previous.FallbackManifests
	}
	cfgPath := schedule.ConfigPath(stateDir)
	_, priorFileErr := os.Stat(cfgPath)
	hadPriorFile := priorFileErr == nil
	if priorFileErr != nil && !os.IsNotExist(priorFileErr) {
		return nil, envelope.NewError(envelope.ErrInternalError, "Failed to inspect existing schedule config: "+priorFileErr.Error())
	}
	if err := scheduleWriteConfigFn(cfgPath, cfg); err != nil {
		return nil, envelope.NewError(
			envelope.ErrInternalError,
			"Failed to write schedule config: "+err.Error(),
		)
	}

	taskArgs := fmt.Sprintf(`schedule run --root "%s" --json`, root)
	reg := scheduleRegistrarFn()
	if err := reg.Register(schedule.TaskName, exePath, taskArgs, interval, timeOfDay); err != nil {
		var rollbackErr error
		if hadPriorFile {
			rollbackErr = scheduleWriteConfigFn(cfgPath, previous)
		} else {
			rollbackErr = os.Remove(cfgPath)
			if os.IsNotExist(rollbackErr) {
				rollbackErr = nil
			}
		}
		message := "Failed to register scheduled task: " + err.Error()
		if rollbackErr != nil {
			message += "; failed to restore prior schedule config: " + rollbackErr.Error()
		}
		return nil, envelope.NewError(envelope.ErrTaskRegistrationFailed, message).
			WithRemediation("Ensure schtasks.exe is available and the user has permission to create tasks.")
	}

	return &ScheduleEnableData{
		Enabled:  true,
		Manifest: cfg.Manifest,
		Interval: cfg.Interval,
		Time:     cfg.Time,
		AutoPush: cfg.AutoPush,
		BackupID: cfg.BackupID,
		TaskName: cfg.TaskName,
		Root:     cfg.Root,
	}, nil
}

// ---------------------------------------------------------------------------
// schedule disable
// ---------------------------------------------------------------------------

func runScheduleDisable(flags ScheduleFlags) (interface{}, *envelope.Error) {
	if !scheduleRunSupportedFn() {
		return nil, envelope.NewError(
			envelope.ErrNotSupported,
			"schedule disable is only supported on Windows.",
		)
	}

	stateDir := scheduleStateDir(flags.Root)
	if err := os.MkdirAll(filepath.Join(stateDir, "schedule"), 0o755); err != nil {
		return nil, envelope.NewError(envelope.ErrInternalError, "Create schedule state directory: "+err.Error())
	}
	lock := flock.New(filepath.Join(stateDir, "schedule", "run.lock"))
	if err := lock.Lock(); err != nil {
		return nil, envelope.NewError(envelope.ErrInternalError, "Failed to acquire schedule state lock: "+err.Error())
	}
	defer func() { _ = lock.Unlock() }()

	cfgPath := schedule.ConfigPath(stateDir)
	cfg, err := schedule.ReadConfig(cfgPath)
	if err != nil {
		return nil, envelope.NewError(
			envelope.ErrInternalError,
			"Failed to read schedule config: "+err.Error(),
		)
	}
	wasEnabled := cfg.Enabled
	cfg.Enabled = false
	if err := scheduleWriteConfigFn(cfgPath, cfg); err != nil {
		return nil, envelope.NewError(
			envelope.ErrInternalError,
			"Failed to write schedule config: "+err.Error(),
		)
	}
	reg := scheduleRegistrarFn()
	if err := reg.Unregister(schedule.TaskName); err != nil {
		cfg.Enabled = wasEnabled
		_ = scheduleWriteConfigFn(cfgPath, cfg)
		return nil, envelope.NewError(envelope.ErrInternalError, "Failed to unregister scheduled task: "+err.Error())
	}

	return &ScheduleDisableData{
		Enabled:  false,
		TaskName: schedule.TaskName,
	}, nil
}

// runScheduleDiscardUpload explicitly resolves an uncertain legacy create by
// stopping automatic retries while retaining the local baseline and artifact.
func runScheduleDiscardUpload(flags ScheduleFlags) (interface{}, *envelope.Error) {
	if !scheduleRunSupportedFn() {
		return nil, envelope.NewError(envelope.ErrNotSupported, "schedule discard-upload is only supported on Windows.")
	}
	if !flags.Confirm || strings.TrimSpace(flags.ArtifactSHA256) == "" {
		return nil, envelope.NewError(envelope.ErrInternalError, "schedule discard-upload requires --artifact-sha256 <sha> --confirm")
	}
	stateDir := scheduleStateDir(flags.Root)
	if err := os.MkdirAll(filepath.Join(stateDir, "schedule"), 0o755); err != nil {
		return nil, envelope.NewError(envelope.ErrInternalError, "Create schedule state directory: "+err.Error())
	}
	lock := flock.New(filepath.Join(stateDir, "schedule", "run.lock"))
	if err := lock.Lock(); err != nil {
		return nil, envelope.NewError(envelope.ErrInternalError, "Failed to acquire schedule state lock: "+err.Error())
	}
	defer func() { _ = lock.Unlock() }()
	cfgPath := schedule.ConfigPath(stateDir)
	cfg, err := schedule.ReadConfig(cfgPath)
	if err != nil {
		return nil, envelope.NewError(envelope.ErrInternalError, "Failed to read schedule config: "+err.Error())
	}
	for i, pending := range cfg.PendingUploads {
		if pending.SHA256 != flags.ArtifactSHA256 {
			continue
		}
		if !pending.LegacyCreateStarted {
			return nil, envelope.NewError(envelope.ErrInternalError, "schedule discard-upload only applies to an uncertain legacy create")
		}
		cfg.ReplacePendingUploads(append(cfg.PendingUploads[:i:i], cfg.PendingUploads[i+1:]...))
		if err := scheduleWriteConfigFn(cfgPath, cfg); err != nil {
			return nil, envelope.NewError(envelope.ErrInternalError, "Failed to persist discarded scheduled upload: "+err.Error())
		}
		return &ScheduleDiscardUploadData{Discarded: true, ArtifactSHA256: pending.SHA256}, nil
	}
	return nil, envelope.NewError(envelope.ErrInternalError, "No uncertain legacy scheduled upload matches --artifact-sha256.")
}

// ---------------------------------------------------------------------------
// schedule status
// ---------------------------------------------------------------------------

func runScheduleStatus(flags ScheduleFlags) (interface{}, *envelope.Error) {
	stateDir := scheduleStateDir(flags.Root)

	cfg, err := schedule.ReadConfig(schedule.ConfigPath(stateDir))
	if err != nil {
		return nil, envelope.NewError(
			envelope.ErrInternalError,
			"Failed to read schedule config: "+err.Error(),
		)
	}

	lr, err := schedule.ReadLastRun(schedule.LastRunPath(stateDir))
	if err != nil {
		// Non-fatal: return status without last-run data.
		lr = nil
	}

	cfg.NormalizePendingUploads()
	pending := SchedulePendingUpload{Pending: len(cfg.PendingUploads) > 0, Count: len(cfg.PendingUploads)}
	if pending.Pending {
		pending.ArtifactSHA = cfg.PendingUploads[0].SHA256
	}
	if lr != nil && lr.AutoBackup != nil {
		pending.LastOutcome = lr.AutoBackup.Outcome
	}
	return &ScheduleStatusData{
		Enabled: cfg.Enabled, Manifest: cfg.Manifest, Interval: cfg.Interval,
		Time: cfg.Time, AutoPush: cfg.AutoPush, BackupID: cfg.BackupID, TaskName: cfg.TaskName,
		PendingUpload: pending, LastRun: lr,
	}, nil
}

// ---------------------------------------------------------------------------
// schedule run
// ---------------------------------------------------------------------------

// runScheduleRun is the task payload: verify in-process, optionally push,
// write last-run.json. Drift is data (exit 0). Hard errors are recorded in
// last-run.json with stable codes. No NDJSON events are emitted.
func runScheduleRun(flags ScheduleFlags) (interface{}, *envelope.Error) {
	if !scheduleRunSupportedFn() {
		return nil, envelope.NewError(
			envelope.ErrNotSupported,
			"schedule run is only supported on Windows.",
		).WithRemediation("Use cron or launchd on Linux/macOS.")
	}

	stateDir := scheduleStateDir(flags.Root)

	runID := "schedule-" + time.Now().UTC().Format("20060102-150405")
	ts := schedule.NowUTC()

	lr := &schedule.LastRun{
		SchemaVersion: "1.0",
		RunID:         runID,
		TimestampUTC:  ts,
		Status:        "running",
	}
	if err := os.MkdirAll(filepath.Join(stateDir, "schedule"), 0o755); err != nil {
		lr.Error = &schedule.LastRunError{Code: string(envelope.ErrInternalError), Message: "Failed to create schedule state directory: " + err.Error()}
		return nil, scheduleRunFailure(schedule.LastRunPath(stateDir), lr, envelope.NewError(envelope.ErrInternalError, lr.Error.Message))
	}
	// The same lock is held by enable and disable before they read or write
	// configuration, so a completed run cannot resurrect stale state after a
	// reconfiguration.
	lock := flock.New(filepath.Join(stateDir, "schedule", "run.lock"))
	locked, lockErr := lock.TryLock()
	if lockErr != nil || !locked {
		message := "A schedule run is already in progress."
		if lockErr != nil {
			message = "Failed to acquire schedule run lock: " + lockErr.Error()
		}
		return nil, envelope.NewError(envelope.ErrInternalError, message).WithRemediation("Wait for the active scheduled run to finish before retrying.")
	}
	defer func() { _ = lock.Unlock() }()
	// Replace any previous healthy result before work starts. If the process is
	// terminated during verification, capture, or push, status reports this
	// run as in-progress instead of falsely retaining the prior healthy state.
	if err := finalizeScheduleRun(schedule.LastRunPath(stateDir), lr); err != nil {
		return nil, scheduleRunFailure(schedule.LastRunPath(stateDir), lr, envelope.NewError(envelope.ErrInternalError, "Failed to write schedule running state: "+err.Error()))
	}

	// Load config.
	cfg, err := schedule.ReadConfig(schedule.ConfigPath(stateDir))
	if err != nil {
		lr.Error = &schedule.LastRunError{
			Code:    string(envelope.ErrInternalError),
			Message: "Failed to read schedule config: " + err.Error(),
		}
		return nil, scheduleRunFailure(schedule.LastRunPath(stateDir), lr, envelope.NewError(envelope.ErrInternalError, lr.Error.Message))
	}

	if !cfg.Enabled {
		lr.Error = &schedule.LastRunError{
			Code:    string(envelope.ErrScheduleDisabled),
			Message: "Schedule is not enabled; run 'schedule enable --manifest <path>' first.",
		}
		return nil, scheduleRunFailure(schedule.LastRunPath(stateDir), lr, envelope.NewError(envelope.ErrScheduleDisabled, lr.Error.Message))
	}

	if err := prepareScheduledPending(stateDir, cfg); err != nil {
		lr.Error = &schedule.LastRunError{Code: string(envelope.ErrInternalError), Message: err.Error()}
		return nil, scheduleRunFailure(schedule.LastRunPath(stateDir), lr, envelope.NewError(envelope.ErrInternalError, lr.Error.Message))
	}
	manifestPath := scheduleManifestForRun(flags, cfg)

	// Verify in-process. Reuse RunVerify with events disabled (no NDJSON for
	// headless scheduled runs — event contract v1 is untouched).
	verifyResult, verifyEnvErr := scheduleVerifyFn(VerifyFlags{
		Manifest: manifestPath,
		Events:   "",
	})

	if verifyEnvErr != nil {
		lr.Error = &schedule.LastRunError{
			Code:    string(verifyEnvErr.Code),
			Message: verifyEnvErr.Message,
		}
		return nil, scheduleRunFailure(schedule.LastRunPath(stateDir), lr, verifyEnvErr)
	}

	// Build verify summary for last-run.json.
	if vr, ok := verifyResult.(*VerifyResult); ok {
		lrVerify := &schedule.LastRunVerify{
			Summary: schedule.LastRunVerifySummary{
				Total: vr.Summary.Total,
				Pass:  vr.Summary.Pass,
				Fail:  vr.Summary.Fail,
			},
		}
		for _, item := range vr.Results {
			if item.Status == "fail" {
				lrVerify.Drifted = append(lrVerify.Drifted, schedule.LastRunDriftItem{
					ID:     item.ID,
					Name:   item.Name,
					Status: item.Status,
					Reason: item.Reason,
				})
			}
		}
		lr.Verify = lrVerify
	}

	// Auto-push captures only after observed drift. It first persists the exact
	// fresh bundle locally, then pushes that bundle. A cloud failure leaves the
	// published artifact and hash in config for the next scheduled run.
	if cfg.AutoPush {
		drifted := lr.Verify != nil && lr.Verify.Summary.Fail > 0
		lr.AutoBackup = runScheduleAutoPush(stateDir, cfg, drifted)
	}

	// Write last-run.json atomically. Drift is data — always exit 0.
	lr.Status = "completed"
	if err := finalizeScheduleRun(schedule.LastRunPath(stateDir), lr); err != nil {
		return nil, scheduleRunFailure(schedule.LastRunPath(stateDir), lr, envelope.NewError(envelope.ErrInternalError, "Failed to write schedule last-run state: "+err.Error()))
	}

	return &ScheduleRunData{
		RunID:        lr.RunID,
		TimestampUTC: lr.TimestampUTC,
		Verify:       lr.Verify,
		AutoBackup:   lr.AutoBackup,
		Error:        lr.Error,
	}, nil
}

func finalizeScheduleRun(path string, lr *schedule.LastRun) error {
	return scheduleWriteLastRunFn(path, lr)
}

func scheduleRunFailure(path string, lr *schedule.LastRun, original *envelope.Error) *envelope.Error {
	lr.Status = "failed"
	if err := finalizeScheduleRun(path, lr); err != nil {
		// Do not leave an older green document visible after a failed run. The
		// absence of last-run is an honest unknown state; status never infers a
		// healthy result from it.
		_ = scheduleRemoveLastRunFn(path)
		return envelope.NewError(envelope.ErrInternalError, "Failed to write schedule last-run state: "+err.Error())
	}
	return original
}

// runScheduleAutoPush retries a persisted capture first. With no pending
// capture it creates one only when verify found real drift. That capture is
// atomically recorded as both the new local baseline and pending cloud input.
func runScheduleAutoPush(stateDir string, cfg *schedule.Config, drifted bool) *schedule.LastRunBackup {
	cfg.NormalizePendingUploads()
	// Capture is independent of Cloud availability. A detected change always
	// advances the local baseline and is queued before any network attempt.
	if drifted {
		captured, captureErr := publishScheduledCapture(stateDir)
		if captureErr != nil {
			return &schedule.LastRunBackup{Outcome: "error"}
		}
		cfg.FallbackManifest = cfg.Manifest
		cfg.FallbackManifests = append(cfg.FallbackManifests, cfg.Manifest)
		cfg.Manifest = captured.path
		appendScheduledPending(cfg, captured)
		if err := pruneScheduledArtifacts(stateDir, cfg); err != nil {
			return &schedule.LastRunBackup{Outcome: "error"}
		}
		if err := scheduleWriteConfigFn(schedule.ConfigPath(stateDir), cfg); err != nil {
			return &schedule.LastRunBackup{Outcome: "error"}
		}
	}
	if len(cfg.PendingUploads) == 0 {
		return &schedule.LastRunBackup{Outcome: "skipped"}
	}

	skippedAll := true
	for len(cfg.PendingUploads) > 0 {
		head := &cfg.PendingUploads[0]
		if !isScheduledPendingArtifact(stateDir, head.Artifact) {
			cfg.ReplacePendingUploads(cfg.PendingUploads[1:])
			if err := scheduleWriteConfigFn(schedule.ConfigPath(stateDir), cfg); err != nil {
				return &schedule.LastRunBackup{Outcome: "error"}
			}
			return &schedule.LastRunBackup{Outcome: "error"}
		}
		if !scheduledArtifactMatches(head.Artifact, head.SHA256) {
			if err := quarantineScheduledArtifact(head.Artifact); err != nil {
				return &schedule.LastRunBackup{Outcome: "error"}
			}
			cfg.ReplacePendingUploads(cfg.PendingUploads[1:])
			if err := pruneScheduledArtifacts(stateDir, cfg); err != nil {
				return &schedule.LastRunBackup{Outcome: "error"}
			}
			if err := scheduleWriteConfigFn(schedule.ConfigPath(stateDir), cfg); err != nil {
				return &schedule.LastRunBackup{Outcome: "error"}
			}
			continue
		}
		scheduledCreate := &upload.ScheduledCreate{
			StateDir: stateDir, BackupID: cfg.BackupID, OperationID: head.OperationID,
			CreateSpool: head.CreateSpool, LegacyCreateStarted: head.LegacyCreateStarted,
			Persist: func(state upload.ScheduledCreate) error {
				head.BackupID = state.BackupID
				if state.BackupID != "" {
					cfg.BackupID = state.BackupID
				}
				head.OperationID = state.OperationID
				head.CreateSpool = state.CreateSpool
				head.LegacyCreateStarted = state.LegacyCreateStarted
				return scheduleWriteConfigFn(schedule.ConfigPath(stateDir), cfg)
			},
		}
		raw, backupErr := scheduleBackupFn(BackupFlags{Subcommand: "push", Profile: head.Artifact, BackupID: cfg.BackupID, IfChanged: true, ScheduledCreate: scheduledCreate})
		if backupErr != nil {
			switch backupErr.Code {
			case envelope.ErrBackupSetupRequired:
				return &schedule.LastRunBackup{Outcome: "setup_required"}
			case envelope.ErrBackupUploadUncertain:
				return &schedule.LastRunBackup{Outcome: "upload_uncertain"}
			case envelope.ErrAuthRequired:
				return &schedule.LastRunBackup{Outcome: "auth_required"}
			case envelope.ErrSubscriptionRequired:
				return &schedule.LastRunBackup{Outcome: "subscription_required"}
			case envelope.ErrBackendUnreachable:
				return &schedule.LastRunBackup{Outcome: "offline"}
			default:
				return &schedule.LastRunBackup{Outcome: "error"}
			}
		}
		if pushed, ok := raw.(*PushResult); !ok || !pushed.Skipped {
			skippedAll = false
			if ok && pushed.BackupID != "" {
				cfg.BackupID = pushed.BackupID
			}
		} else if pushed.BackupID != "" {
			cfg.BackupID = pushed.BackupID
		}
		completedSpool := head.CreateSpool
		cfg.ReplacePendingUploads(cfg.PendingUploads[1:])
		if err := pruneScheduledArtifacts(stateDir, cfg); err != nil {
			return &schedule.LastRunBackup{Outcome: "error"}
		}
		if err := scheduleWriteConfigFn(schedule.ConfigPath(stateDir), cfg); err != nil {
			return &schedule.LastRunBackup{Outcome: "error"}
		}
		// Durable queue drain precedes best-effort cleanup. A crash here leaves an
		// orphaned ciphertext spool, never a queue item whose replay bytes vanished.
		if completedSpool != "" && scheduledCreateSpoolPath(stateDir, completedSpool) {
			_ = pruneScheduledCreateSpools(stateDir, cfg)
		}
	}
	if skippedAll {
		return &schedule.LastRunBackup{Outcome: "skipped"}
	}
	return &schedule.LastRunBackup{Outcome: "pushed"}
}

func scheduledCreateSpoolPath(stateDir, path string) bool {
	root := filepath.Clean(filepath.Join(stateDir, "schedule", "create-spool"))
	rel, err := filepath.Rel(root, filepath.Clean(path))
	return err == nil && rel != "." && rel != "" && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// pruneScheduledCreateSpools removes only unreferenced direct children of the
// scheduler-owned spool directory. It intentionally ignores directories and
// every non-canonical reference so queue state can never authorize deletion
// outside the state root.
func pruneScheduledCreateSpools(stateDir string, cfg *schedule.Config) error {
	dir := filepath.Clean(filepath.Join(stateDir, "schedule", "create-spool"))
	keep := make(map[string]bool, len(cfg.PendingUploads))
	for _, pending := range cfg.PendingUploads {
		if scheduledCreateSpoolPath(stateDir, pending.CreateSpool) {
			keep[filepath.Clean(pending.CreateSpool)] = true
		}
	}
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read scheduled create spools: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		if keep[filepath.Clean(path)] {
			continue
		}
		if err := scheduleRemoveArtifactFn(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove orphaned scheduled create spool: %w", err)
		}
	}
	return nil
}

// pruneScheduledArtifacts keeps the current baseline, queued captures, and one
// local fallback. It never deletes a configured profile outside Endstate's
// schedule-pending directory.
func pruneScheduledArtifacts(stateDir string, cfg *schedule.Config) error {
	pendingDir := filepath.Clean(filepath.Join(stateDir, "schedule", "pending"))
	keep := map[string]bool{cfg.Manifest: true}
	for _, pending := range cfg.PendingUploads {
		if !isScheduledPendingArtifact(stateDir, pending.Artifact) {
			continue
		}
		keep[pending.Artifact] = true
	}
	var fallback string
	for i := len(cfg.FallbackManifests) - 1; i >= 0; i-- {
		candidate := cfg.FallbackManifests[i]
		if fallback == "" && candidate != "" && scheduledArtifactExists(candidate) {
			fallback = candidate
			keep[candidate] = true
		}
	}
	cfg.FallbackManifest = fallback
	if fallback == "" {
		cfg.FallbackManifests = nil
	} else {
		cfg.FallbackManifests = []string{fallback}
	}
	entries, err := os.ReadDir(pendingDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read scheduled artifacts: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		path := filepath.Join(pendingDir, entry.Name())
		if !keep[path] {
			if err := scheduleRemoveArtifactFn(path); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("remove superseded scheduled artifact: %w", err)
			}
		}
	}
	return nil
}

// prepareScheduledPending clears a stale retry queue before selecting the
// verification baseline. A capture that became the baseline is never silently
// replaced here: if it is missing, verification reports that fact rather than
// inventing a new capture without observed drift.
func prepareScheduledPending(stateDir string, cfg *schedule.Config) error {
	cfg.NormalizePendingUploads()
	changed := false
	lostBaseline := false
	valid := cfg.PendingUploads[:0]
	for _, pending := range cfg.PendingUploads {
		if !isScheduledPendingArtifact(stateDir, pending.Artifact) {
			changed = true
			if cfg.Manifest == pending.Artifact {
				lostBaseline = true
			}
			continue
		}
		if scheduledArtifactMatches(pending.Artifact, pending.SHA256) {
			valid = append(valid, pending)
			continue
		}
		if err := quarantineScheduledArtifact(pending.Artifact); err != nil {
			return fmt.Errorf("quarantine stale pending capture: %w", err)
		}
		changed = true
		if cfg.Manifest == pending.Artifact && scheduledFallback(cfg) != "" {
			cfg.Manifest = scheduledFallback(cfg)
		} else if cfg.Manifest == pending.Artifact {
			lostBaseline = true
		}
	}
	cfg.ReplacePendingUploads(valid)
	if lostBaseline {
		// Older builds promoted the one pending capture directly to Manifest.
		// If that file is corrupt there is no trustworthy profile to verify, so
		// publish a new local baseline before continuing. This remains local even
		// while auth or network access is unavailable, and its Cloud delivery is
		// simply appended to the repaired queue.
		captured, err := publishScheduledCapture(stateDir)
		if err != nil {
			return fmt.Errorf("rebuild missing schedule baseline: %w", err)
		}
		cfg.Manifest = captured.path
		appendScheduledPending(cfg, captured)
		changed = true
	}
	if changed {
		if err := scheduleWriteConfigFn(schedule.ConfigPath(stateDir), cfg); err != nil {
			return fmt.Errorf("persist repaired pending queue: %w", err)
		}
	}
	return pruneScheduledCreateSpools(stateDir, cfg)
}

func scheduledFallback(cfg *schedule.Config) string {
	for i := len(cfg.FallbackManifests) - 1; i >= 0; i-- {
		candidate := cfg.FallbackManifests[i]
		if candidate != "" && scheduledArtifactExists(candidate) {
			return candidate
		}
	}
	if cfg.FallbackManifest != "" && scheduledArtifactExists(cfg.FallbackManifest) {
		return cfg.FallbackManifest
	}
	return ""
}

func scheduledArtifactExists(path string) bool { _, err := os.Stat(path); return err == nil }

func isScheduledPendingArtifact(stateDir, path string) bool {
	pendingDir, err := filepath.Abs(filepath.Join(stateDir, "schedule", "pending"))
	if err != nil {
		return false
	}
	candidate, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return false
	}
	return filepath.Dir(candidate) == pendingDir && filepath.Base(candidate) == filepath.Clean(filepath.Base(candidate))
}

type scheduledCapture struct {
	path   string
	sha256 string
}

func publishScheduledCapture(stateDir string) (scheduledCapture, error) {
	pendingDir := filepath.Join(stateDir, "schedule", "pending")
	if err := os.MkdirAll(pendingDir, 0o755); err != nil {
		return scheduledCapture{}, err
	}
	stage := filepath.Join(pendingDir, ".capture-"+time.Now().UTC().Format("20060102-150405.000000000")+".endstate")
	cleanupStage := true
	defer func() {
		if cleanupStage {
			_ = scheduleRemoveArtifactFn(stage)
		}
	}()
	raw, captureErr := scheduleCaptureFn(CaptureFlags{Out: stage})
	if captureErr != nil {
		return scheduledCapture{}, fmt.Errorf("schedule capture: %s", captureErr.Message)
	}
	result, ok := raw.(*CaptureResult)
	if !ok || result.OutputPath == "" {
		return scheduledCapture{}, fmt.Errorf("schedule capture returned no output artifact")
	}
	data, err := os.ReadFile(result.OutputPath)
	if err != nil {
		return scheduledCapture{}, err
	}
	if len(data) == 0 {
		return scheduledCapture{}, fmt.Errorf("schedule capture wrote an empty artifact")
	}
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])
	finalPath := filepath.Join(pendingDir, hash+".endstate")
	if _, err := os.Stat(finalPath); err == nil {
		if !scheduledArtifactMatches(finalPath, hash) {
			return scheduledCapture{}, fmt.Errorf("schedule capture final path has unexpected content")
		}
		if err := os.Remove(result.OutputPath); err != nil {
			return scheduledCapture{}, err
		}
		return scheduledCapture{path: finalPath, sha256: hash}, nil
	} else if !os.IsNotExist(err) {
		return scheduledCapture{}, err
	}
	if err := os.Rename(result.OutputPath, finalPath); err != nil {
		return scheduledCapture{}, err
	}
	cleanupStage = false
	return scheduledCapture{path: finalPath, sha256: hash}, nil
}

func appendScheduledPending(cfg *schedule.Config, captured scheduledCapture) {
	for _, pending := range cfg.PendingUploads {
		if pending.Artifact == captured.path && pending.SHA256 == captured.sha256 {
			return
		}
	}
	cfg.ReplacePendingUploads(append(cfg.PendingUploads, schedule.PendingUpload{Artifact: captured.path, SHA256: captured.sha256, CapturedAt: schedule.NowUTC()}))
}

func scheduledArtifactMatches(path, expected string) bool {
	if expected == "" {
		return false
	}
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		return false
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]) == expected
}

// quarantineScheduledArtifact retains corrupt pending bytes for diagnosis
// without letting an impossible retry queue block future observed drift.
func quarantineScheduledArtifact(path string) error {
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	quarantineDir := filepath.Join(filepath.Dir(filepath.Dir(path)), "quarantine")
	if err := os.MkdirAll(quarantineDir, 0o755); err != nil {
		return err
	}
	quarantine := filepath.Join(quarantineDir, filepath.Base(path)+".corrupt-"+time.Now().UTC().Format("20060102-150405.000000000"))
	if err := scheduleRenameArtifactFn(path, quarantine); err != nil {
		return err
	}
	entries, err := os.ReadDir(quarantineDir)
	if err != nil {
		return err
	}
	const maxQuarantine = 5
	if len(entries) <= maxQuarantine {
		return nil
	}
	for _, entry := range entries[:len(entries)-maxQuarantine] {
		if err := scheduleRemoveArtifactFn(filepath.Join(quarantineDir, entry.Name())); err != nil {
			return fmt.Errorf("prune corrupt evidence: %w", err)
		}
	}
	return nil
}

// scheduleManifestForRun keeps the scheduled verification baseline explicit:
// once a fresh capture is published it is the next run's configured profile.
func scheduleManifestForRun(flags ScheduleFlags, cfg *schedule.Config) string {
	if flags.Manifest != "" {
		return flags.Manifest
	}
	return cfg.Manifest
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// scheduleRoot resolves the engine root for schedule operations.
// Priority: explicit --root flag value > ENDSTATE_ROOT env var > exe-walk
// (config.ResolveRepoRoot). Mirrors the resolution order used throughout the
// engine; --root acts exactly as an ENDSTATE_ROOT override per the spec.
func scheduleRoot(flagRoot string) string {
	root, _ := scheduleRootWithError(flagRoot)
	return root
}

func scheduleRootWithError(flagRoot string) (string, error) {
	if flagRoot != "" {
		return canonicalSchedulePath(flagRoot)
	}
	return canonicalSchedulePath(config.ResolveRepoRoot())
}

func canonicalSchedulePath(path string) (string, error) {
	if path == "" {
		return "", nil
	}
	if strings.IndexByte(path, 0) >= 0 {
		return "", fmt.Errorf("path contains a NUL byte")
	}
	abs, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return "", err
	}
	return abs, nil
}

// scheduleStateDir returns the schedule state directory (root/state) for
// the given --root override. When flagRoot is empty it falls through to
// config.ResolveRepoRoot(), matching state.StateDir() exactly.
func scheduleStateDir(flagRoot string) string {
	root := scheduleRoot(flagRoot)
	if root != "" {
		return filepath.Join(root, "state")
	}
	return filepath.Join(".", "state")
}
