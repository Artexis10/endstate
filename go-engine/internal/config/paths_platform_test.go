// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package config

import "testing"

// profileDirFor / expandEnvVarsFor take goos explicitly so platform behavior is
// testable from any host (Windows now, Linux later).

func TestProfileDirFor_WindowsUnchanged(t *testing.T) {
	got := profileDirFor("windows", `C:\Users\me`, "")
	want := `C:\Users\me\Documents\Endstate\Profiles`
	if got != want {
		t.Errorf("profileDirFor(windows) = %q, want %q", got, want)
	}
}

func TestProfileDirFor_LinuxUsesXDGDataHome(t *testing.T) {
	got := profileDirFor("linux", "/home/me", "/home/me/.xdgdata")
	want := "/home/me/.xdgdata/endstate/profiles"
	if got != want {
		t.Errorf("profileDirFor(linux, xdg) = %q, want %q", got, want)
	}
}

func TestProfileDirFor_LinuxDefaultsToLocalShare(t *testing.T) {
	got := profileDirFor("linux", "/home/me", "")
	want := "/home/me/.local/share/endstate/profiles"
	if got != want {
		t.Errorf("profileDirFor(linux, no xdg) = %q, want %q", got, want)
	}
}

func TestExpandEnvVarsFor_WindowsExpandsPercent(t *testing.T) {
	t.Setenv("ENDSTATE_TEST_X", "VAL")
	if got := expandEnvVarsFor("windows", "%ENDSTATE_TEST_X%/a"); got != "VAL/a" {
		t.Errorf("expandEnvVarsFor(windows) = %q, want VAL/a", got)
	}
}

func TestExpandEnvVarsFor_LinuxExpandsDollar(t *testing.T) {
	t.Setenv("ENDSTATE_TEST_Y", "VAL")
	if got := expandEnvVarsFor("linux", "$ENDSTATE_TEST_Y/a"); got != "VAL/a" {
		t.Errorf("expandEnvVarsFor(linux) = %q, want VAL/a", got)
	}
}

func TestExpandEnvVarsFor_LinuxLeavesPercentAlone(t *testing.T) {
	t.Setenv("ENDSTATE_TEST_Z", "VAL")
	in := "%ENDSTATE_TEST_Z%/a"
	if got := expandEnvVarsFor("linux", in); got != in {
		t.Errorf("expandEnvVarsFor(linux, percent) = %q, want unchanged %q", got, in)
	}
}
