// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package dconf

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
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
