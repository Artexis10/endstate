// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"os"
	"runtime"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// ExpandWindowsEnvVars tests
// (mirrors Pester PathResolver.Tests.ps1 — Environment Variable Expansion)
// ---------------------------------------------------------------------------

func TestExpandWindowsEnvVars_USERPROFILE(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-only env var test")
	}

	result := ExpandWindowsEnvVars(`%USERPROFILE%\test`)
	if strings.Contains(result, "%USERPROFILE%") {
		t.Errorf("expected %%USERPROFILE%% to be expanded, got %q", result)
	}
	if !strings.HasSuffix(result, `\test`) {
		t.Errorf("expected result to end with \\test, got %q", result)
	}
}

func TestExpandWindowsEnvVars_LOCALAPPDATA(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-only env var test")
	}

	result := ExpandWindowsEnvVars(`%LOCALAPPDATA%\MyApp`)
	if strings.Contains(result, "%LOCALAPPDATA%") {
		t.Errorf("expected %%LOCALAPPDATA%% to be expanded, got %q", result)
	}
	if !strings.HasSuffix(result, `\MyApp`) {
		t.Errorf("expected result to end with \\MyApp, got %q", result)
	}
}

func TestExpandWindowsEnvVars_APPDATA(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-only env var test")
	}

	result := ExpandWindowsEnvVars(`%APPDATA%\Config`)
	if strings.Contains(result, "%APPDATA%") {
		t.Errorf("expected %%APPDATA%% to be expanded, got %q", result)
	}
	if !strings.HasSuffix(result, `\Config`) {
		t.Errorf("expected result to end with \\Config, got %q", result)
	}
}

func TestExpandWindowsEnvVars_UnknownVar_LeftAsIs(t *testing.T) {
	// Unknown environment variables should be left as-is (matching cmd.exe behaviour).
	result := ExpandWindowsEnvVars("%NONEXISTENT_ENDSTATE_TEST_VAR_XYZ%\\test")
	if !strings.Contains(result, "%NONEXISTENT_ENDSTATE_TEST_VAR_XYZ%") {
		t.Errorf("expected unknown var to remain, got %q", result)
	}
}

func TestExpandWindowsEnvVars_CustomEnvVar(t *testing.T) {
	t.Setenv("ENDSTATE_TEST_PATH_VAR", "/custom/path")

	result := ExpandWindowsEnvVars("%ENDSTATE_TEST_PATH_VAR%/file.txt")
	if strings.Contains(result, "%ENDSTATE_TEST_PATH_VAR%") {
		t.Errorf("expected custom env var to be expanded, got %q", result)
	}
	if !strings.Contains(result, "/custom/path") {
		t.Errorf("expected expanded value to contain /custom/path, got %q", result)
	}
}

func TestExpandWindowsEnvVars_MultipleVars(t *testing.T) {
	t.Setenv("ENDSTATE_VAR_A", "alpha")
	t.Setenv("ENDSTATE_VAR_B", "beta")

	result := ExpandWindowsEnvVars("%ENDSTATE_VAR_A%/%ENDSTATE_VAR_B%/config")
	if result != "alpha/beta/config" {
		t.Errorf("expected alpha/beta/config, got %q", result)
	}
}

func TestExpandWindowsEnvVars_NoVars(t *testing.T) {
	// Plain path with no percent-delimited vars should pass through unchanged.
	input := "/some/plain/path"
	result := ExpandWindowsEnvVars(input)
	if result != input {
		t.Errorf("expected unchanged path %q, got %q", input, result)
	}
}

func TestExpandWindowsEnvVars_EmptyString(t *testing.T) {
	result := ExpandWindowsEnvVars("")
	if result != "" {
		t.Errorf("expected empty string, got %q", result)
	}
}

func TestExpandWindowsEnvVars_TEMP(t *testing.T) {
	// %TEMP% is available on all Windows systems.
	if runtime.GOOS != "windows" {
		t.Skip("Windows-only env var test")
	}

	tempDir := os.Getenv("TEMP")
	if tempDir == "" {
		t.Skip("TEMP env var not set")
	}

	result := ExpandWindowsEnvVars(`%TEMP%\endstate-test`)
	expected := tempDir + `\endstate-test`
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

// ---------------------------------------------------------------------------
// ResolveRepoRoot tests (sentinel: .release-please-manifest.json)
// ---------------------------------------------------------------------------

func TestResolveRepoRoot_WithENDSTATE_ROOT(t *testing.T) {
	t.Setenv("ENDSTATE_ROOT", "/fake/repo/root")
	result := ResolveRepoRoot()
	if result != "/fake/repo/root" {
		t.Errorf("expected /fake/repo/root, got %q", result)
	}
}

func TestResolveRepoRoot_EmptyENDSTATE_ROOT_FallsThrough(t *testing.T) {
	t.Setenv("ENDSTATE_ROOT", "")
	// Should not return empty string from env var path; it tries the walk-up logic.
	// Result depends on filesystem, but we verify it doesn't panic.
	_ = ResolveRepoRoot()
}

// ---------------------------------------------------------------------------
// ProfileDir tests
// ---------------------------------------------------------------------------

func TestProfileDir_ReturnsNonEmpty(t *testing.T) {
	dir := ProfileDir()
	if dir == "" {
		t.Error("expected non-empty profile directory")
	}
	if !strings.Contains(dir, "Endstate") {
		t.Errorf("expected profile dir to contain 'Endstate', got %q", dir)
	}
	if !strings.Contains(dir, "Profiles") {
		t.Errorf("expected profile dir to contain 'Profiles', got %q", dir)
	}
}
