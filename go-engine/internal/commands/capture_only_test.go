// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/driver"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
)

// twoAppEnumerator seeds capture with two detected apps so a selection has
// something to exclude.
func twoAppEnumerator() map[string]fakeInstalledEnumerator {
	return map[string]fakeInstalledEnumerator{
		"winget": {packages: []driver.InstalledPackage{
			{Ref: "Git.Git", DisplayName: "Git", Version: "2.45"},
			{Ref: "Microsoft.VisualStudioCode", DisplayName: "Visual Studio Code", Version: "1.90"},
		}},
	}
}

// ---------------------------------------------------------------------------
// App selection
// ---------------------------------------------------------------------------

func TestRunCapture_OnlySelectsNamedApps(t *testing.T) {
	withCaptureEnumerators(t, twoAppEnumerator(), nil)
	out := filepath.Join(t.TempDir(), "captured.jsonc")

	withMockCatalog(map[string]*modules.Module{}, nil, func() {
		raw, eerr := RunCapture(CaptureFlags{Out: out, Only: "git-git"})
		if eerr != nil {
			t.Fatalf("RunCapture: %v", eerr)
		}
		result := raw.(*CaptureResult)
		// NOTE: CaptureApp.ID is the package REF (Git.Git), while --only matches
		// the manifest app id (git-git). Assert on the ref here and on the manifest
		// id below, because the envelope does not currently surface the token a
		// user must actually pass to --only.
		if len(result.AppsIncluded) != 1 || result.AppsIncluded[0].ID != "Git.Git" {
			t.Fatalf("expected only Git, got %+v", result.AppsIncluded)
		}
		// totalFound stays pre-filter — that is what makes a subset legible as a
		// subset rather than looking like a machine with one app on it.
		if result.Counts.TotalFound != 2 {
			t.Errorf("totalFound should report what is on the machine (2), got %d", result.Counts.TotalFound)
		}
		if result.Counts.Included != 1 {
			t.Errorf("included = %d, want 1", result.Counts.Included)
		}
		if got := result.Counts.TotalFound - result.Counts.Included; got != result.Counts.Skipped {
			t.Errorf("totalFound must equal included + skipped; skipped=%d want %d", result.Counts.Skipped, got)
		}
	})

	m := readCapturedManifest(t, out)
	if len(m.Apps) != 1 || m.Apps[0].ID != "git-git" {
		t.Fatalf("written manifest should hold exactly the selected app id, got %+v", m.Apps)
	}
}

// TestRunCapture_OnlyWithUpdateAddsRatherThanTruncates guards the destructive
// misreading. `capture --only git-git --update` must ADD git-git to an existing
// manifest, not reduce that manifest to git-git alone — which is why the filter
// runs before the merge rather than after it.
func TestRunCapture_OnlyWithUpdateAddsRatherThanTruncates(t *testing.T) {
	withCaptureEnumerators(t, twoAppEnumerator(), nil)
	dir := t.TempDir()
	existing := filepath.Join(dir, "existing.jsonc")
	if err := os.WriteFile(existing, []byte(`{
  "version": 1,
  "name": "existing",
  "apps": [
    {"id": "seven-zip", "refs": {"windows": "7zip.7zip"}}
  ]
}`), 0644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "captured.jsonc")

	withMockCatalog(map[string]*modules.Module{}, nil, func() {
		_, eerr := RunCapture(CaptureFlags{Out: out, Manifest: existing, Update: true, Only: "git-git"})
		if eerr != nil {
			t.Fatalf("RunCapture: %v", eerr)
		}
	})

	m := readCapturedManifest(t, out)
	ids := map[string]bool{}
	for _, app := range m.Apps {
		ids[app.ID] = true
	}
	if !ids["seven-zip"] {
		t.Error("the existing manifest's app was destroyed by --only --update")
	}
	if !ids["git-git"] {
		t.Error("the selected app was not added")
	}
	if ids["microsoft-visualstudiocode"] {
		t.Error("an unselected detected app leaked into the merge")
	}
}

