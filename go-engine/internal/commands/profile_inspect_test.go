// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/driver"
	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/realizer"
	"github.com/Artexis10/endstate/go-engine/internal/snapshot"
)

func TestProfileInspectBuildsReadOnlyDeterministicInventory(t *testing.T) {
	root := t.TempDir()
	manifestPath := filepath.Join(root, "manifest.jsonc")
	if err := os.WriteFile(manifestPath, []byte(`{"version":1,"apps":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	mf := &manifest.Manifest{
		Version: 1,
		Name:    "Fixture profile",
		Apps: []manifest.App{
			{ID: "obsidian-obsidian", Refs: map[string]string{"windows": "Obsidian.Obsidian"}},
			{ID: "duplicate", Refs: map[string]string{"windows": "Obsidian.Obsidian"}},
		},
		ConfigModules: []string{"apps.fixture-legacy", " obsidian "},
		Restore: []manifest.RestoreEntry{
			{FromModule: "fixture-legacy", Source: "configs/fixture-legacy/settings.json"},
			{Source: "configs/obsidian/settings.json"},
		},
	}
	catalogCalls := 0
	result, err := inspectProfile(manifestPath, profileInspectDeps{
		loadManifest:      func(string) (*manifest.Manifest, error) { return mf, nil },
		preflightIncludes: func(string) error { return nil },
		loadCatalog: func(string) (map[string]*modules.Module, []modules.CatalogDiagnostic, error) {
			catalogCalls++
			return map[string]*modules.Module{
				"apps.obsidian": {ID: "apps.obsidian", DisplayName: "Obsidian", Matches: modules.MatchCriteria{Winget: []string{"Obsidian.Obsidian"}}},
			}, nil, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if catalogCalls != 1 {
		t.Fatalf("catalog calls = %d, want 1 after ownership discovery", catalogCalls)
	}
	if result.Profile.Name == nil || *result.Profile.Name != "Fixture profile" {
		t.Fatalf("profile name = %#v", result.Profile.Name)
	}
	if len(result.Apps) != 2 || result.Apps[0].ID != "app:duplicate:1" || result.Apps[1].ID != "app:obsidian-obsidian:1" {
		t.Fatalf("apps = %+v", result.Apps)
	}
	if len(result.SettingsApps) != 2 {
		t.Fatalf("settings rows = %+v, want legacy and ambiguous obsidian rows", result.SettingsApps)
	}
	var obsidian *ProfileInspectSettingsApp
	for index := range result.SettingsApps {
		if result.SettingsApps[index].ModuleIDs[0] == "obsidian" {
			obsidian = &result.SettingsApps[index]
		}
	}
	if obsidian == nil || obsidian.AssociationStatus != "ambiguous" || len(obsidian.CandidateAppIDs) != 2 || obsidian.OwnerID != nil || obsidian.AppID != nil || obsidian.AppIncluded {
		t.Fatalf("obsidian association = %+v", obsidian)
	}
	if result.Summary.AppCount != 2 || result.Summary.SettingsRowCount != 2 || result.Summary.VerifiedSettingsAppCount != 0 || result.Summary.UnidentifiedSettingsRowCount != 2 {
		t.Fatalf("summary = %+v", result.Summary)
	}
}

func TestProfileInspectPreflightRejectsUnsupportedIncludes(t *testing.T) {
	for _, include := range []string{"C:/outside.jsonc", "profile-name", "../outside.json", "archive.endstate"} {
		t.Run(include, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "manifest.jsonc")
			if err := os.WriteFile(path, []byte(`{"version":1,"apps":[],"includes":["`+include+`"]}`), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := preflightProfileInspectIncludes(path); err == nil {
				t.Fatalf("preflight accepted %q", include)
			}
		})
	}
}

func TestProfileInspectPreflightDoesNotTouchForbiddenIncludeTargets(t *testing.T) {
	for _, include := range []string{`\\server\share\forbidden.jsonc`, `C:\forbidden.jsonc`} {
		t.Run(include, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "manifest.jsonc")
			content, err := json.Marshal(map[string]interface{}{"version": 1, "apps": []interface{}{}, "includes": []string{include}})
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, content, 0o644); err != nil {
				t.Fatal(err)
			}
			validationCalls := 0
			loadCalls := 0
			_, err = inspectProfile(path, profileInspectDeps{
				preflightIncludes: preflightProfileInspectIncludes,
				validateManifest: func(string) error {
					validationCalls++
					return nil
				},
				loadManifest: func(string) (*manifest.Manifest, error) {
					loadCalls++
					return nil, errors.New("forbidden include target was touched")
				},
			})
			if err == nil || !errors.Is(err, manifest.ErrValidation) {
				t.Fatalf("inspectProfile error = %v, want validation failure", err)
			}
			if validationCalls != 0 || loadCalls != 0 {
				t.Fatalf("forbidden include invoked validation=%d loader=%d, want neither", validationCalls, loadCalls)
			}
		})
	}
}

func TestProfileInspectPrefersVerifiedSnapshotRefsOverCatalogRefs(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "manifest.jsonc"), []byte(`{"version":2,"apps":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	snapshotBytes := []byte(`{"moduleSchemaVersion":2,"id":"apps.fixture","displayName":"Snapshot","sensitivity":"none","matches":{"winget":["Vendor.Snapshot"]}}`)
	capture := manifest.ConfigCapture{
		CaptureID: "capture-a", ModuleID: "apps.fixture", SourceInstance: manifest.ConfigSourceInstance{},
		CaptureModule:   manifest.CaptureModuleProvenance{SchemaVersion: 2, ContentHash: fmt.Sprintf("%x", sha256.Sum256(snapshotBytes)), SnapshotPath: "provenance/modules/fixture.json"},
		PayloadManifest: []manifest.PayloadManifestEntry{{}, {}},
	}
	mf := &manifest.Manifest{Version: 2, Apps: []manifest.App{{ID: "snapshot-owner", Refs: map[string]string{"windows": "Vendor.Snapshot"}}, {ID: "catalog-owner", Refs: map[string]string{"windows": "Vendor.Catalog"}}}, ConfigCaptures: []manifest.ConfigCapture{capture}}
	result, err := inspectProfile(filepath.Join(root, "manifest.jsonc"), profileInspectDeps{
		loadManifest: func(string) (*manifest.Manifest, error) { return mf, nil }, preflightIncludes: func(string) error { return nil },
		loadCatalog: func(string) (map[string]*modules.Module, []modules.CatalogDiagnostic, error) {
			return map[string]*modules.Module{"apps.fixture": {ID: "apps.fixture", DisplayName: "Catalog", Matches: modules.MatchCriteria{Winget: []string{"Vendor.Catalog"}}}}, nil, nil
		},
		verifySnapshot: func(string, manifest.ConfigCapture) error { return nil },
		readFile:       func(string) ([]byte, error) { return snapshotBytes, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.SettingsApps) != 1 || result.SettingsApps[0].AssociationStatus != "included" || result.SettingsApps[0].AppID == nil || *result.SettingsApps[0].AppID != "app:snapshot-owner:1" || result.SettingsApps[0].CapturedEntryCount != 2 {
		t.Fatalf("settings = %+v", result.SettingsApps)
	}
}

func TestProfileInspectPreservesRootInputErrorClassification(t *testing.T) {
	t.Run("not found", func(t *testing.T) {
		_, err := inspectProfile(filepath.Join(t.TempDir(), "missing.jsonc"), defaultProfileInspectDeps())
		if !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("error = %v, want not-exist", err)
		}
	})
	t.Run("parse", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "broken.jsonc")
		if err := os.WriteFile(path, []byte(`{"version":`), 0o644); err != nil {
			t.Fatal(err)
		}
		_, err := inspectProfile(path, defaultProfileInspectDeps())
		if err == nil || errors.Is(err, manifest.ErrValidation) {
			t.Fatalf("error = %v, want parse error", err)
		}
	})
}

func TestProfileInspectUsesChocolateyOwnerRefs(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "manifest.jsonc")
	if err := os.WriteFile(path, []byte(`{"version":1,"apps":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := inspectProfile(path, profileInspectDeps{
		loadManifest: func(string) (*manifest.Manifest, error) {
			return &manifest.Manifest{Version: 1, Apps: []manifest.App{{ID: "choco", Refs: map[string]string{"windows": "vendor.choco"}}}, ConfigModules: []string{"apps.fixture"}}, nil
		},
		preflightIncludes: func(string) error { return nil },
		loadCatalog: func(string) (map[string]*modules.Module, []modules.CatalogDiagnostic, error) {
			return map[string]*modules.Module{"apps.fixture": {ID: "apps.fixture", DisplayName: "Fixture", Matches: modules.MatchCriteria{Chocolatey: []string{"vendor.choco"}}}}, nil, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.SettingsApps) != 1 || result.SettingsApps[0].AssociationStatus != "included" {
		t.Fatalf("settings = %+v", result.SettingsApps)
	}
}

func TestProfileInspectRejectsSnapshotBytesChangedAfterVerification(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "manifest.jsonc")
	if err := os.WriteFile(path, []byte(`{"version":2,"apps":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	good := []byte(`{"moduleSchemaVersion":2,"id":"apps.fixture","displayName":"Good","sensitivity":"none","matches":{"winget":["Vendor.Good"]}}`)
	hash := fmt.Sprintf("%x", sha256.Sum256(good))
	capture := manifest.ConfigCapture{CaptureID: "capture-a", ModuleID: "apps.fixture", CaptureModule: manifest.CaptureModuleProvenance{SchemaVersion: 2, ContentHash: hash, SnapshotPath: "provenance/modules/fixture.json"}}
	result, err := inspectProfile(path, profileInspectDeps{loadManifest: func(string) (*manifest.Manifest, error) {
		return &manifest.Manifest{Version: 2, ConfigCaptures: []manifest.ConfigCapture{capture}}, nil
	}, preflightIncludes: func(string) error { return nil }, verifySnapshot: func(string, manifest.ConfigCapture) error { return nil }, readFile: func(string) ([]byte, error) {
		return []byte(`{"moduleSchemaVersion":2,"id":"apps.fixture","displayName":"Changed","sensitivity":"none","matches":{"winget":["Vendor.Changed"]}}`), nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Warnings) == 0 || result.SettingsApps[0].DisplayName == "Changed" {
		t.Fatalf("race was trusted: %+v", result)
	}
}

func TestProfileInspectPreflightRejectsSymlinkEscape(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	child := filepath.Join(outside, "child.jsonc")
	if err := os.WriteFile(child, []byte(`{"apps":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "escape.jsonc")
	if err := os.Symlink(child, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	manifestPath := filepath.Join(root, "manifest.jsonc")
	if err := os.WriteFile(manifestPath, []byte(`{"version":1,"apps":[],"includes":["escape.jsonc"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := preflightProfileInspectIncludes(manifestPath); err == nil {
		t.Fatal("symlink escape was accepted")
	}
}

func TestProfileInspectPreflightRejectsLinkedIncludeEvenInsideRoot(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "real")
	if err := os.Mkdir(real, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(real, "child.jsonc"), []byte(`{"apps":[],"includes":["nested.jsonc"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(root, "alias")
	if err := os.Symlink(real, alias); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	path := filepath.Join(root, "manifest.jsonc")
	if err := os.WriteFile(path, []byte(`{"version":1,"apps":[],"includes":["alias/child.jsonc"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := preflightProfileInspectIncludes(path); err == nil {
		t.Fatal("linked include accepted")
	}
}

func TestProfileInspectAbsoluteIncludeSyntaxIsHostIndependent(t *testing.T) {
	for _, value := range []string{`C:/outside.jsonc`, `C:\\outside.jsonc`, `\\server\\share\\x.jsonc`, `//server/share/x.jsonc`} {
		if !isAbsoluteProfileInspectInclude(value) {
			t.Fatalf("%q not absolute", value)
		}
	}
}

// Characterization/acceptance proof: inspection is intentionally read-only.
// These command-package seams panic if the model accidentally reaches any
// current-machine path; this test is deliberately not parallel.
func TestProfileInspectNeverInvokesMachineSeams(t *testing.T) {
	originalSnapshot, originalInstalled, originalMatcher, originalEnumerator, originalRealizer, originalBrew, originalDriver := takeSnapshotFn, listInstalledFn, matchModulesForAppsFn, resolveCaptureEnumeratorFn, newRealizerFn, newBrewDriverFn, newDriverFn
	t.Cleanup(func() {
		takeSnapshotFn, listInstalledFn, matchModulesForAppsFn, resolveCaptureEnumeratorFn, newRealizerFn, newBrewDriverFn, newDriverFn = originalSnapshot, originalInstalled, originalMatcher, originalEnumerator, originalRealizer, originalBrew, originalDriver
	})
	takeSnapshotFn = func() ([]snapshot.SnapshotApp, error) { panic("snapshot") }
	listInstalledFn = func() ([]snapshot.SnapshotApp, error) { panic("detection") }
	matchModulesForAppsFn = func(map[string]*modules.Module, []manifest.App) []*modules.Module { panic("matcher") }
	resolveCaptureEnumeratorFn = func(string, bool) (driver.InstalledEnumerator, error) { panic("driver") }
	newRealizerFn = func() (realizer.Realizer, error) { panic("realizer") }
	newBrewDriverFn = func() (driver.Driver, error) { panic("brew") }
	newDriverFn = func() (driver.Driver, error) { panic("default driver") }
	path := filepath.Join(t.TempDir(), "manifest.jsonc")
	if err := os.WriteFile(path, []byte(`{"version":1,"apps":[{"id":"fixture","refs":{"windows":"Vendor.Fixture"}}],"configModules":["apps.fixture"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	deps := defaultProfileInspectDeps()
	deps.loadCatalog = func(string) (map[string]*modules.Module, []modules.CatalogDiagnostic, error) {
		return map[string]*modules.Module{"apps.fixture": {ID: "apps.fixture", Matches: modules.MatchCriteria{Winget: []string{"Vendor.Fixture"}}}}, nil, nil
	}
	if _, err := inspectProfile(path, deps); err != nil {
		t.Fatal(err)
	}
}

func TestProfileInspectOwnershipMatrixPassA(t *testing.T) {
	inspect := func(t *testing.T, mf *manifest.Manifest, metadata string, catalog map[string]*modules.Module) *ProfileInspectResult {
		t.Helper()
		path := filepath.Join(t.TempDir(), "manifest.jsonc")
		if err := os.WriteFile(path, []byte(`{"version":1,"apps":[]}`), 0o644); err != nil {
			t.Fatal(err)
		}
		result, err := inspectProfile(path, profileInspectDeps{
			loadManifest: func(string) (*manifest.Manifest, error) { return mf, nil }, preflightIncludes: func(string) error { return nil },
			readFile: func(string) ([]byte, error) { return []byte(metadata), nil },
			loadCatalog: func(string) (map[string]*modules.Module, []modules.CatalogDiagnostic, error) {
				return catalog, nil, nil
			},
			verifySnapshot: func(string, manifest.ConfigCapture) error { return nil },
		})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}

	t.Run("v1 unions normalized evidence once", func(t *testing.T) {
		result := inspect(t, &manifest.Manifest{Version: 1, ConfigModules: []string{"apps.fixture", " fixture "}, Restore: []manifest.RestoreEntry{{FromModule: "apps.fixture"}, {Source: "configs/fixture/settings.json"}}}, `{"configModulesIncluded":["apps.fixture"]}`, nil)
		if len(result.SettingsApps) != 1 || !reflect.DeepEqual(result.SettingsApps[0].ModuleIDs, []string{"apps.fixture", "fixture"}) || result.SettingsApps[0].CapturedEntryCount != 2 {
			t.Fatalf("rows = %+v", result.SettingsApps)
		}
	})
	t.Run("unique obsidian and absent owner", func(t *testing.T) {
		catalog := map[string]*modules.Module{"apps.obsidian": {ID: "apps.obsidian", DisplayName: "Obsidian", Matches: modules.MatchCriteria{Winget: []string{"Obsidian.Obsidian"}}}, "apps.absent": {ID: "apps.absent", DisplayName: "Absent", Matches: modules.MatchCriteria{Winget: []string{"Vendor.Absent"}}}}
		result := inspect(t, &manifest.Manifest{Version: 1, Apps: []manifest.App{{ID: "obsidian-obsidian", Refs: map[string]string{"windows": "Obsidian.Obsidian"}}}, ConfigModules: []string{"obsidian", "absent"}}, `{}`, catalog)
		if len(result.SettingsApps) != 2 {
			t.Fatal(result.SettingsApps)
		}
		var included, absent ProfileInspectSettingsApp
		for _, row := range result.SettingsApps {
			if row.AssociationStatus == "included" {
				included = row
			} else {
				absent = row
			}
		}
		if included.AppID == nil || *included.AppID != "app:obsidian-obsidian:1" || !included.AppIncluded || included.OwnerID == nil || len(included.CandidateAppIDs) != 1 {
			t.Fatalf("included=%+v", included)
		}
		if absent.AssociationStatus != "not_in_profile" || absent.OwnerID == nil || *absent.OwnerID != "package:vendor.absent" || absent.AppID != nil || absent.AppIncluded || len(absent.CandidateAppIDs) != 0 {
			t.Fatalf("absent=%+v", absent)
		}
	})
	t.Run("ambiguous unresolved and duplicate app identities", func(t *testing.T) {
		catalog := map[string]*modules.Module{"apps.amb": {ID: "apps.amb", Matches: modules.MatchCriteria{Winget: []string{"Vendor.Amb"}}}}
		result := inspect(t, &manifest.Manifest{Version: 1, Apps: []manifest.App{{ID: "same", Refs: map[string]string{"windows": "Vendor.Amb"}}, {ID: "SAME", Refs: map[string]string{"windows": "Vendor.Amb"}}}, ConfigModules: []string{"amb", "unknown"}}, `{}`, catalog)
		if len(result.Apps) != 2 || result.Apps[0].ID != "app:same:1" || result.Apps[1].ID != "app:same:2" {
			t.Fatalf("apps=%+v", result.Apps)
		}
		var ambiguous, unresolved ProfileInspectSettingsApp
		for _, row := range result.SettingsApps {
			if row.AssociationStatus == "ambiguous" {
				ambiguous = row
			} else {
				unresolved = row
			}
		}
		if ambiguous.OwnerID != nil || ambiguous.AppID != nil || ambiguous.AppIncluded || len(ambiguous.CandidateAppIDs) != 2 {
			t.Fatalf("ambiguous=%+v", ambiguous)
		}
		if unresolved.AssociationStatus != "unresolved" || unresolved.OwnerID != nil || unresolved.AppID != nil || unresolved.AppIncluded || len(unresolved.CandidateAppIDs) != 0 {
			t.Fatalf("unresolved=%+v", unresolved)
		}
	})
	t.Run("groups included modules and applies root exclusions", func(t *testing.T) {
		catalog := map[string]*modules.Module{"apps.one": {ID: "apps.one", Matches: modules.MatchCriteria{Winget: []string{"Vendor.One"}}}, "apps.two": {ID: "apps.two", Matches: modules.MatchCriteria{Winget: []string{"Vendor.One"}}}, "apps.drop": {ID: "apps.drop", Matches: modules.MatchCriteria{Winget: []string{"Vendor.Drop"}}}}
		result := inspect(t, &manifest.Manifest{Version: 1, Apps: []manifest.App{{ID: "one", Refs: map[string]string{"windows": "Vendor.One"}}, {ID: "drop", Refs: map[string]string{"windows": "Vendor.Drop"}}}, Exclude: []string{"Vendor.Drop"}, ExcludeConfigs: []string{"apps.drop"}, ConfigModules: []string{"apps.one", "apps.two", "apps.drop"}}, `{}`, catalog)
		if len(result.Apps) != 1 || len(result.SettingsApps) != 1 || !reflect.DeepEqual(result.SettingsApps[0].ModuleIDs, []string{"apps.one", "apps.two"}) || result.SettingsApps[0].ID != "settings:app:one:1" {
			t.Fatalf("result=%+v", result)
		}
	})
	t.Run("v2 counts captures and legacy lane", func(t *testing.T) {
		result := inspect(t, &manifest.Manifest{Version: 2, ConfigCaptures: []manifest.ConfigCapture{{CaptureID: "a", ModuleID: "apps.one", PayloadManifest: []manifest.PayloadManifestEntry{{}, {}}}, {CaptureID: "b", ModuleID: "apps.one", PayloadManifest: []manifest.PayloadManifestEntry{{}}}}, LegacyConfigLanes: []manifest.LegacyConfigLane{{CaptureID: "legacy", ModuleID: "apps.two"}}, Restore: []manifest.RestoreEntry{{LegacyCaptureID: "legacy"}}}, `{}`, nil)
		if len(result.SettingsApps) != 2 {
			t.Fatal(result.SettingsApps)
		}
		counts := map[string]int{}
		for _, row := range result.SettingsApps {
			counts[row.ModuleIDs[0]] = row.CapturedEntryCount
		}
		if counts["apps.one"] != 3 || counts["apps.two"] != 1 {
			t.Fatalf("counts=%v", counts)
		}
	})
}

func TestProfileInspectAcceptanceMatrixPassB(t *testing.T) {
	t.Run("missing metadata is typed inventory warning", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "manifest.jsonc")
		_ = os.WriteFile(path, []byte(`{"version":1,"apps":[]}`), 0o644)
		result, err := inspectProfile(path, profileInspectDeps{loadManifest: func(string) (*manifest.Manifest, error) { return &manifest.Manifest{Version: 1}, nil }, preflightIncludes: func(string) error { return nil }, readFile: func(string) ([]byte, error) { return nil, os.ErrNotExist }})
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Warnings) == 0 || result.Warnings[0].Code != "PROFILE_INSPECT_METADATA" || result.Warnings[0].Impact != "inventory_incomplete" {
			t.Fatalf("warnings=%+v", result.Warnings)
		}
	})
	t.Run("malformed metadata warning is ordered with snapshot warning", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "manifest.jsonc")
		_ = os.WriteFile(path, []byte(`{"version":2,"apps":[]}`), 0o644)
		capture := manifest.ConfigCapture{CaptureID: "a", ModuleID: "apps.fixture", CaptureModule: manifest.CaptureModuleProvenance{SchemaVersion: 2, SnapshotPath: "provenance/modules/x.json"}}
		result, err := inspectProfile(path, profileInspectDeps{loadManifest: func(string) (*manifest.Manifest, error) {
			return &manifest.Manifest{Version: 2, ConfigCaptures: []manifest.ConfigCapture{capture}}, nil
		}, preflightIncludes: func(string) error { return nil }, readFile: func(string) ([]byte, error) { return []byte(`{bad`), nil }, verifySnapshot: func(string, manifest.ConfigCapture) error { return errors.New("bad") }})
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Warnings) != 2 || result.Warnings[0].Code != "PROFILE_INSPECT_METADATA" || result.Warnings[1].Code != "PROFILE_INSPECT_SNAPSHOT" {
			t.Fatalf("warnings=%+v", result.Warnings)
		}
	})
	t.Run("recursively inspects contained includes", func(t *testing.T) {
		dir := t.TempDir()
		child := filepath.Join(dir, "child.jsonc")
		root := filepath.Join(dir, "root.jsonc")
		if err := os.WriteFile(child, []byte(`{"apps":[{"id":"child","refs":{"windows":"Vendor.Child"}}]}`), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(root, []byte(`{"version":1,"apps":[{"id":"root","refs":{"windows":"Vendor.Root"}}],"includes":["child.jsonc"]}`), 0o644); err != nil {
			t.Fatal(err)
		}
		result, err := inspectProfile(root, defaultProfileInspectDeps())
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Apps) != 2 {
			t.Fatalf("apps=%+v", result.Apps)
		}
	})
	t.Run("install only has non-null empty inventories", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "manifest.jsonc")
		if err := os.WriteFile(path, []byte(`{"version":1,"apps":[]}`), 0o644); err != nil {
			t.Fatal(err)
		}
		result, err := inspectProfile(path, defaultProfileInspectDeps())
		if err != nil {
			t.Fatal(err)
		}
		data, _ := json.Marshal(result)
		var raw map[string]json.RawMessage
		_ = json.Unmarshal(data, &raw)
		for _, field := range []string{"apps", "settingsApps", "warnings"} {
			if string(raw[field]) == "null" {
				t.Fatalf("%s was null: %s", field, data)
			}
		}
	})
	t.Run("invalid snapshot warns and falls back to catalog", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "manifest.jsonc")
		_ = os.WriteFile(path, []byte(`{"version":2,"apps":[]}`), 0o644)
		capture := manifest.ConfigCapture{CaptureID: "a", ModuleID: "apps.fixture", CaptureModule: manifest.CaptureModuleProvenance{SchemaVersion: 2, SnapshotPath: "provenance/modules/x.json"}}
		result, err := inspectProfile(path, profileInspectDeps{loadManifest: func(string) (*manifest.Manifest, error) {
			return &manifest.Manifest{Version: 2, Apps: []manifest.App{{ID: "fixture", Refs: map[string]string{"windows": "Vendor.Fixture"}}}, ConfigCaptures: []manifest.ConfigCapture{capture}}, nil
		}, preflightIncludes: func(string) error { return nil }, verifySnapshot: func(string, manifest.ConfigCapture) error { return errors.New("bad") }, loadCatalog: func(string) (map[string]*modules.Module, []modules.CatalogDiagnostic, error) {
			return map[string]*modules.Module{"apps.fixture": {ID: "apps.fixture", Matches: modules.MatchCriteria{Winget: []string{"Vendor.Fixture"}}}}, nil, nil
		}})
		if err != nil {
			t.Fatal(err)
		}
		if result.SettingsApps[0].AssociationStatus != "included" || len(result.Warnings) == 0 || result.Warnings[0].Impact != "inventory_incomplete" {
			t.Fatalf("result=%+v", result)
		}
	})
	t.Run("unions valid snapshot refs", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "manifest.jsonc")
		_ = os.WriteFile(path, []byte(`{"version":2,"apps":[]}`), 0o644)
		one := []byte(`{"moduleSchemaVersion":2,"id":"apps.fixture","displayName":"Fixture","sensitivity":"none","matches":{"winget":["Vendor.One"]}}`)
		two := []byte(`{"moduleSchemaVersion":2,"id":"apps.fixture","displayName":"Fixture","sensitivity":"none","matches":{"chocolatey":["Vendor.Two"]}}`)
		mk := func(id, p string, b []byte) manifest.ConfigCapture {
			return manifest.ConfigCapture{CaptureID: id, ModuleID: "apps.fixture", CaptureModule: manifest.CaptureModuleProvenance{SchemaVersion: 2, ContentHash: fmt.Sprintf("%x", sha256.Sum256(b)), SnapshotPath: p}}
		}
		captures := []manifest.ConfigCapture{mk("a", "provenance/modules/one.json", one), mk("b", "provenance/modules/two.json", two)}
		result, err := inspectProfile(path, profileInspectDeps{loadManifest: func(string) (*manifest.Manifest, error) {
			return &manifest.Manifest{Version: 2, ConfigCaptures: captures}, nil
		}, preflightIncludes: func(string) error { return nil }, verifySnapshot: func(string, manifest.ConfigCapture) error { return nil }, readFile: func(p string) ([]byte, error) {
			if filepath.Base(p) == "one.json" {
				return one, nil
			}
			return two, nil
		}})
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(result.SettingsApps[0].PackageRefs, []string{"Vendor.One", "Vendor.Two"}) {
			t.Fatalf("refs=%+v", result.SettingsApps)
		}
	})
	t.Run("metadata warnings timestamps labels json and ordering", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "manifest.jsonc")
		_ = os.WriteFile(path, []byte(`{"version":1,"apps":[]}`), 0o644)
		mf := &manifest.Manifest{Version: 1, Captured: "manifest-time", Apps: []manifest.App{{ID: "z-app", Refs: map[string]string{"windows": "Z.Ref"}}, {ID: "a-app", DisplayName: "Alpha"}}, ConfigModules: []string{"apps.zed", "apps.alpha"}}
		catalog := map[string]*modules.Module{"apps.zed": {ID: "apps.zed", Matches: modules.MatchCriteria{Winget: []string{"Z.Ref"}}}, "apps.alpha": {ID: "apps.alpha", DisplayName: "Catalog Alpha"}}
		result, err := inspectProfile(path, profileInspectDeps{loadManifest: func(string) (*manifest.Manifest, error) { return mf, nil }, preflightIncludes: func(string) error { return nil }, readFile: func(string) ([]byte, error) { return []byte(`{"capturedAt":"metadata-time"}`), nil }, loadCatalog: func(string) (map[string]*modules.Module, []modules.CatalogDiagnostic, error) {
			return catalog, nil, nil
		}})
		if err != nil {
			t.Fatal(err)
		}
		if result.Profile.CapturedAt == nil || *result.Profile.CapturedAt != "manifest-time" || len(result.Warnings) == 0 || result.Apps[0].DisplayName != "Alpha" || result.SettingsApps[0].DisplayName != "Catalog Alpha" {
			t.Fatalf("result=%+v", result)
		}
		data, _ := json.Marshal(result)
		if !strings.Contains(string(data), `"name":null`) {
			t.Fatalf("nullable name absent: %s", data)
		}
		if result.Summary.AppCount != len(result.Apps) || result.Summary.SettingsRowCount != len(result.SettingsApps) {
			t.Fatalf("summary=%+v", result.Summary)
		}
	})
}

func TestProfileInspectExplicitReviewRegressions(t *testing.T) {
	t.Run("rejects linked root before loader can choose another base", func(t *testing.T) {
		a, b := t.TempDir(), t.TempDir()
		target := filepath.Join(b, "profile.jsonc")
		if err := os.WriteFile(target, []byte(`{"version":1,"apps":[],"includes":["child.jsonc"]}`), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(b, "child.jsonc"), []byte(`{"apps":[]}`), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(a, "child.jsonc"), []byte(`{"apps":[{"id":"wrong","refs":{"windows":"Vendor.Wrong"}}]}`), 0o644); err != nil {
			t.Fatal(err)
		}
		link := filepath.Join(a, "profile.jsonc")
		if err := os.Symlink(target, link); err != nil {
			t.Skipf("symlink unavailable: %v", err)
		}
		if _, err := inspectProfile(link, defaultProfileInspectDeps()); err == nil || !errors.Is(err, manifest.ErrValidation) {
			t.Fatalf("error=%v, want validation", err)
		}
	})
	t.Run("root child grandchild merges apps and ownership", func(t *testing.T) {
		dir := t.TempDir()
		grand := filepath.Join(dir, "grand.jsonc")
		child := filepath.Join(dir, "child.jsonc")
		root := filepath.Join(dir, "root.jsonc")
		_ = os.WriteFile(grand, []byte(`{"apps":[{"id":"grand","refs":{"windows":"Vendor.Grand"}}],"configModules":["apps.grand"]}`), 0o644)
		_ = os.WriteFile(child, []byte(`{"apps":[{"id":"child","refs":{"windows":"Vendor.Child"}}],"includes":["grand.jsonc"],"configModules":["apps.child"]}`), 0o644)
		_ = os.WriteFile(root, []byte(`{"version":1,"apps":[{"id":"root","refs":{"windows":"Vendor.Root"}}],"includes":["child.jsonc"],"configModules":["apps.root"]}`), 0o644)
		result, err := inspectProfile(root, defaultProfileInspectDeps())
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Apps) != 3 || len(result.SettingsApps) != 3 {
			t.Fatalf("result=%+v", result)
		}
	})
	t.Run("groups absent modules sharing complete ref set", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "manifest.jsonc")
		_ = os.WriteFile(path, []byte(`{"version":1,"apps":[]}`), 0o644)
		catalog := map[string]*modules.Module{"apps.one": {ID: "apps.one", Matches: modules.MatchCriteria{Winget: []string{"Vendor.Absent"}}}, "apps.two": {ID: "apps.two", Matches: modules.MatchCriteria{Winget: []string{"Vendor.Absent"}}}}
		result, err := inspectProfile(path, profileInspectDeps{loadManifest: func(string) (*manifest.Manifest, error) {
			return &manifest.Manifest{Version: 1, ConfigModules: []string{"apps.one", "apps.two"}}, nil
		}, preflightIncludes: func(string) error { return nil }, loadCatalog: func(string) (map[string]*modules.Module, []modules.CatalogDiagnostic, error) {
			return catalog, nil, nil
		}})
		if err != nil {
			t.Fatal(err)
		}
		row := result.SettingsApps[0]
		if len(result.SettingsApps) != 1 || row.AssociationStatus != "not_in_profile" || row.OwnerID == nil || *row.OwnerID != "package:vendor.absent" || !reflect.DeepEqual(row.ModuleIDs, []string{"apps.one", "apps.two"}) {
			t.Fatalf("rows=%+v", result.SettingsApps)
		}
	})
	t.Run("capture evidence wins conflicting snapshot", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "manifest.jsonc")
		_ = os.WriteFile(path, []byte(`{"version":2,"apps":[]}`), 0o644)
		snap := []byte(`{"moduleSchemaVersion":2,"id":"apps.fixture","displayName":"Snapshot","sensitivity":"none","matches":{"winget":["Vendor.Snapshot"]}}`)
		capture := manifest.ConfigCapture{CaptureID: "a", ModuleID: "apps.fixture", SourceInstance: manifest.ConfigSourceInstance{Evidence: &manifest.ConfigSourceInstanceEvidence{Ref: "Vendor.Source"}}, CaptureModule: manifest.CaptureModuleProvenance{SchemaVersion: 2, ContentHash: fmt.Sprintf("%x", sha256.Sum256(snap)), SnapshotPath: "provenance/modules/x.json"}}
		result, err := inspectProfile(path, profileInspectDeps{loadManifest: func(string) (*manifest.Manifest, error) {
			return &manifest.Manifest{Version: 2, Apps: []manifest.App{{ID: "source", Refs: map[string]string{"windows": "Vendor.Source"}}, {ID: "snapshot", Refs: map[string]string{"windows": "Vendor.Snapshot"}}}, ConfigCaptures: []manifest.ConfigCapture{capture}}, nil
		}, preflightIncludes: func(string) error { return nil }, verifySnapshot: func(string, manifest.ConfigCapture) error { return nil }, readFile: func(string) ([]byte, error) { return snap, nil }})
		if err != nil {
			t.Fatal(err)
		}
		if result.SettingsApps[0].AppID == nil || *result.SettingsApps[0].AppID != "app:source:1" {
			t.Fatalf("rows=%+v", result.SettingsApps)
		}
	})
	t.Run("root excludes override child and metadata timestamp falls back", func(t *testing.T) {
		dir := t.TempDir()
		child := filepath.Join(dir, "child.jsonc")
		root := filepath.Join(dir, "root.jsonc")
		_ = os.WriteFile(child, []byte(`{"apps":[],"configModules":["apps.child"],"excludeConfigs":["apps.child"]}`), 0o644)
		_ = os.WriteFile(root, []byte(`{"version":1,"apps":[],"includes":["child.jsonc"],"configModules":["apps.root"],"excludeConfigs":["apps.root"]}`), 0o644)
		result, err := inspectProfile(root, profileInspectDeps{loadManifest: manifest.LoadManifest, preflightIncludes: preflightProfileInspectIncludes, readFile: func(string) ([]byte, error) { return []byte(`{"capturedAt":"metadata-time"}`), nil }})
		if err != nil {
			t.Fatal(err)
		}
		if len(result.SettingsApps) != 1 || !reflect.DeepEqual(result.SettingsApps[0].ModuleIDs, []string{"apps.child"}) || result.Profile.CapturedAt == nil || *result.Profile.CapturedAt != "metadata-time" {
			t.Fatalf("result=%+v", result)
		}
	})
}
