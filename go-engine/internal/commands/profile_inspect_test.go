// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
)

func TestProfileInspectBuildsReadOnlyDeterministicInventory(t *testing.T) {
	root := t.TempDir()
	manifestPath := filepath.Join(root, "manifest.jsonc")
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

func TestProfileInspectPrefersVerifiedSnapshotRefsOverCatalogRefs(t *testing.T) {
	root := t.TempDir()
	capture := manifest.ConfigCapture{
		CaptureID: "capture-a", ModuleID: "apps.fixture", SourceInstance: manifest.ConfigSourceInstance{},
		CaptureModule:   manifest.CaptureModuleProvenance{SchemaVersion: 2, SnapshotPath: "provenance/modules/fixture.json"},
		PayloadManifest: []manifest.PayloadManifestEntry{{}, {}},
	}
	mf := &manifest.Manifest{Version: 2, Apps: []manifest.App{{ID: "snapshot-owner", Refs: map[string]string{"windows": "Vendor.Snapshot"}}, {ID: "catalog-owner", Refs: map[string]string{"windows": "Vendor.Catalog"}}}, ConfigCaptures: []manifest.ConfigCapture{capture}}
	result, err := inspectProfile(filepath.Join(root, "manifest.jsonc"), profileInspectDeps{
		loadManifest: func(string) (*manifest.Manifest, error) { return mf, nil }, preflightIncludes: func(string) error { return nil },
		loadCatalog: func(string) (map[string]*modules.Module, []modules.CatalogDiagnostic, error) {
			return map[string]*modules.Module{"apps.fixture": {ID: "apps.fixture", DisplayName: "Catalog", Matches: modules.MatchCriteria{Winget: []string{"Vendor.Catalog"}}}}, nil, nil
		},
		verifySnapshot: func(string, manifest.ConfigCapture) error { return nil },
		readFile: func(string) ([]byte, error) {
			return []byte(`{"moduleSchemaVersion":2,"id":"apps.fixture","displayName":"Snapshot","sensitivity":"none","matches":{"winget":["Vendor.Snapshot"]}}`), nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.SettingsApps) != 1 || result.SettingsApps[0].AssociationStatus != "included" || result.SettingsApps[0].AppID == nil || *result.SettingsApps[0].AppID != "app:snapshot-owner:1" || result.SettingsApps[0].CapturedEntryCount != 2 {
		t.Fatalf("settings = %+v", result.SettingsApps)
	}
}
