// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package nix

import (
	"path/filepath"
	"testing"
)

func TestNativeProfilesIgnoreRelativeXDGStateHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("ENDSTATE_NIX_PROFILE", "")
	t.Setenv("XDG_STATE_HOME", "relative-state")
	state := filepath.Join(home, ".local", "state")
	if got, want := DefaultProfile(), filepath.Join(state, "endstate", "nix-profile"); got != want {
		t.Errorf("package profile redirected to working directory: got %q, want %q", got, want)
	}
	if got, want := homeProfilePath(), filepath.Join(state, "nix", "profiles", "home-manager"); got != want {
		t.Errorf("settings profile redirected to working directory: got %q, want %q", got, want)
	}
}
