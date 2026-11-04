// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package modules

import (
	"path/filepath"
	"strings"
	"testing"
)

// isHKCUSettings reports whether a registry path/key targets HKCU.
func isHKCUSettings(p string) bool {
	u := strings.ToUpper(strings.TrimSpace(p))
	return strings.HasPrefix(u, `HKCU\`) || strings.HasPrefix(u, `HKEY_CURRENT_USER\`)
}

// windowsSettingsModules returns the Windows-settings tier exactly as the
// production catalog loads it.
func windowsSettingsModules(t *testing.T) map[string]*Module {
	t.Helper()
	catalog, err := GetCatalog(productionRepoRoot())
	if err != nil {
		t.Fatalf("GetCatalog: %v", err)
	}
	tier := map[string]*Module{}
	for id, mod := range catalog {
		if mod.Tier == "windows-settings" {
			tier[id] = mod
		}
	}
	return tier
}

// TestWindowsSettingsCatalog_Loads asserts the production catalog loads the
// windows-settings modules with the value-level shape restore relies on:
// HKCU-only registry-set restore, registry-value-equals verify, and
// registryValues capture.
func TestWindowsSettingsCatalog_Loads(t *testing.T) {
	catalog := windowsSettingsModules(t)

	want := []string{
		"windows-settings.personalization",
		"windows-settings.explorer",
		"windows-settings.taskbar",
	}
	for _, id := range want {
		if _, ok := catalog[id]; !ok {
			t.Errorf("expected windows-settings module %q to load; catalog has %d modules", id, len(catalog))
		}
	}
	if len(catalog) < len(want) {
		t.Fatalf("loaded only %d windows-settings modules, expected at least %d", len(catalog), len(want))
	}

	for id, mod := range catalog {
		// id must match its directory: windows-settings.<dir>
		dir := filepath.Base(mod.ModuleDir)
		if mod.ID != "windows-settings."+dir {
			t.Errorf("%s: id %q does not match directory (expected %q)", dir, mod.ID, "windows-settings."+dir)
		}

		// Every restore must be a value-level, HKCU-only registry-set.
		if len(mod.Restore) == 0 {
			t.Errorf("%s: expected at least one restore entry", id)
		}
		for i, r := range mod.Restore {
			if r.Type != "registry-set" {
				t.Errorf("%s: restore[%d] type %q, expected registry-set", id, i, r.Type)
			}
			if !isHKCUSettings(r.Key) {
				t.Errorf("%s: restore[%d] key %q is not HKCU", id, i, r.Key)
			}
			if r.ValueName == "" {
				t.Errorf("%s: restore[%d] missing valueName", id, i)
			}
		}

		// Every verify must be a value assertion on an HKCU key.
		for i, v := range mod.Verify {
			if v.Type != "registry-value-equals" {
				t.Errorf("%s: verify[%d] type %q, expected registry-value-equals", id, i, v.Type)
			}
			if !isHKCUSettings(v.Path) {
				t.Errorf("%s: verify[%d] path %q is not HKCU", id, i, v.Path)
			}
			if v.ValueName == "" {
				t.Errorf("%s: verify[%d] missing valueName", id, i)
			}
		}

		// Capture must use value-level registryValues, all HKCU.
		if mod.Capture == nil || len(mod.Capture.RegistryValues) == 0 {
			t.Errorf("%s: expected capture.registryValues", id)
			continue
		}
		for i, rv := range mod.Capture.RegistryValues {
			if !isHKCUSettings(rv.Key) {
				t.Errorf("%s: capture.registryValues[%d] key %q is not HKCU", id, i, rv.Key)
			}
			if rv.ValueName == "" {
				t.Errorf("%s: capture.registryValues[%d] missing valueName", id, i)
			}
		}
	}
}

// TestWindowsSettingsCatalog_RestoreVerifyParity asserts each restored value
// has a matching verify on the same key and value name, so applying a setting
// is observably verifiable.
func TestWindowsSettingsCatalog_RestoreVerifyParity(t *testing.T) {
	for id, mod := range windowsSettingsModules(t) {
		verifySet := make(map[string]bool)
		for _, v := range mod.Verify {
			verifySet[strings.ToUpper(v.Path)+"\x00"+strings.ToUpper(v.ValueName)] = true
		}
		for i, r := range mod.Restore {
			if !verifySet[strings.ToUpper(r.Key)+"\x00"+strings.ToUpper(r.ValueName)] {
				t.Errorf("%s: restore[%d] (%s\\%s) has no matching registry-value-equals verify", id, i, r.Key, r.ValueName)
			}
		}
	}
}
