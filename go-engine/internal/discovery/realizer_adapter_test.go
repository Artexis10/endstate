// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package discovery

import (
	"context"
	"errors"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/realizer"
)

type inventoryRealizer struct {
	set realizer.Set
	err error
}

func (backend inventoryRealizer) Name() string                   { return "nix" }
func (backend inventoryRealizer) Current() (realizer.Set, error) { return backend.set, backend.err }
func (backend inventoryRealizer) Plan([]realizer.Installable) (realizer.Diff, error) {
	return realizer.Diff{}, nil
}
func (backend inventoryRealizer) Realize([]realizer.Installable) (realizer.Result, error) {
	return realizer.Result{}, nil
}

func TestRealizerInventoryAdapterPreservesLockedProfileIntent(t *testing.T) {
	adapter := RealizerInventoryAdapter{
		Realizer: inventoryRealizer{set: realizer.Set{Elements: map[string]realizer.Element{
			"ripgrep": {
				Name: "ripgrep", AttrPath: "legacyPackages.x86_64-linux.ripgrep",
				OriginalURL: "github:NixOS/nixpkgs/original#ripgrep",
				URL:         "github:NixOS/nixpkgs/locked#ripgrep",
				StorePaths:  []string{"/nix/store/00000000000000000000000000000000-ripgrep-14.1.1"},
			},
		}}},
		DefaultInput: "github:NixOS/nixpkgs/release",
	}
	inventory, err := adapter.Inventory(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if adapter.ID() != "endstate-profile" || len(inventory.Resolved) != 1 {
		t.Fatalf("inventory = %+v", inventory)
	}
	intent := inventory.Resolved[0]
	if intent.ID != "ripgrep" || intent.Installable != "github:NixOS/nixpkgs/locked#ripgrep" || intent.Input != "github:NixOS/nixpkgs/locked" {
		t.Fatalf("intent = %+v", intent)
	}
	if len(intent.Evidence) != 1 || intent.Evidence[0].Version != "14.1.1" || intent.Evidence[0].Source != "endstate-profile" {
		t.Fatalf("evidence = %+v", intent.Evidence)
	}
}

func TestRealizerInventoryAdapterScopesUnavailableAndReadFailures(t *testing.T) {
	if _, err := (RealizerInventoryAdapter{}).Inventory(context.Background()); !errors.Is(err, ErrNotAvailable) {
		t.Fatalf("nil realizer error = %v, want not available", err)
	}
	want := errors.New("profile unreadable")
	_, err := (RealizerInventoryAdapter{Realizer: inventoryRealizer{err: want}}).Inventory(context.Background())
	if !errors.Is(err, want) {
		t.Fatalf("read error = %v, want wrapped %v", err, want)
	}
}

func TestProfileInstallableUsesFullAttributeWhenLockedURLHasNoFragment(t *testing.T) {
	element := realizer.Element{
		Name: "go", AttrPath: "legacyPackages.x86_64-linux.go",
		URL: "github:NixOS/nixpkgs/1111111111111111111111111111111111111111",
	}
	got := profileInstallable(profileAttribute("go", element), element, "github:NixOS/nixpkgs/release")
	want := "github:NixOS/nixpkgs/1111111111111111111111111111111111111111#legacyPackages.x86_64-linux.go"
	if got != want {
		t.Fatalf("profile installable = %q, want %q", got, want)
	}
}
