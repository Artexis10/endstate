// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package dconf

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"strings"
	"testing"
	"time"
)

// A non-zero exit is data for the caller, and a chatty command must not grow memory or hang.
func TestRunCommandReportsExitCodeAndBoundsOutput(t *testing.T) {
	out, err := runCommand(context.Background(), []string{"/bin/sh", "-c", "printf out; printf err >&2; exit 3"}, []string{"PATH=/usr/bin:/bin"})
	if err != nil || out.ExitCode != 3 || string(out.Stdout) != "out" || out.Stderr != "err" {
		t.Fatalf("exit result: %+v %v", out, err)
	}
	if _, err := runCommand(context.Background(), []string{"/bin/sh", "-c", "head -c 5000000 /dev/zero"}, []string{"PATH=/usr/bin:/bin"}); err == nil {
		t.Fatal("output beyond the bound was accepted")
	}
}

// The writer proof rests on /proc: the user must come from the Uid line's
// effective field, and the environment must be flagged when it is cut short.
func TestReadProcessReportsEffectiveUserAndEnvironment(t *testing.T) {
	process, err := readProcess(uint32(os.Getpid()))
	if err != nil {
		t.Skipf("no readable /proc: %v", err)
	}
	if process.UID != uint32(os.Geteuid()) || process.Truncated || !bytes.Contains(process.Environ, []byte("PATH=")) {
		t.Fatalf("own process: uid=%d truncated=%v environ=%d bytes", process.UID, process.Truncated, len(process.Environ))
	}
	if uid, err := effectiveUID([]byte("Name:\tx\nUid:\t1000\t2000\t3000\t4000\n")); err != nil || uid != 2000 {
		t.Fatalf("effective uid: %d %v", uid, err)
	}
	if _, err := effectiveUID([]byte(strings.Repeat("x\n", 3))); err == nil {
		t.Fatal("a status without a Uid line was accepted")
	}
}

// A stopped bus daemon still accepts connections and never answers. Capture
// detection has no deadline of its own, so the handshake must end by itself.
func TestBusConnectorEndsAHandshakeThatNeverAnswers(t *testing.T) {
	// An abstract socket avoids the filesystem path limit of a long scratch directory.
	name := fmt.Sprintf("@endstate-dconf-test-%d-%d", os.Getpid(), time.Now().UnixNano())
	listener, err := net.Listen("unix", name)
	if err != nil {
		t.Skipf("no abstract unix sockets: %v", err)
	}
	defer listener.Close()
	held := make(chan net.Conn, 1)
	go func() {
		if conn, err := listener.Accept(); err == nil {
			held <- conn
		}
	}()
	defer func() {
		select {
		case conn := <-held:
			conn.Close()
		default:
		}
	}()
	done := make(chan error, 1)
	go func() {
		conn, err := busConnector(100*time.Millisecond)(context.Background(), "unix:abstract="+name[1:])
		if err == nil {
			conn.Close()
		}
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("a bus that never answered was accepted")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the handshake with a silent bus was not bounded")
	}
}

// Profile files are read from XDG_DATA_DIRS, which GLib defaults to these two.
func TestProfileCandidatesDefaultToTheXDGSystemDirectories(t *testing.T) {
	ns := namespace{runtimeDir: "/run/user/1", env: map[string]string{}}
	candidates, err := ns.profileCandidates()
	want := []string{"/run/user/1/dconf/profile", "/usr/local/share/dconf/profile/user", "/usr/share/dconf/profile/user"}
	if err != nil || strings.Join(candidates, " ") != strings.Join(want, " ") {
		t.Fatalf("candidates %q, err %v", candidates, err)
	}
}
