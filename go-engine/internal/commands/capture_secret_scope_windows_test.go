// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package commands

import (
	"path/filepath"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/driver"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
)

// TestRunCapture_WarnsWhenModuleSettingsOverlapItsSecrets catches a capture
// that skips registry settings because they sit inside the module's own
// declared secrets, but leaves no structured warning naming the module. The
// skip itself must not fail the capture.
func TestRunCapture_WarnsWhenModuleSettingsOverlapItsSecrets(t *testing.T) {
	withCaptureEnumerators(t, map[string]fakeInstalledEnumerator{
		"winget": {packages: []driver.InstalledPackage{{Ref: "Vendor.Example", DisplayName: "Example", Version: "1.0"}}},
	}, nil)
	secret := `HKCU\Software\Vendor\Example\Secret`
	catalog := map[string]*modules.Module{"apps.example": {
		ID: "apps.example", DisplayName: "Example", Sensitivity: "low",
		Matches: modules.MatchCriteria{Winget: []string{"Vendor.Example"}},
		Capture: &modules.CaptureDef{RegistryKeys: []modules.CaptureRegistryKey{{Key: secret + `\Session`, Dest: "session.reg"}}},
		Secrets: &modules.SecretsDef{RegistryKeys: []string{secret}},
	}}

	withMockCatalog(catalog, nil, func() {
		raw, eerr := RunCapture(CaptureFlags{Out: filepath.Join(t.TempDir(), "captured.jsonc")})
		if eerr != nil {
			t.Fatalf("RunCapture: %v", eerr)
		}
		result := raw.(*CaptureResult)
		warning := warningByCode(result.Warnings, "config_secret_scope_conflict")
		if warning == nil || warning.ModuleID != "apps.example" || warning.Message == "" {
			t.Fatalf("warnings = %+v, want config_secret_scope_conflict for apps.example", result.Warnings)
		}
		for _, module := range result.ConfigModules {
			if module.ID == "apps.example" && len(module.Warnings) == 1 && module.Warnings[0] == warning.Message {
				return
			}
		}
		t.Fatalf("configModules = %+v, want the module row to carry the same warning", result.ConfigModules)
	})
}
