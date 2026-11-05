// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package planner

import "github.com/Artexis10/endstate/go-engine/internal/modules"

func mappingAcceptsSource(mapping modules.PlatformMappingDef, source SourceCapture) bool {
	return mapping.From.Platform == sourceCapturePlatform(source) && mapping.From.ConfigSetID == source.ConfigSetID && mapping.From.GenerationID == source.Generation && containsString(mapping.AcceptsSourceFingerprints, source.GenerationFingerprint)
}

func (r *CompatibilityResolver) hasPlatformMapping(source SourceCapture) bool {
	current := r.modules[source.ModuleID]
	for _, mapping := range current.mappings {
		if mappingAcceptsSource(mapping, source) {
			return true
		}
	}
	return false
}

func resolvePlatformMapping(plan PlanSet, current pinnedCompatibilityModule, target TargetInstance) PlanSet {
	matches := []modules.PlatformMappingDef{}
	targetSetID := ""
	for _, mapping := range current.mappings {
		if !mappingAcceptsSource(mapping, plan.Source) {
			continue
		}
		if targetSetID != "" && targetSetID != mapping.To.ConfigSetID {
			return finishCompatibility(plan, ResolutionUnknown, ReasonAmbiguousGeneration, StatusSkipped)
		}
		targetSetID = mapping.To.ConfigSetID
		matches = append(matches, mapping)
	}
	if len(matches) == 0 {
		return finishCompatibility(plan, ResolutionIncompatible, ReasonPlatformMappingMissing, StatusSkipped)
	}
	if !plan.Source.SourceSnapshotLinked || !plan.Source.SourcePayloadVerified {
		return finishCompatibility(plan, ResolutionUnknown, ReasonPayloadIntegrityFailed, StatusFailed)
	}
	set, exists := current.sets[targetSetID]
	if !exists {
		return finishCompatibility(plan, ResolutionUnknown, ReasonConfigSetMissing, StatusSkipped)
	}
	generation, err := modules.SelectGeneration(&set, targetVersionEvidence(target))
	if err != nil {
		reason := ReasonUnknownGeneration
		if modules.GenerationMatchCode(err) == modules.GenerationAmbiguous {
			reason = ReasonAmbiguousGeneration
		}
		return finishCompatibility(plan, ResolutionUnknown, reason, StatusSkipped)
	}
	var selected *modules.PlatformMappingDef
	for index := range matches {
		if matches[index].To.Platform == current.platform && matches[index].To.GenerationID == generation.ID {
			if selected != nil {
				return finishCompatibility(plan, ResolutionUnknown, ReasonAmbiguousGeneration, StatusSkipped)
			}
			selected = &matches[index]
		}
	}
	if selected == nil {
		return finishCompatibility(plan, ResolutionIncompatible, ReasonPlatformMappingMissing, StatusSkipped)
	}
	plan.Mapping = clonePlatformMapping(selected)
	plan.TargetSecrets = cloneSecretsDef(current.secrets)
	plan.TargetGenerationDef = cloneGeneration(generation)
	target.Generation = generation.ID
	target.GenerationFingerprint = generation.Fingerprint
	target.ModuleRevision = current.revision
	plan.TargetInstances[0] = target
	plan.Resolution.TargetConfigSetID = targetSetID
	plan.Resolution.MappingID = selected.ID
	plan.Resolution.TargetGeneration = generation.ID
	plan.Resolution.RestoreModuleRevision = current.revision
	plan.Resolution.Resolution = ResolutionMigrate
	plan.Resolution.MigrationPath = []string{plan.Source.Generation, generation.ID}
	return plan
}

// TargetConfigSetID preserves old plans whose target set equals the source set.
func TargetConfigSetID(set PlanSet) string {
	if set.Resolution.TargetConfigSetID != "" {
		return set.Resolution.TargetConfigSetID
	}
	return set.Source.ConfigSetID
}

func clonePlatformMapping(mapping *modules.PlatformMappingDef) *modules.PlatformMappingDef {
	if mapping == nil {
		return nil
	}
	copied := modules.ClonePlatformMappings([]modules.PlatformMappingDef{*mapping})
	return &copied[0]
}
func cloneSecretsDef(secrets *modules.SecretsDef) *modules.SecretsDef {
	if secrets == nil {
		return nil
	}
	copied := *secrets
	copied.Files = append([]string(nil), secrets.Files...)
	copied.RegistryKeys = append([]string(nil), secrets.RegistryKeys...)
	return &copied
}
