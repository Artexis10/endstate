// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package schedule

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/safepath"
	"github.com/godbus/dbus/v5"
	"github.com/gofrs/flock"
)

// Fixed names and owned property names come from the systemd unit schema.
const ServiceName = "endstate-drift-check.service"
const TimerName = "endstate-drift-check.timer"
const systemdBackend = "systemd-user"
const unitFormatVersion = 1

// SystemdManager keeps native job waiting separate from typed observations.
// The same frozen session must supply both interfaces.
type SystemdManager interface {
	UnitPaths(context.Context) ([]string, error)
	Properties(context.Context, string, string) (map[string]dbus.Variant, error)
	Command(context.Context, ...string) error
}

type SystemdRegistrar struct {
	Manager SystemdManager
	UnitDir string
	// WriteUnit is the existing atomic writer, replaceable at the native boundary.
	WriteUnit func(string, []byte, os.FileMode) error
	paths     []string
}

func newSystemdRegistrar() *SystemdRegistrar {
	env := os.Environ()
	configHome := os.Getenv("XDG_CONFIG_HOME")
	if configHome == "" {
		home, err := os.UserHomeDir()
		if err == nil {
			configHome = filepath.Join(home, ".config")
		}
	}
	return &SystemdRegistrar{Manager: newSessionManager(env), UnitDir: filepath.Join(configHome, "systemd", "user")}
}
func (r *SystemdRegistrar) Availability(ctx context.Context, cfg *Config) Availability {

	a := Availability{Backend: systemdBackend}
	paths := r.paths
	var err error
	if paths == nil {
		paths, err = r.Manager.UnitPaths(ctx)
	}
	if err != nil {
		a.Reason = "USER_MANAGER_UNAVAILABLE"
		a.Detail = err.Error()
		return a
	}
	dir := r.UnitDir
	if cfg != nil && cfg.Registration != nil {
		dir = cfg.Registration.UnitDir
	}
	if !filepath.IsAbs(dir) {
		a.Reason = "UNIT_NAMESPACE_UNAVAILABLE"
		a.Detail = "user unit directory is not absolute"
		return a
	}

	target := filepath.Clean(dir)
	found := false
	for _, p := range paths {
		if filepath.Clean(p) == target {
			found = true
		}
	}
	if !found {
		a.Reason = "UNIT_NAMESPACE_UNAVAILABLE"
		a.Detail = "selected user unit directory is outside Manager.UnitPath"
		return a
	}
	r.paths = append([]string(nil), paths...)
	a.Supported = true

	return a
}
func (r *SystemdRegistrar) LockRegistration(ctx context.Context) (func(), error) {
	paths, err := r.Manager.UnitPaths(ctx)
	if err != nil {
		return nil, registrationError("preflight", "USER_MANAGER_UNAVAILABLE", err)
	}
	r.paths = append([]string(nil), paths...)

	// Every caller chooses the first absolute Manager.UnitPath entry, irrespective
	// of its target UnitDir. Never fall through to another inode after failure.
	anchor := ""
	for _, p := range r.paths {
		if filepath.IsAbs(p) {
			anchor = filepath.Join(p, ".endstate-registration.lock")
			break
		}
	}
	if anchor == "" {
		return nil, registrationError("lock", "UNIT_NAMESPACE_UNAVAILABLE", errors.New("manager has no absolute unit path"))
	}
	if err := validateUnitDirectory(filepath.Dir(anchor)); err != nil {
		return nil, registrationError("lock", "LOCK_FAILED", err)
	}
	if err := os.MkdirAll(filepath.Dir(anchor), 0700); err != nil {
		return nil, registrationError("lock", "LOCK_FAILED", err)
	}
	if info, err := os.Lstat(anchor); err == nil && !info.Mode().IsRegular() {
		return nil, registrationError("lock", "LOCK_FAILED", errors.New("namespace lock is not a regular file"))
	} else if err != nil && !os.IsNotExist(err) {
		return nil, registrationError("lock", "LOCK_FAILED", err)
	}
	lock := flock.New(anchor)
	ok, err := lock.TryLockContext(ctx, 50*time.Millisecond)
	if err != nil {
		return nil, registrationError("lock", "LOCK_FAILED", err)
	}
	if !ok {
		return nil, registrationError("lock", "LOCK_FAILED", ctx.Err())
	}
	return func() { _ = lock.Unlock() }, nil
}

