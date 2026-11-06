// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package dconf

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/config"
	"github.com/Artexis10/endstate/go-engine/internal/desktopsettings"
	"github.com/Artexis10/endstate/go-engine/internal/sessionbus"
)

// session is one proven view of the desktop: the bus connection, the writer
// that owns ca.desrt.dconf, and the pinned environment for the child commands.
// Every Read and Write opens its own, so nothing is trusted across calls.
type session struct {
	host             Host
	target           desktopsettings.Target
	dconf, gsettings string
	conn             Conn
	owner            string
	scope            string
	reader, writer   namespace
	env              []string
}

func (s *session) close() { _ = s.conn.Close() }

// key is the dconf key path; Target.Validate fixes Path as a "/"-terminated directory.
func (s *session) key() string { return s.target.Path + s.target.Key }

func (s *session) run(ctx context.Context, argv []string) (Output, error) {
	return s.host.Run(ctx, argv, s.env)
}

// open resolves the command paths and session bus, then proves that the
// session's dconf writer is this user's and resolves this process's database.
func (b *Backend) open(ctx context.Context, target desktopsettings.Target) (_ *session, err error) {
	if err := target.Validate(); err != nil {
		return nil, err
	}
	// A leading dash would make a positional argument a command option.
	if strings.HasPrefix(target.Schema, "-") || strings.HasPrefix(target.Key, "-") {
		return nil, fmt.Errorf("invalid boolean GSettings target")
	}
	h := b.host
	s := &session{host: h, target: target}
	if s.dconf, err = h.LookPath(dconfCommand); err != nil {
		return nil, missingCommand(dconfCommand, err)
	}
	if s.gsettings, err = h.LookPath(gsettingsCommand); err != nil {
		return nil, missingCommand(gsettingsCommand, err)
	}
	environ := h.Environ()
	if s.reader, err = resolveNamespace(parseEnvironment(environ), "this process"); err != nil {
		return nil, err
	}
	address, busErr := sessionbus.Address(environ)
	if busErr != nil {
		return nil, refuse(ReasonRuntimeUnavailable, "no existing user session bus").because(busErr).fix("run from a logged-in desktop session")
	}
	if s.conn, err = h.Connect(ctx, address); err != nil {
		return nil, refuse(ReasonRuntimeUnavailable, "cannot connect to the user session bus").because(err).fix("run from a logged-in desktop session")
	}
	defer func() {
		if err != nil {
			s.close()
		}
	}()
	if err = s.proveWriter(ctx); err != nil {
		return nil, err
	}
	s.scope = fmt.Sprintf("dconf:user-db:user;uid=%d;db=%s", h.UID, s.reader.database)
	s.env = s.childEnvironment(environ, address)
	return s, nil
}

// proveWriter starts the writer by name, then requires that its user and
// resolved configuration directory equal this process's. dconf offers no way
// to ask the writer for its directory, so its initial environment stands in.
func (s *session) proveWriter(ctx context.Context) error {
	if err := s.conn.StartServiceByName(ctx, writerName); err != nil {
		return refuse(ReasonRuntimeUnavailable, "the dconf writer is not available on the session bus").because(err).fix("install the dconf service and run from a desktop session")
	}
	owner, err := s.conn.NameOwner(ctx, writerName)
	if err != nil {
		return refuse(ReasonRuntimeUnavailable, "the dconf writer has no owner on the session bus").because(err)
	}
	credentials, err := s.conn.Credentials(ctx, owner)
	if err != nil {
		return refuse(ReasonScopeUnproven, "the bus did not identify the dconf writer").because(err)
	}
	if int64(credentials.UID) != int64(s.host.UID) {
		return refuse(ReasonScopeUnproven, "the dconf writer runs as another user")
	}
	process, err := s.host.Process(credentials.PID)
	if err != nil {
		return refuse(ReasonScopeUnproven, "cannot read the dconf writer's process").because(err)
	}
	// A truncated environment may hide the variable that moves its directory.
	if process.Truncated || process.UID != credentials.UID {
		return refuse(ReasonScopeUnproven, "the dconf writer's environment is incomplete or belongs to another process")
	}
	// The process ID may have been reused while its environment was read.
	if again, err := s.conn.NameOwner(ctx, writerName); err != nil || again != owner {
		return refuse(ReasonScopeUnproven, "the dconf writer changed while it was inspected")
	}
	if s.writer, err = resolveNamespace(parseEnvironment(strings.Split(string(process.Environ), "\x00")), "the dconf writer"); err != nil {
		return err
	}
	if !sameDirectory(s.reader.configRoot, s.writer.configRoot) {
		return refuse(ReasonScopeUnproven, fmt.Sprintf("the dconf writer uses %s but this process uses %s", s.writer.configRoot, s.reader.configRoot)).
			fix("run the command from the desktop session's own environment")
	}
	s.owner = owner
	return nil
}

