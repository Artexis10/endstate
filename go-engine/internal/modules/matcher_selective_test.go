// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package modules

import (
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/manifest"
)

// capturable builds a minimal module that passes the "has a capture section"
// gate, so these tests exercise matching rather than that filter.
func capturable(id string, m MatchCriteria) *Module {
	return &Module{
		ID:      id,
		Matches: m,
		Capture: &CaptureDef{Files: []CaptureFile{{Source: "s", Dest: "d"}}},
	}
}

// TestMatchModulesForAppsSelective_IgnoresPathExists is the Discovery-B guard.
//
// 141 of 357 catalog modules declare pathExists, and the shared matcher checks
// it against the filesystem without consulting the app list at all. Under an
// explicit selection that would bundle configs for most installed apps
// regardless of what the user picked — a payload leak in exactly the flow where
// the artifact is handed to another person.
func TestMatchModulesForAppsSelective_IgnoresPathExists(t *testing.T) {
	catalog := map[string]*Module{
		"apps.git":        capturable("apps.git", MatchCriteria{Winget: []string{"Git.Git"}}),
		"apps.everywhere": capturable("apps.everywhere", MatchCriteria{PathExists: []string{"."}}),
	}
	apps := []manifest.App{{ID: "git-git", Refs: map[string]string{"windows": "Git.Git"}}}

	matched := MatchModulesForAppsSelective(catalog, apps)

	if len(matched) != 1 {
		t.Fatalf("expected exactly the ref-matched module, got %d: %v", len(matched), moduleIDs(matched))
	}
	if matched[0].ID != "apps.git" {
		t.Errorf("expected apps.git, got %s", matched[0].ID)
	}
}

// TestMatchModulesForApps_StillIncludesPathExists locks the unfiltered path so
// the selective variant is provably additive — capture and apply without --only
// must behave exactly as before.
func TestMatchModulesForApps_StillIncludesPathExists(t *testing.T) {
	catalog := map[string]*Module{
		"apps.git":        capturable("apps.git", MatchCriteria{Winget: []string{"Git.Git"}}),
		"apps.everywhere": capturable("apps.everywhere", MatchCriteria{PathExists: []string{"."}}),
	}
	apps := []manifest.App{{ID: "git-git", Refs: map[string]string{"windows": "Git.Git"}}}

	matched := MatchModulesForApps(catalog, apps)

	if len(matched) != 2 {
		t.Fatalf("legacy matcher must still include pathExists modules, got %d: %v", len(matched), moduleIDs(matched))
	}
}

// TestMatchModulesForAppsSelective_MatchesChocolateyRefs verifies the selective
// variant narrows only the pathExists axis, not package-ref matching.
func TestMatchModulesForAppsSelective_MatchesChocolateyRefs(t *testing.T) {
	catalog := map[string]*Module{
		"apps.git": capturable("apps.git", MatchCriteria{Chocolatey: []string{"git"}}),
	}
	apps := []manifest.App{
		{ID: "git", Driver: "chocolatey", Refs: map[string]string{"windows": "git"}},
	}

	matched := MatchModulesForAppsSelective(catalog, apps)

	if len(matched) != 1 {
		t.Fatalf("expected the chocolatey-matched module, got %d: %v", len(matched), moduleIDs(matched))
	}
}

// TestMatchModulesForAppsSelective_NoRefMatchYieldsNothing confirms an empty
// result rather than a filesystem-driven fallback.
func TestMatchModulesForAppsSelective_NoRefMatchYieldsNothing(t *testing.T) {
	catalog := map[string]*Module{
		"apps.everywhere": capturable("apps.everywhere", MatchCriteria{PathExists: []string{"."}}),
	}
	apps := []manifest.App{{ID: "git-git", Refs: map[string]string{"windows": "Git.Git"}}}

	if matched := MatchModulesForAppsSelective(catalog, apps); len(matched) != 0 {
		t.Fatalf("expected no matches, got %v", moduleIDs(matched))
	}
}

func moduleIDs(mods []*Module) []string {
	out := make([]string, 0, len(mods))
	for _, m := range mods {
		out = append(out, m.ID)
	}
	return out
}
