// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package nix

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/realizer"
)

// captureScript returns a ScriptRunner that records the (single) script path it
// was asked to run and replays the scripted stderr/exit/err.
func captureScript(stderr []byte, exit int, runErr error) (ScriptRunner, *string) {
	got := new(string)
	r := func(path string, args ...string) ([]byte, []byte, int, error) {
		*got = path
		return nil, stderr, exit, runErr
	}
	return r, got
}

// setupHomeLink points XDG_STATE_HOME at a temp dir and creates a
// home-manager-<gen>-link symlink in the home-manager profile dir, returning the
// (arbitrary) store path it points at. The target need not exist — RollbackHome
// only reads the link to locate the generation's `activate` script.
func setupHomeLink(t *testing.T, gen int) string {
	t.Helper()
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	profiles := filepath.Join(state, "nix", "profiles")
	if err := os.MkdirAll(profiles, 0o755); err != nil {
		t.Fatalf("mkdir profiles: %v", err)
	}
	store := filepath.Join(t.TempDir(), "xxxx-home-manager-generation")
	link := filepath.Join(profiles, "home-manager-"+strconv.Itoa(gen)+"-link")
	if err := os.Symlink(store, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	return store
}

// TestRollbackHome_Success_RunsActivateAndReturnsNewGen: RollbackHome(M) resolves
// the home-manager-<M>-link snapshot, runs its `activate` script, and returns the
// new (forward) home-manager generation read via the homeGenFn seam.
func TestRollbackHome_Success_RunsActivateAndReturnsNewGen(t *testing.T) {
	store := setupHomeLink(t, 3)
	run, gotPath := captureScript(nil, 0, nil)
	b := &Backend{runScript: run, homeGenFn: func() int { return 9 }}

	gen, err := b.RollbackHome(3)
	if err != nil {
		t.Fatalf("expected nil error on successful re-activation, got %v", err)
	}
	if gen != 9 {
		t.Errorf("generation = %d, want 9 (the new forward gen from homeGenFn)", gen)
	}
	want := filepath.Join(store, "activate")
	if *gotPath != want {
		t.Errorf("ran script %q, want %q", *gotPath, want)
	}
}

// TestRollbackHome_MissingSnapshot_DistinctErrorNoExec: when the
// home-manager-<M>-link is absent (snapshot garbage-collected), RollbackHome
// returns the distinguishable ErrHomeSnapshotMissing and runs nothing, so the
// command layer can fall back.
func TestRollbackHome_MissingSnapshot_DistinctErrorNoExec(t *testing.T) {
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state) // no profiles dir, no link
	ran := false
	b := &Backend{runScript: func(path string, args ...string) ([]byte, []byte, int, error) {
		ran = true
		return nil, nil, 0, nil
	}}

	_, err := b.RollbackHome(5)
	if !errors.Is(err, realizer.ErrHomeSnapshotMissing) {
		t.Fatalf("want ErrHomeSnapshotMissing, got %v", err)
	}
	if ran {
		t.Error("activate must NOT run when the snapshot link is missing")
	}
}

// TestRollbackHome_Failure_RemapsToRollbackFailed: a non-zero exit whose plain
// stderr matches no systemic anchor classifies as ROLLBACK_FAILED (not
// INSTALL_FAILED), with the raw text confined to Err.Raw (the moat).
func TestRollbackHome_Failure_RemapsToRollbackFailed(t *testing.T) {
	setupHomeLink(t, 2)
	run, _ := captureScript([]byte("error: activation script failed midway"), 1, nil)
	b := &Backend{runScript: run}

	_, err := b.RollbackHome(2)
	rerr := asRealizerErr(t, err)
	if rerr.Code != envelope.ErrRollbackFailed {
		t.Errorf("Code = %q, want ROLLBACK_FAILED", rerr.Code)
	}
	if !strings.Contains(rerr.Raw, "activation script failed") {
		t.Errorf("raw text not retained in Err.Raw: %q", rerr.Raw)
	}
}

// TestRollbackHome_Permission_Systemic: a permission anchor surfaces
// PERMISSION_DENIED.
func TestRollbackHome_Permission_Systemic(t *testing.T) {
	setupHomeLink(t, 2)
	run, _ := captureScript([]byte("error: opening file '/nix/store/.lock': Permission denied"), 1, nil)
	b := &Backend{runScript: run}

	_, err := b.RollbackHome(2)
	rerr := asRealizerErr(t, err)
	if rerr.Code != envelope.ErrPermissionDenied {
		t.Errorf("Code = %q, want PERMISSION_DENIED", rerr.Code)
	}
}

// TestRollbackHome_Daemon_Systemic: a daemon anchor surfaces REALIZER_UNAVAILABLE
// (passed through classify, not remapped).
func TestRollbackHome_Daemon_Systemic(t *testing.T) {
	setupHomeLink(t, 2)
	run, _ := captureScript([]byte("error: cannot connect to socket at '/nix/var/nix/daemon-socket/socket'"), 1, nil)
	b := &Backend{runScript: run}

	_, err := b.RollbackHome(2)
	rerr := asRealizerErr(t, err)
	if rerr.Code != envelope.ErrRealizerUnavailable {
		t.Errorf("Code = %q, want REALIZER_UNAVAILABLE", rerr.Code)
	}
}

// TestRollbackHome_Spawn_Unavailable: a spawn failure (runner returns a non-nil
// error) surfaces REALIZER_UNAVAILABLE regardless of any text.
func TestRollbackHome_Spawn_Unavailable(t *testing.T) {
	setupHomeLink(t, 2)
	run, _ := captureScript(nil, -1, errors.New("exec: activate not found"))
	b := &Backend{runScript: run}

	_, err := b.RollbackHome(2)
	rerr := asRealizerErr(t, err)
	if rerr.Code != envelope.ErrRealizerUnavailable {
		t.Errorf("Code = %q, want REALIZER_UNAVAILABLE", rerr.Code)
	}
}