// childEnvironment pins what the commands resolve their database, schemas, and
// bus from. The writer's XDG_DATA_DIRS keeps a bundled schema from answering
// for the session's, and the cleared variables are the GLib settings that
// change a command's output or backend.
func (s *session) childEnvironment(environ []string, address string) []string {
	pinned := map[string]bool{
		"DBUS_SESSION_BUS_ADDRESS": true, "HOME": true, "XDG_CONFIG_HOME": true, "XDG_RUNTIME_DIR": true, "XDG_DATA_DIRS": true,
		"G_MESSAGES_DEBUG": true, "GSETTINGS_SCHEMA_DIR": true, "GSETTINGS_BACKEND": true, "GIO_EXTRA_MODULES": true,
	}
	var env []string
	for _, entry := range environ {
		if key, _, ok := strings.Cut(entry, "="); !ok || !pinned[key] {
			env = append(env, entry)
		}
	}
	env = append(env, "DBUS_SESSION_BUS_ADDRESS="+address, "HOME="+s.reader.home, "XDG_CONFIG_HOME="+s.reader.configRoot, "XDG_RUNTIME_DIR="+s.reader.runtimeDir)
	if dirs := s.writer.env["XDG_DATA_DIRS"]; dirs != "" {
		env = append(env, "XDG_DATA_DIRS="+dirs)
	}
	return env
}

// prove checks everything that makes an empty `dconf read` mean inheritance.
func (s *session) prove(ctx context.Context) error {
	if err := s.proveProfileAbsent(); err != nil {
		return err
	}
	if err := checkDatabase(s.reader.database); err != nil {
		return err
	}
	if err := s.proveSchema(ctx); err != nil {
		return err
	}
	// Under the fallback profile the user database is the only source, so the
	// default value and the locks are always empty. Output means a profile that
	// the candidate paths above did not reveal.
	for _, argv := range [][]string{{s.dconf, "read", "-d", s.key()}, {s.dconf, "list-locks", s.target.Path}} {
		out, err := s.run(ctx, argv)
		if err != nil || out.ExitCode != 0 {
			return commandFailed(argv[1], out, err)
		}
		if len(out.Stdout) != 0 {
			return refuse(ReasonProfileUnsupported, "dconf reports a default value or locks, so the user database is not the only source")
		}
	}
	return nil
}

// proveProfileAbsent requires that no dconf profile override exists for the
// reader or the writer. The writer ignores profiles; its environment stands in
// for the session clients that read through one.
func (s *session) proveProfileAbsent() error {
	candidates := []string{filepath.Join(s.host.RuntimeProfileDir, strconv.Itoa(s.host.UID)), s.host.SystemProfile}
	for _, ns := range []namespace{s.reader, s.writer} {
		if _, set := ns.env["DCONF_PROFILE"]; set {
			return refuse(ReasonProfileUnsupported, "DCONF_PROFILE is set for "+ns.who)
		}
		more, err := ns.profileCandidates()
		if err != nil {
			return err
		}
		candidates = append(candidates, more...)
	}
	for _, candidate := range candidates {
		_, err := os.Lstat(candidate)
		if err == nil {
			return refuse(ReasonProfileUnsupported, "a dconf profile exists at "+candidate).fix("only the default user profile is supported")
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return refuse(ReasonScopeUnproven, "cannot rule out a dconf profile at "+candidate).because(err)
		}
	}
	return nil
}

