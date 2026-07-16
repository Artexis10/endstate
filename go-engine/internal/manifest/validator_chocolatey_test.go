// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package manifest

import "testing"

func TestValidateManifestApps_NormalizesKnownDriverNames(t *testing.T) {
	m := &Manifest{Version: 1, Apps: []App{
		{ID: "git", Driver: "WiNgEt", Refs: map[string]string{"windows": "Git.Git"}},
		{ID: "ripgrep", Driver: "BrEw", Refs: map[string]string{"darwin": "ripgrep"}},
		{ID: "sevenzip", Driver: "ChOcOlAtEy", Refs: map[string]string{"windows": "7zip"}},
	}}

	if errs := ValidateManifestApps(m); len(errs) != 0 {
		t.Fatalf("known driver names must validate, got %+v", errs)
	}
	want := []string{"winget", "brew", "chocolatey"}
	for i := range want {
		if m.Apps[i].Driver != want[i] {
			t.Errorf("apps[%d].Driver = %q, want %q", i, m.Apps[i].Driver, want[i])
		}
	}
}

func TestValidateManifestApps_RejectsUnknownDriver(t *testing.T) {
	for _, name := range []string{"scoop", "nix"} {
		t.Run(name, func(t *testing.T) {
			m := &Manifest{Version: 1, Apps: []App{
				{ID: "tool", Driver: name, Refs: map[string]string{"windows": "tool"}},
			}}

			errs := ValidateManifestApps(m)
			if !findCode(errs, "UNSUPPORTED_APP_DRIVER") {
				t.Fatalf("expected UNSUPPORTED_APP_DRIVER, got %+v", errs)
			}
		})
	}
}

func TestValidateManifestApps_ChocolateyRequiresWindowsRef(t *testing.T) {
	m := &Manifest{Version: 1, Apps: []App{
		{ID: "tool", Driver: "chocolatey", Refs: map[string]string{"darwin": "tool"}},
	}}

	errs := ValidateManifestApps(m)
	if !findCode(errs, "CHOCOLATEY_DRIVER_REQUIRES_WINDOWS_REF") {
		t.Fatalf("expected CHOCOLATEY_DRIVER_REQUIRES_WINDOWS_REF, got %+v", errs)
	}
}

func TestLoadManifest_NormalizesChocolateyDriver(t *testing.T) {
	path := writeSecretsManifest(t, `{
  "version": 1,
  "apps": [
    {"id": "sevenzip", "driver": "Chocolatey", "refs": {"windows": "7zip"}}
  ]
}`)

	m, err := LoadManifest(path)
	if err != nil {
		t.Fatalf("LoadManifest() error = %v", err)
	}
	if got := m.Apps[0].Driver; got != "chocolatey" {
		t.Errorf("Driver = %q, want chocolatey", got)
	}
}

func TestValidateProfile_RejectsUnknownDriver(t *testing.T) {
	path := writeSecretsManifest(t, `{
  "version": 1,
  "apps": [
    {"id": "tool", "driver": "scoop", "refs": {"windows": "tool"}}
  ]
}`)

	result := ValidateProfile(path)
	if result.Valid || !findCode(result.Errors, "UNSUPPORTED_APP_DRIVER") {
		t.Fatalf("ValidateProfile() = %+v, want UNSUPPORTED_APP_DRIVER", result)
	}
}
