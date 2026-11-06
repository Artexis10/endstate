// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/Artexis10/endstate/go-engine/internal/safepath"
	"github.com/Artexis10/endstate/go-engine/internal/schedule"
	"github.com/godbus/dbus/v5"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/envelope"
)

func TestRunScheduleLinuxManualDisabledWithoutManager(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux manual scheduling")
	}
	withStateRoot(t, t.TempDir())
	_, err := RunSchedule(ScheduleFlags{Subcommand: "run"})
	if err == nil || err.Code != envelope.ErrScheduleDisabled {
		t.Fatalf("manual run must read saved intent without a manager: %v", err)
	}
}

// isolatedScheduleManager models only the native boundary. Config, units,
// command handlers, verification and run locking use production code.
type isolatedScheduleManager struct {
	dir, root, executable string
	paths                 []string
	enabled, active       bool
	failCommand, failRead string
	commands              []string
	overrides             map[string]dbus.Variant
	dropIns               []string
	onActivate            func()
	afterCommand          func(string)
	pathsErr              error
	loadedFragments       map[string]bool
}

func (m *isolatedScheduleManager) UnitPaths(context.Context) ([]string, error) {
	return m.paths, m.pathsErr
}
func (m *isolatedScheduleManager) Command(_ context.Context, args ...string) error {
	op := strings.Join(args, " ")
	m.commands = append(m.commands, op)
	if op == m.failCommand {
		return errors.New("injected native failure")
	}
	switch args[0] {
	case "enable":
		m.enabled = true
		m.active = true
		if m.onActivate != nil {
			m.onActivate()
		}
	case "stop":
		m.active = false
	case "disable":
		m.enabled = false
	}
	if args[0] == "daemon-reload" && m.loadedFragments != nil {
		for _, name := range []string{schedule.ServiceName, schedule.TimerName} {
			_, err := os.Stat(filepath.Join(m.dir, name))
			m.loadedFragments[name] = err == nil
		}
	}
	if m.afterCommand != nil {
		m.afterCommand(op)
	}
	return nil
}
func (m *isolatedScheduleManager) Properties(_ context.Context, name, kind string) (map[string]dbus.Variant, error) {
	if kind == m.failRead {
		return nil, errors.New("injected native read failure")
	}
	values := map[string]interface{}{}
	_, err := os.Stat(filepath.Join(m.dir, name))
	present := err == nil
	if m.loadedFragments != nil {
		present = m.loadedFragments[name]
	}
	switch kind {
	case "Unit":
		load, fragment := "not-found", ""
		if present {
			load = "loaded"
			fragment = filepath.Join(m.dir, name)
		}
		active, enabled := "inactive", "disabled"
		if m.active {
			active = "active"
		}
		if m.enabled {
			enabled = "enabled"
		}
		values = map[string]interface{}{"LoadState": load, "FragmentPath": fragment, "DropInPaths": m.dropIns, "ActiveState": active, "UnitFileState": enabled}
	case "Service":
		values = map[string]interface{}{"Result": "success", "ExecMainStatus": int32(0), "Type": "oneshot", "Restart": "no", "WorkingDirectory": "", "RemainAfterExit": false, "ExecStartEx": []schedule.ExecCommandEx{{Path: m.executable, Args: []string{m.executable, "schedule", "run", "--root", m.root, "--json"}, Flags: []string{"no-env-expand"}}}}
		for _, field := range []string{"ExecCondition", "ExecStartPre", "ExecStartPost", "ExecReload", "ExecStop", "ExecStopPost"} {
			values[field] = []schedule.ExecCommand{}
		}
	case "Timer":
		values = map[string]interface{}{"Unit": schedule.ServiceName, "Persistent": true, "OnClockChange": false, "OnTimezoneChange": false, "TimersCalendar": []schedule.Calendar{{Base: "OnCalendar", Expression: "*-*-* 09:00:00"}}, "TimersMonotonic": []struct {
			Base        string
			Value, Next uint64
		}{}, "NextElapseUSecRealtime": uint64(1794000000000000)}
	}
	out := map[string]dbus.Variant{}
	for key, value := range values {
		out[key] = dbus.MakeVariant(value)
	}
	for key, value := range m.overrides {
		if _, exists := out[key]; exists {
			out[key] = value
		}
	}
	return out, nil
}
func isolatedSchedule(t *testing.T) (ScheduleFlags, *schedule.SystemdRegistrar, *isolatedScheduleManager) {
	t.Helper()
	root := t.TempDir()
	withStateRoot(t, root)
	file := filepath.Join(root, "manifest.jsonc")
	content := `{"version":1,"name":"scheduled profile","apps":[],"verify":[{"type":"file-exists","path":"` + filepath.ToSlash(filepath.Join(root, "expected.conf")) + `"}]}`
	if err := os.WriteFile(file, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "systemd", "user")
	manager := &isolatedScheduleManager{dir: dir, root: root, executable: exe, paths: []string{filepath.Join(filepath.Dir(dir), "control"), dir}}
	reg := &schedule.SystemdRegistrar{Manager: manager, UnitDir: dir}
	previous := scheduleRegistrarFn
	scheduleRegistrarFn = func() schedule.Registrar { return reg }
	t.Cleanup(func() { scheduleRegistrarFn = previous })
	return ScheduleFlags{Subcommand: "enable", Manifest: file, Root: root}, reg, manager
}
func scheduleConfigForTest(t *testing.T, root string) *schedule.Config {
	t.Helper()
	cfg, err := schedule.ReadConfig(schedule.ConfigPath(filepath.Join(root, "state")))
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}
func TestRunScheduleLinuxLifecycle(t *testing.T) {
	flags, reg, manager := isolatedSchedule(t)
	if _, err := RunSchedule(flags); err != nil {
		t.Fatal(err)
	}
	cfg := scheduleConfigForTest(t, flags.Root)
	if !cfg.Enabled || cfg.Registration.Phase != "commit" {
		t.Fatalf("saved registration=%+v", cfg)
	}
	manager.dropIns = []string{filepath.Join(manager.dir, "policy.conf")}
	manager.overrides = map[string]dbus.Variant{"WorkingDirectory": dbus.MakeVariant(flags.Root)}
	data, err := RunSchedule(ScheduleFlags{Subcommand: "status", Root: flags.Root})
	if err != nil {
		t.Fatal(err)
	}
	status := data.(*ScheduleStatusData)
	if !status.Enabled || status.Registration.Enabled == nil || !*status.Registration.Enabled || status.Registration.Active == nil || !*status.Registration.Active || status.Registration.StateReason != "" || len(status.Registration.Units[schedule.ServiceName].DropInPaths) != 1 {
		t.Fatalf("status=%+v", status)
	}
	// The real verifier sees a missing file as drift, then a restored file as clean.
	for _, wantFail := range []int{1, 0} {
		if wantFail == 0 {
			if err := os.WriteFile(filepath.Join(flags.Root, "expected.conf"), []byte("restored"), 0600); err != nil {
				t.Fatal(err)
			}
		}
		result, err := RunSchedule(ScheduleFlags{Subcommand: "run", Root: flags.Root})
		if err != nil {
			t.Fatal(err)
		}
		if got := result.(*ScheduleRunData).Verify; got == nil || got.Summary.Fail != wantFail {
			t.Fatalf("verify=%+v want failures %d", got, wantFail)
		}
	}
	// Reassertion retains one registration and all queue fields.
	cfg = scheduleConfigForTest(t, flags.Root)
	cfg.PendingUploads = []schedule.PendingUpload{{Artifact: "retained", SHA256: "evidence", OperationID: "retained-operation", CreateSpool: "retained-spool"}}
	if err := schedule.WriteConfig(schedule.ConfigPath(filepath.Join(flags.Root, "state")), cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := RunSchedule(flags); err != nil {
		t.Fatal(err)
	}
	if got := scheduleConfigForTest(t, flags.Root).PendingUploads; len(got) != 1 || got[0].OperationID != "retained-operation" {
		t.Fatalf("queue=%+v", got)
	}
	// The recorded directory governs cleanup after the selected directory changes.
	reg.UnitDir = filepath.Join(t.TempDir(), "different-user-units")
	if _, err := RunSchedule(ScheduleFlags{Subcommand: "disable", Root: flags.Root}); err != nil {
		t.Fatal(err)
	}
	if manager.enabled || manager.active || scheduleConfigForTest(t, flags.Root).Enabled {
		t.Fatal("disable left timer/config enabled")
	}
	for _, name := range []string{schedule.ServiceName, schedule.TimerName} {
		if _, err := os.Lstat(filepath.Join(manager.dir, name)); !os.IsNotExist(err) {
			t.Fatalf("unit remains: %s %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(manager.paths[0], ".endstate-registration.lock")); err != nil {
		t.Fatalf("namespace lock inode was removed: %v", err)
	}
	if len(scheduleConfigForTest(t, flags.Root).PendingUploads) != 1 {
		t.Fatal("disable discarded replay evidence")
	}
}
func TestRunScheduleLinuxInitialWriteFailurePreservesHealthyTimer(t *testing.T) {
	flags, _, manager := isolatedSchedule(t)
	if _, err := RunSchedule(flags); err != nil {
		t.Fatal(err)
	}
	previous := scheduleConfigForTest(t, flags.Root)
	before := len(manager.commands)
	original := scheduleWriteConfigFn
	scheduleWriteConfigFn = func(string, *schedule.Config) error { return errors.New("disk full") }
	t.Cleanup(func() { scheduleWriteConfigFn = original })
	flags.Time = "10:30"
	if _, err := RunSchedule(flags); err == nil {
		t.Fatal("expected initial write failure")
	}
	after := scheduleConfigForTest(t, flags.Root)
	if !manager.enabled || !manager.active || len(manager.commands) != before || !reflect.DeepEqual(previous, after) {
		t.Fatalf("healthy registration changed: before=%+v after=%+v commands=%v", previous, after, manager.commands[before:])
	}
}
func TestRunScheduleLinuxRecoversEachPartialEnable(t *testing.T) {
	// Each case represents a different effect or final configuration commit.
	for _, phase := range []string{"publish-service", "publish-timer", "reload", "activate", "commit"} {
		for _, recoverWith := range []string{"enable", "disable"} {
			t.Run(phase+"/"+recoverWith, func(t *testing.T) {
				flags, reg, manager := isolatedSchedule(t)
				original := scheduleWriteConfigFn
				if phase == "commit" {
					scheduleWriteConfigFn = func(path string, cfg *schedule.Config) error {
						if cfg.Enabled {
							return errors.New("commit failed")
						}
						return original(path, cfg)
					}
					t.Cleanup(func() { scheduleWriteConfigFn = original })
				} else if strings.HasPrefix(phase, "publish-") {
					reg.WriteUnit = func(path string, data []byte, mode os.FileMode) error {
						if (phase == "publish-service" && filepath.Base(path) == schedule.ServiceName) || (phase == "publish-timer" && filepath.Base(path) == schedule.TimerName) {
							return errors.New("unit write failed")
						}
						return safepath.AtomicWriteFile(path, data, mode)
					}
				} else if phase == "reload" {
					manager.failCommand = "daemon-reload"
				} else {
					manager.failCommand = "enable --now " + schedule.TimerName
				}
				if _, err := RunSchedule(flags); err == nil {
					t.Fatal("expected injected registration failure")
				}
				saved := scheduleConfigForTest(t, flags.Root)
				if saved.Enabled || saved.Registration == nil || saved.Registration.Operation != "enable" {
					t.Fatalf("pending evidence=%+v", saved)
				}
				if manager.active || manager.enabled {
					t.Fatalf("failed registration still active: %+v", manager)
				}
				scheduleWriteConfigFn = original
				reg.WriteUnit = nil
				manager.failCommand = ""
				flags.Subcommand = recoverWith
				if _, err := RunSchedule(flags); err != nil {
					t.Fatalf("recovery: %v", err)
				}
				if got := scheduleConfigForTest(t, flags.Root).Enabled; got != (recoverWith == "enable") {
					t.Fatalf("enabled=%v", got)
				}
			})
		}
	}
}
func TestRunScheduleLinuxRejectsModifiedForeignSymlinkAndOverrides(t *testing.T) {
	for _, conflict := range []string{"modified", "foreign", "symlink", "fragment", "execution", "calendar", "hook"} {
		t.Run(conflict, func(t *testing.T) {
			flags, _, manager := isolatedSchedule(t)
			if conflict != "foreign" && conflict != "symlink" {
				if _, err := RunSchedule(flags); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.MkdirAll(manager.dir, 0755); err != nil {
					t.Fatal(err)
				}
			}
			path := filepath.Join(manager.dir, schedule.ServiceName)
			switch conflict {
			case "modified", "foreign":
				if err := os.WriteFile(path, []byte("# user policy\n[Service]\nExecStart=/bin/true\n"), 0600); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				target := filepath.Join(t.TempDir(), "foreign.service")
				if err := os.WriteFile(target, []byte("foreign"), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, path); err != nil {
					t.Fatal(err)
				}
			case "fragment":
				manager.overrides = map[string]dbus.Variant{"FragmentPath": dbus.MakeVariant("/other/policy.service")}
			case "execution":
				manager.overrides = map[string]dbus.Variant{"ExecStartEx": dbus.MakeVariant([]schedule.ExecCommandEx{{Path: "/bin/true", Args: []string{"/bin/true"}}})}
			case "calendar":
				manager.overrides = map[string]dbus.Variant{"TimersCalendar": dbus.MakeVariant([]schedule.Calendar{{Base: "OnCalendar", Expression: "*-*-* 23:00:00"}})}
			case "hook":
				manager.overrides = map[string]dbus.Variant{"ExecStartPost": dbus.MakeVariant([]schedule.ExecCommand{{Path: "/bin/true", Args: []string{"/bin/true"}}})}
			}
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			beforeCommands := len(manager.commands)
			if _, err := RunSchedule(flags); err == nil {
				t.Fatal("accepted conflict")
			}
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, after) {
				t.Fatal("changed conflicting unit")
			}
			if conflict == "fragment" && len(manager.commands) != beforeCommands {
				t.Fatalf("mutated shadowing unit: %v", manager.commands[beforeCommands:])
			}
			if conflict == "modified" || conflict == "foreign" || conflict == "symlink" {
				if _, err := RunSchedule(ScheduleFlags{Subcommand: "disable", Root: flags.Root}); err == nil {
					t.Fatal("disable accepted conflicting unit")
				}
			}
		})
	}
}
func TestRunScheduleLinuxStatusKeepsIndependentFailuresVisible(t *testing.T) {
	flags, _, manager := isolatedSchedule(t)
	if _, err := RunSchedule(flags); err != nil {
		t.Fatal(err)
	}
	path := schedule.LastRunPath(filepath.Join(flags.Root, "state"))
	if err := os.WriteFile(path, []byte("broken result"), 0600); err != nil {
		t.Fatal(err)
	}
	manager.failRead = "Unit"
	data, err := RunSchedule(ScheduleFlags{Subcommand: "status", Root: flags.Root})
	if err != nil {
		t.Fatal(err)
	}
	status := data.(*ScheduleStatusData)
	if !status.Enabled || status.Registration.Enabled != nil || status.Registration.Active != nil || status.Registration.StateReason != "INSPECTION_FAILED" || status.LastRunError == nil || status.LastRun != nil {
		t.Fatalf("status hides unknown/failure: %+v", status)
	}
}
func TestRunScheduleLinuxCatchupWaitsForCommittedConfig(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux blocking run lock")
	}
	flags, _, manager := isolatedSchedule(t)
	started := make(chan struct{})
	finished := make(chan *envelope.Error, 1)
	manager.onActivate = func() {
		go func() {
			close(started)
			_, err := RunSchedule(ScheduleFlags{Subcommand: "run", Root: flags.Root})
			finished <- err
		}()
		<-started
		select {
		case err := <-finished:
			t.Fatalf("catch-up returned before config commit: %v", err)
		case <-time.After(100 * time.Millisecond):
		}
	}
	if _, err := RunSchedule(flags); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("catch-up stayed blocked after registration")
	}
}

