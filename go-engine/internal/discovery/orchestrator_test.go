// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package discovery

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/packagecatalog"
)

type fakeAdapter struct {
	id     string
	result AdapterInventory
	err    error
}

func (adapter fakeAdapter) ID() string { return adapter.id }
func (adapter fakeAdapter) Inventory(context.Context) (AdapterInventory, error) {
	return adapter.result, adapter.err
}

type cancelingAdapter struct{ cancel context.CancelFunc }

func (cancelingAdapter) ID() string { return "z-canceling-source" }
func (adapter cancelingAdapter) Inventory(context.Context) (AdapterInventory, error) {
	adapter.cancel()
	return AdapterInventory{}, context.Canceled
}

func TestDiscoveryCancellationDoesNotPublishPartialInventory(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	orchestrator := Orchestrator{
		Catalog: testPackageCatalog(t), NixpkgsInput: "github:NixOS/nixpkgs/release",
		Adapters: []Adapter{
			fakeAdapter{id: "a-readable", result: AdapterInventory{Items: []InventoryItem{{Source: "debian", Ref: "ripgrep"}}}},
			cancelingAdapter{cancel: cancel},
		},
	}
	result, err := orchestrator.Discover(ctx, Request{})
	if !errors.Is(err, context.Canceled) || len(result.Resolved) != 0 {
		t.Fatalf("cancellation published partial inventory: result=%+v error=%v", result, err)
	}
}

func testPackageCatalog(t *testing.T) *packagecatalog.Catalog {
	t.Helper()
	dir := t.TempDir()
	data := `{"schemaVersion":1,"id":"ripgrep","displayName":"ripgrep","nix":{"attribute":"ripgrep"},"aliases":{"debian":["ripgrep"],"desktop":["ripgrep.desktop"]},"configModule":"apps.ripgrep"}`
	if err := os.WriteFile(filepath.Join(dir, "ripgrep.jsonc"), []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	catalog, err := packagecatalog.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	return catalog
}

func TestOrchestratorScopesFailuresAndReconcilesDeterministically(t *testing.T) {
	items := []InventoryItem{
		{Source: "debian", Kind: EvidencePackage, Ref: "unmapped", DisplayName: "Unmapped", Explicit: true, UserFacing: true},
		{Source: "debian", Kind: EvidencePackage, Ref: "ripgrep", DisplayName: "ripgrep", Version: "14.1", Explicit: true},
		{Source: "desktop", Kind: EvidenceDesktop, Ref: "ripgrep.desktop", DisplayName: "ripgrep", UserFacing: true},
	}
	makeOrchestrator := func(items []InventoryItem) Orchestrator {
		return Orchestrator{
			Adapters: []Adapter{
				fakeAdapter{id: "z-failed", err: errors.New("malformed inventory")},
				fakeAdapter{id: "a-unavailable", err: ErrNotAvailable},
				fakeAdapter{id: "m-native", result: AdapterInventory{Items: items, Ignored: 7}},
			},
			Catalog:      testPackageCatalog(t),
			NixpkgsInput: "github:NixOS/nixpkgs/1111111111111111111111111111111111111111",
		}
	}

	first, err := makeOrchestrator(items).Discover(context.Background(), Request{})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Sources) != 3 || first.Sources[0].ID != "a-unavailable" || first.Sources[2].ID != "z-failed" {
		t.Fatalf("source statuses are not deterministic: %+v", first.Sources)
	}
	if len(first.Resolved) != 1 || first.Resolved[0].ID != "ripgrep" || len(first.Resolved[0].Evidence) != 2 {
		t.Fatalf("resolved intents = %+v", first.Resolved)
	}
	if first.Resolved[0].Installable != "github:NixOS/nixpkgs/1111111111111111111111111111111111111111#ripgrep" {
		t.Fatalf("installable = %q", first.Resolved[0].Installable)
	}
	if len(first.Unresolved) != 1 || first.Unresolved[0].Reason != "mapping_missing" {
		t.Fatalf("unresolved = %+v", first.Unresolved)
	}
	if first.Counts != (Counts{Discovered: 2, Resolved: 1, Selected: 1, Unresolved: 1, Ignored: 7}) {
		t.Fatalf("counts = %+v", first.Counts)
	}
	if len(first.Settings) != 0 {
		t.Fatalf("a package-to-module association was presented as detected live settings: %+v", first.Settings)
	}
	if len(first.Warnings) != 1 || first.Warnings[0].Source != "z-failed" {
		t.Fatalf("warnings = %+v", first.Warnings)
	}

	reversed := append([]InventoryItem(nil), items...)
	for left, right := 0, len(reversed)-1; left < right; left, right = left+1, right-1 {
		reversed[left], reversed[right] = reversed[right], reversed[left]
	}
	second, err := makeOrchestrator(reversed).Discover(context.Background(), Request{})
	if err != nil {
		t.Fatal(err)
	}
	one, _ := json.Marshal(first)
	two, _ := json.Marshal(second)
	if string(one) != string(two) {
		t.Fatalf("input ordering changed result:\n%s\n%s", one, two)
	}
}

