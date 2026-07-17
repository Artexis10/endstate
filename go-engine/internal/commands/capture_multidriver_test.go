// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/driver"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/realizer"
)

type fakeInstalledEnumerator struct {
	packages []driver.InstalledPackage
	err      error
}

func (f fakeInstalledEnumerator) EnumerateInstalled() ([]driver.InstalledPackage, error) {
	return f.packages, f.err
}

func withCaptureEnumerators(
	t *testing.T,
	byDriver map[string]fakeInstalledEnumerator,
	resolveErr map[string]error,
) {
	t.Helper()
	origResolve := resolveCaptureEnumeratorFn
	origRealizer := newRealizerFn
	origGOOS := captureGOOSFn
	resolveCaptureEnumeratorFn = func(name string, _ bool) (driver.InstalledEnumerator, error) {
		if err := resolveErr[name]; err != nil {
			return nil, err
		}
		e, ok := byDriver[name]
		if !ok {
			return nil, errors.New("unexpected capture driver: " + name)
		}
		return e, nil
	}
	newRealizerFn = func() (realizer.Realizer, error) { return nil, ErrNoRealizer }
	captureGOOSFn = func() string { return "windows" }
	t.Cleanup(func() {
		resolveCaptureEnumeratorFn = origResolve
		newRealizerFn = origRealizer
		captureGOOSFn = origGOOS
	})
}

func TestRunCapture_MultipleWindowsDriversKeepBothAndWarnOnDuplicateName(t *testing.T) {
	withCaptureEnumerators(t, map[string]fakeInstalledEnumerator{
		"winget": {packages: []driver.InstalledPackage{{Ref: "Git.Git", DisplayName: "Git", Version: "2.45"}}},
		"chocolatey": {packages: []driver.InstalledPackage{
			{Ref: "git", DisplayName: "Git", Version: "2.45.1"},
			{Ref: "ripgrep", DisplayName: "ripgrep", Version: "14.1"},
		}},
	}, nil)

	out := filepath.Join(t.TempDir(), "captured.jsonc")
	withMockCatalog(map[string]*modules.Module{}, nil, func() {
		raw, eerr := RunCapture(CaptureFlags{Out: out, Pin: true})
		if eerr != nil {
			t.Fatalf("RunCapture: %v", eerr)
		}
		result := raw.(*CaptureResult)
		if len(result.AppsIncluded) != 3 {
			t.Fatalf("appsIncluded = %d, want 3", len(result.AppsIncluded))
		}
		if len(result.Warnings) != 1 || result.Warnings[0].Code != "possible_duplicate" {
			t.Fatalf("warnings = %+v, want one possible_duplicate", result.Warnings)
		}
		if result.Warnings[0].Driver != "chocolatey" || result.Warnings[0].Ref != "git" {
			t.Fatalf("duplicate warning provenance = %+v", result.Warnings[0])
		}
	})

	apps := readManifestApps(t, out)
	if len(apps) != 3 {
		t.Fatalf("manifest apps = %d, want 3", len(apps))
	}
	gotOrder := make([]string, 0, len(apps))
	gotVersions := make(map[string]string, len(apps))
	seen := map[string]bool{}
	for _, app := range apps {
		id := app["id"].(string)
		gotOrder = append(gotOrder, id)
		gotVersions[id] = app["version"].(string)
		drv, _ := app["driver"].(string)
		refs := app["refs"].(map[string]interface{})
		ref := refs["windows"].(string)
		if drv == "" {
			drv = "winget"
		}
		seen[drv+":"+ref] = true
	}
	if want := []string{"git", "git-git", "ripgrep"}; !reflect.DeepEqual(gotOrder, want) {
		t.Fatalf("manifest order = %v, want stable %v", gotOrder, want)
	}
	if gotVersions["git"] != "2.45.1" || gotVersions["git-git"] != "2.45" || gotVersions["ripgrep"] != "14.1" {
		t.Fatalf("captured pinned versions = %+v", gotVersions)
	}
	for _, key := range []string{"winget:Git.Git", "chocolatey:git", "chocolatey:ripgrep"} {
		if !seen[key] {
			t.Errorf("missing captured identity %s in %+v", key, seen)
		}
	}
}

func TestRunCapture_ImplicitUnavailableChocolateyIsWarning(t *testing.T) {
	withCaptureEnumerators(t,
		map[string]fakeInstalledEnumerator{"winget": {packages: []driver.InstalledPackage{{Ref: "Git.Git", DisplayName: "Git"}}}},
		map[string]error{"chocolatey": errors.New("choco missing")},
	)

	out := filepath.Join(t.TempDir(), "captured.jsonc")
	withMockCatalog(map[string]*modules.Module{}, nil, func() {
		raw, eerr := RunCapture(CaptureFlags{Out: out})
		if eerr != nil {
			t.Fatalf("RunCapture: %v", eerr)
		}
		warnings := raw.(*CaptureResult).Warnings
		if len(warnings) != 1 || warnings[0].Code != "optional_driver_unavailable" || warnings[0].Driver != "chocolatey" {
			t.Fatalf("warnings = %+v", warnings)
		}
	})
}

