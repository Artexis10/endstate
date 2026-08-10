// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/schedule"
	"github.com/gofrs/flock"
)

// ---------------------------------------------------------------------------
// Fake registrar for command-handler tests
// ---------------------------------------------------------------------------

type fakeScheduleRegistrar struct {
	registerErr     error
	unregisterErr   error
	registerCalls   int
	unregisterCalls int
	lastArgs        string
}

func (f *fakeScheduleRegistrar) Register(taskName, exePath, args, interval, timeOfDay string) error {
	f.registerCalls++
	f.lastArgs = args
	return f.registerErr
}

func (f *fakeScheduleRegistrar) Unregister(taskName string) error {
	f.unregisterCalls++
	return f.unregisterErr
}

// withFakeRegistrar installs a fake registrar for the duration of the test
// and restores the original on cleanup.
func withFakeRegistrar(t *testing.T, reg *fakeScheduleRegistrar) {
	t.Helper()
	orig := scheduleRegistrarFn
	scheduleRegistrarFn = func() schedule.Registrar { return reg }
	t.Cleanup(func() { scheduleRegistrarFn = orig })
}

// withStateRoot points ENDSTATE_ROOT at a temp dir for the duration of the test.
func withStateRoot(t *testing.T, dir string) {
	t.Helper()
	orig := os.Getenv("ENDSTATE_ROOT")
	os.Setenv("ENDSTATE_ROOT", dir)
	t.Cleanup(func() { os.Setenv("ENDSTATE_ROOT", orig) })
}

// ---------------------------------------------------------------------------
// 2.6 Unit tests: handlers, capabilities shape, last-run, error envelopes
// ---------------------------------------------------------------------------

// TestRunScheduleStatus_NeverRun verifies that status returns enabled:false
// and no lastRun when neither config.json nor last-run.json exist.
func TestRunScheduleStatus_NeverRun(t *testing.T) {
	dir := t.TempDir()
	withStateRoot(t, dir)

	data, envErr := RunSchedule(ScheduleFlags{Subcommand: "status"})
	if envErr != nil {
		t.Fatalf("status returned error: %v", envErr)
	}
	sd, ok := data.(*ScheduleStatusData)
	if !ok {
		t.Fatalf("data type = %T, want *ScheduleStatusData", data)
	}
	if sd.Enabled {
		t.Error("Enabled = true on never-configured machine, want false")
	}
	if sd.LastRun != nil {
		t.Errorf("LastRun = %v, want nil (never-run)", sd.LastRun)
	}
}

// TestRunScheduleStatus_SurfacesDrift verifies that status exposes drifted items
// from last-run.json so clients can distinguish drift from clean state.
func TestRunScheduleStatus_SurfacesDrift(t *testing.T) {
	dir := t.TempDir()
	withStateRoot(t, dir)

	// Pre-write a config and a last-run with drift.
	stateDir := filepath.Join(dir, "state")
	cfg := &schedule.Config{
		SchemaVersion: "1.0",
		Enabled:       true,
		Manifest:      filepath.Join(dir, "manifest.jsonc"),
		Interval:      "daily",
		Time:          "09:00",
		TaskName:      schedule.TaskName,
		Root:          dir,
	}
	if err := schedule.WriteConfig(schedule.ConfigPath(stateDir), cfg); err != nil {
		t.Fatalf("WriteConfig: %v", err)
	}
	lr := &schedule.LastRun{
		SchemaVersion: "1.0",
		RunID:         "schedule-20260710-090000",
		TimestampUTC:  "2026-07-10T09:00:00Z",
		Verify: &schedule.LastRunVerify{
			Summary: schedule.LastRunVerifySummary{Total: 3, Pass: 0, Fail: 3},
			Drifted: []schedule.LastRunDriftItem{
				{ID: "vscode", Status: "fail", Reason: "missing"},
				{ID: "git", Status: "fail", Reason: "missing"},
				{ID: "gh", Status: "fail", Reason: "missing"},
			},
		},
	}
	if err := schedule.WriteLastRun(schedule.LastRunPath(stateDir), lr); err != nil {
		t.Fatalf("WriteLastRun: %v", err)
	}

	data, envErr := RunSchedule(ScheduleFlags{Subcommand: "status"})
	if envErr != nil {
		t.Fatalf("status error: %v", envErr)
	}
	sd := data.(*ScheduleStatusData)
	if sd.LastRun == nil {
		t.Fatal("LastRun = nil, want drift data")
	}
	if sd.LastRun.Verify == nil {
		t.Fatal("LastRun.Verify = nil")
	}
	if len(sd.LastRun.Verify.Drifted) != 3 {
		t.Errorf("drifted len = %d, want 3", len(sd.LastRun.Verify.Drifted))
	}
}

func TestRunScheduleStatus_ExposesPendingUploadWithoutLocalPath(t *testing.T) {
	dir := t.TempDir()
	withStateRoot(t, dir)
	stateDir := filepath.Join(dir, "state")
	cfg := &schedule.Config{SchemaVersion: "1.0", Enabled: true, PendingArtifact: `C:\\private\\pending.endstate`, PendingSHA256: "abc123"}
	if err := schedule.WriteConfig(schedule.ConfigPath(stateDir), cfg); err != nil {
		t.Fatal(err)
	}
	if err := schedule.WriteLastRun(schedule.LastRunPath(stateDir), &schedule.LastRun{AutoBackup: &schedule.LastRunBackup{Outcome: "auth_required"}}); err != nil {
		t.Fatal(err)
	}
	data, envErr := RunSchedule(ScheduleFlags{Subcommand: "status"})
	if envErr != nil {
		t.Fatal(envErr)
	}
	status := data.(*ScheduleStatusData)
	if !status.PendingUpload.Pending || status.PendingUpload.Count != 1 || status.PendingUpload.ArtifactSHA != "abc123" || status.PendingUpload.LastOutcome != "auth_required" {
		t.Fatalf("pending upload = %+v", status.PendingUpload)
	}
}