func TestLinuxDiscoveryCountsReconcile(t *testing.T) {
	for _, selection := range []struct {
		name     string
		only     []string
		selected int
		plans    int
	}{
		{name: "all", selected: 3, plans: 4},
		{name: "module", only: []string{"apps.ripgrep"}, selected: 1, plans: 1},
		{name: "settings only", only: []string{"standalone"}, selected: 1, plans: 1},
		{name: "equal package and settings ids", only: []string{"ripgrep"}, selected: 2, plans: 3},
	} {
		t.Run(selection.name, func(t *testing.T) {
			candidates := []SettingsCandidate{
				{ID: "apps.ripgrep", ModuleID: "apps.ripgrep", PackageID: "ripgrep", Actions: []SettingsAction{SettingsCapture}},
				{ID: "alternate", PackageID: "ripgrep", Actions: []SettingsAction{SettingsCapture}},
				{ID: "standalone", Actions: []SettingsAction{SettingsCapture}},
				{ID: "ripgrep", Actions: []SettingsAction{SettingsCapture}},
				{ID: "review-only"},
			}
			plans := make([]SettingsFilePlan, 0, 4)
			for _, candidate := range candidates[:4] {
				plans = append(plans, SettingsFilePlan{CandidateID: candidate.ID, Target: "${home}/" + candidate.ID, Source: filepath.Join(t.TempDir(), "config")})
			}
			result, err := (Orchestrator{
				Catalog: testPackageCatalog(t), NixpkgsInput: "github:NixOS/nixpkgs/release",
				Adapters: []Adapter{
					fakeAdapter{id: "packages", result: AdapterInventory{Ignored: 7, Items: []InventoryItem{
						{Source: "debian", Ref: "ripgrep", Explicit: true},
						{Source: "desktop", Ref: "ripgrep.desktop", UserFacing: true},
						{Source: "debian", Ref: "unmapped", Explicit: true},
						{Source: "debian", Ref: "unmapped", Explicit: true},
					}}},
					fakeAdapter{id: "settings", result: AdapterInventory{Settings: candidates, SettingsFiles: plans}},
				},
			}).Discover(context.Background(), Request{Only: selection.only})
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Resolved) != 1 || len(result.Settings) != 5 || len(result.Unresolved) != 1 {
				t.Fatalf("selection truncated the public inventory: %+v", result)
			}
			data, err := json.Marshal(result.Counts)
			if err != nil {
				t.Fatal(err)
			}
			var counts map[string]int
			if err := json.Unmarshal(data, &counts); err != nil {
				t.Fatal(err)
			}
			for name, want := range map[string]int{"discovered": 5, "resolved": 1, "settings": 3, "settingsOnly": 2, "unresolved": 2, "ignored": 7, "selected": selection.selected} {
				if counts[name] != want {
					t.Errorf("counts.%s = %d, want %d; counts=%s", name, counts[name], want, data)
				}
			}
			if len(result.Sources) != 2 || result.Sources[0].Discovered != 4 || result.Sources[1].Discovered != 5 {
				t.Errorf("raw source observations changed: %+v", result.Sources)
			}
			if len(result.SettingsFiles) != selection.plans {
				t.Errorf("selected private plans = %d, want %d", len(result.SettingsFiles), selection.plans)
			}
		})
	}
}

