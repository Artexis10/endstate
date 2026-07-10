// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package schedule

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// ---------------------------------------------------------------------------
// Fake Registrar (test seam)
// ---------------------------------------------------------------------------

// fakeRegistrar records Register/Unregister calls and can be scripted to fail.
type fakeRegistrar struct {
	registerErr   error
	unregisterErr error

	registerCalls   int
	unregisterCalls int

	lastTaskName  string
	lastExePath   string
	lastArgs      string
	lastInterval  string
	lastTimeOfDay string
}

func (f *fakeRegistrar) Register(taskName, exePath, args, interval, timeOfDay string) error {
	f.registerCalls++
	f.lastTaskName = taskName
	f.lastExePath = exePath
	f.lastArgs = args
	f.lastInterval = interval
	f.lastTimeOfDay = timeOfDay
	return f.registerErr
}

func (f *fakeRegistrar) Unregister(taskName string) error {
	f.unregisterCalls++
	f.lastTaskName = taskName
	return f.unregisterErr
}

// ---------------------------------------------------------------------------
// Enable / disable helpers (mirror what command handlers do)
// ---------------------------------------------------------------------------

// enableSchedule simulates the schedule enable logic against a test stateDir
// and fake registrar. It is the seam-level analogue of RunScheduleEnable.
func enableSchedule(t *testing.T, stateDir, manifestPath, interval, timeOfDay string, autoPush bool, reg Registrar) error {
	t.Helper()

	exePath := "/fake/endstate"
	root := stateDir

	args := "schedule run --root \"" + root + "\" --json"
	if err := reg.Register(TaskName, exePath, args, interval, timeOfDay); err != nil {
		return err
	}

	cfg := &Config{
		SchemaVersion: "1.0",
		Enabled:       true,
		Manifest:      manifestPath,
		Interval:      interval,
		Time:          timeOfDay,
		AutoPush:      autoPush,
		TaskName:      TaskName,
		Root:          root,
		RegisteredAt:  NowUTC(),
	}
	return WriteConfig(ConfigPath(stateDir), cfg)
}

// disableSchedule simulates schedule disable against a test stateDir.
func disableSchedule(t *testing.T, stateDir string, reg Registrar) error {
	t.Helper()

	if err := reg.Unregister(TaskName); err != nil {
		return err
	}

	cfgPath := ConfigPath(stateDir)
	cfg, err := ReadConfig(cfgPath)
	if err != nil {
		return err
	}
	cfg.Enabled = false
	return WriteConfig(cfgPath, cfg)
}

// ---------------------------------------------------------------------------
// 1.3 Tests: seam-level invariants
// ---------------------------------------------------------------------------

// TestEnable_IdempotentRegister verifies that calling enable twice results in
// exactly two Register calls (schtasks /F handles idempotency at the OS level;
// the engine always re-asserts), and that config.json reflects enabled:true.
func TestEnable_IdempotentRegister(t *testing.T) {
	stateDir := t.TempDir()
	reg := &fakeRegistrar{}

	manifestPath := filepath.Join(stateDir, "manifest.jsonc")
	_ = os.WriteFile(manifestPath, []byte(`{}`), 0644)

	// First enable.
	if err := enableSchedule(t, stateDir, manifestPath, "daily", "09:00", false, reg); err != nil {
		t.Fatalf("first enable: %v", err)
	}
	// Second enable (idempotent re-assert).
	if err := enableSchedule(t, stateDir, manifestPath, "daily", "09:00", false, reg); err != nil {
		t.Fatalf("second enable: %v", err)
	}

	if reg.registerCalls != 2 {
		t.Errorf("registerCalls = %d, want 2", reg.registerCalls)
	}

	cfg, err := ReadConfig(ConfigPath(stateDir))
	if err != nil {
		t.Fatalf("ReadConfig: %v", err)
	}
	if !cfg.Enabled {
		t.Error("config.Enabled = false after enable, want true")
	}
	if cfg.Manifest != manifestPath {
		t.Errorf("config.Manifest = %q, want %q", cfg.Manifest, manifestPath)
	}
	if cfg.TaskName != TaskName {
		t.Errorf("config.TaskName = %q, want %q", cfg.TaskName, TaskName)
	}
}

