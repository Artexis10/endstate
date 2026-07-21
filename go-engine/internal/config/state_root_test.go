// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"path/filepath"
	"testing"
)

// TestStateRootFor_IsUserScopedNotRelative is the regression for backups landing
// in the working directory.
//
// When no repo root resolves — the normal case for someone applying a bundle
// they were handed — the previous fallback was a CWD-relative "state/backups".
// A recipient's pre-overwrite backups then landed wherever they happened to run
// the command from, which makes "backup before overwrite" unverifiable and
// revert unusable.
func TestStateRootFor_IsUserScopedNotRelative(t *testing.T) {
	tests := []struct {
		goos     string
		home     string
		xdgState string
		want     string
	}{
		{goos: "windows", home: `C:\Users\someone`, want: `C:\Users\someone\AppData\Local\Endstate\state`},
		{goos: "darwin", home: "/Users/someone", want: "/Users/someone/Library/Application Support/Endstate/state"},
		{goos: "linux", home: "/home/someone", want: "/home/someone/.local/state/endstate"},
		{goos: "linux", home: "/home/someone", xdgState: "/custom/state", want: "/custom/state/endstate"},
	}

	for _, tt := range tests {
		t.Run(tt.goos, func(t *testing.T) {
			got := stateRootFor(tt.goos, tt.home, tt.xdgState)
			if got != tt.want {
				t.Errorf("stateRootFor(%s) = %q, want %q", tt.goos, got, tt.want)
			}
		})
	}
}

// TestStateRoot_PrefersRepoRoot keeps development and repo-checkout behaviour
// unchanged: state stays inside the checkout when one resolves.
//
// The root and the expectation are both built with the host's separator. An
// earlier version hardcoded a Windows-shaped path and passed on Windows while
// failing on Linux and macOS, where filepath.Join produces a mixed separator.
func TestStateRoot_PrefersRepoRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "repo")
	t.Setenv("ENDSTATE_ROOT", root)

	got := StateRoot()

	if want := filepath.Join(root, "state"); got != want {
		t.Errorf("StateRoot() = %q, want %q", got, want)
	}
}
