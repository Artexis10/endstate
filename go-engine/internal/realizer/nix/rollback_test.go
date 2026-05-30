// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package nix

import (
	"errors"
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/realizer"
)

// captureRun returns a Runner that records the args of its (single) invocation
// and replays the scripted stderr/exit/err.
func captureRun(stderr []byte, exit int, runErr error) (Runner, *[]string) {
	got := &[]string{}
	r := func(args ...string) ([]byte, []byte, int, error) {
		*got = append([]string(nil), args...)
		return nil, stderr, exit, runErr
	}
	return r, got
}

func asRealizerErr(t *testing.T, err error) *realizer.Error {
	t.Helper()
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	rerr, ok := err.(*realizer.Error)
	if !ok {
		t.Fatalf("expected *realizer.Error, got %T (%v)", err, err)
	}
	return rerr
}

// TestRollback_Previous: bare rollback (to <= 0) emits `profile rollback
// --profile <p>` with NO --to, and exit 0 yields no error.
func TestRollback_Previous(t *testing.T) {
	run, got := captureRun(nil, 0, nil)
	b := &Backend{Profile: "/tmp/endstate-nix-test-profile", Run: run}

	if err := b.Rollback(0); err != nil {
		t.Fatalf("expected nil error on successful rollback, got %v", err)
	}
	joined := strings.Join(*got, " ")
	if !strings.Contains(joined, "profile rollback --profile /tmp/endstate-nix-test-profile") {
		t.Errorf("args missing rollback invocation: %q", joined)
	}
	if strings.Contains(joined, "--to") {
		t.Errorf("bare rollback must not pass --to, got %q", joined)
	}
}

// TestRollback_ToVersion: Rollback(N>0) emits `--to N`.
func TestRollback_ToVersion(t *testing.T) {
	run, got := captureRun(nil, 0, nil)
	b := &Backend{Profile: "/tmp/endstate-nix-test-profile", Run: run}

	if err := b.Rollback(4); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	joined := strings.Join(*got, " ")
	if !strings.Contains(joined, "--to 4") {
		t.Errorf("expected --to 4 in args, got %q", joined)
	}
}

// TestRollback_Failure_RemapsToRollbackFailed: a non-zero exit whose text
// matches no systemic anchor classifies as ROLLBACK_FAILED (not INSTALL_FAILED),
// with the raw text confined to Err.Raw.
func TestRollback_Failure_RemapsToRollbackFailed(t *testing.T) {
	raw := "error: profile version 99 does not exist"
	stderr := mkStderr([]struct {
		level int
		text  string
	}{{0, raw}}, nil)
	run, _ := captureRun(stderr, 1, nil)
	b := &Backend{Profile: "/tmp/endstate-nix-test-profile", Run: run}

	rerr := asRealizerErr(t, b.Rollback(99))
	if rerr.Code != envelope.ErrRollbackFailed {
		t.Errorf("Code = %q, want ROLLBACK_FAILED", rerr.Code)
	}
	if !strings.Contains(rerr.Raw, "does not exist") {
		t.Errorf("raw text not retained in Err.Raw: %q", rerr.Raw)
	}
}

// TestRollback_Daemon_Systemic: a daemon-class anchor surfaces
// REALIZER_UNAVAILABLE (passed through classify, not remapped).
func TestRollback_Daemon_Systemic(t *testing.T) {
	stderr := mkStderr([]struct {
		level int
		text  string
	}{{0, "error: cannot connect to socket at '/nix/var/nix/daemon-socket/socket'"}}, nil)
	run, _ := captureRun(stderr, 1, nil)
	b := &Backend{Profile: "/tmp/endstate-nix-test-profile", Run: run}

	rerr := asRealizerErr(t, b.Rollback(0))
	if rerr.Code != envelope.ErrRealizerUnavailable {
		t.Errorf("Code = %q, want REALIZER_UNAVAILABLE", rerr.Code)
	}
}

// TestRollback_Permission_Systemic: a permission-class anchor surfaces
// PERMISSION_DENIED.
func TestRollback_Permission_Systemic(t *testing.T) {
	stderr := mkStderr([]struct {
		level int
		text  string
	}{{0, "error: opening file: Permission denied"}}, nil)
	run, _ := captureRun(stderr, 1, nil)
	b := &Backend{Profile: "/tmp/endstate-nix-test-profile", Run: run}

	rerr := asRealizerErr(t, b.Rollback(0))
	if rerr.Code != envelope.ErrPermissionDenied {
		t.Errorf("Code = %q, want PERMISSION_DENIED", rerr.Code)
	}
}

// TestRollback_Spawn: a spawn failure (runner returns a non-nil error) surfaces
// REALIZER_UNAVAILABLE.
func TestRollback_Spawn(t *testing.T) {
	run, _ := captureRun(nil, -1, errors.New("exec: nix not found"))
	b := &Backend{Profile: "/tmp/endstate-nix-test-profile", Run: run}

	rerr := asRealizerErr(t, b.Rollback(0))
	if rerr.Code != envelope.ErrRealizerUnavailable {
		t.Errorf("Code = %q, want REALIZER_UNAVAILABLE", rerr.Code)
	}
}