// TestDisable_KeepsConfig verifies that after disable the task is unregistered
// but config.json remains with enabled:false (not deleted).
func TestDisable_KeepsConfig(t *testing.T) {
	stateDir := t.TempDir()
	reg := &fakeRegistrar{}

	manifestPath := filepath.Join(stateDir, "manifest.jsonc")
	_ = os.WriteFile(manifestPath, []byte(`{}`), 0644)

	if err := enableSchedule(t, stateDir, manifestPath, "daily", "09:00", false, reg); err != nil {
		t.Fatalf("enable: %v", err)
	}
	if err := disableSchedule(t, stateDir, reg); err != nil {
		t.Fatalf("disable: %v", err)
	}

	if reg.unregisterCalls != 1 {
		t.Errorf("unregisterCalls = %d, want 1", reg.unregisterCalls)
	}

	cfgPath := ConfigPath(stateDir)
	if _, err := os.Stat(cfgPath); os.IsNotExist(err) {
		t.Fatal("config.json was deleted after disable, want retained")
	}

	cfg, err := ReadConfig(cfgPath)
	if err != nil {
		t.Fatalf("ReadConfig: %v", err)
	}
	if cfg.Enabled {
		t.Error("config.Enabled = true after disable, want false")
	}
	// Manifest path should still be present.
	if cfg.Manifest != manifestPath {
		t.Errorf("config.Manifest = %q after disable, want %q", cfg.Manifest, manifestPath)
	}
}

// TestEnable_RegistrationFailureDoesNotHalfEnable verifies that when Register
// returns an error, the config is not written (so we never persist enabled:true
// for a task that was never registered).
func TestEnable_RegistrationFailureDoesNotHalfEnable(t *testing.T) {
	stateDir := t.TempDir()
	reg := &fakeRegistrar{
		registerErr: errors.New("simulated schtasks failure"),
	}

	manifestPath := filepath.Join(stateDir, "manifest.jsonc")
	_ = os.WriteFile(manifestPath, []byte(`{}`), 0644)

	err := enableSchedule(t, stateDir, manifestPath, "daily", "09:00", false, reg)
	if err == nil {
		t.Fatal("expected error from enableSchedule with failing registrar, got nil")
	}

	// Config must NOT be written, or if it exists it must not be enabled:true.
	cfgPath := ConfigPath(stateDir)
	if _, statErr := os.Stat(cfgPath); statErr == nil {
		cfg, readErr := ReadConfig(cfgPath)
		if readErr != nil {
			t.Fatalf("ReadConfig: %v", readErr)
		}
		if cfg.Enabled {
			t.Error("config.Enabled = true after registration failure, want false")
		}
	}
	// (File absent is the expected case; both paths are valid as long as Enabled != true.)
}

// TestBakedRootInRegisteredArgs verifies that the registered task command line
// contains --root with the stateDir root value, so scheduled and interactive
// runs share one state directory.
func TestBakedRootInRegisteredArgs(t *testing.T) {
	stateDir := t.TempDir()
	reg := &fakeRegistrar{}

	manifestPath := filepath.Join(stateDir, "manifest.jsonc")
	_ = os.WriteFile(manifestPath, []byte(`{}`), 0644)

	if err := enableSchedule(t, stateDir, manifestPath, "daily", "09:00", false, reg); err != nil {
		t.Fatalf("enable: %v", err)
	}

	// The args registered must contain --root and the stateDir root.
	if !containsAny(reg.lastArgs, "--root") {
		t.Errorf("registered args %q do not contain --root", reg.lastArgs)
	}
	if !containsAny(reg.lastArgs, stateDir) {
		t.Errorf("registered args %q do not contain stateDir %q", reg.lastArgs, stateDir)
	}

	// Config must also record the root.
	cfg, err := ReadConfig(ConfigPath(stateDir))
	if err != nil {
		t.Fatalf("ReadConfig: %v", err)
	}
	if cfg.Root != stateDir {
		t.Errorf("config.Root = %q, want %q", cfg.Root, stateDir)
	}
}

