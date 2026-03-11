// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package modules

import (
	"os"
	"sort"

	"github.com/Artexis10/endstate/go-engine/internal/config"
	"github.com/Artexis10/endstate/go-engine/internal/manifest"
)

// MatchModulesForApps matches captured apps against the config module catalog.
// For each module with a capture section, it checks:
//   - Whether any app's winget ref matches a module's matches.winget list
//   - Whether any module's matches.pathExists paths exist on the filesystem
//
// Only modules with capture sections are returned. Results are sorted by
// module ID for deterministic output.
func MatchModulesForApps(catalog map[string]*Module, apps []manifest.App) []*Module {
	if len(catalog) == 0 || len(apps) == 0 {
		return nil
	}

	// Collect all winget IDs from captured apps.
	wingetIDs := make(map[string]bool)
	for _, app := range apps {
		if ref, ok := app.Refs["windows"]; ok && ref != "" {
			wingetIDs[ref] = true
		}
	}

	var matched []*Module

	for _, mod := range catalog {
		// Only consider modules with capture sections.
		if mod.Capture == nil || len(mod.Capture.Files) == 0 {
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

		// Check pathExists matches (expand env vars, check filesystem).
		if !isMatch {
			for _, pathPattern := range mod.Matches.PathExists {
				expandedPath := config.ExpandWindowsEnvVars(pathPattern)
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