// ---------------------------------------------------------------------------
// Validation — every rejection happens before anything is written
// ---------------------------------------------------------------------------

func TestRunCapture_OnlyRejectsUnknownAppID(t *testing.T) {
	withCaptureEnumerators(t, twoAppEnumerator(), nil)
	out := filepath.Join(t.TempDir(), "captured.jsonc")

	withMockCatalog(map[string]*modules.Module{}, nil, func() {
		_, eerr := RunCapture(CaptureFlags{Out: out, Only: "git-git,not-a-real-app"})
		if eerr == nil {
			t.Fatal("expected a validation error for an unknown app id")
		}
		if eerr.Code != "MANIFEST_VALIDATION_ERROR" {
			t.Errorf("code = %q, want MANIFEST_VALIDATION_ERROR", eerr.Code)
		}
	})

	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Error("a rejected selection must not write a manifest")
	}
}

func TestRunCapture_OnlyRejectsModuleOnlySelection(t *testing.T) {
	withCaptureEnumerators(t, twoAppEnumerator(), nil)
	out := filepath.Join(t.TempDir(), "captured.jsonc")

	withMockCatalog(map[string]*modules.Module{}, nil, func() {
		_, eerr := RunCapture(CaptureFlags{Out: out, Only: "apps.vscode"})
		if eerr == nil {
			t.Fatal("expected a module-only selection to be rejected")
		}
	})

	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Error("a rejected selection must not write a manifest")
	}
}

func TestRunCapture_OnlyRejectsEmptyAfterNormalisation(t *testing.T) {
	withCaptureEnumerators(t, twoAppEnumerator(), nil)
	out := filepath.Join(t.TempDir(), "captured.jsonc")

	withMockCatalog(map[string]*modules.Module{}, nil, func() {
		if _, eerr := RunCapture(CaptureFlags{Out: out, Only: "  ,  "}); eerr == nil {
			t.Fatal("expected an empty-after-normalisation selection to be rejected")
		}
	})
}

// ---------------------------------------------------------------------------
// Module scoping — the payload-leak regression
// ---------------------------------------------------------------------------

// TestRunCapture_OnlyExcludesPathExistsOnlyModules is the Discovery-B regression.
//
// matcher.go checks matches.pathExists against the filesystem without consulting
// the app list, and 141 of 357 catalog modules declare it. A selection that only
// filtered apps would still bundle configs for every pathExists module present on
// the machine — handing a recipient far more than was selected.
//
// This test fails against an app-filter-only implementation, which is the point.
func TestRunCapture_OnlyExcludesPathExistsOnlyModules(t *testing.T) {
	withCaptureEnumerators(t, twoAppEnumerator(), nil)
	out := filepath.Join(t.TempDir(), "captured.jsonc")

	catalog := map[string]*modules.Module{
		"apps.git": {
			ID:      "apps.git",
			Matches: modules.MatchCriteria{Winget: []string{"Git.Git"}},
			Capture: &modules.CaptureDef{Files: []modules.CaptureFile{{Source: "s", Dest: "d"}}},
		},
		// Matches only because "." exists on every filesystem; no ref match to the
		// selection.
		"apps.everywhere": {
			ID:      "apps.everywhere",
			Matches: modules.MatchCriteria{PathExists: []string{"."}},
			Capture: &modules.CaptureDef{Files: []modules.CaptureFile{{Source: "s", Dest: "d"}}},
		},
	}

	withMockCatalog(catalog, nil, func() {
		raw, eerr := RunCapture(CaptureFlags{Out: out, Only: "git-git"})
		if eerr != nil {
			t.Fatalf("RunCapture: %v", eerr)
		}
		result := raw.(*CaptureResult)
		for _, mod := range result.ConfigModules {
			if mod.ID == "apps.everywhere" {
				t.Fatalf("a pathExists-only module leaked into an explicit selection: %+v", result.ConfigModules)
			}
		}
	})
}

