// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package modules

import (
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/manifest"
)

// SynthesizeAppsFromModules inspects configModules entries in the manifest and
// creates synthetic manual app entries for modules that have pathExists matchers
// but no corresponding app in mf.Apps. This allows config-only modules (e.g.
// Adobe Lightroom Classic with no winget ID) to surface in the planner and GUI.
//
// The function modifies mf.Apps in place, appending synthesized entries.
//
// TODO: excludeConfigs is not yet represented in the Go Manifest type. When
// added, modules listed in excludeConfigs should be skipped here.
func SynthesizeAppsFromModules(mf *manifest.Manifest, catalog map[string]*Module) {
	if len(mf.ConfigModules) == 0 || len(catalog) == 0 {
		return
	}

	// Build lookup sets for existing apps: by ID and by winget ref.
	appIDs := make(map[string]bool, len(mf.Apps))
	wingetRefs := make(map[string]bool)
	for _, app := range mf.Apps {
		appIDs[app.ID] = true
		if ref, ok := app.Refs["windows"]; ok && ref != "" {
			wingetRefs[ref] = true
		}
	}

	for _, moduleID := range mf.ConfigModules {
		mod, exists := catalog[moduleID]
		if !exists {
			continue
		}

		// Only synthesize if the module has pathExists entries.
		if len(mod.Matches.PathExists) == 0 {
			continue
		}

		shortID := stripAppsPrefix(moduleID)

		// Check if a matching app already exists.
		if appIDs[shortID] {
			continue
		}
		if hasWingetOverlap(mod.Matches.Winget, wingetRefs) {
			continue
		}

		// Synthesize a manual app entry.
		app := manifest.App{
			ID:          shortID,
			DisplayName: mod.DisplayName,
			Manual: &manifest.ManualApp{
				VerifyPath: mod.Matches.PathExists[0],
			},
		}

		mf.Apps = append(mf.Apps, app)
		appIDs[shortID] = true
	}
}

// stripAppsPrefix removes the "apps." prefix from a module ID if present.
func stripAppsPrefix(moduleID string) string {
	return strings.TrimPrefix(moduleID, "apps.")
}

// hasWingetOverlap returns true if any of the module's winget refs match an
// existing app's winget ref.
func hasWingetOverlap(moduleWinget []string, existingRefs map[string]bool) bool {
	for _, ref := range moduleWinget {
		if existingRefs[ref] {
			return true
		}
	}
	return false
}
