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
//     contains it. This identifies a repo checkout.
//  3. Walk up again looking for a "modules/apps" directory. This identifies an
//     installed layout, which carries the module catalog but no repo marker.
//  4. If no source produces a result, returns an empty string and the caller
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

	start := filepath.Dir(exe)

	if root := walkUpFor(start, func(dir string) bool {
		_, err := os.Stat(filepath.Join(dir, ".release-please-manifest.json"))
		return err == nil
	}); root != "" {
		return root
	}

	// Installed layout: no repo marker exists, because bootstrap does not write
	// one. Fall back to the catalog itself — an install that carries modules is
	// a usable root. From <install>\bin\lib\endstate.exe this resolves <install>\bin.
	//
	// Ordering is deliberate: this runs only where the marker walk already
	// returned nothing, so a repo checkout and an ENDSTATE_ROOT override both
	// still win and existing behaviour is unchanged. Without this step a
	// PATH-invoked binary resolves no root at all, and capture silently emits an
	// app-list-only manifest with none of the config modules that are the point.
	return walkUpFor(start, func(dir string) bool {
		info, err := os.Stat(filepath.Join(dir, "modules", "apps"))
		return err == nil && info.IsDir()
	})
}

// walkUpFor returns the nearest ancestor of start (inclusive) satisfying match,
// or "" if the filesystem root is reached without a hit.
func walkUpFor(start string, match func(dir string) bool) string {
	dir := start
	for {
		if match(dir) {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// StateRoot returns the directory Endstate writes run state into: backups, the
// restore journal, and state.json.
//
// It is the repo root's state/ when a root resolves, and a stable user-scoped
// directory otherwise. The fallback matters because the alternative — a
// CWD-relative path — puts a recipient's pre-overwrite backups wherever they
// happened to run the command from. Backups that cannot be found are not a
// safety net, and "backup before overwrite" is an invariant, not a best effort.
//
// Returns an empty string only if neither a repo root nor a home directory can
// be determined, leaving the decision to the caller.
func StateRoot() string {
	if root := ResolveRepoRoot(); root != "" {
		return filepath.Join(root, "state")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return stateRootFor(runtime.GOOS, home, os.Getenv("XDG_STATE_HOME"))
}

// stateRootFor computes the user-scoped state directory for the given OS.
// Separators are written for the target OS so the result is host-independent
// and unit-testable from any platform.
func stateRootFor(goos, home, xdgStateHome string) string {
	switch goos {
	case "windows":
		return home + `\AppData\Local\Endstate\state`
	case "darwin":
		return home + "/Library/Application Support/Endstate/state"
	default:
		base := xdgStateHome
		if base == "" {
			base = home + "/.local/state"
		}
		return base + "/endstate"
	}
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
