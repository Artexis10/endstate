// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

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
	TaskName string `json:"taskName"`
	Root     string `json:"root"`
}

// ScheduleDisableData is the data payload for `schedule disable --json`.
type ScheduleDisableData struct {
	Enabled  bool   `json:"enabled"`
	TaskName string `json:"taskName"`
}

// ScheduleStatusData is the data payload for `schedule status --json`.
// It composes config.json + last-run.json and is the sole client-facing
// drift-truth source (CLI is source of truth invariant).
type ScheduleStatusData struct {
	Enabled  bool             `json:"enabled"`
	Manifest string           `json:"manifest,omitempty"`
	Interval string           `json:"interval,omitempty"`
	Time     string           `json:"time,omitempty"`
	AutoPush bool             `json:"autoPush"`
	TaskName string           `json:"taskName,omitempty"`
	LastRun  *schedule.LastRun `json:"lastRun,omitempty"`
}

// ScheduleRunData is the data payload for `schedule run --json`.
// It re-serialises the last-run document into the envelope for manual/debug
// invocations; the task itself writes last-run.json and exits 0.
type ScheduleRunData struct {
	RunID        string                   `json:"runId"`
	TimestampUTC string                   `json:"timestampUtc"`
	Verify       *schedule.LastRunVerify  `json:"verify,omitempty"`
	AutoBackup   *schedule.LastRunBackup  `json:"autoBackup,omitempty"`
	Error        *schedule.LastRunError   `json:"error,omitempty"`
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
	default:
		return nil, envelope.NewError(
			envelope.ErrInternalError,
			fmt.Sprintf("unknown schedule subcommand %q; use enable|disable|status|run", flags.Subcommand),
		)
	}
}

// ---------------------------------------------------------------------------
// schedule enable
// ---------------------------------------------------------------------------

