// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package discovery

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/config"
	"github.com/Artexis10/endstate/go-engine/internal/hmregistry"
)

func TestHomeManagerAdapterFindsLiveSettingsWithoutNix(t *testing.T) {
	home := t.TempDir()
	source := filepath.Join(home, ".config", "ripgrep", "ripgreprc")
	if err := os.MkdirAll(filepath.Dir(source), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte("--hidden\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	registry := &hmregistry.Registry{Entries: []hmregistry.Entry{{
		ID: "ripgrep", Program: "ripgrep", DisplayName: "ripgrep",
		PackageID: "ripgrep", ModuleID: "apps.ripgrep",
		Disposition: hmregistry.FileRoundTrip, Codec: "bounded-regular-file-v1",
		Targets: []hmregistry.Target{{Coordinate: "${xdg.config}/ripgrep/ripgreprc"}},
	}}}
	inventory, err := (HomeManagerAdapter{
		Registry: registry,
		Environment: config.PathEnvironment{
			GOOS: "linux", Home: home, Env: map[string]string{},
		},
	}).Inventory(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(inventory.Settings) != 1 {
		t.Fatalf("settings = %+v", inventory.Settings)
	}
	candidate := inventory.Settings[0]
	if candidate.ID != "apps.ripgrep" || candidate.Portability != PortabilityConfigOnly {
		t.Fatalf("candidate = %+v", candidate)
	}
	wantActions := []SettingsAction{SettingsCapture, SettingsRestore, SettingsVerify, SettingsRevert}
	if !reflect.DeepEqual(candidate.Actions, wantActions) {
		t.Fatalf("actions = %v, want %v", candidate.Actions, wantActions)
	}
	if len(candidate.Evidence) != 1 || candidate.Evidence[0].Ref != "${xdg.config}/ripgrep/ripgreprc" {
		t.Fatalf("evidence = %+v", candidate.Evidence)
	}
	if len(inventory.SettingsFiles) != 1 {
		t.Fatalf("file plans = %+v", inventory.SettingsFiles)
	}
	plan := inventory.SettingsFiles[0]
	if plan.Source != source || plan.Target != "${xdg.config}/ripgrep/ripgreprc" || plan.CandidateID != candidate.ID {
		t.Fatalf("file plan = %+v", plan)
	}
	if len(plan.ObservedSHA256) != 64 {
		t.Fatalf("file plan has no bounded content observation: %+v", plan)
	}
}

func TestHomeManagerAdapterSkipsExcludedMissingAndLinkedTargets(t *testing.T) {
	home := t.TempDir()
	realSource := filepath.Join(home, "real.conf")
	if err := os.WriteFile(realSource, []byte("safe"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(home, ".config", "linked", "config")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realSource, link); err != nil {
		t.Fatal(err)
	}
	registry := &hmregistry.Registry{Entries: []hmregistry.Entry{
		{ID: "excluded", Program: "excluded", DisplayName: "Excluded", Disposition: hmregistry.Excluded, Targets: []hmregistry.Target{{Coordinate: "${xdg.config}/excluded/config"}}},
		{ID: "linked", Program: "linked", DisplayName: "Linked", Disposition: hmregistry.FileRoundTrip, Codec: "bounded-regular-file-v1", Targets: []hmregistry.Target{{Coordinate: "${xdg.config}/linked/config"}}},
		{ID: "missing", Program: "missing", DisplayName: "Missing", Disposition: hmregistry.FileRoundTrip, Codec: "bounded-regular-file-v1", Targets: []hmregistry.Target{{Coordinate: "${xdg.config}/missing/config", Optional: true}}},
	}}
	inventory, err := (HomeManagerAdapter{Registry: registry, Environment: config.PathEnvironment{GOOS: "linux", Home: home, Env: map[string]string{}}}).Inventory(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(inventory.Settings) != 0 || len(inventory.SettingsFiles) != 0 {
		t.Fatalf("unsafe or absent settings escaped: %+v", inventory)
	}
	if inventory.Ignored != 1 {
		t.Fatalf("ignored = %d, want linked live target only", inventory.Ignored)
	}
}

func TestHomeManagerAdapterCarriesReviewedCuratedCodec(t *testing.T) {
	home := t.TempDir()
	source := filepath.Join(home, ".gitconfig")
	if err := os.WriteFile(source, []byte("[user]\nname = Example\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	registry := &hmregistry.Registry{Entries: []hmregistry.Entry{{
		ID: "git", Program: "git", DisplayName: "Git", ModuleID: "apps.git",
		Disposition: hmregistry.CuratedCodec, Codec: "git-config-safe-v1",
		Targets: []hmregistry.Target{{Coordinate: "${home}/.gitconfig"}},
	}}}
	inventory, err := (HomeManagerAdapter{
		Registry:    registry,
		Environment: config.PathEnvironment{GOOS: "linux", Home: home, Env: map[string]string{}},
	}).Inventory(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(inventory.SettingsFiles) != 1 || inventory.SettingsFiles[0].Codec != "git-config-safe-v1" {
		t.Fatalf("curated plans = %+v", inventory.SettingsFiles)
	}
}

func TestOrchestratorCarriesSelectedPrivateSettingsPlansAndUpgradesPortability(t *testing.T) {
	plan := SettingsFilePlan{CandidateID: "apps.ripgrep", AdapterID: "home-manager-live", Target: "${xdg.config}/ripgrep/ripgreprc", Source: "/private/home/.config/ripgrep/ripgreprc"}
	orchestrator := Orchestrator{
		Adapters: []Adapter{fakeAdapter{id: "live", result: AdapterInventory{
			Items: []InventoryItem{{Source: "debian", Kind: EvidencePackage, Ref: "ripgrep", Explicit: true}},
			Settings: []SettingsCandidate{{
				ID: "apps.ripgrep", ModuleID: "apps.ripgrep", DisplayName: "ripgrep settings", PackageID: "ripgrep",
				Portability: PortabilityConfigOnly, Actions: []SettingsAction{SettingsCapture, SettingsRestore},
			}},
			SettingsFiles: []SettingsFilePlan{plan},
		}}},
		Catalog: testPackageCatalog(t), NixpkgsInput: "github:NixOS/nixpkgs/release",
	}
	result, err := orchestrator.Discover(context.Background(), Request{Only: []string{"ripgrep"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Settings) != 1 || result.Settings[0].Portability != PortabilityResolved {
		t.Fatalf("settings = %+v", result.Settings)
	}
	if !reflect.DeepEqual(result.SettingsFiles, []SettingsFilePlan{plan}) {
		t.Fatalf("private plans = %+v", result.SettingsFiles)
	}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	encoded := strings.TrimSpace(string(data))
	if strings.Contains(encoded, "/private/home") || strings.Contains(encoded, "settingsFiles") {
		t.Fatalf("private source leaked into discovery JSON: %s", encoded)
	}
}