// ---------------------------------------------------------------------------
// State file read/write tests
// ---------------------------------------------------------------------------

func TestWriteReadConfig_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := ConfigPath(dir)

	want := &Config{
		SchemaVersion: "1.0",
		Enabled:       true,
		Manifest:      "/some/manifest.jsonc",
		Interval:      "daily",
		Time:          "08:30",
		AutoPush:      true,
		TaskName:      TaskName,
		Root:          dir,
	}
	if err := WriteConfig(path, want); err != nil {
		t.Fatalf("WriteConfig: %v", err)
	}

	got, err := ReadConfig(path)
	if err != nil {
		t.Fatalf("ReadConfig: %v", err)
	}
	if got.Enabled != want.Enabled {
		t.Errorf("Enabled = %v, want %v", got.Enabled, want.Enabled)
	}
	if got.Manifest != want.Manifest {
		t.Errorf("Manifest = %q, want %q", got.Manifest, want.Manifest)
	}
	if got.AutoPush != want.AutoPush {
		t.Errorf("AutoPush = %v, want %v", got.AutoPush, want.AutoPush)
	}
}

func TestReadConfig_MissingFile_ReturnsDefault(t *testing.T) {
	dir := t.TempDir()
	cfg, err := ReadConfig(ConfigPath(dir))
	if err != nil {
		t.Fatalf("ReadConfig on missing file: %v", err)
	}
	if cfg == nil {
		t.Fatal("ReadConfig returned nil, want default")
	}
	if cfg.Enabled {
		t.Error("default config.Enabled = true, want false")
	}
	if cfg.SchemaVersion != "1.0" {
		t.Errorf("default SchemaVersion = %q, want 1.0", cfg.SchemaVersion)
	}
}

func TestWriteReadLastRun_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := LastRunPath(dir)

	want := &LastRun{
		SchemaVersion: "1.0",
		RunID:         "schedule-20260710-090000",
		TimestampUTC:  "2026-07-10T09:00:00Z",
		Verify: &LastRunVerify{
			Summary: LastRunVerifySummary{Total: 5, Pass: 3, Fail: 2},
			Drifted: []LastRunDriftItem{
				{ID: "vscode", Name: "Visual Studio Code", Status: "fail", Reason: "missing"},
				{ID: "git", Name: "Git", Status: "fail", Reason: "version_drift"},
			},
		},
	}
	if err := WriteLastRun(path, want); err != nil {
		t.Fatalf("WriteLastRun: %v", err)
	}

	got, err := ReadLastRun(path)
	if err != nil {
		t.Fatalf("ReadLastRun: %v", err)
	}
	if got == nil {
		t.Fatal("ReadLastRun returned nil")
	}
	if got.RunID != want.RunID {
		t.Errorf("RunID = %q, want %q", got.RunID, want.RunID)
	}
	if got.Verify.Summary.Fail != 2 {
		t.Errorf("Verify.Summary.Fail = %d, want 2", got.Verify.Summary.Fail)
	}
	if len(got.Verify.Drifted) != 2 {
		t.Errorf("Drifted len = %d, want 2", len(got.Verify.Drifted))
	}
}

func TestReadLastRun_MissingFile_ReturnsNil(t *testing.T) {
	dir := t.TempDir()
	lr, err := ReadLastRun(LastRunPath(dir))
	if err != nil {
		t.Fatalf("ReadLastRun on missing file: %v", err)
	}
	if lr != nil {
		t.Errorf("ReadLastRun returned %v, want nil", lr)
	}
}

func TestWriteAtomic_UsesRename(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.json")

	if err := writeAtomic(path, map[string]string{"key": "value"}); err != nil {
		t.Fatalf("writeAtomic: %v", err)
	}

	// tmp file must be gone.
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Error("tmp file still exists after writeAtomic")
	}
	// Target must exist.
	if _, err := os.Stat(path); err != nil {
		t.Errorf("target file missing: %v", err)
	}
}