func runScheduleEnable(flags ScheduleFlags) (interface{}, *envelope.Error) {
	if runtime.GOOS != "windows" {
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
	if _, err := os.Stat(flags.Manifest); os.IsNotExist(err) {
		return nil, envelope.NewError(
			envelope.ErrManifestNotFound,
			"The specified manifest file does not exist.",
		).WithDetail(map[string]string{"path": flags.Manifest})
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
	root := scheduleRoot(flags.Root)

	// Bake the root into the registered task command line so scheduled runs and
	// GUI-spawned runs share one state directory. Task Scheduler cannot set env vars.
	exePath, exeErr := os.Executable()
	if exeErr != nil {
		return nil, envelope.NewError(
			envelope.ErrInternalError,
			"Could not resolve current executable path: "+exeErr.Error(),
		)
	}

	taskArgs := fmt.Sprintf(`schedule run --root "%s" --json`, root)

	reg := scheduleRegistrarFn()
	if err := reg.Register(schedule.TaskName, exePath, taskArgs, interval, timeOfDay); err != nil {
		return nil, envelope.NewError(
			envelope.ErrTaskRegistrationFailed,
			"Failed to register scheduled task: "+err.Error(),
		).WithRemediation("Ensure schtasks.exe is available and the user has permission to create tasks.")
	}

	stateDir := scheduleStateDir(flags.Root)
	cfg := &schedule.Config{
		SchemaVersion: "1.0",
		Enabled:       true,
		Manifest:      flags.Manifest,
		Interval:      interval,
		Time:          timeOfDay,
		AutoPush:      flags.AutoPush,
		TaskName:      schedule.TaskName,
		Root:          root,
		RegisteredAt:  schedule.NowUTC(),
	}
	if err := schedule.WriteConfig(schedule.ConfigPath(stateDir), cfg); err != nil {
		// Best-effort cleanup: unregister the task we just created so we never
		// leave a registered task without a matching config.
		_ = reg.Unregister(schedule.TaskName)
		return nil, envelope.NewError(
			envelope.ErrInternalError,
			"Failed to write schedule config: "+err.Error(),
		)
	}

	return &ScheduleEnableData{
		Enabled:  true,
		Manifest: cfg.Manifest,
		Interval: cfg.Interval,
		Time:     cfg.Time,
		AutoPush: cfg.AutoPush,
		TaskName: cfg.TaskName,
		Root:     cfg.Root,
	}, nil
}

// ---------------------------------------------------------------------------
// schedule disable
// ---------------------------------------------------------------------------

func runScheduleDisable(flags ScheduleFlags) (interface{}, *envelope.Error) {
	if runtime.GOOS != "windows" {
		return nil, envelope.NewError(
			envelope.ErrNotSupported,
			"schedule disable is only supported on Windows.",
		)
	}

	stateDir := scheduleStateDir(flags.Root)

	reg := scheduleRegistrarFn()
	if err := reg.Unregister(schedule.TaskName); err != nil {
		return nil, envelope.NewError(
			envelope.ErrInternalError,
			"Failed to unregister scheduled task: "+err.Error(),
		)
	}

	cfgPath := schedule.ConfigPath(stateDir)
	cfg, err := schedule.ReadConfig(cfgPath)
	if err != nil {
		return nil, envelope.NewError(
			envelope.ErrInternalError,
			"Failed to read schedule config: "+err.Error(),
		)
	}
	cfg.Enabled = false
	if err := schedule.WriteConfig(cfgPath, cfg); err != nil {
		return nil, envelope.NewError(
			envelope.ErrInternalError,
			"Failed to write schedule config: "+err.Error(),
		)
	}

	return &ScheduleDisableData{
		Enabled:  false,
		TaskName: schedule.TaskName,
	}, nil
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

	return &ScheduleStatusData{
		Enabled:  cfg.Enabled,
		Manifest: cfg.Manifest,
		Interval: cfg.Interval,
		Time:     cfg.Time,
		AutoPush: cfg.AutoPush,
		TaskName: cfg.TaskName,
		LastRun:  lr,
	}, nil
}

// ---------------------------------------------------------------------------
// schedule run
// ---------------------------------------------------------------------------

// runScheduleRun is the task payload: verify in-process, optionally push,
// write last-run.json. Drift is data (exit 0). Hard errors are recorded in
// last-run.json with stable codes. No NDJSON events are emitted.
func runScheduleRun(flags ScheduleFlags) (interface{}, *envelope.Error) {
	if runtime.GOOS != "windows" {
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
	}

	// Load config.
	cfg, err := schedule.ReadConfig(schedule.ConfigPath(stateDir))
	if err != nil {
		lr.Error = &schedule.LastRunError{
			Code:    string(envelope.ErrInternalError),
			Message: "Failed to read schedule config: " + err.Error(),
		}
		_ = schedule.WriteLastRun(schedule.LastRunPath(stateDir), lr)
		return nil, envelope.NewError(envelope.ErrInternalError, lr.Error.Message)
	}

	if !cfg.Enabled {
		lr.Error = &schedule.LastRunError{
			Code:    string(envelope.ErrScheduleDisabled),
			Message: "Schedule is not enabled; run 'schedule enable --manifest <path>' first.",
		}
		_ = schedule.WriteLastRun(schedule.LastRunPath(stateDir), lr)
		return nil, envelope.NewError(envelope.ErrScheduleDisabled, lr.Error.Message)
	}

	// Use manifest from flags if provided; otherwise use configured manifest.
	manifestPath := flags.Manifest
	if manifestPath == "" {
		manifestPath = cfg.Manifest
	}

	// Verify in-process. Reuse RunVerify with events disabled (no NDJSON for
	// headless scheduled runs — event contract v1 is untouched).
	verifyResult, verifyEnvErr := RunVerify(VerifyFlags{
		Manifest: manifestPath,
		Events:   "",
	})

	if verifyEnvErr != nil {
		lr.Error = &schedule.LastRunError{
			Code:    string(verifyEnvErr.Code),
			Message: verifyEnvErr.Message,
		}
		_ = schedule.WriteLastRun(schedule.LastRunPath(stateDir), lr)
		return nil, verifyEnvErr
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

	// auto-push: capture + push --if-changed when configured. Auth failures and
	// other outcomes are recorded in last-run.json — never prompted interactively.
	if cfg.AutoPush {
		lr.AutoBackup = runScheduleAutoPush(manifestPath)
	}

	// Write last-run.json atomically. Drift is data — always exit 0.
	_ = schedule.WriteLastRun(schedule.LastRunPath(stateDir), lr)

	return &ScheduleRunData{
		RunID:        lr.RunID,
		TimestampUTC: lr.TimestampUTC,
		Verify:       lr.Verify,
		AutoBackup:   lr.AutoBackup,
		Error:        lr.Error,
	}, nil
}

// runScheduleAutoPush attempts a backup push with if-changed semantics via the
// existing backup command path. It never prompts interactively.
func runScheduleAutoPush(manifestPath string) *schedule.LastRunBackup {
	_, backupErr := RunBackup(BackupFlags{
		Subcommand: "push",
		Profile:    manifestPath,
		IfChanged:  true,
	})
	if backupErr != nil {
		code := string(backupErr.Code)
		switch envelope.ErrorCode(code) {
		case envelope.ErrAuthRequired, envelope.ErrSubscriptionRequired:
			return &schedule.LastRunBackup{Outcome: "auth_required"}
		default:
			return &schedule.LastRunBackup{Outcome: "error"}
		}
	}
	return &schedule.LastRunBackup{Outcome: "pushed"}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// scheduleRoot resolves the engine root for schedule operations.
// Priority: explicit --root flag value > ENDSTATE_ROOT env var > exe-walk
// (config.ResolveRepoRoot). Mirrors the resolution order used throughout the
// engine; --root acts exactly as an ENDSTATE_ROOT override per the spec.
func scheduleRoot(flagRoot string) string {
	if flagRoot != "" {
		return flagRoot
	}
	return config.ResolveRepoRoot()
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