func (s *session) proveSchema(ctx context.Context) error {
	out, err := s.run(ctx, []string{s.gsettings, "range", s.target.Schema, s.target.Key})
	if err != nil {
		return commandFailed("range", out, err)
	}
	if out.ExitCode != 0 || string(out.Stdout) != "type b\n" {
		return refuse(ReasonSchemaUnavailable, fmt.Sprintf("%s %s is not an installed boolean key", s.target.Schema, s.target.Key))
	}
	out, err = s.run(ctx, []string{s.gsettings, "list-schemas", "--print-paths"})
	if err != nil || out.ExitCode != 0 {
		return commandFailed("list-schemas", out, err)
	}
	for _, line := range strings.Split(string(out.Stdout), "\n") {
		if line == s.target.Schema+" "+s.target.Path {
			return nil
		}
	}
	return refuse(ReasonSchemaUnavailable, fmt.Sprintf("%s is not installed at %s", s.target.Schema, s.target.Path))
}

// readValue accepts exactly the three outputs a boolean key can produce.
// Anything else is a raw value of another type and never becomes absence.
func (s *session) readValue(ctx context.Context) (desktopsettings.Value, error) {
	argv := []string{s.dconf, "read", s.key()}
	out, err := s.run(ctx, argv)
	if err != nil || out.ExitCode != 0 {
		return desktopsettings.Value{}, commandFailed("read", out, err)
	}
	switch string(out.Stdout) {
	case "":
		return desktopsettings.Value{Type: "boolean"}, nil
	case "true\n", "false\n":
		value := string(out.Stdout) == "true\n"
		return desktopsettings.Value{Type: "boolean", Present: true, Value: &value}, nil
	}
	return desktopsettings.Value{}, refuse(ReasonValueUnsupported, fmt.Sprintf("%s %s holds a value that is not a boolean", s.target.Schema, s.target.Key))
}

func missingCommand(name string, err error) *Error {
	return refuse(ReasonRuntimeUnavailable, "the "+name+" command is not installed").because(err).fix("install the dconf command line tools and GLib's gsettings")
}

func commandFailed(subcommand string, out Output, err error) *Error {
	if err != nil {
		return refuse(ReasonRuntimeUnavailable, "cannot run "+subcommand).because(err)
	}
	return refuse(ReasonRuntimeUnavailable, fmt.Sprintf("%s failed with exit code %d: %s", subcommand, out.ExitCode, out.Stderr))
}

// checkDatabase gates every empty read. Only a missing database means absence.
// The writer renames a database whose first 24 bytes it rejects and continues
// with an empty one, and it replaces a symbolic link instead of writing through
// it, so Endstate refuses both before any command runs. Tables behind an
// accepted header are not parsed: dconf and its session clients share one reader.
func checkDatabase(database string) error {
	info, err := os.Lstat(database)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return refuse(ReasonDatabaseUnavailable, "cannot inspect the dconf user database").because(err)
	}
	if !info.Mode().IsRegular() {
		return refuse(ReasonDatabaseUnavailable, "the dconf user database is not a regular file")
	}
	file, err := os.Open(database)
	if err != nil {
		return refuse(ReasonDatabaseUnavailable, "cannot read the dconf user database").because(err)
	}
	defer file.Close()
	if opened, err := file.Stat(); err != nil || !os.SameFile(info, opened) {
		return refuse(ReasonDatabaseUnavailable, "the dconf user database changed while it was opened")
	}
	// gvdb header: 8 signature bytes (either byte order), then a zero version word.
	var header [24]byte
	if _, err := io.ReadFull(file, header[:]); err != nil {
		return refuse(ReasonDatabaseUnavailable, "the dconf user database header is unreadable or short").because(err)
	}
	signature := string(header[:8])
	if (signature != "GVariant" && signature != "raVGtnai") || !bytes.Equal(header[8:12], []byte{0, 0, 0, 0}) {
		return refuse(ReasonDatabaseUnavailable, "the dconf writer would reject the user database header")
	}
	return nil
}

