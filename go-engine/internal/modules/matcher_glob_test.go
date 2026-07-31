// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package modules

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/manifest"
)

// TestMatchModulesForApps_PathExistsExpandsGlobs locks glob support for
// matches.pathExists.
//
// Six catalog entries declare versioned install roots that only exist as
// wildcards — e.g. after-effects pins
// "%ProgramFiles%\Adobe\Adobe After Effects*\Support Files\AfterFX.exe" and
// ableton-live pins "%ProgramData%\Ableton\Live*\Program\Ableton Live*.exe".
// A bare os.Stat on those patterns can never succeed, so the modules were
// silently undetectable no matter what was installed.
func TestMatchModulesForApps_PathExistsExpandsGlobs(t *testing.T) {
	root := t.TempDir()
	installed := filepath.Join(root, "Adobe After Effects 2026", "Support Files")
	if err := os.MkdirAll(installed, 0o755); err != nil {
		t.Fatalf("seed install root: %v", err)
	}
	if err := os.WriteFile(filepath.Join(installed, "AfterFX.exe"), []byte("x"), 0o644); err != nil {
		t.Fatalf("seed executable: %v", err)
	}

	pattern := filepath.Join(root, "Adobe After Effects*", "Support Files", "AfterFX.exe")
	catalog := map[string]*Module{
		"apps.after-effects": capturable("apps.after-effects", MatchCriteria{PathExists: []string{pattern}}),
	}
	apps := []manifest.App{{ID: "git-git", Refs: map[string]string{"windows": "Git.Git"}}}

	matched := MatchModulesForApps(catalog, apps)

	if len(matched) != 1 {
		t.Fatalf("expected the globbed pathExists module to match, got %d: %v", len(matched), moduleIDs(matched))
	}
}

// TestMatchModulesForApps_PathExistsGlobWithoutMatchIsNotMatched guards the
// other direction: a wildcard that resolves to nothing must not match, so glob
// support cannot turn into a blanket "any pattern counts" fallback.
func TestMatchModulesForApps_PathExistsGlobWithoutMatchIsNotMatched(t *testing.T) {
	root := t.TempDir()

	pattern := filepath.Join(root, "Adobe After Effects*", "Support Files", "AfterFX.exe")
	catalog := map[string]*Module{
		"apps.after-effects": capturable("apps.after-effects", MatchCriteria{PathExists: []string{pattern}}),
	}
	apps := []manifest.App{{ID: "git-git", Refs: map[string]string{"windows": "Git.Git"}}}

	if matched := MatchModulesForApps(catalog, apps); len(matched) != 0 {
		t.Fatalf("expected no match for an unresolved glob, got %v", moduleIDs(matched))
	}
}

// TestMatchModulesForApps_PathExistsLiteralStillMatches keeps the non-glob path
// byte-identical in behaviour — 181 of the 187 catalog pathExists entries are
// literal paths and must not regress.
func TestMatchModulesForApps_PathExistsLiteralStillMatches(t *testing.T) {
	root := t.TempDir()
	config := filepath.Join(root, "profiles.ini")
	if err := os.WriteFile(config, []byte("x"), 0o644); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	catalog := map[string]*Module{
		"apps.thunderbird": capturable("apps.thunderbird", MatchCriteria{PathExists: []string{config}}),
	}
	apps := []manifest.App{{ID: "git-git", Refs: map[string]string{"windows": "Git.Git"}}}

	if matched := MatchModulesForApps(catalog, apps); len(matched) != 1 {
		t.Fatalf("expected the literal pathExists module to match, got %v", moduleIDs(matched))
	}
}

// TestMatchModulesForApps_PathExistsMatchesRepackagedInstall is the
// Artexis10/endstate#208 regression.
//
// Site-repackaged installers ("neoPackage Mozilla Thunderbird x64 140.13.0")
// carry their own ARP key and publisher, so they are a genuinely different
// package identity and never match matches.winget. Per the durable-identity
// rule that display names never bind identities, the attaching signal for a
// SETTINGS module is that the app's config is present on disk — pathExists —
// not how the app was installed.
func TestMatchModulesForApps_PathExistsMatchesRepackagedInstall(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "profiles.ini"), []byte("x"), 0o644); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	catalog := map[string]*Module{
		"apps.thunderbird": capturable("apps.thunderbird", MatchCriteria{
			Winget:     []string{"Mozilla.Thunderbird"},
			PathExists: []string{filepath.Join(root, "profiles.ini")},
		}),
	}
	// The repackaged entry: winget does not know it, so capture's ARP-inventory
	// union emits it with no windows ref at all.
	apps := []manifest.App{{ID: "neopackage-mozilla-thunderbird-x64", Refs: map[string]string{}}}

	if matched := MatchModulesForApps(catalog, apps); len(matched) != 1 {
		t.Fatalf("expected thunderbird to attach via config presence, got %v", moduleIDs(matched))
	}
}
