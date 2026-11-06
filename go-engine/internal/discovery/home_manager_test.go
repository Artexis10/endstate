// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/config"
	"github.com/Artexis10/endstate/go-engine/internal/hmregistry"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
)

func TestHomeManagerAdapterObservesUnboundSettingsWithoutReadingPayload(t *testing.T) {
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
	filesystem := &authorityObservationFS{}
	inventory, err := (HomeManagerAdapter{
		Registry: registry, FS: filesystem,
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
	if len(candidate.Actions) != 0 || filesystem.opens != 0 {
		t.Fatalf("unbound payload was authorized or read: actions=%v opens=%d", candidate.Actions, filesystem.opens)
	}
	if len(candidate.Evidence) != 1 || candidate.Evidence[0].Ref != "${xdg.config}/ripgrep/ripgreprc" {
		t.Fatalf("evidence = %+v", candidate.Evidence)
	}
	if len(inventory.SettingsFiles) != 0 || len(inventory.Warnings) != 1 || inventory.Warnings[0].Code != "settings_adapter_unbound" {
		t.Fatalf("unbound plans or diagnostics: %+v", inventory)
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

func TestHomeManagerAdapterDoesNotAuthorizeGitDenylistCodec(t *testing.T) {
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
	if len(inventory.SettingsFiles) != 0 || len(inventory.Settings) != 1 || len(inventory.Settings[0].Actions) != 0 {
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

// This filesystem observes the production content-open boundary.
type authorityObservationFS struct{ opens int }

func (f *authorityObservationFS) Lstat(path string) (os.FileInfo, error) { return os.Lstat(path) }
func (f *authorityObservationFS) Open(path string) (homeManagerFile, error) {
	f.opens++
	return os.Open(path)
}

func TestHomeManagerBoundByteCodecRetainsMetadataWithoutOpeningPayload(t *testing.T) {
	registry, err := hmregistry.Load(filepath.Join("..", "..", "..", "catalog", "home-manager", "registry.jsonc"))
	if err != nil {
		t.Fatal(err)
	}
	var entry hmregistry.Entry
	for _, candidate := range registry.Entries {
		if candidate.ID == "ripgrep" {
			entry = candidate
		}
	}
	home := t.TempDir()
	environment := config.PathEnvironment{GOOS: "linux", Home: home, Env: map[string]string{}}
	variant := modules.PlatformVariant{Realization: "endstate-restore", Matches: modules.MatchCriteria{}, HomeManagerAdapter: &modules.HomeManagerAdapterBinding{ID: entry.ID, ReviewFingerprint: entry.ReviewFingerprint, ConfigSetID: "default", GenerationID: "g1"}, Capture: &modules.CaptureDef{}}
	for index, target := range entry.Targets {
		dest := fmt.Sprintf("apps/ripgrep/settings-%d", index)
		variant.Capture.Files = append(variant.Capture.Files, modules.CaptureFile{Source: target.Coordinate, Dest: dest, Optional: target.Optional})
		variant.Restore = append(variant.Restore, modules.RestoreDef{Type: "copy", Source: "./payload/" + dest, Target: target.Coordinate, Optional: target.Optional, Backup: true})
		source, err := config.ResolveEnginePath(target.Coordinate, environment)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(source), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(source, []byte("--hidden\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	body, err := json.Marshal(modules.Module{ModuleSchemaVersion: 3, ID: entry.ModuleID, DisplayName: "ripgrep", Sensitivity: "low", Platforms: map[string]modules.PlatformVariant{"linux": variant}})
	if err != nil {
		t.Fatal(err)
	}
	mod, err := modules.ParseAndValidateModuleJSON(body, "module.jsonc")
	if err != nil {
		t.Fatal(err)
	}
	other := *mod
	other.ID = "apps.unrelated"
	otherBody, err := json.Marshal(other)
	if err != nil {
		t.Fatal(err)
	}
	otherModule, err := modules.ParseAndValidateModuleJSON(otherBody, "other.jsonc")
	if err != nil {
		t.Fatal(err)
	}
	filesystem := &authorityObservationFS{}
	inventory, err := (HomeManagerAdapter{Registry: &hmregistry.Registry{Entries: []hmregistry.Entry{entry}}, Modules: map[string]*modules.Module{mod.ID: mod, otherModule.ID: otherModule}, Packages: testPackageCatalog(t).Entries(), Environment: environment, FS: filesystem}).Inventory(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(inventory.Settings) != 1 || len(inventory.Settings[0].Evidence) != len(entry.Targets) || len(inventory.Settings[0].Actions) != 0 || len(inventory.SettingsFiles) != 0 || filesystem.opens != 0 || len(inventory.Warnings) != 1 || inventory.Warnings[0].Code != "settings_content_policy_unavailable" {
		t.Fatalf("bound metadata granted byte-codec payload authority: inventory=%+v opens=%d", inventory, filesystem.opens)
	}
}

func TestHomeManagerExcludedTargetRemainsVisibleWithoutPayloadAuthority(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "settings"), []byte("excluded content"), 0600); err != nil {
		t.Fatal(err)
	}
	filesystem := &authorityObservationFS{}
	inventory, err := (HomeManagerAdapter{Registry: &hmregistry.Registry{Entries: []hmregistry.Entry{{ID: "excluded", DisplayName: "Excluded", ModuleID: "apps.excluded", Disposition: hmregistry.Excluded, Targets: []hmregistry.Target{{Coordinate: "${home}/settings"}}}}}, Environment: config.PathEnvironment{GOOS: "linux", Home: home, Env: map[string]string{}}, FS: filesystem}).Inventory(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(inventory.Settings) != 1 || len(inventory.Settings[0].Evidence) != 1 || len(inventory.Settings[0].Actions) != 0 || len(inventory.SettingsFiles) != 0 || filesystem.opens != 0 || len(inventory.Warnings) != 1 || inventory.Warnings[0].Code != "settings_adapter_excluded" {
		t.Fatalf("excluded candidate lost evidence or gained payload authority: inventory=%+v opens=%d", inventory, filesystem.opens)
	}
}
