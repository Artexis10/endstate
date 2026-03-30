// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package modules

import (
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/manifest"
)

// ---------------------------------------------------------------------------
// SynthesizeAppsFromModules
// ---------------------------------------------------------------------------

// TestSynthesizeApps_Basic verifies that a module with pathExists and no
// matching app produces a synthesized manual app entry.
func TestSynthesizeApps_Basic(t *testing.T) {
	catalog := map[string]*Module{
		"apps.lightroom-classic": {
			ID:          "apps.lightroom-classic",
			DisplayName: "Adobe Lightroom Classic",
			Matches: MatchCriteria{
				PathExists: []string{`C:\Program Files\Adobe\Adobe Lightroom Classic\Lightroom.exe`},
			},
		},
	}

	mf := &manifest.Manifest{
		ConfigModules: []string{"apps.lightroom-classic"},
		Apps:          []manifest.App{},
	}

	SynthesizeAppsFromModules(mf, catalog)

	if len(mf.Apps) != 1 {
		t.Fatalf("expected 1 synthesized app, got %d", len(mf.Apps))
	}
	app := mf.Apps[0]
	if app.ID != "lightroom-classic" {
		t.Errorf("expected ID=lightroom-classic, got %q", app.ID)
	}
	if app.DisplayName != "Adobe Lightroom Classic" {
		t.Errorf("expected DisplayName from module, got %q", app.DisplayName)
	}
	if app.Manual == nil {
		t.Fatal("expected Manual to be set")
	}
	if app.Manual.VerifyPath != `C:\Program Files\Adobe\Adobe Lightroom Classic\Lightroom.exe` {
		t.Errorf("expected verifyPath from module pathExists[0], got %q", app.Manual.VerifyPath)
	}
}

// TestSynthesizeApps_NoDuplicate verifies that if a matching app already
// exists by ID, no synthesis occurs.
func TestSynthesizeApps_NoDuplicate(t *testing.T) {
	catalog := map[string]*Module{
		"apps.lightroom-classic": {
			ID:          "apps.lightroom-classic",
			DisplayName: "Adobe Lightroom Classic",
			Matches: MatchCriteria{
				PathExists: []string{`C:\Program Files\Adobe\Lightroom.exe`},
			},
		},
	}

	mf := &manifest.Manifest{
		ConfigModules: []string{"apps.lightroom-classic"},
		Apps: []manifest.App{
			{
				ID: "lightroom-classic",
				Manual: &manifest.ManualApp{
					VerifyPath: `C:\custom\path.exe`,
				},
			},
		},
	}

	SynthesizeAppsFromModules(mf, catalog)

	if len(mf.Apps) != 1 {
		t.Errorf("expected no synthesis (app already exists), got %d apps", len(mf.Apps))
	}
	// Original app should be unchanged.
	if mf.Apps[0].Manual.VerifyPath != `C:\custom\path.exe` {
		t.Errorf("existing app was modified")
	}
}

// TestSynthesizeApps_WingetMatch verifies that if a module's winget ref
// matches an existing app's winget ref, no synthesis occurs.
func TestSynthesizeApps_WingetMatch(t *testing.T) {
	catalog := map[string]*Module{
		"apps.vscode": {
			ID:          "apps.vscode",
			DisplayName: "Visual Studio Code",
			Matches: MatchCriteria{
				Winget:     []string{"Microsoft.VisualStudioCode"},
				PathExists: []string{`C:\Program Files\Microsoft VS Code\Code.exe`},
			},
		},
	}

	mf := &manifest.Manifest{
		ConfigModules: []string{"apps.vscode"},
		Apps: []manifest.App{
			{
				ID:   "vscode",
				Refs: map[string]string{"windows": "Microsoft.VisualStudioCode"},
			},
		},
	}

	SynthesizeAppsFromModules(mf, catalog)

	if len(mf.Apps) != 1 {
		t.Errorf("expected no synthesis (winget ref matches existing app), got %d apps", len(mf.Apps))
	}
}

// TestSynthesizeApps_NoPathExists verifies that modules with only winget
// matchers (no pathExists) are NOT synthesized.
func TestSynthesizeApps_NoPathExists(t *testing.T) {
	catalog := map[string]*Module{
		"apps.some-app": {
			ID:          "apps.some-app",
			DisplayName: "Some App",
			Matches: MatchCriteria{
				Winget: []string{"Vendor.SomeApp"},
			},
		},
	}

	mf := &manifest.Manifest{
		ConfigModules: []string{"apps.some-app"},
		Apps:          []manifest.App{},
	}

	SynthesizeAppsFromModules(mf, catalog)

	if len(mf.Apps) != 0 {
		t.Errorf("expected no synthesis for module without pathExists, got %d apps", len(mf.Apps))
	}
}