func (r *SystemdRegistrar) Enable(ctx context.Context, intent Intent, cfg *Config, save func() error) error {
	units, err := renderUnits(intent)
	if err != nil {
		return registrationError("preflight", "INVALID_INTENT", err)
	}
	dir := r.UnitDir
	if cfg.Registration != nil {
		dir = cfg.Registration.UnitDir
	}
	if err := r.validateScope(cfg, dir); err != nil {
		return err
	}
	old := cfg.Registration
	observed, err := admittedUnits(cfg, dir)
	if err != nil {
		return err
	}
	// A previously generated fragment can already be shadowed. Never stop that
	// foreign timer while trying to repair the owned on-disk files.
	if err := r.checkFragments(ctx, cfg, dir); err != nil {
		return err
	}
	reg := &Registration{Backend: systemdBackend, OwnerRoot: intent.Root, UnitDir: dir, Version: unitFormatVersion, Executable: intent.Executable, Operation: "enable", Phase: "prepare", Units: map[string]UnitHashes{}}
	for name, data := range units {
		reg.Units[name] = UnitHashes{Previous: observed[name], Desired: unitHash(data)}
	}
	cfg.Registration = reg
	cfg.TaskName = TimerName
	cfg.Enabled = false
	if err := save(); err != nil {
		cfg.Registration = old
		return registrationError("prepare", "CONFIG_WRITE_FAILED", err)
	}
	attempt := func(phase string, effect func() error) error {
		reg.Phase = phase
		if err := save(); err != nil {
			return registrationError(phase, "CONFIG_WRITE_FAILED", err)
		}
		if err := effect(); err != nil {
			var re *RegistrationError
			if errors.As(err, &re) {
				re.Phase = phase
				return re
			}
			return registrationError(phase, "NATIVE_OPERATION_FAILED", err)
		}
		return nil
	}
	fail := func(err error) error { cfg.Enabled = false; r.stopOwned(ctx, cfg, save); return err }
	if observed[TimerName] != "" {
		if err := attempt("stop", func() error { return r.Manager.Command(ctx, "stop", TimerName) }); err != nil {
			return fail(err)
		}
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fail(registrationError("publish-service", "UNIT_WRITE_FAILED", err))
	}
	write := r.WriteUnit
	if write == nil {
		write = safepath.AtomicWriteFile
	}
	for _, name := range []string{ServiceName, TimerName} {
		phase := "publish-service"
		if name == TimerName {
			phase = "publish-timer"
		}
		if err := attempt(phase, func() error {
			// Recheck the admitted bytes immediately before each atomic replacement.
			if _, err := admittedUnits(cfg, dir); err != nil {
				return err
			}
			return write(filepath.Join(dir, name), units[name], 0644)
		}); err != nil {
			return fail(err)
		}
	}
	if err := attempt("reload", func() error { return r.Manager.Command(ctx, "daemon-reload") }); err != nil {
		return fail(err)
	}
	if err := attempt("inspect", func() error { return r.checkEffective(ctx, cfg, intent) }); err != nil {
		return fail(err)
	}
	// Starting a timer waits for the timer's job, never for its triggered service.
	if err := attempt("activate", func() error { return r.Manager.Command(ctx, "enable", "--now", TimerName) }); err != nil {
		return fail(err)
	}
	if err := attempt("confirm", func() error {
		if err := r.checkEffective(ctx, cfg, intent); err != nil {
			return err
		}
		p, err := r.Manager.Properties(ctx, TimerName, "Unit")
		if err != nil {
			return err
		}
		var active, enabled string
		if err := property(p, "ActiveState", &active); err != nil {
			return err
		}
		if err := property(p, "UnitFileState", &enabled); err != nil {
			return err
		}
		if active != "active" || enabled != "enabled" {
			return errors.New("timer is not observed active and enabled")
		}
		return nil
	}); err != nil {
		return fail(err)
	}
	cfg.Enabled = true
	reg.Phase = "commit"
	if err := save(); err != nil {
		cfg.Enabled = false
		return fail(registrationError("commit", "CONFIG_WRITE_FAILED", err))
	}
	return nil
}

