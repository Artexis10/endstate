// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package discovery

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestNixUserProfileAdapterPreservesLockedFlakeAndFullAttribute(t *testing.T) {
	runner := &fixtureRunner{outputs: map[string][]byte{
		"nix": []byte(`{"version":3,"elements":{"go":{"active":true,"attrPath":"legacyPackages.x86_64-linux.go","originalUrl":"flake:nixpkgs","url":"github:NixOS/nixpkgs/1111111111111111111111111111111111111111","storePaths":["/nix/store/00000000000000000000000000000000-go-1.26.3"]}}}`),
	}}
	inventory, err := (NixUserProfileAdapter{
		Runner: runner, DefaultInput: "github:NixOS/nixpkgs/2222222222222222222222222222222222222222",
	}).Inventory(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(inventory.Resolved) != 1 {
		t.Fatalf("resolved = %+v", inventory.Resolved)
	}
	intent := inventory.Resolved[0]
	if intent.ID != "go" || intent.Attribute != "legacyPackages.x86_64-linux.go" {
		t.Fatalf("profile intent identity = %+v", intent)
	}
	wantInstallable := "github:NixOS/nixpkgs/1111111111111111111111111111111111111111#legacyPackages.x86_64-linux.go"
	if intent.Installable != wantInstallable || intent.Input != "github:NixOS/nixpkgs/1111111111111111111111111111111111111111" {
		t.Fatalf("profile intent provenance = %+v", intent)
	}
	if len(intent.Evidence) != 1 || intent.Evidence[0].Source != "nix-user-profile" || intent.Evidence[0].Version != "1.26.3" {
		t.Fatalf("profile evidence = %+v", intent.Evidence)
	}
	if len(runner.commands) != 1 || !reflect.DeepEqual(runner.commands[0].Args, []string{"profile", "list", "--json", "--no-pretty"}) {
		t.Fatalf("profile command = %+v", runner.commands)
	}
}

func TestNixUserProfileAdapterIsUnavailableWithoutNix(t *testing.T) {
	runner := &fixtureRunner{errors: map[string]error{"nix": ErrNotAvailable}}
	_, err := (NixUserProfileAdapter{Runner: runner}).Inventory(context.Background())
	if !errors.Is(err, ErrNotAvailable) {
		t.Fatalf("error = %v, want ErrNotAvailable", err)
	}
}