// TestSynthesizeApps_IDStripping verifies that the "apps." prefix is stripped
// from module IDs when creating synthesized app entries.
func TestSynthesizeApps_IDStripping(t *testing.T) {
	catalog := map[string]*Module{
		"apps.adobe-photoshop": {
			ID:          "apps.adobe-photoshop",
			DisplayName: "Adobe Photoshop",
			Matches: MatchCriteria{
				PathExists: []string{`C:\Program Files\Adobe\Photoshop.exe`},
			},
		},
	}

	mf := &manifest.Manifest{
		ConfigModules: []string{"apps.adobe-photoshop"},
		Apps:          []manifest.App{},
	}

	SynthesizeAppsFromModules(mf, catalog)

	if len(mf.Apps) != 1 {
		t.Fatalf("expected 1 app, got %d", len(mf.Apps))
	}
	if mf.Apps[0].ID != "adobe-photoshop" {
		t.Errorf("expected ID=adobe-photoshop (apps. stripped), got %q", mf.Apps[0].ID)
	}
}

// TestSynthesizeApps_NoAppsPrefix verifies that module IDs without the "apps."
// prefix are used as-is for the synthesized app ID.
func TestSynthesizeApps_NoAppsPrefix(t *testing.T) {
	catalog := map[string]*Module{
		"custom-tool": {
			ID:          "custom-tool",
			DisplayName: "Custom Tool",
			Matches: MatchCriteria{
				PathExists: []string{`C:\Tools\custom.exe`},
			},
		},
	}

	mf := &manifest.Manifest{
		ConfigModules: []string{"custom-tool"},
		Apps:          []manifest.App{},
	}

	SynthesizeAppsFromModules(mf, catalog)

	if len(mf.Apps) != 1 {
		t.Fatalf("expected 1 app, got %d", len(mf.Apps))
	}
	if mf.Apps[0].ID != "custom-tool" {
		t.Errorf("expected ID=custom-tool (no prefix to strip), got %q", mf.Apps[0].ID)
	}
}

// TestSynthesizeApps_MultiplePathExists verifies that the first pathExists
// entry is used as verifyPath when multiple are present.
func TestSynthesizeApps_MultiplePathExists(t *testing.T) {
	catalog := map[string]*Module{
		"apps.multi-path": {
			ID:          "apps.multi-path",
			DisplayName: "Multi Path App",
			Matches: MatchCriteria{
				PathExists: []string{
					`C:\Program Files\App\primary.exe`,
					`C:\Program Files (x86)\App\secondary.exe`,
				},
			},
		},
	}

	mf := &manifest.Manifest{
		ConfigModules: []string{"apps.multi-path"},
		Apps:          []manifest.App{},
	}

	SynthesizeAppsFromModules(mf, catalog)

	if len(mf.Apps) != 1 {
		t.Fatalf("expected 1 app, got %d", len(mf.Apps))
	}
	if mf.Apps[0].Manual.VerifyPath != `C:\Program Files\App\primary.exe` {
		t.Errorf("expected first pathExists as verifyPath, got %q", mf.Apps[0].Manual.VerifyPath)
	}
}

// TestSynthesizeApps_UnknownModule verifies that unknown module IDs are
// silently skipped (no crash, no synthesis).
func TestSynthesizeApps_UnknownModule(t *testing.T) {
	catalog := map[string]*Module{}

	mf := &manifest.Manifest{
		ConfigModules: []string{"apps.nonexistent"},
		Apps:          []manifest.App{},
	}

	SynthesizeAppsFromModules(mf, catalog)

	if len(mf.Apps) != 0 {
		t.Errorf("expected no synthesis for unknown module, got %d apps", len(mf.Apps))
	}
}

// TestSynthesizeApps_EmptyConfigModules verifies no-op with empty configModules.
func TestSynthesizeApps_EmptyConfigModules(t *testing.T) {
	catalog := map[string]*Module{
		"apps.test": {
			ID:      "apps.test",
			Matches: MatchCriteria{PathExists: []string{`C:\test.exe`}},
		},
	}

	mf := &manifest.Manifest{
		Apps: []manifest.App{},
	}

	SynthesizeAppsFromModules(mf, catalog)

	if len(mf.Apps) != 0 {
		t.Errorf("expected no synthesis with empty configModules, got %d apps", len(mf.Apps))
	}
}