func (r *SystemdRegistrar) Disable(ctx context.Context, cfg *Config, save func() error) error {
	dir := r.UnitDir
	if cfg.Registration != nil {
		dir = cfg.Registration.UnitDir
	}
	if err := r.validateScope(cfg, dir); err != nil {
		return err
	}
	observed, err := admittedUnits(cfg, dir)
	if err != nil {
		return err
	}
	if err := r.checkFragments(ctx, cfg, dir); err != nil {
		return err
	}
	timerOwned := observed[TimerName] != ""
	if !timerOwned && cfg.Registration != nil {
		// A loaded timer can outlive deletion of its fragment. Prove its effective
		// identity before stopping it; missing bytes alone never prove inactivity.
		native, err := r.Manager.Properties(ctx, TimerName, "Unit")
		if err != nil {
			return registrationError("preflight", "INSPECTION_FAILED", err)
		}
		var load string
		if err := property(native, "LoadState", &load); err != nil {
			return registrationError("preflight", "INSPECTION_FAILED", err)
		}
		if load != "not-found" {
			var active, enabled string
			if err := property(native, "ActiveState", &active); err != nil {
				return registrationError("preflight", "INSPECTION_FAILED", err)
			}
			if err := property(native, "UnitFileState", &enabled); err != nil {
				return registrationError("preflight", "INSPECTION_FAILED", err)
			}
			if active != "inactive" && active != "failed" || enabled == "enabled" || enabled == "enabled-runtime" {
				intent := Intent{Executable: cfg.Registration.Executable, Root: cfg.Root, Manifest: cfg.Manifest, Interval: cfg.Interval, Time: cfg.Time, Args: RunArgs(cfg.Root)}
				if err := r.checkEffective(ctx, cfg, intent); err != nil {
					return err
				}
				timerOwned = true
			}
		}
	}
	cfg.Enabled = false
	cfg.TaskName = TimerName
	if cfg.Registration == nil {
		if err := save(); err != nil {
			return registrationError("prepare", "CONFIG_WRITE_FAILED", err)
		}
		return nil
	}
	reg := cfg.Registration
	reg.Operation = "disable"
	reg.Phase = "prepare"
	if err := save(); err != nil {
		return registrationError("prepare", "CONFIG_WRITE_FAILED", err)
	}
	attempt := func(phase string, effect func() error) error {
		reg.Phase = phase
		if err := save(); err != nil {
			return registrationError(phase, "CONFIG_WRITE_FAILED", err)
		}
		if err := effect(); err != nil {
			return registrationError(phase, "NATIVE_OPERATION_FAILED", err)
		}
		return nil
	}
	if timerOwned {
		for _, op := range []string{"stop", "disable"} {
			if err := attempt(op, func() error { return r.Manager.Command(ctx, op, TimerName) }); err != nil {
				return err
			}
		}
	}
	for _, name := range []string{TimerName, ServiceName} {
		if err := attempt("remove-"+name, func() error {
			current, err := admittedUnits(cfg, dir)
			if err != nil {
				return err
			}
			if current[name] == "" {
				return nil
			}
			return os.Remove(filepath.Join(dir, name))
		}); err != nil {
			return err
		}
	}
	if err := attempt("reload", func() error { return r.Manager.Command(ctx, "daemon-reload") }); err != nil {
		return err
	}
	reg.Phase = "disabled"
	if err := save(); err != nil {
		return registrationError("commit", "CONFIG_WRITE_FAILED", err)
	}
	return nil
}

