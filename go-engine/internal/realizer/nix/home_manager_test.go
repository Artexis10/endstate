// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package nix

import (
	"errors"
	"os"
	"runtime"
	"strings"
	"testing"

	"path/filepath"

	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/realizer"
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

func TestNormalizeHomeFlakeRefLeavesRemoteAndExplicitRefsAlone(t *testing.T) {
	for _, reference := range []string{
		"github:owner/dots#me",
		"path:/home/me/dots#me",
		"./dots#me",
	} {
		if got := normalizeHomeFlakeRef(reference); got != reference {
			t.Errorf("normalizeHomeFlakeRef(%q) = %q", reference, got)
		}
	}
}

func TestNormalizeHomeFlakeRefRecognizesPortableAbsolutePaths(t *testing.T) {
	for input, want := range map[string]string{
		"/home/me/dots#me":    "path:/home/me/dots#me",
		`C:\Users\me\dots#me`: "path:C:/Users/me/dots#me",
	} {
		if got := normalizeHomeFlakeRef(input); got != want {
			t.Errorf("normalizeHomeFlakeRef(%q) = %q, want %q", input, got, want)
		}
	}
}

// TestActivateHome_UnresolvedInput: a non-zero exit whose plain-text stderr
// carries an eval failure reports INPUT_PROVENANCE_UNRESOLVED with raw text
// confined to Err.Raw (the moat). home-manager prints plain text, not
// internal-json, so classification anchors against the raw stderr.
func TestActivateHome_UnresolvedInput(t *testing.T) {
	raw := "error: flake 'path:/home/me/dotfiles' does not provide attribute 'homeConfigurations.bad'"
	run, _ := captureRun([]byte(raw), 1, nil)
	b := &Backend{Profile: filepath.Join(t.TempDir(), "profile"), HomePin: "github:nix-community/home-manager", Run: run}

	_, err := b.ActivateHome("/home/me/dotfiles#bad")
	rerr := asRealizerErr(t, err)
	if rerr.Code != realizer.ErrInputProvenanceUnresolved {
		t.Errorf("Code = %q, want INPUT_PROVENANCE_UNRESOLVED", rerr.Code)
	}
	if !strings.Contains(rerr.Raw, "does not provide attribute") {
		t.Errorf("raw text not retained in Err.Raw: %q", rerr.Raw)
	}
}

// TestActivateHome_Permission_Systemic: a permission-class anchor in the
// plain-text stderr surfaces PERMISSION_DENIED.
func TestActivateHome_Permission_Systemic(t *testing.T) {
	run, _ := captureRun([]byte("error: opening file '/nix/store/.lock': Permission denied"), 1, nil)
	b := &Backend{Profile: filepath.Join(t.TempDir(), "profile"), HomePin: "github:nix-community/home-manager", Run: run}

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
	b := &Backend{Profile: filepath.Join(t.TempDir(), "profile"), HomePin: "github:nix-community/home-manager", Run: run}

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
	b := &Backend{Profile: filepath.Join(t.TempDir(), "profile"), HomePin: "github:nix-community/home-manager", Run: run}

	_, err := b.ActivateHome("/dot#me")
	rerr := asRealizerErr(t, err)
	if rerr.Code != envelope.ErrRealizerUnavailable {
		t.Errorf("Code = %q, want REALIZER_UNAVAILABLE", rerr.Code)
	}
}

func TestActivationEnvironmentPreservesNativeDriverHooks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("activation scripts run on Nix hosts")
	}
	t.Setenv("HOME_MANAGER_BACKUP_EXT", "ambient-value")
	t.Setenv("HOME_MANAGER_BACKUP_COMMAND", "native-backup")
	t.Setenv("HOME_MANAGER_BACKUP_OVERWRITE", "native-overwrite")
	t.Setenv("DRY_RUN_CMD", "ambient-dry-run")
	script := filepath.Join(t.TempDir(), "activate")
	body := `#!/bin/sh
[ "$#" = 0 ] || exit 1
printf '%s\n' "$HOME_MANAGER_BACKUP_EXT" "$HOME_MANAGER_BACKUP_COMMAND" "$HOME_MANAGER_BACKUP_OVERWRITE" "$DRY_RUN_CMD"
`
	if err := os.WriteFile(script, []byte(body), 0700); err != nil {
		t.Fatal(err)
	}
	out, stderr, exit, err := defaultScriptRunner(script)
	if err != nil || exit != 0 || string(out) != "endstate-backup\nnative-backup\nnative-overwrite\nambient-dry-run\n" {
		t.Fatalf("native activation environment changed: %q %s %d %v", out, stderr, exit, err)
	}
	if os.Getenv("HOME_MANAGER_BACKUP_EXT") != "ambient-value" {
		t.Fatal("activation changed the parent environment")
	}
}
