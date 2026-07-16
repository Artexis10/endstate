// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package planner

import (
	"sort"

	"github.com/Artexis10/endstate/go-engine/internal/modules"
)

// CompatibilityResolver owns the trusted catalog knowledge pinned for one run.
// It never reads catalog files after construction.
type CompatibilityResolver struct {
	modules  map[string]pinnedCompatibilityModule
	rejected map[string]struct{}
}

type pinnedCompatibilityModule struct {
	schemaVersion     int
	revision          string
	sets              map[string]modules.ConfigSetDef
	instanceDetectors []modules.InstanceDetectorDef
	processPatterns   []string
}

// NewCompatibilityResolver copies and indexes the supplied catalog snapshot
// and its rejected-module diagnostics for deterministic per-run resolution.
func NewCompatibilityResolver(catalog map[string]*modules.Module, diagnostics []modules.CatalogDiagnostic) *CompatibilityResolver {
	resolver := &CompatibilityResolver{
		modules:  make(map[string]pinnedCompatibilityModule, len(catalog)),
		rejected: make(map[string]struct{}),
	}
	for moduleID, current := range catalog {
		if current == nil {
			continue
		}
		pinned := pinnedCompatibilityModule{
			schemaVersion:   current.EffectiveSchemaVersion(),
			revision:        current.Revision,
			sets:            make(map[string]modules.ConfigSetDef),
			processPatterns: sortedUniqueStrings(current.Matches.Exe),
		}
		if current.Config != nil {
			pinned.instanceDetectors = append([]modules.InstanceDetectorDef(nil), current.Config.InstanceDetectors...)
			for _, set := range current.Config.Sets {
				pinned.sets[set.ID] = cloneCompatibilitySet(set)
			}
		}
		resolver.modules[moduleID] = pinned
	}
	for _, diagnostic := range diagnostics {
		if diagnostic.ModuleID != "" {
			resolver.rejected[diagnostic.ModuleID] = struct{}{}
		}
	}
	return resolver
}

// DiscoverTargets runs engine-owned discovery against the immutable detector
// declarations pinned for this resolver. Package evidence and operating-system
// boundaries are supplied by the command for each fresh detection pass.
func (r *CompatibilityResolver) DiscoverTargets(
	moduleID string,
	packageEvidence []modules.PackageEvidence,
	options modules.DiscoveryOptions,
) ([]TargetInstance, error) {
	if r == nil {
		return []TargetInstance{}, nil
	}
	pinned, exists := r.modules[moduleID]
	if !exists || pinned.schemaVersion != 2 || len(pinned.instanceDetectors) == 0 {
		return []TargetInstance{}, nil
	}

	declarations := append([]modules.InstanceDetectorDef(nil), pinned.instanceDetectors...)
	module := &modules.Module{
		ModuleSchemaVersion: pinned.schemaVersion,
		ID:                  moduleID,
		Revision:            pinned.revision,
		Config:              &modules.ConfigDef{InstanceDetectors: declarations},
	}
	instances, err := modules.DiscoverInstances(
		module,
		append([]modules.PackageEvidence(nil), packageEvidence...),
		options,
	)
	if err != nil {
		return nil, err
	}
	targets := make([]TargetInstance, len(instances))
	for index, instance := range instances {
		targets[index] = targetInstanceFromDiscovered(instance)
	}
	return targets, nil
}

// ProcessPatterns returns the deterministic sorted, deduplicated executable
// patterns pinned from module Matches.Exe. The returned slice is caller-owned.
func (r *CompatibilityResolver) ProcessPatterns(moduleID string) []string {
	if r == nil {
		return []string{}
	}
	pinned, exists := r.modules[moduleID]
	if !exists {
		return []string{}
	}
	return append([]string{}, pinned.processPatterns...)
}

func targetInstanceFromDiscovered(instance modules.ConfigInstance) TargetInstance {
	return TargetInstance{
		ID:                instance.ID,
		ModuleID:          instance.ModuleID,
		DetectorID:        instance.DetectorID,
		RawVersion:        instance.Version.Raw,
		NormalizedVersion: instance.Version.Normalized,
		Evidence: InstanceEvidence{
			Type:     instance.Evidence.Type,
			AppID:    instance.Evidence.AppID,
			Backend:  instance.Evidence.Backend,
			Platform: instance.Evidence.Platform,
			Ref:      instance.Evidence.Ref,
			Driver:   instance.Evidence.Driver,
		},
		Root: instance.Root,
	}
}

func sortedUniqueStrings(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}
	ordered := append([]string(nil), values...)
	sort.Strings(ordered)
	result := ordered[:0]
	for _, value := range ordered {
		if len(result) == 0 || result[len(result)-1] != value {
			result = append(result, value)
		}
	}
	return result
}

