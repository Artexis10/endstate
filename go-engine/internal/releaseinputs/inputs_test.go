// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package releaseinputs

import (
	"strings"
	"testing"
)

func TestLoadReturnsImmutableCompatibleInputPair(t *testing.T) {
	inputs, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if inputs.SchemaVersion != 1 {
		t.Fatalf("schemaVersion = %d, want 1", inputs.SchemaVersion)
	}
	for name, input := range map[string]Input{"nixpkgs": inputs.Nixpkgs, "home-manager": inputs.HomeManager} {
		if len(input.Revision) != 40 || strings.Trim(input.Revision, "0123456789abcdef") != "" {
			t.Errorf("%s revision = %q, want full lowercase Git revision", name, input.Revision)
		}
		if !strings.HasSuffix(input.FlakeRef, "/"+input.Revision) {
			t.Errorf("%s flakeRef = %q, want revision-qualified ref", name, input.FlakeRef)
		}
		if !strings.HasPrefix(input.NarHash, "sha256-") {
			t.Errorf("%s narHash = %q, want SRI hash", name, input.NarHash)
		}
	}
	if inputs.HomeManager.CompatibleNixpkgsRevision != inputs.Nixpkgs.Revision {
		t.Fatalf("home-manager compatibility revision = %q, nixpkgs = %q", inputs.HomeManager.CompatibleNixpkgsRevision, inputs.Nixpkgs.Revision)
	}
}

func TestParseRejectsUnknownFieldsAndFloatingRefs(t *testing.T) {
	base := `{"schemaVersion":1,"nixpkgs":{"revision":"1111111111111111111111111111111111111111","flakeRef":"github:NixOS/nixpkgs/1111111111111111111111111111111111111111","narHash":"sha256-x"},"homeManager":{"revision":"2222222222222222222222222222222222222222","flakeRef":"github:nix-community/home-manager/2222222222222222222222222222222222222222","narHash":"sha256-y","compatibleNixpkgsRevision":"1111111111111111111111111111111111111111"}}`
	if _, err := Parse([]byte(strings.Replace(base, `"schemaVersion":1`, `"schemaVersion":1,"surprise":true`, 1))); err == nil {
		t.Fatal("Parse accepted unknown top-level field")
	}
	if _, err := Parse([]byte(strings.Replace(base, "github:NixOS/nixpkgs/1111111111111111111111111111111111111111", "nixpkgs", 1))); err == nil {
		t.Fatal("Parse accepted floating nixpkgs flake ref")
	}
}
