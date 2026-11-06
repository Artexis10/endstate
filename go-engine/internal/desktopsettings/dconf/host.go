// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package dconf

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/sessionbus"
	"github.com/godbus/dbus/v5"
)

// Host holds every effect and ambient fact the backend uses, so tests replace
// them without a desktop session.
type Host struct {
	// UID is this process's user ID.
	UID int
	// Environ returns this process's environment as KEY=VALUE entries.
	Environ func() []string
	// LookPath resolves one command name to the path that Run receives.
	LookPath func(name string) (string, error)
	// Run executes one fixed argument vector with exactly the given environment.
	// A non-zero exit is an Output, not an error.
	Run func(ctx context.Context, argv, env []string) (Output, error)
	// Connect dials an explicit session bus address; it never launches a bus.
	Connect func(ctx context.Context, address string) (Conn, error)
	// Process reads one process's effective user ID and initial environment.
	Process func(pid uint32) (Process, error)
	// RuntimeProfileDir holds per-user dconf profile overrides, one file per
	// user ID. SystemProfile is the system-wide "user" profile.
	RuntimeProfileDir, SystemProfile string
}

// Output is one finished command. Stderr is bounded and only explains failures.
type Output struct {
	Stdout   []byte
	Stderr   string
	ExitCode int
}

// Conn is the slice of the session bus that the writer proof needs.
type Conn interface {
	StartServiceByName(ctx context.Context, name string) error
	NameOwner(ctx context.Context, name string) (string, error)
	Credentials(ctx context.Context, owner string) (Credentials, error)
	Close() error
}

// Credentials are the bus-reported identity of a connection's process.
type Credentials struct{ UID, PID uint32 }

// Process is what /proc reports for one process. Truncated means Environ ended
// at the read bound, so a variable may be missing.
type Process struct {
	UID       uint32
	Environ   []byte
	Truncated bool
}

const (
	maxEnvironBytes = 1 << 20
	maxStdoutBytes  = 4 << 20
	maxStderrBytes  = 2 << 10
	commandTimeout  = 60 * time.Second
	// busTimeout bounds the bus handshake and each bus call, because capture
	// detection passes a context without a deadline. It exceeds dbus-daemon's
	// 25 s service-activation limit, which StartServiceByName can reach.
	busTimeout = 30 * time.Second
)

func systemHost() Host {
	return Host{
		UID:               os.Getuid(),
		Environ:           os.Environ,
		LookPath:          exec.LookPath,
		Run:               runCommand,
		Connect:           busConnector(busTimeout),
		Process:           readProcess,
		RuntimeProfileDir: "/run/dconf/user",
		SystemProfile:     "/etc/dconf/profile/user",
	}
}

// boundedBuffer keeps the first max bytes and drops the rest without failing
// the child's write, so a chatty command cannot block or exhaust memory.
type boundedBuffer struct {
	data     bytes.Buffer
	max      int
	overflow bool
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if room := b.max - b.data.Len(); len(p) <= room {
		b.data.Write(p)
	} else {
		b.data.Write(p[:max(room, 0)])
		b.overflow = true
	}
	return len(p), nil
}

func runCommand(ctx context.Context, argv, env []string) (Output, error) {
	ctx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()
	stdout, stderr := &boundedBuffer{max: maxStdoutBytes}, &boundedBuffer{max: maxStderrBytes}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Env, cmd.Stdout, cmd.Stderr = env, stdout, stderr
	err := cmd.Run()
	if ctx.Err() != nil {
		return Output{}, ctx.Err()
	}
	var exit *exec.ExitError
	if err != nil && !errors.As(err, &exit) {
		return Output{}, err
	}
	if stdout.overflow {
		return Output{}, fmt.Errorf("output exceeds %d bytes", maxStdoutBytes)
	}
	code := 0
	if exit != nil {
		code = exit.ExitCode()
	}
	return Output{Stdout: stdout.data.Bytes(), Stderr: strings.TrimSpace(stderr.data.String()), ExitCode: code}, nil
}

