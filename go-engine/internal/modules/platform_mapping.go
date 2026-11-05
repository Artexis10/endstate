// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package modules

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/manifest"
)

const DiagnosticInvalidPlatformMapping = "invalid_platform_mapping"

func validatePlatformMappings(mod *Module, platform string, projected *Module, filePath string) error {
	ids := map[string]bool{}
	acceptance := map[string]PlatformMappingEndpoint{}
	bridges := map[string]bool{}
	for _, mapping := range projected.Mappings {
		invalid := func(format string, args ...any) error {
			return validationError(mod, filePath, DiagnosticInvalidPlatformMapping, "platform %q mapping %q: %s", platform, mapping.ID, fmt.Sprintf(format, args...))
		}
		if err := validateStableID(mod, filePath, "mapping", mapping.ID); err != nil {
			return err
		}
		if ids[mapping.ID] {
			return invalid("duplicate mapping ID")
		}
		ids[mapping.ID] = true
		if !manifest.IsSupportedConfigPlatform(mapping.From.Platform) || mapping.From.Platform == platform || mapping.To.Platform != platform {
			return invalid("endpoints must bridge a different source platform to the containing variant")
		}
		for _, endpoint := range []PlatformMappingEndpoint{mapping.From, mapping.To} {
			if err := validateStableID(mod, filePath, "mapping config set", endpoint.ConfigSetID); err != nil {
				return err
			}
			if err := validateStableID(mod, filePath, "mapping generation", endpoint.GenerationID); err != nil {
				return err
			}
		}
		var target *GenerationDef
		if projected.Config != nil {
			for _, set := range projected.Config.Sets {
				if set.ID == mapping.To.ConfigSetID {
					for _, g := range set.Generations {
						if g.ID == mapping.To.GenerationID {
							copy := g
							target = &copy
						}
					}
				}
			}
		}
		if target == nil {
			return invalid("target generation does not belong to the containing variant")
		}
		if len(mapping.AcceptsSourceFingerprints) == 0 {
			return invalid("source fingerprint acceptance is required")
		}
		seen := map[string]bool{}
		for _, fingerprint := range mapping.AcceptsSourceFingerprints {
			if !generationFingerprintPattern.MatchString(fingerprint) || seen[fingerprint] {
				return invalid("source fingerprints must be unique lowercase SHA-256 values")
			}
			seen[fingerprint] = true
			key := GenerationIdentity(mod.ID, mapping.From.Platform, mapping.From.ConfigSetID, mapping.From.GenerationID) + "/" + fingerprint
			bridgeKey := key + "/" + mapping.To.ConfigSetID + "/" + mapping.To.GenerationID
			if previous, exists := acceptance[key]; (exists && previous.ConfigSetID != mapping.To.ConfigSetID) || bridges[bridgeKey] {
				return invalid("overlapping source acceptance must select one target set and one bridge per generation")
			}
			acceptance[key] = mapping.To
			bridges[bridgeKey] = true
		}
		if mapping.Operations == nil {
			return invalid("operations must be explicit; use [] for unchanged representation")
		}
		for _, operation := range mapping.Operations {
			if err := validateMigrationOperation(operation); err != nil {
				return invalid("operation: %v", err)
			}
		}
		if len(mapping.Validate) == 0 {
			return invalid("target validation is required")
		}
		for _, check := range mapping.Validate {
			if err := validateValidation(check); err != nil {
				return invalid("validation: %v", err)
			}
			count := 0
			path := strings.TrimPrefix(strings.ReplaceAll(check.Path, `\`, "/"), "./")
			for _, restore := range target.Restore {
				source := strings.TrimPrefix(strings.ReplaceAll(restore.Source, `\`, "/"), "./")
				if source != "" && (path == source || (restore.Type == "copy" && strings.HasPrefix(path, source+"/"))) {
					count++
				}
			}
			if count != 1 {
				return invalid("validation %q must map through exactly one target restore declaration", check.Path)
			}
		}
	}
	return nil
}

// ClonePlatformMappings copies every mutable mapping field, including JSON values.
func ClonePlatformMappings(mappings []PlatformMappingDef) []PlatformMappingDef {
	if mappings == nil {
		return nil
	}
	raw, err := json.Marshal(mappings)
	if err != nil {
		return nil
	}
	var copied []PlatformMappingDef
	if err := decodeModuleJSON(raw, &copied); err != nil {
		return nil
	}
	return copied
}
