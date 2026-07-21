// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package modules

import (
	"os"
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
	return matchModulesForApps(catalog, apps, true)
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
	return matchModulesForApps(catalog, apps, false)
}

func matchModulesForApps(catalog map[string]*Module, apps []manifest.App, includePathExists bool) []*Module {
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
		// Only consider modules with capture sections.
		if mod.Capture == nil || (len(mod.Capture.Files) == 0 && len(mod.Capture.RegistryKeys) == 0) {
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
		if !isMatch && includePathExists {
			for _, pathPattern := range mod.Matches.PathExists {
				expandedPath := config.ExpandEnvVars(pathPattern)
				expandedPath = os.ExpandEnv(expandedPath)
				if _, err := os.Stat(expandedPath); err == nil {
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