// ---------------------------------------------------------------------------
// SynthesizeAppsFromModules — excludeConfigs filtering
// ---------------------------------------------------------------------------

// TestSynthesizeApps_ExcludedModuleIsNotSynthesized verifies that a module
// listed in excludeConfigs is not synthesized into mf.Apps.
func TestSynthesizeApps_ExcludedModuleIsNotSynthesized(t *testing.T) {
	catalog := map[string]*Module{
		"apps.lightroom-classic": {
			ID:          "apps.lightroom-classic",
			DisplayName: "Adobe Lightroom Classic",
			Matches: MatchCriteria{
				PathExists: []string{`C:\Program Files\Adobe\Adobe Lightroom Classic\Lightroom.exe`},
			},
		},
	}

	mf := &manifest.Manifest{
		ConfigModules:  []string{"apps.lightroom-classic"},
		ExcludeConfigs: []string{"apps.lightroom-classic"},
		Apps:           []manifest.App{},
	}

	SynthesizeAppsFromModules(mf, catalog)

	if len(mf.Apps) != 0 {
		t.Errorf("expected no synthesis for excluded module, got %d apps", len(mf.Apps))
	}
}

// TestSynthesizeApps_ExcludedByShortIDIsNotSynthesized verifies that a short
// ID in excludeConfigs (e.g. "lightroom-classic") excludes the qualified
// module ("apps.lightroom-classic") from synthesis.
func TestSynthesizeApps_ExcludedByShortIDIsNotSynthesized(t *testing.T) {
	catalog := map[string]*Module{
		"apps.lightroom-classic": {
			ID:          "apps.lightroom-classic",
			DisplayName: "Adobe Lightroom Classic",
			Matches: MatchCriteria{
				PathExists: []string{`C:\Program Files\Adobe\Lightroom.exe`},
			},
		},
	}

	mf := &manifest.Manifest{
		ConfigModules:  []string{"apps.lightroom-classic"},
		ExcludeConfigs: []string{"lightroom-classic"}, // short ID
		Apps:           []manifest.App{},
	}

	SynthesizeAppsFromModules(mf, catalog)

	if len(mf.Apps) != 0 {
		t.Errorf("expected no synthesis for module excluded by short ID, got %d apps", len(mf.Apps))
	}
}

// TestSynthesizeApps_NonExcludedModuleStillSynthesized verifies that when only
// one of two modules is excluded, the other is still synthesized.
func TestSynthesizeApps_NonExcludedModuleStillSynthesized(t *testing.T) {
	catalog := map[string]*Module{
		"apps.lightroom-classic": {
			ID:          "apps.lightroom-classic",
			DisplayName: "Adobe Lightroom Classic",
			Matches: MatchCriteria{
				PathExists: []string{`C:\Program Files\Adobe\Lightroom.exe`},
			},
		},
		"apps.photoshop": {
			ID:          "apps.photoshop",
			DisplayName: "Adobe Photoshop",
			Matches: MatchCriteria{
				PathExists: []string{`C:\Program Files\Adobe\Photoshop.exe`},
			},
		},
	}

	mf := &manifest.Manifest{
		ConfigModules:  []string{"apps.lightroom-classic", "apps.photoshop"},
		ExcludeConfigs: []string{"apps.lightroom-classic"},
		Apps:           []manifest.App{},
	}

	SynthesizeAppsFromModules(mf, catalog)

	if len(mf.Apps) != 1 {
		t.Fatalf("expected 1 synthesized app (photoshop only), got %d", len(mf.Apps))
	}
	if mf.Apps[0].ID != "photoshop" {
		t.Errorf("expected ID=photoshop, got %q", mf.Apps[0].ID)
	}
}

// TestSynthesizeApps_EmptyCatalog verifies no-op with empty catalog.
func TestSynthesizeApps_EmptyCatalog(t *testing.T) {
	mf := &manifest.Manifest{
		ConfigModules: []string{"apps.test"},
		Apps:          []manifest.App{},
	}

	SynthesizeAppsFromModules(mf, nil)

	if len(mf.Apps) != 0 {
		t.Errorf("expected no synthesis with nil catalog, got %d apps", len(mf.Apps))
	}
}
