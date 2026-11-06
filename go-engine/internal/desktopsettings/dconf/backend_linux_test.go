// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package dconf_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/desktopsettings"
	"github.com/Artexis10/endstate/go-engine/internal/desktopsettings/dconf"
	"github.com/Artexis10/endstate/go-engine/internal/desktopsettings/dconf/dconftest"
)

var clockTarget = desktopsettings.Target{Schema: "org.gnome.desktop.interface", Path: "/org/gnome/desktop/interface/", Key: "clock-show-date", Type: "boolean"}

const clockKey = "/org/gnome/desktop/interface/clock-show-date"

func boolean(present bool, value bool) desktopsettings.Value {
	v := desktopsettings.Value{Type: "boolean", Present: present}
	if present {
		v.Value = &value
	}
	return v
}

func read(s *dconftest.Session) (desktopsettings.Observation, error) {
	return dconf.New(s.Host()).Read(context.Background(), clockTarget)
}

func write(s *dconftest.Session, scope string, value desktopsettings.Value) error {
	return dconf.New(s.Host()).Write(context.Background(), clockTarget, scope, value)
}

// refused fails unless err is the scoped unavailable refusal with this reason
// and the fake session saw no command outside the fixed argument vectors.
func refused(t *testing.T, s *dconftest.Session, err error, reason dconf.Reason) {
	t.Helper()
	var refusal *dconf.Error
	if !errors.As(err, &refusal) || refusal.Reason != reason || !errors.Is(err, desktopsettings.ErrUnavailable) {
		t.Fatalf("want unavailable %s, got %v", reason, err)
	}
	if len(s.Rejected) != 0 {
		t.Fatalf("unexpected commands: %q", s.Rejected)
	}
}

// refusesReadAndWrite proves a refusal blocks inspection and also every mutation.
func refusesReadAndWrite(t *testing.T, s *dconftest.Session, scope string, reason dconf.Reason) {
	t.Helper()
	_, err := read(s)
	refused(t, s, err, reason)
	refused(t, s, write(s, scope, boolean(true, false)), reason)
	if s.Writes != 0 {
		t.Fatalf("refused session still wrote %d times", s.Writes)
	}
}

// An empty read is inheritance only when the user database is missing or sound,
// and an explicit false must stay distinct from it.
func TestGSettingsUserOverrideVersusDefault(t *testing.T) {
	s := dconftest.New(t)
	absentWithoutDatabase, err := read(s)
	if err != nil || absentWithoutDatabase.User.Present {
		t.Fatalf("missing database: %+v %v", absentWithoutDatabase, err)
	}
	s.WriteDatabase(dconftest.ValidHeader())
	for _, test := range []struct {
		stored string
		want   desktopsettings.Value
	}{
		{"", boolean(false, false)},
		{"false", boolean(true, false)},
		{"true", boolean(true, true)},
	} {
		delete(s.Values, clockKey)
		if test.stored != "" {
			s.Values[clockKey] = test.stored
		}
		observed, err := read(s)
		if err != nil || !observed.User.Equal(test.want) || observed.Scope != absentWithoutDatabase.Scope {
			t.Fatalf("stored %q: %+v %v", test.stored, observed, err)
		}
	}
}

// Absence must reset the override: writing false would turn inheritance into a pin.
func TestGSettingsRevertRestoresAbsence(t *testing.T) {
	s := dconftest.New(t)
	scope := mustScope(t, s)
	if err := write(s, scope, boolean(true, false)); err != nil || s.Values[clockKey] != "false" {
		t.Fatalf("write false: %v %v", err, s.Values)
	}
	if err := write(s, scope, boolean(false, false)); err != nil {
		t.Fatal(err)
	}
	if _, stillSet := s.Values[clockKey]; stillSet || s.Writes != 2 {
		t.Fatalf("absence left the override: %v writes=%d", s.Values, s.Writes)
	}
	if err := write(s, "another-scope", boolean(true, true)); !errors.Is(err, desktopsettings.ErrConflict) || s.Writes != 2 {
		t.Fatalf("write under a foreign scope: %v writes=%d", err, s.Writes)
	}
}

