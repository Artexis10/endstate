// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package nix

import (
	"errors"
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/envelope"
)

// TestNixArgs_InsertsBeforeSeparator: for a `nix run <pin> -- <prog args>`
// invocation the experimental-features flag MUST be inserted before the bare
// `--` separator, so it is consumed by nix (not passed through to the
// downstream home-manager program, which would reject it).
func TestNixArgs_InsertsBeforeSeparator(t *testing.T) {
	got := nixArgs([]string{"run", "github:nix-community/home-manager", "--", "switch", "--flake", "/dot#me", "-b", "endstate-backup"})
	joined := strings.Join(got, " ")
	feat := "--extra-experimental-features nix-command flakes"
	if !strings.Contains(joined, feat) {
		t.Fatalf("experimental features not present: %q", joined)
	}
	// The flag must appear BEFORE the bare `--` separator.
	featIdx := strings.Index(joined, "--extra-experimental-features")
	sepIdx := strings.Index(joined, " -- ")
	if sepIdx < 0 {
		t.Fatalf("separator `--` not found in %q", joined)
	}
	if featIdx > sepIdx {
		t.Errorf("experimental flag landed AFTER `--` (would break the downstream program): %q", joined)
	}
}

// TestNixArgs_AppendsWhenNoSeparator: the `nix profile` verbs carry no bare
// `--`, so the flag is appended at the end exactly as before (package path
// behavior is unchanged).
func TestNixArgs_AppendsWhenNoSeparator(t *testing.T) {
	got := nixArgs([]string{"profile", "add", "--profile", "/p", "nixpkgs#ripgrep", "--log-format", "internal-json"})
	joined := strings.Join(got, " ")
	if !strings.HasSuffix(joined, "--extra-experimental-features nix-command flakes") {
		t.Errorf("expected experimental features appended at end, got %q", joined)
	}
}

// TestActivateHome_Success_ArgvAndGeneration: a clean activation emits
// `run <pin> -- switch --flake <ref> -b endstate-backup` and returns the new
// home-manager generation number (read hermetically via the homeGenFn seam).
func TestActivateHome_Success_ArgvAndGeneration(t *testing.T) {
	run, got := captureRun(nil, 0, nil)
	b := &Backend{
		HomePin:   "github:nix-community/home-manager",
		Run:       run,
		homeGenFn: func() int { return 7 },
	}

	gen, err := b.ActivateHome("/home/me/dotfiles#hugo")
	if err != nil {
		t.Fatalf("expected nil error on successful activation, got %v", err)
	}
	if gen != 7 {
		t.Errorf("generation = %d, want 7 (from homeGenFn)", gen)
	}
	joined := strings.Join(*got, " ")
	want := "run github:nix-community/home-manager -- switch --flake /home/me/dotfiles#hugo -b endstate-backup"
	if !strings.Contains(joined, want) {
		t.Errorf("argv = %q, want it to contain %q", joined, want)
	}
}

// TestActivateHome_DefaultPin: when HomePin is empty the engine default
// home-manager pin is used.
func TestActivateHome_DefaultPin(t *testing.T) {
	t.Setenv("ENDSTATE_HOME_MANAGER_PIN", "")
	run, got := captureRun(nil, 0, nil)
	b := &Backend{Run: run, homeGenFn: func() int { return 1 }}
	if _, err := b.ActivateHome("/dot#me"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	joined := strings.Join(*got, " ")
	if !strings.Contains(joined, "run github:nix-community/home-manager --") {
		t.Errorf("default pin not used: %q", joined)
	}
}

// TestActivateHome_EnvPin: ENDSTATE_HOME_MANAGER_PIN overrides the pin.
func TestActivateHome_EnvPin(t *testing.T) {
	t.Setenv("ENDSTATE_HOME_MANAGER_PIN", "github:nix-community/home-manager/release-25.05")
	run, got := captureRun(nil, 0, nil)
	b := &Backend{Run: run, homeGenFn: func() int { return 1 }}
	if _, err := b.ActivateHome("/dot#me"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	joined := strings.Join(*got, " ")
	if !strings.Contains(joined, "run github:nix-community/home-manager/release-25.05 --") {
		t.Errorf("env pin not used: %q", joined)
	}
}

// TestActivateHome_Eval_InstallFailed: a non-zero exit whose plain-text stderr
// carries an eval anchor classifies as INSTALL_FAILED with the raw text
// confined to Err.Raw (the moat). home-manager prints plain text, not
// internal-json, so classification anchors against the raw stderr.
func TestActivateHome_Eval_InstallFailed(t *testing.T) {
	raw := "error: flake 'path:/home/me/dotfiles' does not provide attribute 'homeConfigurations.bad'"
	run, _ := captureRun([]byte(raw), 1, nil)
	b := &Backend{HomePin: "github:nix-community/home-manager", Run: run}

	_, err := b.ActivateHome("/home/me/dotfiles#bad")
	rerr := asRealizerErr(t, err)
	if rerr.Code != envelope.ErrInstallFailed {
		t.Errorf("Code = %q, want INSTALL_FAILED", rerr.Code)
	}
	if !strings.Contains(rerr.Raw, "does not provide attribute") {
		t.Errorf("raw text not retained in Err.Raw: %q", rerr.Raw)
	}
}

// TestActivateHome_Permission_Systemic: a permission-class anchor in the
// plain-text stderr surfaces PERMISSION_DENIED.
func TestActivateHome_Permission_Systemic(t *testing.T) {
	run, _ := captureRun([]byte("error: opening file '/nix/store/.lock': Permission denied"), 1, nil)
	b := &Backend{HomePin: "github:nix-community/home-manager", Run: run}

	_, err := b.ActivateHome("/dot#me")
	rerr := asRealizerErr(t, err)
	if rerr.Code != envelope.ErrPermissionDenied {
		t.Errorf("Code = %q, want PERMISSION_DENIED", rerr.Code)
	}
}

// TestActivateHome_Daemon_Systemic: a daemon-class anchor surfaces
// REALIZER_UNAVAILABLE.
func TestActivateHome_Daemon_Systemic(t *testing.T) {
	run, _ := captureRun([]byte("error: cannot connect to socket at '/nix/var/nix/daemon-socket/socket'"), 1, nil)
	b := &Backend{HomePin: "github:nix-community/home-manager", Run: run}

	_, err := b.ActivateHome("/dot#me")
	rerr := asRealizerErr(t, err)
	if rerr.Code != envelope.ErrRealizerUnavailable {
		t.Errorf("Code = %q, want REALIZER_UNAVAILABLE", rerr.Code)
	}
}

// TestActivateHome_Spawn: a spawn failure (runner returns a non-nil error)
// surfaces REALIZER_UNAVAILABLE regardless of any text.
func TestActivateHome_Spawn(t *testing.T) {
	run, _ := captureRun(nil, -1, errors.New("exec: nix not found"))
	b := &Backend{HomePin: "github:nix-community/home-manager", Run: run}

	_, err := b.ActivateHome("/dot#me")
	rerr := asRealizerErr(t, err)
	if rerr.Code != envelope.ErrRealizerUnavailable {
		t.Errorf("Code = %q, want REALIZER_UNAVAILABLE", rerr.Code)
	}
}