func TestOrchestratorMarksDetectedSettingsResolvedWhenPackageEvidenceExists(t *testing.T) {
	orchestrator := Orchestrator{
		Adapters: []Adapter{
			fakeAdapter{id: "home-manager", result: AdapterInventory{Settings: []SettingsCandidate{{
				ID: "apps.ripgrep", ModuleID: "apps.ripgrep", DisplayName: "ripgrep settings",
				PackageID: "ripgrep", Portability: PortabilityConfigOnly,
				Actions:  []SettingsAction{SettingsCapture, SettingsRestore},
				Evidence: []Evidence{{Source: "home-manager", Kind: EvidenceConfig, Ref: "${xdg.config}/ripgrep/ripgreprc"}},
			}}}},
			fakeAdapter{id: "native", result: AdapterInventory{Items: []InventoryItem{{
				Source: "debian", Kind: EvidencePackage, Ref: "ripgrep", Explicit: true,
			}}}},
		},
		Catalog: testPackageCatalog(t), NixpkgsInput: "github:NixOS/nixpkgs/release",
	}

	result, err := orchestrator.Discover(context.Background(), Request{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Settings) != 1 || result.Settings[0].Portability != PortabilityResolved {
		t.Fatalf("settings = %+v, want one detected resolved settings candidate", result.Settings)
	}
}

func TestOrchestratorFailsOnlyWithoutTrustworthySelectedSource(t *testing.T) {
	orchestrator := Orchestrator{Adapters: []Adapter{
		fakeAdapter{id: "missing", err: ErrNotAvailable},
		fakeAdapter{id: "broken", err: errors.New("permission denied")},
	}}
	if _, err := orchestrator.Discover(context.Background(), Request{}); err == nil {
		t.Fatal("Discover accepted a run with no trustworthy source")
	}

	orchestrator.Adapters = append(orchestrator.Adapters, fakeAdapter{id: "empty-but-readable", result: AdapterInventory{}})
	if _, err := orchestrator.Discover(context.Background(), Request{}); err != nil {
		t.Fatalf("a trustworthy empty source should scope the other failures: %v", err)
	}
}

func TestOrchestratorReconcilesManagedProfileWithNativeEvidence(t *testing.T) {
	orchestrator := Orchestrator{
		Adapters: []Adapter{
			fakeAdapter{id: "endstate-profile", result: AdapterInventory{Resolved: []ResolvedIntent{{
				ID: "ripgrep", DisplayName: "ripgrep", Attribute: "ripgrep",
				Input: "github:NixOS/nixpkgs/profile", Installable: "github:NixOS/nixpkgs/profile#ripgrep",
				MappingRevision: "endstate-profile-v1",
				Evidence:        []Evidence{{Source: "endstate-profile", Kind: EvidencePackage, Ref: "github:NixOS/nixpkgs/profile#ripgrep"}},
			}}}},
			fakeAdapter{id: "native", result: AdapterInventory{Items: []InventoryItem{{
				Source: "debian", Kind: EvidencePackage, Ref: "ripgrep", Version: "14.1", Explicit: true,
			}}}},
		},
		Catalog: testPackageCatalog(t), NixpkgsInput: "github:NixOS/nixpkgs/release",
	}
	result, err := orchestrator.Discover(context.Background(), Request{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Resolved) != 1 {
		t.Fatalf("resolved = %+v", result.Resolved)
	}
	intent := result.Resolved[0]
	if intent.Installable != "github:NixOS/nixpkgs/profile#ripgrep" || len(intent.Evidence) != 2 {
		t.Fatalf("reconciled intent = %+v", intent)
	}
	if result.Counts.Discovered != 1 || result.Counts.Resolved != 1 {
		t.Fatalf("counts = %+v", result.Counts)
	}
}

func TestOrchestratorPrefersEndstateProfileOverDuplicateUserProfileIntent(t *testing.T) {
	orchestrator := Orchestrator{Adapters: []Adapter{
		fakeAdapter{id: "endstate-profile", result: AdapterInventory{Resolved: []ResolvedIntent{{
			ID: "ripgrep", DisplayName: "ripgrep", Attribute: "ripgrep",
			Input: "github:NixOS/nixpkgs/endstate", Installable: "github:NixOS/nixpkgs/endstate#ripgrep",
			MappingRevision: "endstate-profile-v1",
			Evidence:        []Evidence{{Source: "endstate-profile", Kind: EvidencePackage, Ref: "github:NixOS/nixpkgs/endstate#ripgrep"}},
		}}}},
		fakeAdapter{id: "nix-user-profile", result: AdapterInventory{Resolved: []ResolvedIntent{{
			ID: "ripgrep", DisplayName: "ripgrep", Attribute: "legacyPackages.x86_64-linux.ripgrep",
			Input: "github:NixOS/nixpkgs/user", Installable: "github:NixOS/nixpkgs/user#legacyPackages.x86_64-linux.ripgrep",
			MappingRevision: "nix-user-profile-v1",
			Evidence:        []Evidence{{Source: "nix-user-profile", Kind: EvidencePackage, Ref: "github:NixOS/nixpkgs/user#legacyPackages.x86_64-linux.ripgrep"}},
		}}}},
	}}
	result, err := orchestrator.Discover(context.Background(), Request{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Resolved) != 1 || result.Resolved[0].Installable != "github:NixOS/nixpkgs/endstate#ripgrep" || len(result.Resolved[0].Evidence) != 2 {
		t.Fatalf("reconciled profile intent = %+v", result.Resolved)
	}
}
