// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

// Package dconftest is a fake desktop session for tests of the dconf backend.
// It models only what the backend relies on: the three dconf and two gsettings
// argument vectors, the session bus writer, and the writer's environment.
// Filesystem checks use real temporary directories.
package dconftest

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/desktopsettings/dconf"
)

// ValidHeader is a 24-byte gvdb header that the dconf writer accepts.
func ValidHeader() []byte { return append([]byte("GVariant"), make([]byte, 16)...) }

// Session is one fake desktop session. Fields are public so a test states its
// scenario directly; a zero value of a failure switch means "healthy".
type Session struct {
	t testing.TB

	UID, WriterUID, WriterPID uint32
	Owner                     string

	// Home and Runtime are real directories. The reader and writer environments
	// start as the same HOME and XDG_RUNTIME_DIR; tests edit the maps to diverge.
	// DataDir is their only XDG_DATA_DIRS entry, so no test reads the host's own
	// dconf profile directories.
	Home, Runtime, DataDir           string
	Reader, Writer                   map[string]string
	RuntimeProfileDir, SystemProfile string

	// Values holds each user override as dconf prints it ("true", "'yes'").
	// Defaults and Locks are what a profile would add to `read -d` and `list-locks`.
	Values, Defaults, Locks map[string]string
	// Schemas maps a schema ID to its path; KeyTypes maps "schema key" to the
	// gsettings type letter. A schema or key that is absent is not installed.
	Schemas, KeyTypes map[string]string
	// SchemaDataDirs, when set, is the only XDG_DATA_DIRS under which gsettings
	// finds the schemas, as when a bundled directory shadows the system's.
	SchemaDataDirs *string

	MissingCommand      map[string]bool
	NoBus, NoService    bool
	ProcessErr          error
	TruncatedEnviron    bool
	ProcessUID          *uint32
	FailMutation        bool
	OnProcess, OnMutate func()

	// Writes counts dconf write and reset commands that succeeded. Rejected
	// holds every argument vector outside the fixed shapes.
	Writes   int
	Rejected [][]string
}

// New returns a healthy session for the GNOME clock key, with no database file.
func New(t testing.TB) *Session {
	t.Helper()
	base := t.TempDir()
	s := &Session{
		t: t, UID: 4242, WriterUID: 4242, WriterPID: 777, Owner: ":1.42",
		Home: filepath.Join(base, "home"), Runtime: filepath.Join(base, "run"), DataDir: filepath.Join(base, "share"),
		RuntimeProfileDir: filepath.Join(base, "run-dconf-user"), SystemProfile: filepath.Join(base, "etc-dconf-profile-user"),
		Values: map[string]string{}, Defaults: map[string]string{}, Locks: map[string]string{},
		Schemas:        map[string]string{"org.gnome.desktop.interface": "/org/gnome/desktop/interface/"},
		KeyTypes:       map[string]string{"org.gnome.desktop.interface clock-show-date": "b"},
		MissingCommand: map[string]bool{},
	}
	for _, dir := range []string{s.Home, s.Runtime, s.DataDir} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	s.Reader = map[string]string{"HOME": s.Home, "XDG_RUNTIME_DIR": s.Runtime, "XDG_DATA_DIRS": s.DataDir, "DBUS_SESSION_BUS_ADDRESS": s.BusAddress(), "PATH": "/usr/bin"}
	s.Writer = map[string]string{"HOME": s.Home, "XDG_RUNTIME_DIR": s.Runtime, "XDG_DATA_DIRS": s.DataDir}
	return s
}

func (s *Session) BusAddress() string { return "unix:path=" + filepath.Join(s.Runtime, "bus") }

// Database is where the reader's environment keeps the user database.
func (s *Session) Database() string { return databaseOf(s.Reader) }

func databaseOf(env map[string]string) string {
	root := env["XDG_CONFIG_HOME"]
	if root == "" {
		root = filepath.Join(env["HOME"], ".config")
	}
	return filepath.Join(root, "dconf", "user")
}

// WriteDatabase replaces the reader's user database file with content.
func (s *Session) WriteDatabase(content []byte) {
	s.t.Helper()
	database := s.Database()
	if err := os.MkdirAll(filepath.Dir(database), 0o700); err != nil {
		s.t.Fatal(err)
	}
	if err := os.WriteFile(database, content, 0o600); err != nil {
		s.t.Fatal(err)
	}
}

// Host returns the injected ports for this session.
func (s *Session) Host() dconf.Host {
	return dconf.Host{
		UID:      int(s.UID),
		Environ:  func() []string { return list(s.Reader) },
		LookPath: s.lookPath,
		Run:      s.run,
		Connect:  s.connect,
		Process:  s.process,

		RuntimeProfileDir: s.RuntimeProfileDir, SystemProfile: s.SystemProfile,
	}
}

func list(env map[string]string) []string {
	entries := make([]string, 0, len(env))
	for key, value := range env {
		entries = append(entries, key+"="+value)
	}
	sort.Strings(entries)
	return entries
}

