// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package schedule

import (
	"context"
	"errors"
	"github.com/godbus/dbus/v5"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestSystemdNativeParserAcceptsLiteralArguments(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("native systemd parser")
	}
	analyzer, err := exec.LookPath("systemd-analyze")
	if err != nil {
		t.Skip("systemd-analyze is not installed")
	}
	dir := t.TempDir()
	runtimeDir, err := os.MkdirTemp(os.TempDir(), "es-runtime-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(runtimeDir)

	executable := filepath.Join(dir, `engine %n $HOME space`)
	if err := os.WriteFile(executable, []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(dir, "root %n $HOME \"quoted\" \\ newline\nsecond")
	intent := Intent{Executable: executable, Root: root, Manifest: filepath.Join(root, "manifest.jsonc"), Args: []string{"schedule", "run", "--root", root, "--json"}, Interval: "daily", Time: "09:00"}
	units, err := renderUnits(intent)
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range units {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, analyzer, "--user", "--generators=no", "--man=no", "verify", filepath.Join(dir, ServiceName), filepath.Join(dir, TimerName))
	cmd.Env = append(os.Environ(), "XDG_RUNTIME_DIR="+runtimeDir, "DBUS_SESSION_BUS_ADDRESS=unix:path="+filepath.Join(runtimeDir, "no-bus"), "SYSTEMD_UNIT_PATH="+dir+":/usr/lib/systemd/user")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("native unit parser: %v\n%s", err, out)
	}
	service := string(units[ServiceName])
	if !strings.Contains(service, "ExecStart=:") || !strings.Contains(service, "%%n") || !strings.Contains(service, "$HOME") || strings.Contains(service, "--manifest") || !strings.Contains(service, `\x0a`) {
		t.Fatalf("literal native argv encoding: %s", service)
	}
}

func TestWindowsArgumentsPreserveQuotesAndTrailingSlashes(t *testing.T) {
	for _, tc := range []struct{ arg, want string }{{"", `""`}, {`C:\engine path\`, `"C:\engine path\\"`}, {`a"b`, `"a\"b"`}, {`plain`, `"plain"`}} {
		t.Run(tc.arg, func(t *testing.T) {
			if got := windowsArgument(tc.arg); got != tc.want {
				t.Fatalf("argument=%q encoded=%q want=%q", tc.arg, got, tc.want)
			}
		})
	}
}

func TestSystemdNamespaceLockIgnoresSelectedUnitDirectory(t *testing.T) {
	dir := t.TempDir()
	paths := []string{filepath.Join(dir, "shared"), filepath.Join(dir, "one"), filepath.Join(dir, "two")}
	manager := &lockOnlyManager{paths: paths}
	first := &SystemdRegistrar{Manager: manager, UnitDir: paths[1]}
	second := &SystemdRegistrar{Manager: manager, UnitDir: paths[2]}
	unlock, err := first.LockRegistration(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if release, err := second.LockRegistration(ctx); err == nil {
		release()
		t.Fatal("caller UnitDir bypassed shared lock")
	}
	before, err := os.Stat(filepath.Join(paths[0], ".endstate-registration.lock"))
	if err != nil {
		t.Fatal(err)
	}
	unlock()
	release, err := second.LockRegistration(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	release()
	after, err := os.Stat(filepath.Join(paths[0], ".endstate-registration.lock"))
	if err != nil || !os.SameFile(before, after) {
		t.Fatalf("lock inode changed: %v", err)
	}
}

type lockOnlyManager struct{ paths []string }

func (m *lockOnlyManager) UnitPaths(context.Context) ([]string, error) { return m.paths, nil }
func (*lockOnlyManager) Properties(context.Context, string, string) (map[string]dbus.Variant, error) {
	panic("lock inspected units")
}
func (*lockOnlyManager) Command(context.Context, ...string) error { panic("lock mutated manager") }

func TestSystemdRejectsUnrepresentableExecutableBeforeSaving(t *testing.T) {
	for _, name := range []string{"/path/engine\"quoted", "/path/engine'quoted", "/path/engine\\slash", "/path/engine\nline"} {
		t.Run(name, func(t *testing.T) {
			reg := &SystemdRegistrar{}
			saved := false
			err := reg.Enable(context.Background(), Intent{Executable: name, Root: "/root", Args: []string{"schedule", "run"}, Interval: "daily", Time: "09:00"}, &Config{}, func() error { saved = true; return nil })
			var detail *RegistrationError
			if !errors.As(err, &detail) || detail.Reason != "INVALID_INTENT" || saved {
				t.Fatalf("preflight=%v saved=%v", err, saved)
			}
		})
	}
}
func TestSystemdExecTupleMatchesNativeSchema(t *testing.T) {
	// Native systemd tuples include both realtime and monotonic start/stop times.
	tuple := [][]interface{}{{"/bin/true", []string{"/bin/true"}, []string{"no-env-expand"}, uint64(1), uint64(2), uint64(3), uint64(4), uint32(5), int32(6), int32(7)}}
	values := map[string]dbus.Variant{"ExecStartEx": dbus.MakeVariantWithSignature(tuple, dbus.ParseSignatureMust("a(sasasttttuii)"))}

	var parsed []ExecCommandEx
	if err := property(values, "ExecStartEx", &parsed); err != nil {
		t.Fatal(err)
	}
	if len(parsed) != 1 || parsed[0].StopMonotonic != 4 || parsed[0].Status != 7 {
		t.Fatalf("native tuple=%+v", parsed)
	}
}
func TestSystemdAvailabilityNeverDiscoversOrLaunchesSession(t *testing.T) {
	manager := newSessionManager(nil)
	if _, err := manager.UnitPaths(context.Background()); err == nil {
		t.Fatal("discovered ambient session despite missing explicit coordinates")
	}
}

func TestSystemdNativeCommandUsesFrozenExplicitBus(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("isolated POSIX process boundary")
	}
	dir := t.TempDir()
	record := filepath.Join(dir, "environment")
	script := "#!/bin/sh\nprintf '%s\\n' \"$DBUS_SESSION_BUS_ADDRESS\" \"$SYSTEMCTL_FORCE_BUS\" \"$XDG_RUNTIME_DIR\" \"$@\" > \"$ENDSTATE_TEST_MANAGER_ARGS\"\n"
	if err := os.WriteFile(filepath.Join(dir, "systemctl"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	address := "unix:path=" + filepath.Join(dir, "explicit-session")
	env := []string{"PATH=" + os.Getenv("PATH"), "DBUS_SESSION_BUS_ADDRESS=" + address, "SYSTEMCTL_FORCE_BUS=0", "XDG_RUNTIME_DIR=" + filepath.Join(dir, "runtime"), "ENDSTATE_TEST_MANAGER_ARGS=" + record}
	manager := newSessionManager(env)
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "unix:path=/ambient-session")
	if err := manager.Command(context.Background(), "daemon-reload"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(record)
	if err != nil {
		t.Fatal(err)
	}
	expected := address + "\n1\n" + filepath.Join(dir, "runtime") + "\n--user\ndaemon-reload\n"
	if string(data) != expected {
		t.Fatalf("frozen manager environment=%q want=%q", data, expected)
	}
}