func (r *SystemdRegistrar) validateScope(cfg *Config, dir string) error {
	if !filepath.IsAbs(dir) {
		return registrationError("preflight", "OWNERSHIP_CONFLICT", errors.New("recorded unit directory is not absolute"))
	}
	found := false
	for _, p := range r.paths {
		if filepath.Clean(p) == filepath.Clean(dir) {
			found = true
		}
	}
	if !found {
		return registrationError("preflight", "UNIT_NAMESPACE_UNAVAILABLE", errors.New("recorded unit directory is outside Manager.UnitPath"))
	}
	if err := validateUnitDirectory(dir); err != nil {
		return registrationError("preflight", "OWNERSHIP_CONFLICT", err)
	}
	if reg := cfg.Registration; reg != nil {
		if reg.Backend != systemdBackend || reg.Version != unitFormatVersion || reg.OwnerRoot != cfg.Root || len(reg.Units) != 2 {
			return registrationError("preflight", "OWNERSHIP_CONFLICT", errors.New("registration evidence does not match root or format"))
		}
	}
	return nil
}
func validateUnitDirectory(dir string) error {
	existing := dir
	for {
		_, err := os.Lstat(existing)
		if os.IsNotExist(err) {
			parent := filepath.Dir(existing)
			if parent == existing {
				return err
			}
			existing = parent
			continue
		}
		if err != nil {
			return err
		}
		break
	}
	if err := safepath.ValidateRoot(existing); err != nil {
		return err
	}
	return nil
}

func unitHash(data []byte) string { hash := sha256.Sum256(data); return hex.EncodeToString(hash[:]) }
func admittedUnits(cfg *Config, dir string) (map[string]string, error) {
	observed := map[string]string{}
	for _, name := range []string{ServiceName, TimerName} {
		hash, err := admittedUnit(cfg, dir, name)
		if err != nil {
			return nil, err
		}
		observed[name] = hash
	}
	return observed, nil
}
func admittedUnit(cfg *Config, dir, name string) (string, error) {
	path := filepath.Join(dir, name)
	data, _, err := safepath.ReadRegularFileBounded(path, 1024*1024)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", registrationError("preflight", "OWNERSHIP_CONFLICT", fmt.Errorf("%s: %w", path, err))
	}
	hash := unitHash(data)
	reg := cfg.Registration
	if reg == nil || reg.OwnerRoot != cfg.Root || reg.Backend != systemdBackend || reg.Version != unitFormatVersion || reg.UnitDir != dir {
		return "", registrationError("preflight", "OWNERSHIP_CONFLICT", fmt.Errorf("%s has no admitted registration evidence", path))
	}
	admitted, ok := reg.Units[name]
	if !ok || (hash != admitted.Previous && hash != admitted.Desired) {
		return "", registrationError("preflight", "OWNERSHIP_CONFLICT", fmt.Errorf("%s differs from admitted contents", path))
	}
	return hash, nil
}

func (r *SystemdRegistrar) checkFragments(ctx context.Context, cfg *Config, dir string) error {
	for _, name := range []string{ServiceName, TimerName} {
		p, err := r.Manager.Properties(ctx, name, "Unit")
		if err != nil {
			return registrationError("preflight", "INSPECTION_FAILED", err)
		}
		var fragment, load string
		if err := property(p, "LoadState", &load); err != nil {
			return err
		}
		if load == "not-found" {
			continue
		}
		if err := property(p, "FragmentPath", &fragment); err != nil {
			return err
		}
		if fragment != filepath.Join(dir, name) {
			return registrationError("preflight", "REGISTRATION_OVERRIDDEN", fmt.Errorf("%s FragmentPath=%q", name, fragment))
		}
		if cfg.Registration == nil {
			return registrationError("preflight", "OWNERSHIP_CONFLICT", fmt.Errorf("%s is loaded without admitted file bytes", name))
		}
	}
	return nil
}
func (r *SystemdRegistrar) stopOwned(ctx context.Context, cfg *Config, save func() error) {
	dir := cfg.Registration.UnitDir
	hash, err := admittedUnit(cfg, dir, TimerName)
	if err != nil || hash == "" {
		return
	}
	p, err := r.Manager.Properties(ctx, TimerName, "Unit")
	if err != nil {
		return
	}
	var fragment string
	if property(p, "FragmentPath", &fragment) != nil || fragment != filepath.Join(dir, TimerName) {
		return
	}
	for _, operation := range []string{"stop", "disable"} {
		cfg.Registration.CleanupPhase = operation
		_ = save()
		_ = r.Manager.Command(ctx, operation, TimerName)
	}
}

