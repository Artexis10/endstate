// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package modules

// FilterCatalogForPlatform selects only executable declarations for one host.
// Flat schema-v1/v2 modules are the established Windows format and therefore
// remain inert on Linux and Darwin. Schema-v3 projection is added at this same
// boundary so no caller can accidentally execute a different host's paths.
func FilterCatalogForPlatform(catalog map[string]*Module, platform string) map[string]*Module {
	filtered := make(map[string]*Module)
	for id, mod := range catalog {
		if mod == nil {
			continue
		}
		if mod.SourceSchemaVersion == 3 {
			if mod.Platform == platform {
				filtered[id] = mod
			}
			continue
		}
		if mod.EffectiveSchemaVersion() <= 2 && platform == "windows" {
			filtered[id] = mod
			continue
		}
		if mod.EffectiveSchemaVersion() == 3 {
			filtered[id] = ProjectModuleForPlatform(mod, platform)
			if filtered[id] == nil {
				delete(filtered, id)
			}
		}
	}
	return filtered
}

// ProjectModuleForPlatform returns one executable schema-v3 host variant, or
// nil when the authored module does not support that platform.
func ProjectModuleForPlatform(mod *Module, platform string) *Module {
	if mod == nil || mod.EffectiveSchemaVersion() != 3 {
		return nil
	}
	variant, ok := mod.Platforms[platform]
	if !ok {
		return nil
	}
	return projectPlatformVariant(mod, platform, variant)
}

func projectPlatformVariant(mod *Module, platform string, variant PlatformVariant) *Module {
	schemaVersion := 1
	if variant.Config != nil {
		schemaVersion = 2
	}
	return &Module{
		ModuleSchemaVersion: schemaVersion,
		ID:                  mod.ID, DisplayName: mod.DisplayName, Sensitivity: mod.Sensitivity,
		Matches: variant.Matches, Verify: variant.Verify, Restore: variant.Restore,
		Capture: variant.Capture, Secrets: variant.Secrets, Notes: mod.Notes,
		Config: variant.Config, Curation: mod.Curation,
		FilePath: mod.FilePath, ModuleDir: mod.ModuleDir, Revision: mod.Revision,
		Unversioned: schemaVersion == 1, canonicalSnapshot: mod.CanonicalSnapshot(),
		SourceSchemaVersion: 3, Platform: platform, Realization: variant.Realization,
	}
}