// ResolveCandidate resolves one already-selected target candidate. Target
// selection and collision handling are deliberately separate planner stages.
func (r *CompatibilityResolver) ResolveCandidate(source SourceCapture, target TargetInstance) PlanSet {
	result := ConfigResolution{
		CaptureID:                   source.CaptureID,
		ModuleID:                    source.ModuleID,
		ConfigSetID:                 source.ConfigSetID,
		SourceInstanceID:            source.Instance.ID,
		TargetInstanceID:            target.ID,
		SourceGeneration:            source.Generation,
		SourceGenerationFingerprint: source.GenerationFingerprint,
		MigrationPath:               []string{},
		CaptureModuleRevision:       source.ModuleRevision,
		ResolvedTargets:             []string{},
	}
	plan := PlanSet{
		Source:          source,
		TargetInstances: []TargetInstance{target},
		Resolution:      result,
	}

	if source.CaptureModuleSchemaVersion != 2 {
		return finishCompatibility(plan, ResolutionUnknown, ReasonUnsupportedModuleSchema, StatusSkipped)
	}
	current, ok := r.modules[source.ModuleID]
	if !ok {
		if _, rejected := r.rejected[source.ModuleID]; rejected {
			return finishCompatibility(plan, ResolutionUnknown, ReasonUnsupportedModuleSchema, StatusSkipped)
		}
		return finishCompatibility(plan, ResolutionUnknown, ReasonCatalogModuleMissing, StatusSkipped)
	}
	plan.Resolution.RestoreModuleRevision = current.revision
	if current.schemaVersion != 2 {
		return finishCompatibility(plan, ResolutionUnknown, ReasonUnsupportedModuleSchema, StatusSkipped)
	}
	set, ok := current.sets[source.ConfigSetID]
	if !ok {
		return finishCompatibility(plan, ResolutionUnknown, ReasonConfigSetMissing, StatusSkipped)
	}
	sourceGeneration := findGeneration(&set, source.Generation)
	if sourceGeneration == nil {
		return finishCompatibility(plan, ResolutionUnknown, ReasonSourceGenerationUnknown, StatusSkipped)
	}
	if source.GenerationFingerprint != sourceGeneration.Fingerprint &&
		!containsString(sourceGeneration.AcceptsSourceFingerprints, source.GenerationFingerprint) {
		return finishCompatibility(plan, ResolutionUnknown, ReasonSourceGenerationDefinitionChanged, StatusSkipped)
	}
	if source.PayloadIntegrityFailed {
		return finishCompatibility(plan, ResolutionUnknown, ReasonPayloadIntegrityFailed, StatusFailed)
	}
	target.ModuleRevision = current.revision
	plan.TargetInstances[0] = target
	targetGeneration, err := modules.SelectGeneration(&set, targetVersionEvidence(target))
	if err != nil {
		reason := ReasonUnknownGeneration
		if modules.GenerationMatchCode(err) == modules.GenerationAmbiguous {
			reason = ReasonAmbiguousGeneration
		}
		return finishCompatibility(plan, ResolutionUnknown, reason, StatusSkipped)
	}

	target.Generation = targetGeneration.ID
	target.GenerationFingerprint = targetGeneration.Fingerprint
	plan.TargetInstances[0] = target
	plan.TargetGenerationDef = cloneGeneration(targetGeneration)
	plan.Resolution.TargetGeneration = targetGeneration.ID
	if targetGeneration.ID == source.Generation {
		plan.Resolution.Resolution = ResolutionDirect
		return plan
	}
	if targetGeneration.Order < sourceGeneration.Order {
		return finishCompatibility(plan, ResolutionIncompatible, ReasonDowngradeUnsupported, StatusSkipped)
	}

	path, edges, unique := uniqueMigrationRoute(&set, sourceGeneration.ID, targetGeneration.ID)
	if !unique {
		return finishCompatibility(plan, ResolutionIncompatible, ReasonMigrationPathMissing, StatusSkipped)
	}
	plan.Resolution.Resolution = ResolutionMigrate
	plan.Resolution.MigrationPath = path
	plan.MigrationEdges = edges
	return plan
}

func targetVersionEvidence(target TargetInstance) modules.VersionEvidence {
	if target.NormalizedVersion == "" {
		return modules.NewVersionEvidence(target.RawVersion)
	}
	return modules.VersionEvidence{
		Raw:        target.RawVersion,
		Normalized: target.NormalizedVersion,
		Numeric:    true,
	}
}