func TestRunScheduleLinuxInterruptedEffectsRecover(t *testing.T) {
	for _, phase := range []string{"publish-service", "reload", "activate", "commit"} {
		for _, recovery := range []string{"enable", "disable"} {
			t.Run(phase+"/"+recovery, func(t *testing.T) {
				flags, reg, manager := isolatedSchedule(t)
				original := scheduleWriteConfigFn
				if phase == "publish-service" {
					reg.WriteUnit = func(path string, data []byte, mode os.FileMode) error {
						if err := safepath.AtomicWriteFile(path, data, mode); err != nil {
							return err
						}
						panic("interrupted after publication")
					}
				} else if phase == "commit" {
					scheduleWriteConfigFn = func(path string, cfg *schedule.Config) error {
						if cfg.Enabled {
							panic("interrupted before enabled commit")
						}
						return original(path, cfg)
					}
					t.Cleanup(func() { scheduleWriteConfigFn = original })
				} else {
					manager.afterCommand = func(op string) {
						if (phase == "reload" && op == "daemon-reload") || (phase == "activate" && op == "enable --now "+schedule.TimerName) {
							panic("interrupted after effect")
						}
					}
				}
				interrupted := false
				func() {
					defer func() {
						if recover() != nil {
							interrupted = true
						}
					}()
					_, _ = RunSchedule(flags)
				}()
				if !interrupted {
					t.Fatal("interruption did not occur")
				}
				pending := scheduleConfigForTest(t, flags.Root)
				if pending.Enabled || pending.Registration == nil {
					t.Fatalf("lost intent: %+v", pending)
				}
				reg.WriteUnit = nil
				manager.afterCommand = nil
				scheduleWriteConfigFn = original
				flags.Subcommand = recovery
				if _, err := RunSchedule(flags); err != nil {
					t.Fatalf("recovery: %v", err)
				}
				if got := scheduleConfigForTest(t, flags.Root).Enabled; got != (recovery == "enable") {
					t.Fatalf("enabled=%v", got)
				}
			})
		}
	}
}
func TestRunScheduleLinuxAnotherRootCannotReplaceRegistration(t *testing.T) {
	flags, _, manager := isolatedSchedule(t)
	if _, err := RunSchedule(flags); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(manager.dir, schedule.ServiceName)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	otherRoot := t.TempDir()
	flags.Root = otherRoot
	if _, err := RunSchedule(flags); err == nil {
		t.Fatal("another root replaced registration")
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("changed original: %v", err)
	}
	if _, err := os.Stat(schedule.ConfigPath(filepath.Join(otherRoot, "state"))); !os.IsNotExist(err) {
		t.Fatalf("created foreign config: %v", err)
	}
}

