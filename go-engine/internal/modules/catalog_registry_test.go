// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package modules

import (
	"strings"
	"testing"
)

// TestCatalogIntegrity_RegistrySecretScopeIsConsistent catches a shipped
// module whose registry capture contradicts its own secret declaration. Such
// an entry is skipped at capture time, so the module silently loses settings
// on every user's machine.
func TestCatalogIntegrity_RegistrySecretScopeIsConsistent(t *testing.T) {
	catalog, err := GetCatalog(productionRepoRoot())
	if err != nil {
		t.Fatal(err)
	}
	for id, mod := range catalog {
		if _, conflicts := ResolveRegistrySecretScope(mod); len(conflicts) != 0 {
			t.Errorf("%s: registry capture contradicts its secrets: %+v", id, conflicts)
		}
	}
}

// TestCatalogIntegrity_RegistrySetBindsToCapture catches a module that ships
// preset registry data. A registry-set or registry-value-equals entry must
// carry no type or data of its own and must name a value the module captures,
// because capture supplies the old PC's type and data.
func TestCatalogIntegrity_RegistrySetBindsToCapture(t *testing.T) {
	catalog, err := GetCatalog(productionRepoRoot())
	if err != nil {
		t.Fatal(err)
	}
	for id, mod := range catalog {
		captured := map[string]bool{}
		if mod.Capture != nil {
			for _, value := range mod.Capture.RegistryValues {
				captured[strings.ToLower(value.Key)+"\x00"+strings.ToLower(value.ValueName)] = true
			}
		}
		for index, restore := range mod.Restore {
			if restore.Type != "registry-set" {
				continue
			}
			if restore.ValueType != "" || restore.Data != "" {
				t.Errorf("%s: restore[%d] ships preset registry data", id, index)
			}
			if !captured[strings.ToLower(restore.Key)+"\x00"+strings.ToLower(restore.ValueName)] {
				t.Errorf("%s: restore[%d] names %s\\%s, which the module does not capture", id, index, restore.Key, restore.ValueName)
			}
		}
		for index, verify := range mod.Verify {
			if verify.Type != "registry-value-equals" {
				continue
			}
			if verify.ValueType != "" || verify.Data != "" {
				t.Errorf("%s: verify[%d] ships preset registry data", id, index)
			}
			if !captured[strings.ToLower(verify.Path)+"\x00"+strings.ToLower(verify.ValueName)] {
				t.Errorf("%s: verify[%d] names %s\\%s, which the module does not capture", id, index, verify.Path, verify.ValueName)
			}
		}
	}
}