func mustScope(t *testing.T, s *dconftest.Session) string {
	t.Helper()
	observed, err := read(s)
	if err != nil {
		t.Fatal(err)
	}
	return observed.Scope
}

// A raw value of another type must never be read as absence, or apply would overwrite it.
func TestDconfReportsStoredValueOfAnotherTypeAsUnsupported(t *testing.T) {
	s := dconftest.New(t)
	s.Values[clockKey] = "'yes'"
	_, err := read(s)
	var refusal *dconf.Error
	if !errors.Is(err, desktopsettings.ErrUnsupported) || !errors.As(err, &refusal) || refusal.Reason != dconf.ReasonValueUnsupported || errors.Is(err, desktopsettings.ErrUnavailable) {
		t.Fatalf("want unsupported value, got %v", err)
	}
}

// The dconf writer renames a rejected database and replaces a symbolic link, so
// both must stop a write before it runs. A byte-swapped signature is accepted.
func TestDconfChecksDatabaseHeaderBeforeTrustingAnEmptyRead(t *testing.T) {
	header := func(signature string, version byte) []byte {
		return append(append([]byte(signature), version, 0, 0, 0), make([]byte, 12)...)
	}
	for _, test := range []struct {
		name    string
		content []byte
		ok      bool
	}{
		{"valid", header("GVariant", 0), true},
		{"valid byte swapped", header("raVGtnai", 0), true},
		{"short", header("GVariant", 0)[:23], false},
		{"unknown signature", header("NotGvdb!", 0), false},
		{"nonzero version", header("GVariant", 1), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			s := dconftest.New(t)
			scope := mustScope(t, s)
			s.WriteDatabase(test.content)
			if test.ok {
				if _, err := read(s); err != nil {
					t.Fatal(err)
				}
				return
			}
			refusesReadAndWrite(t, s, scope, dconf.ReasonDatabaseUnavailable)
		})
	}
	t.Run("symbolic link", func(t *testing.T) {
		s := dconftest.New(t)
		scope := mustScope(t, s)
		real := filepath.Join(t.TempDir(), "managed-user-db")
		if err := os.WriteFile(real, dconftest.ValidHeader(), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(s.Database()), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(real, s.Database()); err != nil {
			t.Skipf("symbolic links unavailable: %v", err)
		}
		refusesReadAndWrite(t, s, scope, dconf.ReasonDatabaseUnavailable)
	})
	t.Run("unreadable", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("root reads any file")
		}
		s := dconftest.New(t)
		scope := mustScope(t, s)
		s.WriteDatabase(dconftest.ValidHeader())
		if err := os.Chmod(s.Database(), 0); err != nil {
			t.Fatal(err)
		}
		refusesReadAndWrite(t, s, scope, dconf.ReasonDatabaseUnavailable)
	})
}

// Validation mode moves XDG_CONFIG_HOME but leaves the session bus, so the
// session's writer would change the operator's real database.
func TestDconfRefusesWriterThatResolvesAnotherConfigDirectory(t *testing.T) {
	s := dconftest.New(t)
	scope := mustScope(t, s)
	sandbox := filepath.Join(t.TempDir(), "sandbox-xdg-config")
	for _, dir := range []string{sandbox, filepath.Join(s.Home, ".config")} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	s.Reader["XDG_CONFIG_HOME"] = sandbox
	refusesReadAndWrite(t, s, scope, dconf.ReasonScopeUnproven)

	// A terminal that spells the default out is the same directory, not a mismatch.
	s.Reader["XDG_CONFIG_HOME"] = filepath.Join(s.Home, ".config") + "/"
	if _, err := read(s); err != nil {
		t.Fatalf("same directory spelled differently: %v", err)
	}
}