// ExecCommand and Calendar are the exact typed D-Bus tuples from systemd.
type ExecCommand struct {
	Path                                       string
	Args                                       []string
	IgnoreFailure                              bool
	Start, StartMonotonic, Stop, StopMonotonic uint64
	PID                                        uint32
	Code, Status                               int32
}

// ExecCommandEx exposes the execution flags as well as the literal argument vector.
type ExecCommandEx struct {
	Path                                       string
	Args, Flags                                []string
	Start, StartMonotonic, Stop, StopMonotonic uint64
	PID                                        uint32
	Code, Status                               int32
}
type Calendar struct {
	Base, Expression string
	Next             uint64
}

func property(p map[string]dbus.Variant, name string, to interface{}) error {
	value, ok := p[name]
	if !ok {
		return fmt.Errorf("missing typed property %s", name)
	}
	if err := dbus.Store([]interface{}{value.Value()}, to); err != nil {
		return fmt.Errorf("property %s: %w", name, err)
	}
	return nil
}
func (r *SystemdRegistrar) checkEffective(ctx context.Context, cfg *Config, intent Intent) error {
	dir := cfg.Registration.UnitDir
	for _, name := range []string{ServiceName, TimerName} {
		p, err := r.Manager.Properties(ctx, name, "Unit")
		if err != nil {
			return registrationError("inspect", "INSPECTION_FAILED", err)
		}
		var fragment string
		if err := property(p, "FragmentPath", &fragment); err != nil {
			return err
		}
		var drops []string
		if err := property(p, "DropInPaths", &drops); err != nil {
			return err
		}
		conflict := func(field string, value interface{}) error {
			return registrationError("inspect", "REGISTRATION_OVERRIDDEN", fmt.Errorf("%s %s=%v (fragment %s, drop-ins %v)", name, field, value, fragment, drops))
		}
		if fragment != filepath.Join(dir, name) {
			return conflict("FragmentPath", fragment)
		}
		kind := "Service"
		if name == TimerName {
			kind = "Timer"
		}
		p, err = r.Manager.Properties(ctx, name, kind)
		if err != nil {
			return err
		}
		if name == ServiceName {
			var commands []ExecCommandEx
			if err := property(p, "ExecStartEx", &commands); err != nil {
				return err
			}
			expected := append([]string{intent.Executable}, intent.Args...)
			// no-env-expand is systemd's native colon prefix, which preserves literal dollars.
			if len(commands) != 1 || commands[0].Path != intent.Executable || !reflect.DeepEqual(commands[0].Args, expected) || !reflect.DeepEqual(commands[0].Flags, []string{"no-env-expand"}) {
				return conflict("ExecStart", commands)
			}
			// systemd defines these additional executable hooks; none belongs to this service.
			for _, field := range []string{"ExecCondition", "ExecStartPre", "ExecStartPost", "ExecReload", "ExecStop", "ExecStopPost"} {
				var hooks []ExecCommand
				if err := property(p, field, &hooks); err != nil {
					return err
				}
				if len(hooks) > 0 {
					return conflict(field, hooks)
				}
			}
			for field, want := range map[string]string{"Type": "oneshot", "Restart": "no"} {
				var actual string
				if err := property(p, field, &actual); err != nil {
					return err
				}
				if actual != want {
					return conflict(field, actual)
				}
			}
			var remain bool
			if err := property(p, "RemainAfterExit", &remain); err != nil {
				return err
			}
			if remain {
				return conflict("RemainAfterExit", remain)
			}
		} else {
			var unit string
			var persistent bool
			var calendars []Calendar
			if err := property(p, "Unit", &unit); err != nil {
				return err
			}
			if unit != ServiceName {
				return conflict("Unit", unit)
			}
			if err := property(p, "Persistent", &persistent); err != nil {
				return err
			}
			if !persistent {
				return conflict("Persistent", persistent)
			}
			if err := property(p, "TimersCalendar", &calendars); err != nil {
				return err
			}
			if len(calendars) != 1 || calendars[0].Base != "OnCalendar" || calendars[0].Expression != calendar(intent.Interval, intent.Time) {
				return conflict("TimersCalendar", calendars)
			}
			// These systemd timer fields add triggers outside the owned calendar cadence.
			for _, field := range []string{"OnClockChange", "OnTimezoneChange"} {
				var extra bool
				if err := property(p, field, &extra); err != nil {
					return err
				}
				if extra {
					return conflict(field, extra)
				}
			}
			var monotonic []struct {
				Base        string
				Value, Next uint64
			}
			if err := property(p, "TimersMonotonic", &monotonic); err != nil {
				return err
			}
			if len(monotonic) > 0 {
				return conflict("TimersMonotonic", monotonic)
			}
		}
	}
	return nil
}
func calendar(interval, clock string) string {
	day := ""
	if interval == "weekly" {
		day = "Mon "
	}
	return day + "*-*-* " + clock + ":00"
}
func renderUnits(intent Intent) (map[string][]byte, error) {
	if !filepath.IsAbs(intent.Executable) || !filepath.IsAbs(intent.Root) {
		return nil, errors.New("executable and root must be absolute")
	}
	// systemd's executable parser rejects quotes, backslashes and control bytes,
	// even when escaped. Reject them before saving intent or touching native state.
	for _, c := range intent.Executable {
		if c < 32 || c == 127 || strings.ContainsRune("'\"\\", c) {
			return nil, errors.New("systemd executable path cannot contain quotes, backslashes or control characters")
		}
	}
	if intent.Interval != "daily" && intent.Interval != "weekly" {
		return nil, errors.New("interval must be daily or weekly")
	}
	parsed, err := time.Parse("15:04", intent.Time)
	if err != nil || parsed.Format("15:04") != intent.Time {
		return nil, errors.New("time must use HH:MM")
	}
	args := append([]string{intent.Executable}, intent.Args...)
	encoded := make([]string, len(args))
	for i, arg := range args {
		if strings.ContainsRune(arg, 0) {
			return nil, errors.New("argument contains NUL")
		}
		encoded[i] = systemdArgument(arg)
	}
	marker := fmt.Sprintf("# Endstate schedule v%d root-sha256=%s\n", unitFormatVersion, unitHash([]byte(intent.Root)))
	service := marker + "[Unit]\nDescription=Endstate scheduled drift check\n\n[Service]\nType=oneshot\nExecStart=:" + strings.Join(encoded, " ") + "\nRemainAfterExit=no\nRestart=no\n"
	timer := marker + "[Unit]\nDescription=Endstate drift check timer\n\n[Timer]\nOnCalendar=" + calendar(intent.Interval, intent.Time) + "\nPersistent=true\nUnit=" + ServiceName + "\n\n[Install]\nWantedBy=timers.target\n"
	return map[string][]byte{ServiceName: []byte(service), TimerName: []byte(timer)}, nil
}

// systemd.syntax quoting plus systemd.service specifier/environment escaping.
func systemdArgument(value string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, c := range []byte(value) {
		switch c {
		case '\\', '"':
			b.WriteByte('\\')
			b.WriteByte(c)
		case '%':
			b.WriteString("%%")
		case '$':
			b.WriteByte(c)
		default:
			if c < 32 || c == 127 {
				fmt.Fprintf(&b, "\\x%02x", c)
			} else {
				b.WriteByte(c)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}