// sameDirectory compares two directories by identity. Two missing directories
// are the same only when their cleaned paths are, because no identity exists yet.
func sameDirectory(a, b string) bool {
	left, leftErr := os.Stat(a)
	right, rightErr := os.Stat(b)
	switch {
	case leftErr == nil && rightErr == nil:
		return os.SameFile(left, right)
	case errors.Is(leftErr, fs.ErrNotExist) && errors.Is(rightErr, fs.ErrNotExist):
		return filepath.Clean(a) == filepath.Clean(b)
	}
	return false
}

// namespace is one environment's resolved view of the paths that decide which
// dconf database and profile it uses. Paths follow GLib: values are absolute.
type namespace struct {
	who                          string
	env                          map[string]string
	home, configRoot, runtimeDir string
	database                     string
}

func parseEnvironment(entries []string) map[string]string {
	env := map[string]string{}
	for _, entry := range entries {
		// The first entry wins, as getenv does.
		if key, value, ok := strings.Cut(entry, "="); ok {
			if _, seen := env[key]; !seen {
				env[key] = value
			}
		}
	}
	return env
}

// resolveNamespace applies the engine's ${xdg.config} rule. GLib differs from
// it for a relative XDG_CONFIG_HOME (GLib uses it, the rule ignores it) and for
// an unset HOME (GLib asks the password database), so both are refused.
func resolveNamespace(env map[string]string, who string) (namespace, error) {
	ns := namespace{who: who, env: env, home: env["HOME"], runtimeDir: env["XDG_RUNTIME_DIR"]}
	if !path.IsAbs(ns.home) {
		return ns, refuse(ReasonScopeUnproven, "HOME is unset or relative for "+who)
	}
	if raw := env["XDG_CONFIG_HOME"]; raw != "" && !path.IsAbs(raw) {
		return ns, refuse(ReasonScopeUnproven, "XDG_CONFIG_HOME is relative for "+who)
	}
	if !path.IsAbs(ns.runtimeDir) {
		return ns, refuse(ReasonRuntimeUnavailable, "XDG_RUNTIME_DIR is unset or relative for "+who).fix("run from a logged-in desktop session")
	}
	environment := config.PathEnvironment{GOOS: "linux", Home: ns.home, Env: map[string]string{"XDG_CONFIG_HOME": env["XDG_CONFIG_HOME"]}}
	var err error
	if ns.configRoot, err = config.ResolveEnginePath("${xdg.config}", environment); err != nil {
		return ns, refuse(ReasonScopeUnproven, "cannot resolve the configuration directory for "+who).because(err)
	}
	if ns.database, err = config.ResolveEnginePath(desktopsettings.UserDatabaseCoordinate, environment); err != nil {
		return ns, refuse(ReasonScopeUnproven, "cannot resolve the dconf user database for "+who).because(err)
	}
	return ns, nil
}

// profileCandidates lists the profile files dconf may read in this environment.
func (ns namespace) profileCandidates() ([]string, error) {
	candidates := []string{filepath.Join(ns.runtimeDir, "dconf", "profile")}
	dataDirs := ns.env["XDG_DATA_DIRS"]
	if dataDirs == "" {
		dataDirs = "/usr/local/share:/usr/share"
	}
	for _, dir := range strings.Split(dataDirs, ":") {
		// GLib keeps an empty entry, which dconf then resolves against the working directory.
		if !path.IsAbs(dir) {
			return nil, refuse(ReasonScopeUnproven, "XDG_DATA_DIRS holds an empty or relative entry for "+ns.who).fix("remove it from XDG_DATA_DIRS")
		}
		candidates = append(candidates, filepath.Join(dir, "dconf", "profile", "user"))
	}
	return candidates, nil
}
