// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
)

// ResolveRepoRoot returns the absolute path to the Endstate repo root.
//
// Resolution order:
//  1. ENDSTATE_ROOT environment variable (if set and non-empty).
//  2. Walk up from the directory containing the running executable, looking for
//     ".release-please-manifest.json" — the repo root is the directory that
//     contains it.
//  3. If neither source produces a result, returns an empty string and the caller
//     must handle the missing-root case.
func ResolveRepoRoot() string {
	if root := os.Getenv("ENDSTATE_ROOT"); root != "" {
		return root
	}

	exe, err := os.Executable()
	if err != nil {
		return ""
	}

	// Resolve symlinks so we start from the real binary location.
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return ""
	}

	dir := filepath.Dir(exe)
	for {
		candidate := filepath.Join(dir, ".release-please-manifest.json")
		if _, err := os.Stat(candidate); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached filesystem root without finding manifest.
			break
		}
		dir = parent
	}

	return ""
}

// ProfileDir returns the path to the Endstate profiles directory for the host
// platform. On Windows this is <home>\Documents\Endstate\Profiles; on Linux it
// follows the XDG Base Directory specification; on macOS it uses Application
// Support. Returns an empty string if the home directory cannot be determined.
func ProfileDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return profileDirFor(runtime.GOOS, home, os.Getenv("XDG_DATA_HOME"))
}

// profileDirFor computes the profiles directory for the given OS. Path
// separators are written explicitly for the target OS so the result is
// host-independent and unit-testable from any platform. The Windows path is
// unchanged from the historical Documents location.
func profileDirFor(goos, home, xdgDataHome string) string {
	switch goos {
	case "windows":
		return home + `\Documents\Endstate\Profiles`
	case "darwin":
		return home + "/Library/Application Support/Endstate/Profiles"
	default: // linux and other unix-likes
		base := xdgDataHome
		if base == "" {
			base = home + "/.local/share"
		}
		return base + "/endstate/profiles"
	}
}

// windowsEnvVarPattern matches %VAR_NAME% style variable references.
var windowsEnvVarPattern = regexp.MustCompile(`%([^%]+)%`)

// ExpandWindowsEnvVars replaces every %VAR% occurrence in s with the value of
// the corresponding environment variable. Unknown variables are left as-is
// (matching cmd.exe behaviour).
func ExpandWindowsEnvVars(s string) string {
	return windowsEnvVarPattern.ReplaceAllStringFunc(s, func(match string) string {
		// Strip the surrounding percent signs.
		name := match[1 : len(match)-1]
		if val, ok := os.LookupEnv(name); ok {
			return val
		}
		return match
	})
}

// ExpandEnvVars expands environment-variable references in s using the host
// platform's convention: %VAR% on Windows, $VAR / ${VAR} elsewhere. On Windows
// it is identical to ExpandWindowsEnvVars.
func ExpandEnvVars(s string) string {
	return expandEnvVarsFor(runtime.GOOS, s)
}

// expandEnvVarsFor expands environment variables in s for the given OS. Split
// from ExpandEnvVars so platform behavior is unit-testable from any host.
func expandEnvVarsFor(goos, s string) string {
	if goos == "windows" {
		return ExpandWindowsEnvVars(s)
	}
	return os.ExpandEnv(s)
}
