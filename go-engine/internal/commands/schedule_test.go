// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/schedule"
)

// ---------------------------------------------------------------------------
// Fake registrar for command-handler tests
// ---------------------------------------------------------------------------

type fakeScheduleRegistrar struct {
	registerErr   error
	unregisterErr error
	registerCalls int
	lastArgs      string
}

func (f *fakeScheduleRegistrar) Register(taskName, exePath, args, interval, timeOfDay string) error {
	f.registerCalls++
	f.lastArgs = args
	return f.registerErr
}

func (f *fakeScheduleRegistrar) Unregister(taskName string) error {
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
// disabled schedule returns a stable error and records it in last-run.json.
func TestRunScheduleRun_DisabledReturnsError(t *testing.T) {
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
	if envErr.Code != envelope.ErrNotSupported {
		t.Errorf("error code = %q, want NOT_SUPPORTED", envErr.Code)
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