// Each row is one way the writer's identity or environment cannot be proven.
func TestDconfRefusesWhenWriterScopeCannotBeProven(t *testing.T) {
	other := uint32(1)
	for _, test := range []struct {
		name   string
		change func(*dconftest.Session)
	}{
		{"writer runs as another user", func(s *dconftest.Session) { s.WriterUID = 1000 }},
		{"process belongs to another user than the bus reported", func(s *dconftest.Session) { s.ProcessUID = &other }},
		{"environment truncated at the read bound", func(s *dconftest.Session) { s.TruncatedEnviron = true }},
		{"process unreadable", func(s *dconftest.Session) { s.ProcessErr = os.ErrPermission }},
		{"writer replaced while inspected", func(s *dconftest.Session) {
			inspections := 0
			s.OnProcess = func() { inspections++; s.Owner = fmt.Sprintf(":1.%d", 100+inspections) }
		}},
		{"relative XDG_CONFIG_HOME here", func(s *dconftest.Session) { s.Reader["XDG_CONFIG_HOME"] = "relative/config" }},
		{"relative XDG_CONFIG_HOME in the writer", func(s *dconftest.Session) { s.Writer["XDG_CONFIG_HOME"] = "relative/config" }},
		{"HOME unset here", func(s *dconftest.Session) { delete(s.Reader, "HOME") }},
		{"HOME unset in the writer", func(s *dconftest.Session) { delete(s.Writer, "HOME") }},
	} {
		t.Run(test.name, func(t *testing.T) {
			s := dconftest.New(t)
			scope := mustScope(t, s)
			test.change(s)
			refusesReadAndWrite(t, s, scope, dconf.ReasonScopeUnproven)
		})
	}
}

// A missing dependency or session scopes only the desktop lane and says how to fix it.
func TestDconfReportsMissingRuntimeWithRemediation(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*dconftest.Session)
	}{
		{"no dconf command", func(s *dconftest.Session) { s.MissingCommand["dconf"] = true }},
		{"no gsettings command", func(s *dconftest.Session) { s.MissingCommand["gsettings"] = true }},
		{"no session bus address", func(s *dconftest.Session) {
			delete(s.Reader, "DBUS_SESSION_BUS_ADDRESS")
			delete(s.Reader, "XDG_RUNTIME_DIR")
		}},
		{"bus refuses connection", func(s *dconftest.Session) { s.NoBus = true }},
		{"no dconf service to activate", func(s *dconftest.Session) { s.NoService = true }},
		{"writer without a runtime directory", func(s *dconftest.Session) { delete(s.Writer, "XDG_RUNTIME_DIR") }},
	} {
		t.Run(test.name, func(t *testing.T) {
			s := dconftest.New(t)
			test.change(s)
			_, err := read(s)
			var refusal *dconf.Error
			if !errors.As(err, &refusal) || !errors.Is(err, desktopsettings.ErrUnavailable) || refusal.Reason != dconf.ReasonRuntimeUnavailable {
				t.Fatalf("want scoped runtime refusal, got %v", err)
			}
			if refusal.Remediation == "" {
				t.Fatalf("no remediation: %v", err)
			}
		})
	}
}