func TestRunCapture_ExplicitUnavailableDriverFails(t *testing.T) {
	withCaptureEnumerators(t, nil, map[string]error{"chocolatey": errors.New("choco missing")})
	_, eerr := RunCapture(CaptureFlags{Out: filepath.Join(t.TempDir(), "captured.jsonc"), Drivers: []string{"Chocolatey"}})
	if eerr == nil {
		t.Fatal("expected explicitly selected unavailable driver to fail")
	}
}

func TestRunCapture_ExplicitAvailableDriverFiltersOtherLanes(t *testing.T) {
	withCaptureEnumerators(t, map[string]fakeInstalledEnumerator{
		"chocolatey": {packages: []driver.InstalledPackage{{Ref: "ripgrep", DisplayName: "ripgrep"}}},
	}, nil)
	out := filepath.Join(t.TempDir(), "captured.jsonc")
	withMockCatalog(map[string]*modules.Module{}, nil, func() {
		raw, eerr := RunCapture(CaptureFlags{Out: out, Drivers: []string{"CHOCOLATEY", "chocolatey"}})
		if eerr != nil {
			t.Fatalf("RunCapture: %v", eerr)
		}
		apps := raw.(*CaptureResult).AppsIncluded
		if len(apps) != 1 || apps[0].Source != "chocolatey" || apps[0].ID != "ripgrep" {
			t.Fatalf("appsIncluded = %+v", apps)
		}
	})
}

func TestRunCapture_DerivedIDCollisionUsesDeterministicDriverSuffix(t *testing.T) {
	withCaptureEnumerators(t, map[string]fakeInstalledEnumerator{
		"winget":     {packages: []driver.InstalledPackage{{Ref: "Foo", DisplayName: "Winget Foo"}}},
		"chocolatey": {packages: []driver.InstalledPackage{{Ref: "foo", DisplayName: "Chocolatey Foo"}}},
	}, nil)
	out := filepath.Join(t.TempDir(), "captured.jsonc")
	withMockCatalog(map[string]*modules.Module{}, nil, func() {
		if _, eerr := RunCapture(CaptureFlags{Out: out}); eerr != nil {
			t.Fatalf("RunCapture: %v", eerr)
		}
	})
	apps := readManifestApps(t, out)
	ids := map[string]bool{}
	for _, app := range apps {
		ids[app["id"].(string)] = true
	}
	if !ids["foo"] || !ids["foo-chocolatey"] {
		t.Fatalf("collision IDs = %+v", ids)
	}
}

func TestRunCapture_DeduplicatesAuthoritativeDriverRefIdentity(t *testing.T) {
	withCaptureEnumerators(t, map[string]fakeInstalledEnumerator{
		"chocolatey": {packages: []driver.InstalledPackage{
			{Ref: "ripgrep", DisplayName: "ripgrep", Version: "14.1"},
			{Ref: "ripgrep", DisplayName: "ripgrep", Version: "14.1"},
		}},
	}, nil)
	out := filepath.Join(t.TempDir(), "captured.jsonc")
	withMockCatalog(map[string]*modules.Module{}, nil, func() {
		if _, eerr := RunCapture(CaptureFlags{Out: out, Drivers: []string{"chocolatey"}}); eerr != nil {
			t.Fatalf("RunCapture: %v", eerr)
		}
	})
	apps := readManifestApps(t, out)
	if len(apps) != 1 {
		t.Fatalf("authoritative duplicate produced %d apps: %+v", len(apps), apps)
	}
}