// TestRunCapture_OnlyIncludesExplicitlyNamedModule verifies the escape hatch:
// a module excluded by ref matching can still be requested by name.
func TestRunCapture_OnlyIncludesExplicitlyNamedModule(t *testing.T) {
	withCaptureEnumerators(t, twoAppEnumerator(), nil)
	out := filepath.Join(t.TempDir(), "captured.jsonc")

	catalog := map[string]*modules.Module{
		"apps.git": {
			ID:      "apps.git",
			Matches: modules.MatchCriteria{Winget: []string{"Git.Git"}},
			Capture: &modules.CaptureDef{Files: []modules.CaptureFile{{Source: "s", Dest: "d"}}},
		},
		"apps.everywhere": {
			ID:      "apps.everywhere",
			Matches: modules.MatchCriteria{PathExists: []string{"."}},
			Capture: &modules.CaptureDef{Files: []modules.CaptureFile{{Source: "s", Dest: "d"}}},
		},
	}

	withMockCatalog(catalog, nil, func() {
		if _, eerr := RunCapture(CaptureFlags{Out: out, Only: "git-git,apps.everywhere"}); eerr != nil {
			t.Fatalf("naming a module outright must be accepted: %v", eerr)
		}
	})
}

func TestRunCapture_OnlyRejectsUnknownModuleID(t *testing.T) {
	withCaptureEnumerators(t, twoAppEnumerator(), nil)
	out := filepath.Join(t.TempDir(), "captured.jsonc")

	catalog := map[string]*modules.Module{
		"apps.git": {
			ID:      "apps.git",
			Matches: modules.MatchCriteria{Winget: []string{"Git.Git"}},
			Capture: &modules.CaptureDef{Files: []modules.CaptureFile{{Source: "s", Dest: "d"}}},
		},
	}

	withMockCatalog(catalog, nil, func() {
		_, eerr := RunCapture(CaptureFlags{Out: out, Only: "git-git,apps.not-a-module"})
		if eerr == nil {
			t.Fatal("expected an unknown module id to be rejected")
		}
		// A mistyped token is user input, not a bundle failure — the code must
		// survive the plain-error return of the finalize path.
		if eerr.Code != "MANIFEST_VALIDATION_ERROR" {
			t.Errorf("code = %q, want MANIFEST_VALIDATION_ERROR", eerr.Code)
		}
	})
}

// ---------------------------------------------------------------------------
// Token grammar
// ---------------------------------------------------------------------------

func TestParseCaptureOnly_SplitsAppAndModuleNamespaces(t *testing.T) {
	sel := parseCaptureOnly("git-git, apps.vscode ,microsoft-visualstudiocode,apps.git")

	if len(sel.appIDs) != 2 || sel.appIDs[0] != "git-git" || sel.appIDs[1] != "microsoft-visualstudiocode" {
		t.Errorf("appIDs = %v", sel.appIDs)
	}
	if len(sel.moduleIDs) != 2 || sel.moduleIDs[0] != "apps.vscode" || sel.moduleIDs[1] != "apps.git" {
		t.Errorf("moduleIDs = %v", sel.moduleIDs)
	}
}

// TestParseCaptureOnly_BareTokenIsAlwaysAnApp pins the deliberate asymmetry with
// --restore-filter, which does accept a bare short module id. Here bare always
// means app, because "vscode" would otherwise be ambiguous between the two.
func TestParseCaptureOnly_BareTokenIsAlwaysAnApp(t *testing.T) {
	sel := parseCaptureOnly("vscode")

	if len(sel.appIDs) != 1 || sel.appIDs[0] != "vscode" {
		t.Errorf("bare token must parse as an app id, got appIDs=%v moduleIDs=%v", sel.appIDs, sel.moduleIDs)
	}
	if len(sel.moduleIDs) != 0 {
		t.Errorf("bare token must not parse as a module id, got %v", sel.moduleIDs)
	}
}

func TestParseCaptureOnly_EmptyIsInactive(t *testing.T) {
	if parseCaptureOnly("").active() {
		t.Error("an empty --only must be inactive")
	}
	if parseCaptureOnly(" , ").active() {
		t.Error("a blank-only --only must be inactive")
	}
}