func (s *Session) lookPath(name string) (string, error) {
	if s.MissingCommand[name] {
		return "", fmt.Errorf("%s: not found in PATH", name)
	}
	return "/fake/bin/" + name, nil
}

func (s *Session) connect(_ context.Context, address string) (dconf.Conn, error) {
	if s.NoBus || address != s.BusAddress() {
		return nil, errors.New("connection refused")
	}
	return connection{s}, nil
}

func (s *Session) process(pid uint32) (dconf.Process, error) {
	if s.OnProcess != nil {
		s.OnProcess()
	}
	if s.ProcessErr != nil || pid != s.WriterPID {
		return dconf.Process{}, errors.Join(errors.New("cannot read process"), s.ProcessErr)
	}
	uid := s.WriterUID
	if s.ProcessUID != nil {
		uid = *s.ProcessUID
	}
	return dconf.Process{UID: uid, Environ: []byte(strings.Join(list(s.Writer), "\x00") + "\x00"), Truncated: s.TruncatedEnviron}, nil
}

type connection struct{ s *Session }

func (connection) Close() error { return nil }
func (c connection) StartServiceByName(context.Context, string) error {
	if c.s.NoService {
		return errors.New("ServiceUnknown")
	}
	return nil
}
func (c connection) NameOwner(context.Context, string) (string, error) {
	if c.s.Owner == "" {
		return "", errors.New("NameHasNoOwner")
	}
	return c.s.Owner, nil
}
func (c connection) Credentials(context.Context, string) (dconf.Credentials, error) {
	return dconf.Credentials{UID: c.s.WriterUID, PID: c.s.WriterPID}, nil
}

// run accepts only the fixed argument vectors and records any other one.
func (s *Session) run(_ context.Context, argv, env []string) (dconf.Output, error) {
	child := map[string]string{}
	for _, entry := range env {
		if key, value, ok := strings.Cut(entry, "="); ok {
			child[key] = value
		}
	}
	// GLib writes debug and info messages to standard output when asked to.
	noise := ""
	if _, debug := child["G_MESSAGES_DEBUG"]; debug {
		noise = "(process:1): GLib-DEBUG: noise\n"
	}
	ok := func(text string) (dconf.Output, error) { return dconf.Output{Stdout: []byte(noise + text)}, nil }
	failed := func(text string) (dconf.Output, error) {
		return dconf.Output{ExitCode: 1, Stderr: text}, nil
	}
	args := argv[1:]
	switch filepath.Base(argv[0]) {
	case "dconf":
		switch {
		case len(args) == 2 && args[0] == "read":
			return ok(line(s.Values, args[1]))
		case len(args) == 3 && args[0] == "read" && args[1] == "-d":
			return ok(line(s.Defaults, args[2]))
		case len(args) == 2 && args[0] == "list-locks":
			return ok(s.Locks[args[1]])
		case len(args) == 3 && args[0] == "write" && (args[2] == "true" || args[2] == "false"):
			return s.mutate(child, args[1], args[2], failed)
		case len(args) == 2 && args[0] == "reset":
			return s.mutate(child, args[1], "", failed)
		}
	case "gsettings":
		visible := child["GSETTINGS_SCHEMA_DIR"] == "" && (s.SchemaDataDirs == nil || child["XDG_DATA_DIRS"] == *s.SchemaDataDirs)
		switch {
		case len(args) == 3 && args[0] == "range":
			kind, installed := s.KeyTypes[args[1]+" "+args[2]]
			if !visible || !installed {
				return failed("No such schema or key")
			}
			return ok("type " + kind + "\n")
		case len(args) == 2 && args[0] == "list-schemas" && args[1] == "--print-paths":
			var out strings.Builder
			for _, schema := range sortedKeys(s.Schemas) {
				if visible {
					out.WriteString(schema + " " + s.Schemas[schema] + "\n")
				}
			}
			return ok(out.String())
		}
	}
	s.Rejected = append(s.Rejected, append([]string(nil), argv...))
	return dconf.Output{}, fmt.Errorf("fake session: unexpected argument vector %q", argv)
}

func line(values map[string]string, key string) string {
	if value, found := values[key]; found {
		return value + "\n"
	}
	return ""
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// mutate applies a write (text non-empty) or reset to the session's store, and
// leaves a valid database file behind as the writer does.
func (s *Session) mutate(child map[string]string, key, text string, failed func(string) (dconf.Output, error)) (dconf.Output, error) {
	if child["DBUS_SESSION_BUS_ADDRESS"] != s.BusAddress() || s.FailMutation {
		return failed("cannot reach the dconf writer")
	}
	if s.OnMutate != nil {
		s.OnMutate()
	}
	if text == "" {
		delete(s.Values, key)
	} else {
		s.Values[key] = text
	}
	s.Writes++
	database := databaseOf(s.Writer)
	if _, err := os.Lstat(database); errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(filepath.Dir(database), 0o700); err != nil {
			s.t.Fatal(err)
		}
		if err := os.WriteFile(database, ValidHeader(), 0o600); err != nil {
			s.t.Fatal(err)
		}
	}
	return dconf.Output{}, nil
}