func TestRunCapture_UpdateUsesDriverAndRefIdentity(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, "existing.jsonc")
	out := filepath.Join(dir, "captured.jsonc")
	manifestBytes, err := json.Marshal(map[string]interface{}{
		"version": 1,
		"name":    "existing",
		"apps": []map[string]interface{}{{
			"id":   "git",
			"refs": map[string]string{"windows": "git"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(existing, manifestBytes, 0644); err != nil {
		t.Fatal(err)
	}

	withCaptureEnumerators(t, map[string]fakeInstalledEnumerator{
		"chocolatey": {packages: []driver.InstalledPackage{{Ref: "git", DisplayName: "Git"}}},
	}, nil)
	withMockCatalog(map[string]*modules.Module{}, nil, func() {
		if _, eerr := RunCapture(CaptureFlags{
			Out: out, Manifest: existing, Update: true, Drivers: []string{"chocolatey"},
		}); eerr != nil {
			t.Fatalf("RunCapture: %v", eerr)
		}
	})
	apps := readManifestApps(t, out)
	if len(apps) != 2 {
		t.Fatalf("manifest apps = %+v, want both winget and chocolatey git", apps)
	}
	identities := map[string]bool{}
	for _, app := range apps {
		driverName, _ := app["driver"].(string)
		if driverName == "" {
			driverName = "winget"
		}
		ref := app["refs"].(map[string]interface{})["windows"].(string)
		identities[driverName+":"+ref] = true
	}
	if !identities["winget:git"] || !identities["chocolatey:git"] {
		t.Fatalf("identities = %+v", identities)
	}
}

func TestRunCapture_UpdateExistingIDCollisionIsCaseInsensitive(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, "existing.jsonc")
	out := filepath.Join(dir, "captured.jsonc")
	if err := os.WriteFile(existing, []byte(`{
  "version": 1,
  "name": "existing",
  "apps": [{"id":"Git","refs":{"windows":"Legacy.Git"}}]
}`), 0o644); err != nil {
		t.Fatal(err)
	}

	withCaptureEnumerators(t, map[string]fakeInstalledEnumerator{
		"chocolatey": {packages: []driver.InstalledPackage{{Ref: "git", DisplayName: "Git"}}},
	}, nil)
	withMockCatalog(map[string]*modules.Module{}, nil, func() {
		if _, eerr := RunCapture(CaptureFlags{
			Out: out, Manifest: existing, Update: true, Drivers: []string{"chocolatey"},
		}); eerr != nil {
			t.Fatalf("RunCapture: %v", eerr)
		}
	})

	apps := readManifestApps(t, out)
	ids := map[string]bool{}
	for _, app := range apps {
		id := app["id"].(string)
		lower := strings.ToLower(id)
		if ids[lower] {
			t.Fatalf("duplicate case-insensitive ID %q in %+v", id, apps)
		}
		ids[lower] = true
	}
	if !ids["git"] || !ids["git-chocolatey"] {
		t.Fatalf("IDs = %+v, want Git and git-chocolatey", ids)
	}
}

func TestCapturePackageModuleMapIncludesDriverAndChocolateyRefs(t *testing.T) {
	mods := []*modules.Module{{
		ID: "apps.git",
		Matches: modules.MatchCriteria{
			Winget:     []string{"Git.Git"},
			Chocolatey: []string{"git"},
		},
	}}

	got := buildPackageModuleMap(mods)
	if len(got["winget:Git.Git"]) != 1 || got["winget:Git.Git"][0] != "apps.git" {
		t.Fatalf("winget packageModuleMap = %+v", got)
	}
	if len(got["chocolatey:git"]) != 1 || got["chocolatey:git"][0] != "apps.git" {
		t.Fatalf("chocolatey packageModuleMap = %+v", got)
	}
}

func TestCapturePackageModuleMapCanonicalizesChocolateyRefsAndDeduplicatesModules(t *testing.T) {
	mods := []*modules.Module{{
		ID: "apps.git",
		Matches: modules.MatchCriteria{
			Winget:     []string{"Git.Git", "Git.Git"},
			Chocolatey: []string{"Git.Install", " git.install ", "GIT.INSTALL"},
		},
	}, {
		ID:      "apps.git",
		Matches: modules.MatchCriteria{Chocolatey: []string{"git.install"}},
	}, {
		ID:      "apps.shared",
		Matches: modules.MatchCriteria{Chocolatey: []string{"GIT.INSTALL"}},
	}}

	got := buildPackageModuleMap(mods)
	if _, exists := got["chocolatey:Git.Install"]; exists {
		t.Fatalf("mixed-case Chocolatey key survived canonicalization: %+v", got)
	}
	if want := []string{"apps.git", "apps.shared"}; !reflect.DeepEqual(got["chocolatey:git.install"], want) {
		t.Fatalf("canonical Chocolatey owners = %v, want %v", got["chocolatey:git.install"], want)
	}
	if want := []string{"apps.git", "apps.git"}; !reflect.DeepEqual(got["winget:Git.Git"], want) {
		t.Fatalf("Winget key/value behavior changed: %v, want %v", got["winget:Git.Git"], want)
	}
}

func TestLegacyConfigModuleMapExcludesChocolateyOnlyRefs(t *testing.T) {
	mods := []*modules.Module{{
		ID:      "apps.choco-only",
		Matches: modules.MatchCriteria{Chocolatey: []string{"some-package"}},
	}, {
		ID:      "apps.path-only",
		Matches: modules.MatchCriteria{PathExists: []string{`C:\\Example`}},
	}}

	got := buildConfigModuleMap(mods)
	if _, leaked := got["apps.choco-only"]; leaked {
		t.Fatalf("legacy configModuleMap leaked Chocolatey-only module: %+v", got)
	}
	if got["apps.path-only"] != "apps.path-only" {
		t.Fatalf("path-only fallback missing: %+v", got)
	}
}