func TestRunScheduleLinuxCopiedConfigCannotDisableOriginalRoot(t *testing.T) {
	flags, _, manager := isolatedSchedule(t)
	if _, err := RunSchedule(flags); err != nil {
		t.Fatal(err)
	}
	originalPath := schedule.ConfigPath(filepath.Join(flags.Root, "state"))
	original, err := os.ReadFile(originalPath)
	if err != nil {
		t.Fatal(err)
	}
	units := map[string][]byte{}
	for _, name := range []string{schedule.ServiceName, schedule.TimerName} {
		units[name], err = os.ReadFile(filepath.Join(manager.dir, name))
		if err != nil {
			t.Fatal(err)
		}
	}
	copiedRoot := t.TempDir()
	copiedPath := schedule.ConfigPath(filepath.Join(copiedRoot, "state"))
	if err := os.MkdirAll(filepath.Dir(copiedPath), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(copiedPath, original, 0600); err != nil {
		t.Fatal(err)
	}
	before := len(manager.commands)
	_, registrationErr := RunSchedule(ScheduleFlags{Subcommand: "disable", Root: copiedRoot})
	if registrationErr == nil || registrationErr.Code != envelope.ErrTaskRegistrationFailed {
		t.Fatalf("copied-root disable must reject foreign ownership: %v", registrationErr)
	}
	if !manager.active || !manager.enabled || len(manager.commands) != before {
		t.Fatalf("copied-root disable changed native registration: active=%v enabled=%v commands=%v", manager.active, manager.enabled, manager.commands[before:])
	}
	for _, path := range []string{originalPath, copiedPath} {
		after, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(original, after) {
			t.Fatalf("copied-root disable changed configuration %s: %v", path, err)
		}
	}
	for name, original := range units {
		after, err := os.ReadFile(filepath.Join(manager.dir, name))
		if err != nil || !bytes.Equal(original, after) {
			t.Fatalf("copied-root disable changed foreign unit %s: %v", name, err)
		}
	}
}
func TestRunScheduleLinuxManualCommandsNeverProbeManager(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux manual operations")
	}
	root := t.TempDir()
	withStateRoot(t, root)
	previous := scheduleRegistrarFn
	scheduleRegistrarFn = func() schedule.Registrar { t.Fatal("manual command probed registration"); return nil }
	t.Cleanup(func() { scheduleRegistrarFn = previous })
	_, runErr := RunSchedule(ScheduleFlags{Subcommand: "run", Root: root})
	if runErr == nil || runErr.Code != envelope.ErrScheduleDisabled {
		t.Fatalf("run=%v", runErr)
	}
	cfg := &schedule.Config{SchemaVersion: "1.0", Root: root, PendingUploads: []schedule.PendingUpload{{Artifact: "retained", SHA256: "abc", LegacyCreateStarted: true}}}
	if err := schedule.WriteConfig(schedule.ConfigPath(filepath.Join(root, "state")), cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := RunSchedule(ScheduleFlags{Subcommand: "discard-upload", Root: root, Confirm: true, ArtifactSHA256: "abc"}); err != nil {
		t.Fatal(err)
	}
}

func TestRunScheduleLinuxUnavailableRegistrationKeepsManualStatus(t *testing.T) {
	flags, _, manager := isolatedSchedule(t)
	manager.pathsErr = errors.New("user manager unavailable")
	if _, err := RunSchedule(flags); err == nil || err.Code != envelope.ErrNotSupported {
		t.Fatalf("enable=%v", err)
	}
	if len(manager.commands) != 0 {
		t.Fatalf("unavailable manager received effects: %v", manager.commands)
	}
	if _, err := os.Stat(schedule.ConfigPath(filepath.Join(flags.Root, "state"))); !os.IsNotExist(err) {
		t.Fatalf("unavailable registration wrote config: %v", err)
	}
	data, err := RunSchedule(ScheduleFlags{Subcommand: "status", Root: flags.Root})
	if err != nil {
		t.Fatal(err)
	}
	status := data.(*ScheduleStatusData)
	if status.Registration.Supported || status.Registration.Reason != "USER_MANAGER_UNAVAILABLE" || status.Registration.Enabled != nil || status.Registration.Active != nil {
		t.Fatalf("unavailable status=%+v", status)
	}
	caps, envErr := RunCapabilities()
	if envErr != nil {
		t.Fatal(envErr)
	}
	feature := caps.(CapabilitiesData).Features.Schedule
	if feature.Supported != schedule.ManualSupported() || feature.Registration.Supported || feature.Registration.Backend != "systemd-user" {
		t.Fatalf("capability split=%+v", feature)
	}
}

func TestRunScheduleLinuxRecoversMixedPriorAndDesiredUnits(t *testing.T) {
	flags, reg, manager := isolatedSchedule(t)
	if _, err := RunSchedule(flags); err != nil {
		t.Fatal(err)
	}
	cfg := scheduleConfigForTest(t, flags.Root)
	cfg.Time = "10:30"
	intent := schedule.Intent{Executable: manager.executable + " updated", Root: flags.Root, Manifest: flags.Manifest, Args: []string{"schedule", "run", "--root", flags.Root, "--json"}, Interval: "daily", Time: "10:30"}
	reg.WriteUnit = func(path string, data []byte, mode os.FileMode) error {
		if err := safepath.AtomicWriteFile(path, data, mode); err != nil {
			return err
		}
		panic("interrupted between service and timer publication")
	}
	interrupted := false
	func() {
		defer func() {
			if recover() != nil {
				interrupted = true
			}
		}()
		_ = reg.Enable(context.Background(), intent, cfg, func() error {
			return schedule.WriteConfig(schedule.ConfigPath(filepath.Join(flags.Root, "state")), cfg)
		})
	}()
	if !interrupted {
		t.Fatal("did not interrupt between unit versions")
	}
	pending := scheduleConfigForTest(t, flags.Root)
	service, err := os.ReadFile(filepath.Join(manager.dir, schedule.ServiceName))
	if err != nil {
		t.Fatal(err)
	}
	timer, err := os.ReadFile(filepath.Join(manager.dir, schedule.TimerName))
	if err != nil {
		t.Fatal(err)
	}
	serviceHash := sha256.Sum256(service)
	timerHash := sha256.Sum256(timer)
	if hex.EncodeToString(serviceHash[:]) != pending.Registration.Units[schedule.ServiceName].Desired || hex.EncodeToString(timerHash[:]) != pending.Registration.Units[schedule.TimerName].Previous {
		t.Fatal("fixture did not retain mixed prior/desired bytes")
	}
	reg.WriteUnit = nil
	if _, err := RunSchedule(flags); err != nil {
		t.Fatalf("reassert current executable from mixed versions: %v", err)
	}
	if !scheduleConfigForTest(t, flags.Root).Enabled {
		t.Fatal("recovery did not commit current desired registration")
	}
}

func TestRunScheduleLinuxDisableStopsLoadedTimerWithMissingFragment(t *testing.T) {
	flags, _, manager := isolatedSchedule(t)
	if _, err := RunSchedule(flags); err != nil {
		t.Fatal(err)
	}
	manager.loadedFragments = map[string]bool{schedule.ServiceName: true, schedule.TimerName: true}
	if err := os.Remove(filepath.Join(manager.dir, schedule.TimerName)); err != nil {
		t.Fatal(err)
	}
	if _, err := RunSchedule(ScheduleFlags{Subcommand: "disable", Root: flags.Root}); err != nil {
		t.Fatal(err)
	}
	if manager.active || manager.enabled || scheduleConfigForTest(t, flags.Root).Enabled {
		t.Fatal("missing fragment was mistaken for a stopped native timer")
	}
}
