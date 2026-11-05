// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package modules

import (
	"encoding/json"
	"strings"
)

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
	// Round-trip declarations so executable mutations cannot change authored variants.
	raw, err := json.Marshal(variant)
	if err != nil {
		return nil
	}
	var owned PlatformVariant
	if err := decodeModuleJSON(raw, &owned); err != nil {
		return nil
	}
	if owned.Config == nil && (owned.Capture != nil || len(owned.Restore) > 0) {
		// OpenSpec fixes the flat schema-v3 shorthand to default/g1 and ./payload/.
		for index := range owned.Restore {
			owned.Restore[index].Source = strings.TrimPrefix(owned.Restore[index].Source, "./payload/")
		}
		owned.Config = &ConfigDef{
			InstanceDetectors: []InstanceDetectorDef{{ID: "default", Type: "path", Glob: "${home}"}},
			Sets:              []ConfigSetDef{{ID: "default", Generations: []GenerationDef{{ID: "g1", Order: 1, Capture: owned.Capture, Restore: owned.Restore}}}},
		}
		owned.Capture = nil
		owned.Restore = nil
	}
	if err := setPlatformGenerationFingerprints(mod, platform, &owned); err != nil {
		return nil
	}
	variant = owned
	schemaVersion := 1
	if variant.Config != nil {
		schemaVersion = 2
	}
	var curation *CurationDef
	if mod.Curation != nil {
		raw, err := json.Marshal(mod.Curation)
		if err != nil {
			return nil
		}
		if err := decodeModuleJSON(raw, &curation); err != nil {
			return nil
		}
	}
	return &Module{
		ModuleSchemaVersion: schemaVersion,
		ID:                  mod.ID, DisplayName: mod.DisplayName, Sensitivity: mod.Sensitivity,
		Matches: variant.Matches, Verify: variant.Verify, Restore: variant.Restore,
		Capture: variant.Capture, Secrets: variant.Secrets, Notes: mod.Notes,
		Config: variant.Config, Curation: curation,
		FilePath: mod.FilePath, ModuleDir: mod.ModuleDir, Tier: mod.Tier, Revision: mod.Revision,
		Unversioned: schemaVersion == 1, canonicalSnapshot: mod.CanonicalSnapshot(),
		SourceSchemaVersion: 3, Platform: platform, Realization: variant.Realization,
	}
}

// GenerationIdentity formats the scoped identity without changing local IDs.
func GenerationIdentity(moduleID, platform, setID, generationID string) string {
	if platform == "" {
		return moduleID + "/" + setID + "/" + generationID
	}
	return moduleID + "/" + platform + "/" + setID + "/" + generationID
}

// UsesImplicitConfigGeneration proves that the pinned authored variant used the
// flat schema-v3 shorthand, rather than an authored config or migration graph.
func (mod *Module) UsesImplicitConfigGeneration() bool {
	if mod == nil || mod.SourceSchemaVersion != 3 {
		return false
	}
	var authored Module
	if err := decodeModuleJSON(mod.canonicalSnapshot, &authored); err != nil || authored.EffectiveSchemaVersion() != 3 {
		return false
	}
	variant, ok := authored.Platforms[mod.Platform]
	return ok && variant.Config == nil && (variant.Capture != nil || len(variant.Restore) > 0)
}
