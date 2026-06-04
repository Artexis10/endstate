// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package manifest

import (
	"strings"
	"testing"
)

// findCode reports whether errs contains a ValidationError with the given code.
func findCode(errs []ValidationError, code string) bool {
	for _, e := range errs {
		if e.Code == code {
			return true
		}
	}
	return false
}

// TestValidateManifestApps_CaskRefWithoutBrewDriver: a darwin ref marked as a
// Cask (cask: prefix) without driver:"brew" is rejected — the Nix realizer must
// never see a cask ref. Host-independent (validation runs on all OSes).
func TestValidateManifestApps_CaskRefWithoutBrewDriver(t *testing.T) {
	m := &Manifest{
		Version: 1,
		Apps: []App{
			{ID: "chrome", Refs: map[string]string{"darwin": "cask:google-chrome"}},
		},
	}
	errs := ValidateManifestApps(m)
	if !findCode(errs, "CASK_REF_REQUIRES_BREW_DRIVER") {
		t.Fatalf("expected CASK_REF_REQUIRES_BREW_DRIVER, got %+v", errs)
	}
}

// TestValidateManifestApps_CaskRefWithBrewDriver: a darwin cask ref WITH
// driver:"brew" is valid.
func TestValidateManifestApps_CaskRefWithBrewDriver(t *testing.T) {
	m := &Manifest{
		Version: 1,
		Apps: []App{
			{ID: "chrome", Driver: "brew", Refs: map[string]string{"darwin": "cask:google-chrome"}},
		},
	}
	errs := ValidateManifestApps(m)
	if findCode(errs, "CASK_REF_REQUIRES_BREW_DRIVER") {
		t.Fatalf("cask ref with driver:brew must be valid, got %+v", errs)
	}
	if findCode(errs, "BREW_DRIVER_REQUIRES_DARWIN_REF") {
		t.Fatalf("a darwin ref is present, must not require one, got %+v", errs)
	}
}

// TestValidateManifestApps_CaskDriverCaseInsensitive: driver matching is
// case-insensitive ("Brew" counts as the brew driver).
func TestValidateManifestApps_CaskDriverCaseInsensitive(t *testing.T) {
	m := &Manifest{
		Version: 1,
		Apps: []App{
			{ID: "chrome", Driver: "Brew", Refs: map[string]string{"darwin": "cask:google-chrome"}},
		},
	}
	errs := ValidateManifestApps(m)
	if findCode(errs, "CASK_REF_REQUIRES_BREW_DRIVER") {
		t.Fatalf("driver matching must be case-insensitive, got %+v", errs)
	}
}

// TestValidateManifestApps_BrewDriverWithoutDarwinRef: driver:"brew" with no
// darwin ref is rejected — a brew app must have a darwin package to install.
func TestValidateManifestApps_BrewDriverWithoutDarwinRef(t *testing.T) {
	m := &Manifest{
		Version: 1,
		Apps: []App{
			{ID: "ghostty", Driver: "brew", Refs: map[string]string{"windows": "Ghostty.Ghostty"}},
		},
	}
	errs := ValidateManifestApps(m)
	if !findCode(errs, "BREW_DRIVER_REQUIRES_DARWIN_REF") {
		t.Fatalf("expected BREW_DRIVER_REQUIRES_DARWIN_REF, got %+v", errs)
	}
}

// TestValidateManifestApps_BrewDriverWithBareDarwinRef: driver:"brew" with a
// bare (formula) darwin ref is valid — no cask prefix required.
func TestValidateManifestApps_BrewDriverWithBareDarwinRef(t *testing.T) {
	m := &Manifest{
		Version: 1,
		Apps: []App{
			{ID: "ripgrep", Driver: "brew", Refs: map[string]string{"darwin": "ripgrep"}},
		},
	}
	errs := ValidateManifestApps(m)
	if len(errs) != 0 {
		t.Fatalf("a brew formula app must validate cleanly, got %+v", errs)
	}
}

// TestValidateManifestApps_NoBrewNoCask_Unchanged: a plain nix-style app (no
// driver, bare darwin ref) is unaffected by the new checks.
func TestValidateManifestApps_NoBrewNoCask_Unchanged(t *testing.T) {
	m := &Manifest{
		Version: 1,
		Apps: []App{
			{ID: "ripgrep", Refs: map[string]string{"darwin": "ripgrep", "linux": "ripgrep"}},
		},
	}
	errs := ValidateManifestApps(m)
	if len(errs) != 0 {
		t.Fatalf("a plain nix app must validate cleanly, got %+v", errs)
	}
}

// TestLoadManifest_CaskRefWithoutBrewDriver_FailsLoad: the cask validation
// surfaces through the loader (host-independent) with a clear message.
func TestLoadManifest_CaskRefWithoutBrewDriver_FailsLoad(t *testing.T) {
	p := writeSecretsManifest(t, `{
  "version": 1, "name": "cask-noload", "apps": [
    { "id": "chrome", "refs": { "darwin": "cask:google-chrome" } }
  ]
}`)
	_, err := LoadManifest(p)
	if err == nil {
		t.Fatal("expected a load error for a cask ref without driver:brew, got nil")
	}
	if !strings.Contains(err.Error(), "brew") {
		t.Errorf("error = %q, want it to mention the brew driver requirement", err)
	}
}