func uniqueMigrationRoute(set *modules.ConfigSetDef, sourceID, targetID string) ([]string, []modules.MigrationEdgeDef, bool) {
	adjacency := make(map[string][]modules.MigrationEdgeDef, len(set.Migrations))
	for _, edge := range set.Migrations {
		adjacency[edge.From] = append(adjacency[edge.From], cloneMigrationEdge(edge))
	}
	for from := range adjacency {
		sort.Slice(adjacency[from], func(left, right int) bool {
			if adjacency[from][left].To != adjacency[from][right].To {
				return adjacency[from][left].To < adjacency[from][right].To
			}
			return adjacency[from][left].From < adjacency[from][right].From
		})
	}

	pathCount := 0
	var selectedPath []string
	var selectedEdges []modules.MigrationEdgeDef
	visited := map[string]bool{sourceID: true}
	var visit func(string, []string, []modules.MigrationEdgeDef)
	visit = func(current string, path []string, edges []modules.MigrationEdgeDef) {
		if pathCount > 1 {
			return
		}
		if current == targetID {
			pathCount++
			if pathCount == 1 {
				selectedPath = append([]string(nil), path...)
				selectedEdges = cloneMigrationEdges(edges)
			}
			return
		}
		for _, edge := range adjacency[current] {
			if visited[edge.To] {
				continue
			}
			visited[edge.To] = true
			visit(edge.To, append(path, edge.To), append(edges, edge))
			delete(visited, edge.To)
		}
	}
	visit(sourceID, []string{sourceID}, nil)
	return selectedPath, selectedEdges, pathCount == 1
}

func finishCompatibility(plan PlanSet, resolution Resolution, reason ResolutionReason, status TerminalStatus) PlanSet {
	plan.Resolution.Resolution = resolution
	plan.Resolution.Reason = reasonPointerValue(reason)
	plan.Resolution.Status = status
	return plan
}

func reasonPointerValue(reason ResolutionReason) *ResolutionReason { return &reason }

func findGeneration(set *modules.ConfigSetDef, generationID string) *modules.GenerationDef {
	if set == nil {
		return nil
	}
	for index := range set.Generations {
		if set.Generations[index].ID == generationID {
			return &set.Generations[index]
		}
	}
	return nil
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func cloneCompatibilitySet(set modules.ConfigSetDef) modules.ConfigSetDef {
	cloned := set
	cloned.Generations = make([]modules.GenerationDef, len(set.Generations))
	for index := range set.Generations {
		cloned.Generations[index] = *cloneGeneration(&set.Generations[index])
	}
	cloned.Migrations = cloneMigrationEdges(set.Migrations)
	return cloned
}

func cloneMigrationEdges(edges []modules.MigrationEdgeDef) []modules.MigrationEdgeDef {
	if edges == nil {
		return nil
	}
	cloned := make([]modules.MigrationEdgeDef, len(edges))
	for index, edge := range edges {
		cloned[index] = cloneMigrationEdge(edge)
	}
	return cloned
}

func cloneMigrationEdge(edge modules.MigrationEdgeDef) modules.MigrationEdgeDef {
	cloned := edge
	cloned.Operations = append([]modules.MigrationOperationDef(nil), edge.Operations...)
	for index := range cloned.Operations {
		cloned.Operations[index].Value = cloneJSONValue(edge.Operations[index].Value)
	}
	cloned.Validate = append([]modules.ValidationDef(nil), edge.Validate...)
	return cloned
}

func cloneGeneration(generation *modules.GenerationDef) *modules.GenerationDef {
	if generation == nil {
		return nil
	}
	cloned := *generation
	cloned.Matches = append([]modules.VersionSelectorDef(nil), generation.Matches...)
	cloned.AcceptsSourceFingerprints = append([]string(nil), generation.AcceptsSourceFingerprints...)
	cloned.Capture = cloneCaptureDef(generation.Capture)
	cloned.Restore = cloneRestoreDefs(generation.Restore)
	cloned.Validate = append([]modules.ValidationDef(nil), generation.Validate...)
	return &cloned
}

func cloneCaptureDef(capture *modules.CaptureDef) *modules.CaptureDef {
	if capture == nil {
		return nil
	}
	cloned := *capture
	cloned.Files = append([]modules.CaptureFile(nil), capture.Files...)
	cloned.RegistryKeys = append([]modules.CaptureRegistryKey(nil), capture.RegistryKeys...)
	cloned.RegistryValues = append([]modules.CaptureRegistryValue(nil), capture.RegistryValues...)
	cloned.ExcludeGlobs = append([]string(nil), capture.ExcludeGlobs...)
	return &cloned
}

func cloneRestoreDefs(restores []modules.RestoreDef) []modules.RestoreDef {
	if restores == nil {
		return nil
	}
	cloned := append([]modules.RestoreDef(nil), restores...)
	for index := range cloned {
		cloned[index].Exclude = append([]string(nil), restores[index].Exclude...)
	}
	return cloned
}

func cloneJSONValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		cloned := make(map[string]any, len(typed))
		for key, item := range typed {
			cloned[key] = cloneJSONValue(item)
		}
		return cloned
	case []any:
		cloned := make([]any, len(typed))
		for index, item := range typed {
			cloned[index] = cloneJSONValue(item)
		}
		return cloned
	default:
		return value
	}
}
