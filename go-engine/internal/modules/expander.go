// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package modules

import (
	"fmt"
	"os"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/manifest"
)

// ExpandConfigModules expands configModules references in a manifest by
// looking up each referenced module ID in the catalog, then injecting the
// module's restore entries into the manifest's Restore array and verify
// entries into the manifest's Verify array.
//
// Unknown module references are skipped with a warning to stderr.
// Module restore source paths (./payload/apps/<id>/) are preserved as-is;
// path rewriting is handled separately by the bundle system.
func ExpandConfigModules(m *manifest.Manifest, catalog map[string]*Module) error {
	if len(m.ConfigModules) == 0 {
		return nil
	}

	for _, moduleID := range m.ConfigModules {
		// Skip modules that are excluded by the manifest's excludeConfigs list.
		if isExcluded(moduleID, m.ExcludeConfigs) {
			continue
		}

		mod, exists := catalog[moduleID]
		if !exists {
			fmt.Fprintf(os.Stderr, "Warning: unknown config module %q referenced in configModules, skipping\n", moduleID)
			continue
		}

		// Inject restore entries.
		for _, r := range mod.Restore {
			entry := manifest.RestoreEntry{
				Type:       r.Type,
				Source:     r.Source,
				Target:     r.Target,
				Pattern:    r.Pattern,
				Reason:     r.Reason,
				Backup:     r.Backup,
				Optional:   r.Optional,
				Exclude:    r.Exclude,
				FromModule: moduleID,
			}
			m.Restore = append(m.Restore, entry)
		}

		// Inject verify entries.
		for _, v := range mod.Verify {
			entry := manifest.VerifyEntry{
				Type:      v.Type,
				Command:   v.Command,
				Path:      v.Path,
				ValueName: v.ValueName,
			}
			m.Verify = append(m.Verify, entry)
		}
	}

	return nil
}

// isExcluded reports whether moduleID is present in the excludeConfigs list.
// Both short IDs ("vscode") and qualified IDs ("apps.vscode") are matched
// against the exclude list entries using the same short/qualified equivalence
// used by the restore filter: "vscode" matches "apps.vscode" and vice-versa.
func isExcluded(moduleID string, excludeConfigs []string) bool {
	if len(excludeConfigs) == 0 {
		return false
	}
	shortID := strings.TrimPrefix(moduleID, "apps.")
	for _, ex := range excludeConfigs {
		exShort := strings.TrimPrefix(ex, "apps.")
		if exShort == shortID {
			return true
		}
	}
	return false
}
