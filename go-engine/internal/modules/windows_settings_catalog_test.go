// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package modules

import (
	"path/filepath"
	"strings"
	"testing"
)

// windowsSettingsRoot is the value-level OS-settings catalog directory, relative
// to this test file. It is a sibling of modules/apps and loaded via the same
// catalog loader (LoadCatalog + StripJsoncComments).
func windowsSettingsRoot() string {
	return filepath.Join("..", "..", "..", "modules", "windows-settings")
}

// isHKCUSettings reports whether a registry path/key targets HKCU.
func isHKCUSettings(p string) bool {
	u := strings.ToUpper(strings.TrimSpace(p))
	return strings.HasPrefix(u, `HKCU\`) || strings.HasPrefix(u, `HKEY_CURRENT_USER\`)
}

// TestWindowsSettingsCatalog_Loads asserts the seed windows-settings modules
// parse cleanly through the catalog loader and carry the expected value-level
// shape: HKCU-only registry-set restore, registry-value-equals verify, and
// registryValues capture — fully reversible single-value writes.
func TestWindowsSettingsCatalog_Loads(t *testing.T) {
	root := windowsSettingsRoot()
	catalog, err := LoadCatalog(root)
	if err != nil {
		t.Fatalf("LoadCatalog(%s): %v", root, err)
	}

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
			switch strings.ToUpper(r.ValueType) {
			case "REG_DWORD", "REG_SZ", "REG_EXPAND_SZ":
			default:
				t.Errorf("%s: restore[%d] unsupported valueType %q", id, i, r.ValueType)
			}
		}

		// Every verify must be a value-DATA assertion on an HKCU key.
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

// TestWindowsSettingsCatalog_RestoreVerifyParity asserts each restore value has a
// matching verify (same key/value/data), so applying a setting is observably
// verifiable — the verification-first invariant for the OS-settings tier.
func TestWindowsSettingsCatalog_RestoreVerifyParity(t *testing.T) {
	catalog, err := LoadCatalog(windowsSettingsRoot())
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}

	for id, mod := range catalog {
		type kv struct{ key, name, data string }
		verifySet := make(map[kv]bool)
		for _, v := range mod.Verify {
			verifySet[kv{strings.ToUpper(v.Path), v.ValueName, v.Data}] = true
		}
		for i, r := range mod.Restore {
			k := kv{strings.ToUpper(r.Key), r.ValueName, r.Data}
			if !verifySet[k] {
				t.Errorf("%s: restore[%d] (%s\\%s=%s) has no matching registry-value-equals verify",
					id, i, r.Key, r.ValueName, r.Data)
			}
		}
	}
}
