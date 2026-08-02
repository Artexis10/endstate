// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package modules

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/config"
	"github.com/Artexis10/endstate/go-engine/internal/manifest"
)

// MatchModulesForApps matches captured apps against the config module catalog.
// For each module with a capture section, it checks:
//   - Whether an app's Windows ref matches the matcher for its selected driver
//   - Whether any module's matches.pathExists paths exist on the filesystem
//
// Only modules with capture sections are returned. Results are sorted by
// module ID for deterministic output.
func MatchModulesForApps(catalog map[string]*Module, apps []manifest.App) []*Module {
	return matchModulesForApps(catalog, apps, true, true)
}

// MatchModulesForAppsIncludingInstall matches package-backed modules even when
// they deliberately declare no capture lane. Apply and verify use this to
// retain install-only module identity and ownership; capture keeps using
// MatchModulesForApps so it cannot invent configuration payloads.
func MatchModulesForAppsIncludingInstall(catalog map[string]*Module, apps []manifest.App) []*Module {
	return matchModulesForApps(catalog, apps, true, false)
}

// MatchModulesForAppsSelective matches apps against the catalog by package
// reference only (winget/chocolatey), ignoring matches.pathExists.
//
// Under an explicit selection (--only), a module that merely has a path on this
// filesystem is not part of the selection — it has to be named. 141 of 357
// catalog modules declare pathExists, and the branch below checks it against the
// filesystem without consulting the app list at all, so including it would pull
// in configs for most installed apps regardless of what the user picked. That is
// a payload leak precisely when the artifact is being handed to another person.
func MatchModulesForAppsSelective(catalog map[string]*Module, apps []manifest.App) []*Module {
	return matchModulesForApps(catalog, apps, false, true)
}

func matchModulesForApps(catalog map[string]*Module, apps []manifest.App, includePathExists, requireCapture bool) []*Module {
	if len(catalog) == 0 || len(apps) == 0 {
		return nil
	}

	// Collect Windows refs by selected driver. An omitted driver retains the
	// legacy Winget default; explicit drivers never cross-match.
	wingetIDs := make(map[string]bool)
	chocolateyIDs := make(map[string]bool)
	for _, app := range apps {
		ref := app.Refs["windows"]
		if ref == "" {
			continue
		}
		switch {
		case strings.EqualFold(app.Driver, "chocolatey"):
			chocolateyIDs[strings.ToLower(ref)] = true
		case app.Driver == "" || strings.EqualFold(app.Driver, "winget"):
			wingetIDs[ref] = true
		}
	}

	var matched []*Module

	for _, mod := range catalog {
		hasCapture := moduleHasCaptureDeclarations(mod)
		// Only consider modules with a legacy capture lane or at least one
		// schema-v2 generation capture lane.
		if requireCapture && !hasCapture {
			continue
		}

		isMatch := false

		// Check winget ID matches.
		for _, wingetPattern := range mod.Matches.Winget {
			if wingetIDs[wingetPattern] {
				isMatch = true
				break
			}
		}

		// Check Chocolatey ID matches.
		if !isMatch {
			for _, chocolateyPattern := range mod.Matches.Chocolatey {
				if chocolateyIDs[strings.ToLower(chocolateyPattern)] {
					isMatch = true
					break
				}
			}
		}

		// Check pathExists matches (expand env vars, check filesystem).
		if !isMatch && includePathExists && hasCapture {
			for _, pathPattern := range mod.Matches.PathExists {
				if pathExistsOnHost(pathPattern) {
					isMatch = true
					break
				}
			}
		}

		if isMatch {
			matched = append(matched, mod)
		}
	}

	// Sort deterministically by module ID.
	sort.Slice(matched, func(i, j int) bool {
		return matched[i].ID < matched[j].ID
	})

	return matched
}

func moduleHasCaptureDeclarations(mod *Module) bool {
	if mod == nil {
		return false
	}
	if mod.Capture != nil && (len(mod.Capture.Files) > 0 || len(mod.Capture.RegistryKeys) > 0) {
		return true
	}
	if mod.Config == nil {
		return false
	}
	for _, set := range mod.Config.Sets {
		for _, generation := range set.Generations {
			capture := generation.Capture
			if capture != nil && (len(capture.Files) > 0 || len(capture.RegistryKeys) > 0 || len(capture.RegistryValues) > 0) {
				return true
			}
		}
	}
	return false
}

// pathExistsOnHost reports whether a matches.pathExists entry resolves to
// something on this filesystem, after environment expansion.
//
// Patterns carrying a wildcard go through filepath.Glob; everything else keeps
// the original os.Stat. Versioned install roots can only be expressed as
// wildcards — after-effects pins
// "%ProgramFiles%\Adobe\Adobe After Effects*\Support Files\AfterFX.exe" — and a
// bare Stat on such a pattern can never succeed, so those modules were silently
// undetectable. Glob is existence-checked by definition, so a wildcard that
// resolves to nothing stays unmatched rather than becoming a blanket match.
func pathExistsOnHost(pathPattern string) bool {
	expandedPath := config.ExpandEnvVars(pathPattern)
	expandedPath = os.ExpandEnv(expandedPath)

	if strings.ContainsAny(expandedPath, "*?[") {
		matches, err := filepath.Glob(expandedPath)
		return err == nil && len(matches) > 0
	}

	_, err := os.Stat(expandedPath)
	return err == nil
}
