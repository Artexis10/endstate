// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package modules

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// TestCatalogIntegrity_NoLegacySensitiveKey guards against reintroducing the
// old module-level "sensitive" block key, which was renamed to "secrets".
// The regex matches the block key (`"sensitive":`) but NOT the unrelated tier
// field `"sensitivity":`, which is a different field and remains valid.
func TestCatalogIntegrity_NoLegacySensitiveKey(t *testing.T) {
	root := productionModulesRoot()
	dirs := diskModuleDirs(t, root)
	legacy := regexp.MustCompile(`"sensitive"\s*:`)
	for _, d := range dirs {
		b, err := os.ReadFile(filepath.Join(root, d, "module.jsonc"))
		if err != nil {
			t.Fatalf("read %s: %v", d, err)
		}
		if legacy.Match(b) {
			t.Errorf("%s/module.jsonc uses the legacy \"sensitive\" block key — rename it to \"secrets\" "+
				"(the tier field \"sensitivity\" is a different field and is fine).", d)
		}
	}
}
