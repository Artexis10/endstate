// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"os"
	"path/filepath"
	"regexp"
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

// ProfileDir returns the path to the Endstate profiles directory under the user's
// home directory: <home>/Documents/Endstate/Profiles.
//
// If the home directory cannot be determined, it returns an empty string.
func ProfileDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, "Documents", "Endstate", "Profiles")
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