// Reads are exact only under the fallback profile, so any other source stops the lane.
func TestDconfRefusesEveryProfileSource(t *testing.T) {
	touch := func(path string) {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("user-db:user\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, test := range []struct {
		name   string
		change func(*dconftest.Session)
		reason dconf.Reason
	}{
		{"system profile", func(s *dconftest.Session) { touch(s.SystemProfile) }, dconf.ReasonProfileUnsupported},
		{"per-user runtime profile", func(s *dconftest.Session) { touch(filepath.Join(s.RuntimeProfileDir, "4242")) }, dconf.ReasonProfileUnsupported},
		{"profile in the runtime directory", func(s *dconftest.Session) { touch(filepath.Join(s.Runtime, "dconf", "profile")) }, dconf.ReasonProfileUnsupported},
		{"profile named by this process", func(s *dconftest.Session) { s.Reader["DCONF_PROFILE"] = "work" }, dconf.ReasonProfileUnsupported},
		{"profile named by the writer", func(s *dconftest.Session) { s.Writer["DCONF_PROFILE"] = "work" }, dconf.ReasonProfileUnsupported},
		{"profile in the writer's data directory", func(s *dconftest.Session) {
			dir := filepath.Join(t.TempDir(), "share")
			touch(filepath.Join(dir, "dconf", "profile", "user"))
			s.Writer["XDG_DATA_DIRS"] = dir + ":/usr/share"
		}, dconf.ReasonProfileUnsupported},
		{"relative data directory", func(s *dconftest.Session) { s.Writer["XDG_DATA_DIRS"] = "share:/usr/share" }, dconf.ReasonScopeUnproven},
		{"default value reported by dconf", func(s *dconftest.Session) { s.Defaults[clockKey] = "true" }, dconf.ReasonProfileUnsupported},
		{"locks reported by dconf", func(s *dconftest.Session) {
			s.Locks["/org/gnome/desktop/interface/"] = "/org/gnome/desktop/interface/clock-show-date\n"
		}, dconf.ReasonProfileUnsupported},
	} {
		t.Run(test.name, func(t *testing.T) {
			s := dconftest.New(t)
			scope := mustScope(t, s)
			test.change(s)
			refusesReadAndWrite(t, s, scope, test.reason)
		})
	}
}

// The reviewed key must exist as a boolean at the declared path in the installed schemas.
func TestDconfRefusesSchemaThatDoesNotDescribeTheKey(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*dconftest.Session)
	}{
		{"schema not installed", func(s *dconftest.Session) { delete(s.KeyTypes, "org.gnome.desktop.interface clock-show-date") }},
		{"key is not boolean", func(s *dconftest.Session) { s.KeyTypes["org.gnome.desktop.interface clock-show-date"] = "s" }},
		{"schema lives at another path", func(s *dconftest.Session) { s.Schemas["org.gnome.desktop.interface"] = "/org/example/interface/" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			s := dconftest.New(t)
			scope := mustScope(t, s)
			test.change(s)
			refusesReadAndWrite(t, s, scope, dconf.ReasonSchemaUnavailable)
		})
	}
}

// An AppImage-style environment must not change what the commands report.
func TestDconfPinsChildEnvironmentToTheSessionsView(t *testing.T) {
	s := dconftest.New(t)
	sessionDirs := "/usr/share"
	s.SchemaDataDirs = &sessionDirs
	s.Writer["XDG_DATA_DIRS"] = sessionDirs
	s.Reader["XDG_DATA_DIRS"] = "/tmp/.mount_endstate/usr/share:/usr/share"
	s.Reader["G_MESSAGES_DEBUG"] = "all"
	s.Reader["GSETTINGS_SCHEMA_DIR"] = "/tmp/.mount_endstate/schemas"
	s.Values[clockKey] = "true"
	observed, err := read(s)
	if err != nil || !observed.User.Equal(boolean(true, true)) {
		t.Fatalf("ambient GLib settings leaked into the commands: %+v %v", observed, err)
	}
}

// A failed command and a replaced writer both fail the write; the accepted
// executor then rolls back through a fresh classification.
func TestDconfWriteFailsWhenCommandFailsOrWriterChanges(t *testing.T) {
	s := dconftest.New(t)
	scope := mustScope(t, s)
	s.FailMutation = true
	if err := write(s, scope, boolean(true, true)); err == nil || len(s.Values) != 0 {
		t.Fatalf("failed command reported success: %v %v", err, s.Values)
	}
	s.FailMutation = false
	s.OnMutate = func() { s.Owner = ":1.99" }
	if err := write(s, scope, boolean(true, true)); err == nil {
		t.Fatal("write passed although the dconf writer changed during it")
	}
}