type busConn struct {
	conn    *dbus.Conn
	timeout time.Duration
	// release ends the connection's context, which godbus ties to its lifetime.
	release context.CancelFunc
}

// busConnector dials with godbus, whose handshake takes no deadline of its own.
// The handshake runs under a timer that cancels the connection's context, and
// a connection that survives it lives until Close.
func busConnector(timeout time.Duration) func(context.Context, string) (Conn, error) {
	return func(ctx context.Context, address string) (Conn, error) {
		ctx, release := context.WithCancel(ctx)
		timer := time.AfterFunc(timeout, release)
		conn, err := sessionbus.Connect(ctx, address)
		if !timer.Stop() && err == nil {
			err = fmt.Errorf("session bus handshake exceeded %s", timeout)
		}
		if err != nil {
			release()
			return nil, err
		}
		return busConn{conn: conn, timeout: timeout, release: release}, nil
	}
}

func (c busConn) Close() error {
	defer c.release()
	return c.conn.Close()
}

// call runs one bus-daemon method under the per-call bound and stores its reply.
func (c busConn) call(ctx context.Context, method string, reply any, args ...any) error {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	return c.conn.BusObject().CallWithContext(ctx, "org.freedesktop.DBus."+method, 0, args...).Store(reply)
}

func (c busConn) StartServiceByName(ctx context.Context, name string) error {
	var reply uint32
	err := c.call(ctx, "StartServiceByName", &reply, name, uint32(0))
	// The bus reports 1 for a service it started and 2 for one already running.
	if err == nil && reply != 1 && reply != 2 {
		err = fmt.Errorf("unexpected start reply %d", reply)
	}
	return err
}
func (c busConn) NameOwner(ctx context.Context, name string) (string, error) {
	var owner string
	err := c.call(ctx, "GetNameOwner", &owner, name)
	return owner, err
}
func (c busConn) Credentials(ctx context.Context, owner string) (Credentials, error) {
	var values map[string]dbus.Variant
	if err := c.call(ctx, "GetConnectionCredentials", &values, owner); err != nil {
		return Credentials{}, err
	}
	uid, uidOK := values["UnixUserID"].Value().(uint32)
	pid, pidOK := values["ProcessID"].Value().(uint32)
	if !uidOK || !pidOK {
		return Credentials{}, errors.New("bus reported no user or process ID")
	}
	return Credentials{UID: uid, PID: pid}, nil
}

// readProcess reads /proc/<pid>/status and the initial environment. The kernel
// fixes both formats; a bound larger than any real manager environment makes
// truncation an exceptional, reported state.
func readProcess(pid uint32) (Process, error) {
	dir := "/proc/" + strconv.FormatUint(uint64(pid), 10)
	status, err := os.ReadFile(dir + "/status")
	if err != nil {
		return Process{}, err
	}
	uid, err := effectiveUID(status)
	if err != nil {
		return Process{}, err
	}
	file, err := os.Open(dir + "/environ")
	if err != nil {
		return Process{}, err
	}
	defer file.Close()
	environ, err := io.ReadAll(io.LimitReader(file, maxEnvironBytes+1))
	if err != nil {
		return Process{}, err
	}
	truncated := len(environ) > maxEnvironBytes
	if truncated {
		environ = environ[:maxEnvironBytes]
	}
	return Process{UID: uid, Environ: environ, Truncated: truncated}, nil
}

// effectiveUID parses the "Uid:" line (real, effective, saved, filesystem).
// The bus reports the effective user ID that the peer socket carried.
func effectiveUID(status []byte) (uint32, error) {
	for _, line := range strings.Split(string(status), "\n") {
		if fields := strings.Fields(line); len(fields) >= 3 && fields[0] == "Uid:" {
			uid, err := strconv.ParseUint(fields[2], 10, 32)
			return uint32(uid), err
		}
	}
	return 0, errors.New("process status has no Uid line")
}
