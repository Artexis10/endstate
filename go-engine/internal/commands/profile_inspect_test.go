// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
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