// TestRunScheduleStatus_DistinguishesHardError verifies that a last-run.json
// with an error block is surfaced via status (not silenced as "clean").
func TestRunScheduleStatus_DistinguishesHardError(t *testing.T) {
	dir := t.TempDir()
	withStateRoot(t, dir)

	stateDir := filepath.Join(dir, "state")
	cfg := &schedule.Config{SchemaVersion: "1.0", Enabled: true, TaskName: schedule.TaskName}
	_ = schedule.WriteConfig(schedule.ConfigPath(stateDir), cfg)

	lr := &schedule.LastRun{
		SchemaVersion: "1.0",
		RunID:         "schedule-20260710-090001",
		TimestampUTC:  "2026-07-10T09:00:01Z",
		Error: &schedule.LastRunError{
			Code:    "MANIFEST_NOT_FOUND",
			Message: "manifest missing",
		},
	}
	_ = schedule.WriteLastRun(schedule.LastRunPath(stateDir), lr)

	data, envErr := RunSchedule(ScheduleFlags{Subcommand: "status"})
	if envErr != nil {
		t.Fatalf("status error: %v", envErr)
	}
	sd := data.(*ScheduleStatusData)
	if sd.LastRun == nil {
		t.Fatal("LastRun = nil, want error block")
	}
	if sd.LastRun.Error == nil {
		t.Fatal("LastRun.Error = nil, want error with code")
	}
	if sd.LastRun.Error.Code != "MANIFEST_NOT_FOUND" {
		t.Errorf("LastRun.Error.Code = %q, want MANIFEST_NOT_FOUND", sd.LastRun.Error.Code)
	}
}

// TestRunScheduleEnable_NonWindows verifies NOT_SUPPORTED on non-Windows.
func TestRunScheduleEnable_NonWindows(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("NOT_SUPPORTED path only applies to non-Windows")
	}

	_, envErr := RunSchedule(ScheduleFlags{
		Subcommand: "enable",
		Manifest:   "/some/manifest.jsonc",
	})
	if envErr == nil {
		t.Fatal("expected error on non-Windows, got nil")
	}
	if envErr.Code != envelope.ErrNotSupported {
		t.Errorf("error code = %q, want NOT_SUPPORTED", envErr.Code)
	}
}

// TestRunScheduleDisable_NonWindows verifies NOT_SUPPORTED on non-Windows.
func TestRunScheduleDisable_NonWindows(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("NOT_SUPPORTED path only applies to non-Windows")
	}

	_, envErr := RunSchedule(ScheduleFlags{Subcommand: "disable"})
	if envErr == nil {
		t.Fatal("expected error on non-Windows, got nil")
	}
	if envErr.Code != envelope.ErrNotSupported {
		t.Errorf("error code = %q, want NOT_SUPPORTED", envErr.Code)
	}
}

// TestRunScheduleEnable_RegistrationFailure verifies TASK_REGISTRATION_FAILED
// error code and that config is not persisted as enabled.
func TestRunScheduleEnable_RegistrationFailure(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("schedule enable Windows-only")
	}

	dir := t.TempDir()
	withStateRoot(t, dir)

	manifestPath := filepath.Join(dir, "manifest.jsonc")
	_ = os.WriteFile(manifestPath, []byte(`{"version":1}`), 0644)

	reg := &fakeScheduleRegistrar{registerErr: errors.New("access denied")}
	withFakeRegistrar(t, reg)

	_, envErr := RunSchedule(ScheduleFlags{
		Subcommand: "enable",
		Manifest:   manifestPath,
	})
	if envErr == nil {
		t.Fatal("expected error, got nil")
	}
	if envErr.Code != envelope.ErrTaskRegistrationFailed {
		t.Errorf("error code = %q, want TASK_REGISTRATION_FAILED", envErr.Code)
	}

	// Config must not be written with enabled:true.
	stateDir := filepath.Join(dir, "state")
	cfg, _ := schedule.ReadConfig(schedule.ConfigPath(stateDir))
	if cfg != nil && cfg.Enabled {
		t.Error("config.Enabled = true after registration failure, want false")
	}
}

