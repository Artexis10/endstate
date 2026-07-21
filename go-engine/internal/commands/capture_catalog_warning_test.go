// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/driver"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
)

// warningByCode returns the warning with the given code, or nil.
func warningByCode(warnings []CommandWarning, code string) *CommandWarning {
	for i := range warnings {
		if warnings[i].Code == code {
			return &warnings[i]
		}
	}
	return nil
}

// withRepoRoot overrides only the repo-root seam, leaving the catalog loader
// alone, so a test can exercise the unresolvable-root path.
func withRepoRoot(root string, f func()) {
	orig := resolveRepoRootFn
	resolveRepoRootFn = func() string { return root }
	defer func() { resolveRepoRootFn = orig }()
	f()
}

// TestRunCapture_WarnsWhenRepoRootUnresolvable is the regression for the silent
// degradation this change fixes.
//
// A PATH-invoked binary resolves no repo root, so capture matched no config
// modules and emitted an app-list-only manifest with NO warning — indistinguishable
// from a successful capture. Endstate's differentiator over a package-list export
// is the settings, so losing them must never be invisible.
func TestRunCapture_WarnsWhenRepoRootUnresolvable(t *testing.T) {
	withCaptureEnumerators(t, map[string]fakeInstalledEnumerator{
		"winget": {packages: []driver.InstalledPackage{{Ref: "Git.Git", DisplayName: "Git", Version: "2.45"}}},
	}, nil)

	withRepoRoot("", func() {
		raw, eerr := RunCapture(CaptureFlags{Out: filepath.Join(t.TempDir(), "captured.jsonc")})
		if eerr != nil {
			t.Fatalf("RunCapture: %v", eerr)
		}
		warnings := raw.(*CaptureResult).Warnings
		if warningByCode(warnings, "module_catalog_unavailable") == nil {
			t.Fatalf("expected module_catalog_unavailable warning, got %+v", warnings)
		}
	})
}

// TestRunCapture_FailsWhenCatalogLoadFails pins the other failure mode to an
// error rather than a warning.
//
// An unresolvable root and an unreadable catalog are different conditions: the
// first is an install that never had a catalog (warn, still produce a useful
// app list), the second is a catalog that exists but is broken (fail loudly —
// silently downgrading would hide real corruption).
func TestRunCapture_FailsWhenCatalogLoadFails(t *testing.T) {
	withCaptureEnumerators(t, map[string]fakeInstalledEnumerator{
		"winget": {packages: []driver.InstalledPackage{{Ref: "Git.Git", DisplayName: "Git", Version: "2.45"}}},
	}, nil)

	withMockCatalog(nil, errors.New("catalog unreadable"), func() {
		_, eerr := RunCapture(CaptureFlags{Out: filepath.Join(t.TempDir(), "captured.jsonc")})
		if eerr == nil {
			t.Fatal("expected an unreadable catalog to fail the capture, got success")
		}
	})
}

// TestRunCapture_NoWarningWhenCatalogWiredButEmpty pins the distinction the
// warning condition depends on. A catalog that loads with zero modules is a
// correctly wired install that simply matched nothing — not a misconfiguration.
// Conflating the two would fire this warning on every capture that happens to
// match no modules, training users to ignore it.
func TestRunCapture_NoWarningWhenCatalogWiredButEmpty(t *testing.T) {
	withCaptureEnumerators(t, map[string]fakeInstalledEnumerator{
		"winget": {packages: []driver.InstalledPackage{{Ref: "Git.Git", DisplayName: "Git", Version: "2.45"}}},
	}, nil)

	withMockCatalog(map[string]*modules.Module{}, nil, func() {
		raw, eerr := RunCapture(CaptureFlags{Out: filepath.Join(t.TempDir(), "captured.jsonc")})
		if eerr != nil {
			t.Fatalf("RunCapture: %v", eerr)
		}
		warnings := raw.(*CaptureResult).Warnings
		if w := warningByCode(warnings, "module_catalog_unavailable"); w != nil {
			t.Fatalf("a wired-but-empty catalog must not warn, got %+v", w)
		}
	})
}

// TestRunCapture_NoWarningUnderSanitize verifies --sanitize, which deliberately
// attaches no config modules, does not produce a misconfiguration warning.
func TestRunCapture_NoWarningUnderSanitize(t *testing.T) {
	withCaptureEnumerators(t, map[string]fakeInstalledEnumerator{
		"winget": {packages: []driver.InstalledPackage{{Ref: "Git.Git", DisplayName: "Git", Version: "2.45"}}},
	}, nil)

	withRepoRoot("", func() {
		raw, eerr := RunCapture(CaptureFlags{Out: filepath.Join(t.TempDir(), "captured.jsonc"), Sanitize: true})
		if eerr != nil {
			t.Fatalf("RunCapture: %v", eerr)
		}
		warnings := raw.(*CaptureResult).Warnings
		if w := warningByCode(warnings, "module_catalog_unavailable"); w != nil {
			t.Fatalf("--sanitize opts out of config modules deliberately; must not warn, got %+v", w)
		}
	})
}