func TestRunScheduleEnable_CorruptExistingConfigDoesNotRegisterTask(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("schedule enable Windows-only")
	}
	dir := t.TempDir()
	withStateRoot(t, dir)
	manifestPath := filepath.Join(dir, "manifest.jsonc")
	if err := os.WriteFile(manifestPath, []byte(`{"version":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	stateDir := filepath.Join(dir, "state")
	if err := os.MkdirAll(filepath.Dir(schedule.ConfigPath(stateDir)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(schedule.ConfigPath(stateDir), []byte(`{not json`), 0o600); err != nil {
		t.Fatal(err)
	}
	reg := &fakeScheduleRegistrar{}
	withFakeRegistrar(t, reg)
	_, envErr := RunSchedule(ScheduleFlags{Subcommand: "enable", Manifest: manifestPath})
	if envErr == nil {
		t.Fatal("expected corrupt config error")
	}
	if reg.registerCalls != 0 {
		t.Fatalf("Register calls = %d, want 0", reg.registerCalls)
	}
}

func TestRunScheduleEnable_ConfigWriteFailureKeepsExistingHealthyTask(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("schedule enable Windows-only")
	}
	dir := t.TempDir()
	withStateRoot(t, dir)
	manifest := filepath.Join(dir, "manifest.jsonc")
	if err := os.WriteFile(manifest, []byte(`{"version":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	stateDir := filepath.Join(dir, "state")
	if err := schedule.WriteConfig(schedule.ConfigPath(stateDir), &schedule.Config{SchemaVersion: "1.0", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	reg := &fakeScheduleRegistrar{}
	withFakeRegistrar(t, reg)
	orig := scheduleWriteConfigFn
	t.Cleanup(func() { scheduleWriteConfigFn = orig })
	scheduleWriteConfigFn = func(string, *schedule.Config) error { return errors.New("disk full") }
	if _, err := RunSchedule(ScheduleFlags{Subcommand: "enable", Manifest: manifest}); err == nil {
		t.Fatal("expected config write error")
	}
	if reg.unregisterCalls != 0 {
		t.Fatalf("Unregister calls = %d, want 0", reg.unregisterCalls)
	}
}

func TestRunScheduleEnable_ReconfigurationWriteFailureDoesNotMutatePriorTask(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("schedule enable Windows-only")
	}
	dir := t.TempDir()
	withStateRoot(t, dir)
	manifest := filepath.Join(dir, "manifest.jsonc")
	if err := os.WriteFile(manifest, []byte(`{"version":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	oldRoot := dir
	stateDir := filepath.Join(dir, "state")
	if err := schedule.WriteConfig(schedule.ConfigPath(stateDir), &schedule.Config{SchemaVersion: "1.0", Enabled: true, Root: oldRoot, Interval: "weekly", Time: "08:30"}); err != nil {
		t.Fatal(err)
	}
	reg := &fakeScheduleRegistrar{}
	withFakeRegistrar(t, reg)
	orig := scheduleWriteConfigFn
	t.Cleanup(func() { scheduleWriteConfigFn = orig })
	scheduleWriteConfigFn = func(string, *schedule.Config) error { return errors.New("disk full") }
	if _, err := RunSchedule(ScheduleFlags{Subcommand: "enable", Manifest: manifest, Interval: "daily", Time: "09:00"}); err == nil {
		t.Fatal("expected write failure")
	}
	if reg.registerCalls != 0 {
		t.Fatalf("Register calls = %d, want 0 before config write succeeds", reg.registerCalls)
	}
	persisted, err := schedule.ReadConfig(schedule.ConfigPath(stateDir))
	if err != nil || !persisted.Enabled || persisted.Root != oldRoot || persisted.Interval != "weekly" {
		t.Fatalf("persisted config changed: %+v %v", persisted, err)
	}
}

func TestRunScheduleDisable_WaitsForRunningScheduleLockBeforeMutatingState(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("schedule disable Windows-only")
	}
	dir := t.TempDir()
	withStateRoot(t, dir)
	stateDir := filepath.Join(dir, "state")
	cfgPath := schedule.ConfigPath(stateDir)
	if err := schedule.WriteConfig(cfgPath, &schedule.Config{SchemaVersion: "1.0", Enabled: true, TaskName: schedule.TaskName}); err != nil {
		t.Fatal(err)
	}
	lock := flock.New(filepath.Join(stateDir, "schedule", "run.lock"))
	if err := lock.Lock(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lock.Unlock() }()
	reg := &fakeScheduleRegistrar{}
	withFakeRegistrar(t, reg)
	done := make(chan *envelope.Error, 1)
	go func() { _, err := RunSchedule(ScheduleFlags{Subcommand: "disable"}); done <- err }()
	select {
	case err := <-done:
		t.Fatalf("disable completed while a run lock was held: %v", err)
	case <-time.After(150 * time.Millisecond):
	}
	if reg.unregisterCalls != 0 {
		t.Fatalf("Unregister calls while run held lock = %d, want 0", reg.unregisterCalls)
	}
	before, err := schedule.ReadConfig(cfgPath)
	if err != nil || !before.Enabled {
		t.Fatalf("config changed while run held lock: %+v, %v", before, err)
	}
	if err := lock.Unlock(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("disable after lock release: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("disable did not complete after run lock release")
	}
	after, err := schedule.ReadConfig(cfgPath)
	if err != nil || after.Enabled || reg.unregisterCalls != 1 {
		t.Fatalf("task/config agreement after disable = cfg:%+v unregister:%d err:%v", after, reg.unregisterCalls, err)
	}
}

// TestRunScheduleEnable_BakesRoot verifies that the registered task args
// contain --root with the resolved root value.
func TestRunScheduleEnable_BakesRoot(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("schedule enable Windows-only")
	}

	dir := t.TempDir()
	withStateRoot(t, dir)

	manifestPath := filepath.Join(dir, "manifest.jsonc")
	_ = os.WriteFile(manifestPath, []byte(`{"version":1}`), 0644)

	reg := &fakeScheduleRegistrar{}
	withFakeRegistrar(t, reg)

	_, envErr := RunSchedule(ScheduleFlags{
		Subcommand: "enable",
		Manifest:   manifestPath,
	})
	if envErr != nil {
		t.Fatalf("enable error: %v", envErr)
	}

	if reg.registerCalls != 1 {
		t.Errorf("registerCalls = %d, want 1", reg.registerCalls)
	}
	// Registered args must contain --root.
	if !containsSubstr(reg.lastArgs, "--root") {
		t.Errorf("registered args %q do not contain --root", reg.lastArgs)
	}
}

// TestRunScheduleEnable_Idempotent verifies that calling enable twice
// results in two Register calls (schtasks /F makes it idempotent at OS level).
func TestRunScheduleEnable_Idempotent(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("schedule enable Windows-only")
	}

	dir := t.TempDir()
	withStateRoot(t, dir)

	manifestPath := filepath.Join(dir, "manifest.jsonc")
	_ = os.WriteFile(manifestPath, []byte(`{"version":1}`), 0644)

	reg := &fakeScheduleRegistrar{}
	withFakeRegistrar(t, reg)

	for i := 0; i < 2; i++ {
		_, envErr := RunSchedule(ScheduleFlags{
			Subcommand: "enable",
			Manifest:   manifestPath,
			Interval:   "daily",
			Time:       "09:00",
		})
		if envErr != nil {
			t.Fatalf("enable #%d error: %v", i+1, envErr)
		}
	}
	if reg.registerCalls != 2 {
		t.Errorf("registerCalls = %d, want 2", reg.registerCalls)
	}
}

// TestRunScheduleRun_DisabledReturnsError verifies that schedule run on a
// disabled schedule returns SCHEDULE_DISABLED and records it in last-run.json.
func TestRunScheduleRun_DisabledReturnsError(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("schedule run disabled-config path only reachable on Windows (non-Windows returns NOT_SUPPORTED first)")
	}

	dir := t.TempDir()
	withStateRoot(t, dir)

	// Write a disabled config.
	stateDir := filepath.Join(dir, "state")
	cfg := &schedule.Config{SchemaVersion: "1.0", Enabled: false}
	_ = schedule.WriteConfig(schedule.ConfigPath(stateDir), cfg)

	_, envErr := RunSchedule(ScheduleFlags{Subcommand: "run"})
	if envErr == nil {
		t.Fatal("expected error for disabled schedule, got nil")
	}
	if envErr.Code != envelope.ErrScheduleDisabled {
		t.Errorf("error code = %q, want SCHEDULE_DISABLED", envErr.Code)
	}

	// last-run.json must record the error.
	lr, readErr := schedule.ReadLastRun(schedule.LastRunPath(stateDir))
	if readErr != nil {
		t.Fatalf("ReadLastRun: %v", readErr)
	}
	if lr == nil {
		t.Fatal("last-run.json not written for disabled run")
	}
	if lr.Error == nil || lr.Error.Code == "" {
		t.Error("last-run.json has no error block")
	}
	if lr.Error != nil && lr.Error.Code != string(envelope.ErrScheduleDisabled) {
		t.Errorf("last-run.json error code = %q, want SCHEDULE_DISABLED", lr.Error.Code)
	}
}

// TestRunScheduleRun_NonWindows verifies that schedule run on a non-Windows
// platform returns NOT_SUPPORTED before attempting any state access.
func TestRunScheduleRun_NonWindows(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("NOT_SUPPORTED path only applies to non-Windows")
	}

	_, envErr := RunSchedule(ScheduleFlags{Subcommand: "run"})
	if envErr == nil {
		t.Fatal("expected error on non-Windows, got nil")
	}
	if envErr.Code != envelope.ErrNotSupported {
		t.Errorf("error code = %q, want NOT_SUPPORTED", envErr.Code)
	}
}

func TestRunScheduleRun_ReplacesPriorHealthyLastRunWithRunningMarkerBeforeVerify(t *testing.T) {
	dir := t.TempDir()
	withStateRoot(t, dir)
	stateDir := filepath.Join(dir, "state")
	if err := schedule.WriteConfig(schedule.ConfigPath(stateDir), &schedule.Config{SchemaVersion: "1.0", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := schedule.WriteLastRun(schedule.LastRunPath(stateDir), &schedule.LastRun{
		SchemaVersion: "1.0",
		RunID:         "schedule-prior",
		TimestampUTC:  "2026-08-10T00:00:00Z",
		Status:        "completed",
		Verify:        &schedule.LastRunVerify{Summary: schedule.LastRunVerifySummary{Total: 1, Pass: 1}},
	}); err != nil {
		t.Fatal(err)
	}

	origSupported, origVerify := scheduleRunSupportedFn, scheduleVerifyFn
	t.Cleanup(func() { scheduleRunSupportedFn, scheduleVerifyFn = origSupported, origVerify })
	scheduleRunSupportedFn = func() bool { return true }
	scheduleVerifyFn = func(VerifyFlags) (interface{}, *envelope.Error) {
		marker, err := schedule.ReadLastRun(schedule.LastRunPath(stateDir))
		if err != nil {
			t.Fatalf("ReadLastRun: %v", err)
		}
		if marker == nil || marker.Status != "running" {
			t.Fatalf("last-run during verification = %+v, want running marker", marker)
		}
		if marker.RunID == "schedule-prior" || marker.Verify != nil {
			t.Fatalf("prior healthy result remained visible during verification: %+v", marker)
		}
		return &VerifyResult{Summary: VerifySummary{Total: 1, Pass: 1}}, nil
	}

	if _, envErr := runScheduleRun(ScheduleFlags{}); envErr != nil {
		t.Fatalf("runScheduleRun: %v", envErr)
	}
	persisted, err := schedule.ReadLastRun(schedule.LastRunPath(stateDir))
	if err != nil {
		t.Fatalf("ReadLastRun: %v", err)
	}
	if persisted == nil || persisted.Status != "completed" {
		t.Fatalf("completed last-run = %+v, want completed status", persisted)
	}
}

func TestScheduleAutoPush_PersistsExactCaptureUntilCloudSuccess(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "state")
	origCapture, origBackup := scheduleCaptureFn, scheduleBackupFn
	t.Cleanup(func() {
		scheduleCaptureFn, scheduleBackupFn = origCapture, origBackup
	})
	scheduleCaptureFn = func(flags CaptureFlags) (interface{}, *envelope.Error) {
		if err := os.WriteFile(flags.Out, []byte("fresh capture"), 0o600); err != nil {
			t.Fatalf("write capture: %v", err)
		}
		return &CaptureResult{OutputPath: flags.Out}, nil
	}
	scheduleBackupFn = func(BackupFlags) (interface{}, *envelope.Error) {
		return nil, envelope.NewError(envelope.ErrAuthRequired, "sign in")
	}
	cfg := &schedule.Config{SchemaVersion: "1.0", AutoPush: true, Manifest: "prior.endstate"}
	got := runScheduleAutoPush(stateDir, cfg, true)
	if got.Outcome != "auth_required" {
		t.Fatalf("outcome = %q, want auth_required", got.Outcome)
	}
	if !scheduledArtifactMatches(cfg.PendingArtifact, cfg.PendingSHA256) {
		t.Fatal("fresh pending artifact was not persisted with its exact hash")
	}
	if cfg.Manifest != cfg.PendingArtifact {
		t.Fatalf("manifest baseline = %q, want fresh pending artifact %q", cfg.Manifest, cfg.PendingArtifact)
	}
	baseline, baselineHash := cfg.Manifest, cfg.PendingSHA256

	scheduleBackupFn = func(flags BackupFlags) (interface{}, *envelope.Error) {
		if flags.Profile != cfg.PendingArtifact {
			t.Fatalf("retry profile = %q, want persisted artifact %q", flags.Profile, cfg.PendingArtifact)
		}
		return nil, nil
	}
	got = runScheduleAutoPush(stateDir, cfg, false)
	if got.Outcome != "pushed" {
		t.Fatalf("outcome = %q, want pushed", got.Outcome)
	}
	if cfg.PendingArtifact != "" || cfg.PendingSHA256 != "" {
		t.Fatal("pending state remained after successful cloud push")
	}
	if cfg.Manifest != baseline || !scheduledArtifactMatches(cfg.Manifest, baselineHash) {
		t.Fatal("successful upload did not retain the fresh capture as the local baseline")
	}
	persisted, err := schedule.ReadConfig(schedule.ConfigPath(stateDir))
	if err != nil {
		t.Fatalf("ReadConfig: %v", err)
	}
	if persisted.Manifest != cfg.Manifest || persisted.PendingArtifact != "" || persisted.PendingSHA256 != "" {
		t.Fatalf("restart config = %+v, want retained baseline and no pending state", persisted)
	}
	if got := scheduleManifestForRun(ScheduleFlags{}, persisted); got != baseline {
		t.Fatalf("next scheduled verify manifest = %q, want new baseline %q", got, baseline)
	}
}

func TestScheduleAutoPush_DrainClearsCompatibilityFieldsAfterReload(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "state")
	artifact := filepath.Join(stateDir, "schedule", "pending", "capture.endstate")
	if err := os.MkdirAll(filepath.Dir(artifact), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(artifact, []byte("capture"), 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte("capture"))
	cfg := &schedule.Config{
		SchemaVersion:   "1.0",
		Manifest:        artifact,
		BackupID:        "backup-1",
		PendingArtifact: artifact,
		PendingSHA256:   fmt.Sprintf("%x", sum),
		PendingUploads:  []schedule.PendingUpload{{Artifact: artifact, SHA256: fmt.Sprintf("%x", sum)}},
	}
	backupCalls := 0
	origBackup := scheduleBackupFn
	t.Cleanup(func() { scheduleBackupFn = origBackup })
	scheduleBackupFn = func(BackupFlags) (interface{}, *envelope.Error) {
		backupCalls++
		if backupCalls > 1 {
			t.Fatal("successful queue drain retried the same artifact")
		}
		return &PushResult{VersionID: "v-1"}, nil
	}

	if got := runScheduleAutoPush(stateDir, cfg, false); got.Outcome != "pushed" {
		t.Fatalf("outcome = %q, want pushed", got.Outcome)
	}
	if backupCalls != 1 {
		t.Fatalf("backup calls = %d, want 1", backupCalls)
	}
	persisted, err := schedule.ReadConfig(schedule.ConfigPath(stateDir))
	if err != nil {
		t.Fatalf("ReadConfig: %v", err)
	}
	if len(persisted.PendingUploads) != 0 || persisted.PendingArtifact != "" || persisted.PendingSHA256 != "" || persisted.BackupID != "backup-1" {
		t.Fatalf("persisted pending state = %+v, want empty queue and compatibility fields", persisted)
	}
}

func TestScheduleRootCanonicalizesRelativeExplicitRoot(t *testing.T) {
	got, err := canonicalSchedulePath(filepath.Join(".", "test-root", "..", "state-root"))
	want, err := filepath.Abs(filepath.Join("state-root"))
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("scheduleRoot = %q, want %q", got, want)
	}
}

func TestRunScheduleEnable_InvalidRootAbortsBeforeRegistration(t *testing.T) {
	dir := t.TempDir()
	withStateRoot(t, dir)
	manifest := filepath.Join(dir, "manifest.jsonc")
	if err := os.WriteFile(manifest, []byte(`{"version":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	reg := &fakeScheduleRegistrar{}
	withFakeRegistrar(t, reg)
	origSupported := scheduleRunSupportedFn
	t.Cleanup(func() { scheduleRunSupportedFn = origSupported })
	scheduleRunSupportedFn = func() bool { return true }
	if _, envErr := runScheduleEnable(ScheduleFlags{Manifest: manifest, Root: "\x00"}); envErr == nil {
		t.Fatal("schedule enable accepted an invalid root")
	}
	if reg.registerCalls != 0 {
		t.Fatalf("register calls = %d, want 0", reg.registerCalls)
	}
	if _, err := canonicalSchedulePath("\x00"); err == nil {
		t.Fatal("canonicalSchedulePath returned no error for invalid path")
	}
}

func TestPublishScheduledCapture_RemovesStageAfterCaptureFailure(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "state")
	origCapture := scheduleCaptureFn
	t.Cleanup(func() { scheduleCaptureFn = origCapture })
	scheduleCaptureFn = func(flags CaptureFlags) (interface{}, *envelope.Error) {
		if err := os.WriteFile(flags.Out, []byte("partial"), 0o600); err != nil {
			t.Fatal(err)
		}
		return nil, envelope.NewError(envelope.ErrInternalError, "capture failed")
	}

	if _, err := publishScheduledCapture(stateDir); err == nil {
		t.Fatal("publishScheduledCapture returned nil after capture failure")
	}
	stages, err := filepath.Glob(filepath.Join(stateDir, "schedule", "pending", ".capture-*.endstate"))
	if err != nil {
		t.Fatal(err)
	}
	if len(stages) != 0 {
		t.Fatalf("partial capture stages = %v, want none", stages)
	}
}

func TestAppendScheduledPending_DeduplicatesIdenticalCapture(t *testing.T) {
	captured := scheduledCapture{path: "capture.endstate", sha256: "same"}
	cfg := &schedule.Config{PendingUploads: []schedule.PendingUpload{{Artifact: captured.path, SHA256: captured.sha256}}}
	appendScheduledPending(cfg, captured)
	if len(cfg.PendingUploads) != 1 {
		t.Fatalf("pending queue = %#v, want existing single capture", cfg.PendingUploads)
	}
}

func TestScheduleDiscardUpload_RemovesOnlyUncertainLegacyQueueItem(t *testing.T) {
	dir := t.TempDir()
	withStateRoot(t, dir)
	stateDir := filepath.Join(dir, "state")
	artifact := filepath.Join(stateDir, "schedule", "pending", "capture.endstate")
	if err := os.MkdirAll(filepath.Dir(artifact), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(artifact, []byte("capture"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := &schedule.Config{Manifest: artifact, PendingUploads: []schedule.PendingUpload{{Artifact: artifact, SHA256: "uncertain", LegacyCreateStarted: true}}}
	if err := schedule.WriteConfig(schedule.ConfigPath(stateDir), cfg); err != nil {
		t.Fatal(err)
	}
	origSupported := scheduleRunSupportedFn
	t.Cleanup(func() { scheduleRunSupportedFn = origSupported })
	scheduleRunSupportedFn = func() bool { return true }
	data, envErr := runScheduleDiscardUpload(ScheduleFlags{ArtifactSHA256: "uncertain", Confirm: true})
	if envErr != nil {
		t.Fatalf("discard upload: %v", envErr)
	}
	if !data.(*ScheduleDiscardUploadData).Discarded {
		t.Fatal("discarded = false")
	}
	persisted, err := schedule.ReadConfig(schedule.ConfigPath(stateDir))
	if err != nil {
		t.Fatal(err)
	}
	if len(persisted.PendingUploads) != 0 || persisted.PendingArtifact != "" || persisted.PendingSHA256 != "" {
		t.Fatalf("persisted pending state = %+v", persisted)
	}
	if _, err := os.Stat(artifact); err != nil {
		t.Fatalf("discard removed local capture: %v", err)
	}
}

func TestPrepareScheduledPending_FilteringToEmptyClearsCompatibilityFields(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "state")
	artifact := filepath.Join(t.TempDir(), "outside-pending.endstate")
	cfg := &schedule.Config{
		SchemaVersion:   "1.0",
		PendingArtifact: artifact,
		PendingSHA256:   "sha",
		PendingUploads:  []schedule.PendingUpload{{Artifact: artifact, SHA256: "sha"}},
	}

	if err := prepareScheduledPending(stateDir, cfg); err != nil {
		t.Fatal(err)
	}
	if len(cfg.PendingUploads) != 0 || cfg.PendingArtifact != "" || cfg.PendingSHA256 != "" {
		t.Fatalf("pending state = %+v, want empty queue and compatibility fields", cfg)
	}
	persisted, err := schedule.ReadConfig(schedule.ConfigPath(stateDir))
	if err != nil {
		t.Fatalf("ReadConfig: %v", err)
	}
	if len(persisted.PendingUploads) != 0 || persisted.PendingArtifact != "" || persisted.PendingSHA256 != "" {
		t.Fatalf("persisted pending state = %+v, want empty queue and compatibility fields", persisted)
	}
}

func TestScheduleAutoPush_StalePendingCaptureDoesNotBlockFreshDrift(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "state")
	pendingDir := filepath.Join(stateDir, "schedule", "pending")
	if err := os.MkdirAll(pendingDir, 0o755); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(pendingDir, "stale.endstate")
	if err := os.WriteFile(stale, []byte("corrupt"), 0o600); err != nil {
		t.Fatal(err)
	}
	origCapture, origBackup := scheduleCaptureFn, scheduleBackupFn
	t.Cleanup(func() { scheduleCaptureFn, scheduleBackupFn = origCapture, origBackup })
	scheduleCaptureFn = func(flags CaptureFlags) (interface{}, *envelope.Error) {
		if err := os.WriteFile(flags.Out, []byte("fresh replacement"), 0o600); err != nil {
			t.Fatal(err)
		}
		return &CaptureResult{OutputPath: flags.Out}, nil
	}
	scheduleBackupFn = func(BackupFlags) (interface{}, *envelope.Error) {
		return nil, envelope.NewError(envelope.ErrAuthRequired, "sign in")
	}
	cfg := &schedule.Config{SchemaVersion: "1.0", Manifest: "old.endstate", PendingArtifact: stale, PendingSHA256: "not-the-file-hash"}
	if err := prepareScheduledPending(stateDir, cfg); err != nil {
		t.Fatal(err)
	}
	got := runScheduleAutoPush(stateDir, cfg, true)
	if got.Outcome != "auth_required" {
		t.Fatalf("outcome = %q, want auth_required", got.Outcome)
	}
	if cfg.PendingArtifact == stale || cfg.Manifest == "old.endstate" || !scheduledArtifactMatches(cfg.PendingArtifact, cfg.PendingSHA256) {
		t.Fatalf("stale pending state did not advance to a fresh captured baseline: %+v", cfg)
	}
	quarantined, err := filepath.Glob(filepath.Join(stateDir, "schedule", "quarantine", filepath.Base(stale)+".corrupt-*"))
	if err != nil || len(quarantined) != 1 {
		t.Fatalf("quarantined stale artifacts = %v, %v; want one", quarantined, err)
	}
}

func TestRunScheduleRun_QuarantinesStalePendingBeforeVerification(t *testing.T) {
	dir := t.TempDir()
	withStateRoot(t, dir)
	stateDir := filepath.Join(dir, "state")
	baseline := filepath.Join(dir, "baseline.endstate")
	stale := filepath.Join(dir, "stale.endstate")
	if err := os.WriteFile(baseline, []byte("baseline"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stale, []byte("corrupt"), 0o600); err != nil {
		t.Fatal(err)
	}
	// This is the production state written by the prior singular-pending
	// implementation: the queued capture became the manifest baseline, with no
	// earlier fallback recorded. A restart must republish a local baseline rather
	// than verify forever against the corrupt path.
	cfg := &schedule.Config{SchemaVersion: "1.0", Enabled: true, AutoPush: true, Manifest: stale, PendingArtifact: stale, PendingSHA256: "wrong"}
	if err := schedule.WriteConfig(schedule.ConfigPath(stateDir), cfg); err != nil {
		t.Fatal(err)
	}
	origSupported, origVerify, origCapture, origBackup := scheduleRunSupportedFn, scheduleVerifyFn, scheduleCaptureFn, scheduleBackupFn
	t.Cleanup(func() {
		scheduleRunSupportedFn, scheduleVerifyFn, scheduleCaptureFn, scheduleBackupFn = origSupported, origVerify, origCapture, origBackup
	})
	scheduleRunSupportedFn = func() bool { return true }
	scheduleVerifyFn = func(flags VerifyFlags) (interface{}, *envelope.Error) {
		if flags.Manifest == stale || flags.Manifest == "" {
			t.Fatalf("verify manifest = %q, want a recovered local baseline", flags.Manifest)
		}
		return &VerifyResult{Summary: VerifySummary{Total: 1, Fail: 1}}, nil
	}
	scheduleCaptureFn = func(flags CaptureFlags) (interface{}, *envelope.Error) {
		if err := os.WriteFile(flags.Out, []byte("fresh"), 0o600); err != nil {
			t.Fatal(err)
		}
		return &CaptureResult{OutputPath: flags.Out}, nil
	}
	scheduleBackupFn = func(BackupFlags) (interface{}, *envelope.Error) {
		return nil, envelope.NewError(envelope.ErrAuthRequired, "sign in")
	}
	if _, envErr := runScheduleRun(ScheduleFlags{}); envErr != nil {
		t.Fatalf("runScheduleRun: %v", envErr)
	}
	persisted, err := schedule.ReadConfig(schedule.ConfigPath(stateDir))
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Manifest == baseline || persisted.PendingArtifact == "" || !scheduledArtifactMatches(persisted.PendingArtifact, persisted.PendingSHA256) {
		t.Fatalf("stale queue did not advance through real orchestration: %+v", persisted)
	}
}

func TestScheduleAutoPush_RecordsOfflinePendingOutcome(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "state")
	artifact := filepath.Join(stateDir, "schedule", "pending", "capture.endstate")
	if err := os.MkdirAll(filepath.Dir(artifact), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(artifact, []byte("pending"), 0o600); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(artifact)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	origBackup := scheduleBackupFn
	t.Cleanup(func() { scheduleBackupFn = origBackup })
	scheduleBackupFn = func(BackupFlags) (interface{}, *envelope.Error) {
		return nil, envelope.NewError(envelope.ErrBackendUnreachable, "offline")
	}
	cfg := &schedule.Config{SchemaVersion: "1.0", PendingArtifact: artifact, PendingSHA256: hex.EncodeToString(sum[:])}
	if got := runScheduleAutoPush(stateDir, cfg, false); got.Outcome != "offline" {
		t.Fatalf("outcome = %q, want offline", got.Outcome)
	}
}

func TestScheduleAutoPush_RecordsSubscriptionRequiredPendingOutcome(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "state")
	artifact := filepath.Join(stateDir, "schedule", "pending", "capture.endstate")
	if err := os.MkdirAll(filepath.Dir(artifact), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(artifact, []byte("pending"), 0o600); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(artifact)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	origBackup := scheduleBackupFn
	t.Cleanup(func() { scheduleBackupFn = origBackup })
	scheduleBackupFn = func(BackupFlags) (interface{}, *envelope.Error) {
		return nil, envelope.NewError(envelope.ErrSubscriptionRequired, "subscription inactive")
	}
	cfg := &schedule.Config{SchemaVersion: "1.0", PendingArtifact: artifact, PendingSHA256: hex.EncodeToString(sum[:])}
	if got := runScheduleAutoPush(stateDir, cfg, false); got.Outcome != "subscription_required" {
		t.Fatalf("outcome = %q, want subscription_required", got.Outcome)
	}
}

func TestPruneScheduledArtifacts_BoundsFallbacksAndReportsRemovalFailure(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "state")
	pendingDir := filepath.Join(stateDir, "schedule", "pending")
	if err := os.MkdirAll(pendingDir, 0o755); err != nil {
		t.Fatal(err)
	}
	paths := []string{filepath.Join(pendingDir, "old.endstate"), filepath.Join(pendingDir, "fallback.endstate"), filepath.Join(pendingDir, "current.endstate"), filepath.Join(pendingDir, "queued.endstate")}
	for _, path := range paths {
		if err := os.WriteFile(path, []byte(path), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cfg := &schedule.Config{Manifest: paths[2], PendingUploads: []schedule.PendingUpload{{Artifact: paths[3]}}, FallbackManifests: []string{paths[0], paths[1]}}
	if err := pruneScheduledArtifacts(stateDir, cfg); err != nil {
		t.Fatal(err)
	}
	if len(cfg.FallbackManifests) != 1 || cfg.FallbackManifests[0] != paths[1] {
		t.Fatalf("fallbacks = %v", cfg.FallbackManifests)
	}
	if _, err := os.Stat(paths[0]); !os.IsNotExist(err) {
		t.Fatalf("old fallback remains: %v", err)
	}
	orig := scheduleRemoveArtifactFn
	t.Cleanup(func() { scheduleRemoveArtifactFn = orig })
	scheduleRemoveArtifactFn = func(string) error { return errors.New("acl") }
	if err := os.WriteFile(paths[0], []byte("again"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := pruneScheduledArtifacts(stateDir, cfg); err == nil {
		t.Fatal("expected removal error")
	}
}

func TestQuarantineScheduledArtifact_BoundsEvidenceOutsidePending(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "state")
	pendingDir := filepath.Join(stateDir, "schedule", "pending")
	if err := os.MkdirAll(pendingDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 6; i++ {
		path := filepath.Join(pendingDir, fmt.Sprintf("bad-%d.endstate", i))
		if err := os.WriteFile(path, []byte("bad"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := quarantineScheduledArtifact(path); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := os.ReadDir(filepath.Join(stateDir, "schedule", "quarantine"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 5 {
		t.Fatalf("quarantine entries = %d, want 5", len(entries))
	}
	if entries, err := os.ReadDir(pendingDir); err != nil || len(entries) != 0 {
		t.Fatalf("pending evidence = %v, %v", entries, err)
	}
}

func TestPrepareScheduledPending_RejectsExternalAndTraversalArtifactsWithoutTouchingThem(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "state")
	external := filepath.Join(t.TempDir(), "external.endstate")
	if err := os.WriteFile(external, []byte("external"), 0o600); err != nil {
		t.Fatal(err)
	}
	traversal := filepath.Join(stateDir, "schedule", "pending", "..", "external.endstate")
	cfg := &schedule.Config{PendingUploads: []schedule.PendingUpload{{Artifact: external, SHA256: "x"}, {Artifact: traversal, SHA256: "x"}}}
	orig := scheduleWriteConfigFn
	t.Cleanup(func() { scheduleWriteConfigFn = orig })
	scheduleWriteConfigFn = func(string, *schedule.Config) error { return nil }
	if err := prepareScheduledPending(stateDir, cfg); err != nil {
		t.Fatal(err)
	}
	if len(cfg.PendingUploads) != 0 {
		t.Fatalf("pending = %#v", cfg.PendingUploads)
	}
	got, err := os.ReadFile(external)
	if err != nil || string(got) != "external" {
		t.Fatalf("external file changed: %q %v", got, err)
	}
}

func TestScheduleAutoPush_RetriesQueueOldestFirstThenAppendsFreshDrift(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "state")
	oldest := filepath.Join(stateDir, "schedule", "pending", "oldest.endstate")
	newer := filepath.Join(stateDir, "schedule", "pending", "newer.endstate")
	for _, artifact := range []string{oldest, newer} {
		if err := os.MkdirAll(filepath.Dir(artifact), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(artifact, []byte(filepath.Base(artifact)), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	hash := func(path string) string {
		b, _ := os.ReadFile(path)
		s := sha256.Sum256(b)
		return hex.EncodeToString(s[:])
	}
	cfg := &schedule.Config{SchemaVersion: "1.0", Manifest: newer, PendingUploads: []schedule.PendingUpload{
		{Artifact: oldest, SHA256: hash(oldest)}, {Artifact: newer, SHA256: hash(newer)},
	}}
	var pushed []string
	origCapture, origBackup, origWrite := scheduleCaptureFn, scheduleBackupFn, scheduleWriteConfigFn
	t.Cleanup(func() {
		scheduleCaptureFn, scheduleBackupFn, scheduleWriteConfigFn = origCapture, origBackup, origWrite
	})
	scheduleWriteConfigFn = func(string, *schedule.Config) error { return nil }
	scheduleCaptureFn = func(flags CaptureFlags) (interface{}, *envelope.Error) {
		if err := os.WriteFile(flags.Out, []byte("fresh"), 0o600); err != nil {
			t.Fatal(err)
		}
		return &CaptureResult{OutputPath: flags.Out}, nil
	}
	scheduleBackupFn = func(flags BackupFlags) (interface{}, *envelope.Error) {
		pushed = append(pushed, flags.Profile)
		return &PushResult{}, nil
	}
	if got := runScheduleAutoPush(stateDir, cfg, true); got.Outcome != "pushed" {
		t.Fatalf("outcome = %q", got.Outcome)
	}
	if len(pushed) != 3 || pushed[0] != oldest || pushed[1] != newer || pushed[2] == oldest || pushed[2] == newer {
		t.Fatalf("push order = %v, want oldest, newer, fresh", pushed)
	}
	if len(cfg.PendingUploads) != 0 {
		t.Fatalf("queue left after successful pushes: %#v", cfg.PendingUploads)
	}
}

func TestFinalizeScheduleRun_ReturnsLastRunWriteFailure(t *testing.T) {
	orig := scheduleWriteLastRunFn
	t.Cleanup(func() { scheduleWriteLastRunFn = orig })
	scheduleWriteLastRunFn = func(string, *schedule.LastRun) error { return errors.New("disk full") }
	if err := finalizeScheduleRun("ignored", &schedule.LastRun{}); err == nil {
		t.Fatal("finalizeScheduleRun returned nil when last-run write failed")
	}
}

func TestScheduleRunFailure_RemovesPriorHealthyLastRunWhenFailureCannotPersist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "last-run.json")
	if err := os.WriteFile(path, []byte(`{"verify":{"summary":{"fail":0}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	originalWrite, originalRemove := scheduleWriteLastRunFn, scheduleRemoveLastRunFn
	t.Cleanup(func() {
		scheduleWriteLastRunFn, scheduleRemoveLastRunFn = originalWrite, originalRemove
	})
	scheduleWriteLastRunFn = func(string, *schedule.LastRun) error { return errors.New("disk full") }
	scheduleRemoveLastRunFn = os.Remove

	err := scheduleRunFailure(path, &schedule.LastRun{}, envelope.NewError(envelope.ErrInternalError, "verify failed"))
	if err == nil || err.Code != envelope.ErrInternalError {
		t.Fatalf("scheduleRunFailure = %#v, want last-run persistence error", err)
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Fatalf("prior healthy last-run remains visible: %v", statErr)
	}
}

// TestCapabilities_ScheduleFeature verifies the capabilities payload advertises
// features.schedule with the correct shape.
func TestCapabilities_ScheduleFeature(t *testing.T) {
	data, envErr := RunCapabilities()
	if envErr != nil {
		t.Fatalf("capabilities error: %v", envErr)
	}
	caps, ok := data.(CapabilitiesData)
	if !ok {
		t.Fatalf("data type = %T, want CapabilitiesData", data)
	}

	// features.schedule must be present.
	sched := caps.Features.Schedule
	if !sched.BundleManifestSupported {
		t.Error("features.schedule.bundleManifestSupported = false, want true")
	}
	if runtime.GOOS == "windows" {
		if !sched.Supported {
			t.Error("features.schedule.supported = false on Windows, want true")
		}
	} else {
		if sched.Supported {
			t.Error("features.schedule.supported = true on non-Windows, want false")
		}
	}

	// commands.schedule must be present with required flags.
	cmd, ok := caps.Commands["schedule"]
	if !ok {
		t.Fatal("commands.schedule missing from capabilities")
	}
	if !cmd.Supported {
		t.Error("commands.schedule.supported = false, want true")
	}
	requiredFlags := []string{"--manifest", "--interval", "--time", "--auto-push", "--root", "--json"}
	flagSet := make(map[string]bool, len(cmd.Flags))
	for _, f := range cmd.Flags {
		flagSet[f] = true
	}
	for _, rf := range requiredFlags {
		if !flagSet[rf] {
			t.Errorf("commands.schedule.flags missing %q", rf)
		}
	}
}

func TestRunScheduleAutoPush_FirstBackupNeedsManualSetup(t *testing.T) {
	dir := t.TempDir()
	artifact := filepath.Join(dir, "schedule", "pending", "capture.json")
	if err := os.MkdirAll(filepath.Dir(artifact), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(artifact, []byte("capture"), 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte("capture"))
	cfg := &schedule.Config{PendingUploads: []schedule.PendingUpload{{Artifact: artifact, SHA256: fmt.Sprintf("%x", sum)}}}
	orig := scheduleBackupFn
	t.Cleanup(func() { scheduleBackupFn = orig })
	scheduleBackupFn = func(BackupFlags) (interface{}, *envelope.Error) {
		return nil, envelope.NewError(envelope.ErrBackupSetupRequired, "manual setup")
	}
	if got := runScheduleAutoPush(dir, cfg, false); got.Outcome != "setup_required" {
		t.Fatalf("outcome = %q, want setup_required", got.Outcome)
	}
	if len(cfg.PendingUploads) != 1 {
		t.Fatal("setup-required queue item was discarded")
	}
}

func TestRunScheduleAutoPush_PersistsDrainBeforeSpoolCleanup(t *testing.T) {
	dir := t.TempDir()
	artifact := filepath.Join(dir, "schedule", "pending", "capture.json")
	spool := filepath.Join(dir, "schedule", "create-spool", "operation.json")
	if err := os.MkdirAll(filepath.Dir(artifact), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(spool), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(artifact, []byte("capture"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(spool, []byte("ciphertext"), 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte("capture"))
	cfg := &schedule.Config{PendingUploads: []schedule.PendingUpload{{Artifact: artifact, SHA256: fmt.Sprintf("%x", sum), CreateSpool: spool}}}
	origBackup, origWrite, origRemove := scheduleBackupFn, scheduleWriteConfigFn, scheduleRemoveArtifactFn
	t.Cleanup(func() {
		scheduleBackupFn, scheduleWriteConfigFn, scheduleRemoveArtifactFn = origBackup, origWrite, origRemove
	})
	scheduleBackupFn = func(BackupFlags) (interface{}, *envelope.Error) { return &PushResult{VersionID: "v"}, nil }
	persisted := false
	scheduleWriteConfigFn = func(string, *schedule.Config) error { persisted = len(cfg.PendingUploads) == 0; return nil }
	scheduleRemoveArtifactFn = func(path string) error {
		if path == spool && !persisted {
			t.Fatal("spool deleted before queue drain persisted")
		}
		return os.Remove(path)
	}
	if got := runScheduleAutoPush(dir, cfg, false); got.Outcome != "pushed" {
		t.Fatalf("outcome = %q", got.Outcome)
	}
}

// ---------------------------------------------------------------------------
// Helper
// ---------------------------------------------------------------------------

func containsSubstr(s, sub string) bool {
	if len(sub) == 0 {
		return true
	}
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
