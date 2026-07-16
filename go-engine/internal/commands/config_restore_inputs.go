// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"fmt"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/bundle"
	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/planner"
	"github.com/Artexis10/endstate/go-engine/internal/safepath"
)

// configRestoreBuildRequest is the read-only command boundary shared by
// restore-capable commands. Production command wiring is intentionally left
// for the orchestration slice.
type configRestoreBuildRequest struct {
	Manifest       *manifest.Manifest
	ManifestPath   string
	RepoRoot       string
	RestoreFilter  string
	RestoreTargets []string
}

// configRestoreInputs is an immutable command-scoped view of all restore
// inputs. Generation and explicit legacy lanes are kept separate from
// anonymous inline restores so the latter never acquire fabricated resolution
// identity.
type configRestoreInputs struct {
	hasConfigPayloads bool
	generationSources []configRestoreSource
	legacyLanes       []configRestoreLegacyLane
	ordinaryRestores  []manifest.RestoreEntry
	targetMappings    map[string]string
}

type configRestoreSource struct {
	source          planner.SourceCapture
	payloadRoot     string
	payloadManifest []manifest.PayloadManifestEntry
	selected        bool
}

type configRestoreLegacyLane struct {
	captureID      string
	moduleID       string
	configSetID    string
	payloadRoot    string
	restoreEntries []manifest.RestoreEntry
	selected       bool
}

type configRestoreInputErrorDetail struct {
	CaptureID string `json:"captureId,omitempty"`
	Field     string `json:"field"`
	Value     string `json:"value,omitempty"`
	Reason    string `json:"reason"`
}

func emptyConfigRestoreInputs() configRestoreInputs {
	return configRestoreInputs{
		generationSources: []configRestoreSource{},
		legacyLanes:       []configRestoreLegacyLane{},
		ordinaryRestores:  []manifest.RestoreEntry{},
		targetMappings:    map[string]string{},
	}
}

// buildConfigRestoreInputs snapshots manifest-owned restore facts without
// reading bundled module snapshots or current catalog files. Explicit target
// mappings are validated against every generation-aware capture before module
// filtering, then mappings for filtered captures are discarded.
func buildConfigRestoreInputs(request configRestoreBuildRequest) (configRestoreInputs, *envelope.Error) {
	inputs := emptyConfigRestoreInputs()
	if request.Manifest == nil {
		return inputs, invalidConfigRestoreInput("manifest", "", "manifest is nil")
	}

	version, ok := request.Manifest.Version.(int)
	if !ok || (version != 1 && version != 2) {
		return inputs, invalidConfigRestoreInput("version", fmt.Sprint(request.Manifest.Version), "loaded manifest version is not exactly 1 or 2")
	}

	knownCaptures := make(map[string]struct{}, len(request.Manifest.ConfigCaptures))
	for _, capture := range request.Manifest.ConfigCaptures {
		knownCaptures[capture.CaptureID] = struct{}{}
	}
	mappings, mappingErr := parseRestoreTargetMappings(request.RestoreTargets, knownCaptures)
	if mappingErr != nil {
		return inputs, mappingErr
	}

	filter := parseConfigRestoreFilter(request.RestoreFilter)
	if version == 1 {
		if len(request.Manifest.ConfigCaptures) != 0 || len(request.Manifest.LegacyConfigLanes) != 0 {
			return inputs, invalidConfigRestoreInput("version", "1", "manifest v1 cannot contain v2 config lanes")
		}
		if envErr := buildV1ConfigRestoreInputs(&inputs, request.Manifest.Restore, filter); envErr != nil {
			return emptyConfigRestoreInputs(), envErr
		}
		return inputs, nil
	}

	if envErr := buildV2ConfigRestoreInputs(&inputs, request, mappings, filter); envErr != nil {
		return emptyConfigRestoreInputs(), envErr
	}
	return inputs, nil
}

func buildV1ConfigRestoreInputs(
	inputs *configRestoreInputs,
	restores []manifest.RestoreEntry,
	filter map[string]struct{},
) *envelope.Error {
	grouped := make(map[string][]manifest.RestoreEntry)
	for index, entry := range restores {
		if entry.LegacyCaptureID != "" {
			return invalidConfigRestoreInput(fmt.Sprintf("restore[%d].legacyCaptureId", index), entry.LegacyCaptureID, "manifest v1 cannot carry explicit v2 legacy identity")
		}
		if entry.FromModule == "" {
			inputs.ordinaryRestores = append(inputs.ordinaryRestores, cloneConfigRestoreEntry(entry))
			continue
		}
		if strings.TrimSpace(entry.FromModule) != entry.FromModule {
			return invalidConfigRestoreInput(fmt.Sprintf("restore[%d].fromModule", index), entry.FromModule, "manifest v1 module identity is ambiguous")
		}
		grouped[entry.FromModule] = append(grouped[entry.FromModule], cloneConfigRestoreEntry(entry))
	}

	moduleIDs := make([]string, 0, len(grouped))
	for moduleID := range grouped {
		moduleIDs = append(moduleIDs, moduleID)
	}
	sort.Strings(moduleIDs)
	inputs.hasConfigPayloads = len(moduleIDs) > 0
	for _, moduleID := range moduleIDs {
		inputs.legacyLanes = append(inputs.legacyLanes, configRestoreLegacyLane{
			captureID:      bundle.LegacyCaptureID(moduleID),
			moduleID:       moduleID,
			configSetID:    "legacy",
			restoreEntries: cloneConfigRestoreEntries(grouped[moduleID]),
			selected:       configRestoreFilterIncludes(filter, moduleID),
		})
	}
	return nil
}

func buildV2ConfigRestoreInputs(
	inputs *configRestoreInputs,
	request configRestoreBuildRequest,
	mappings map[string]string,
	filter map[string]struct{},
) *envelope.Error {
	manifestValue := request.Manifest
	inputs.hasConfigPayloads = len(manifestValue.ConfigCaptures) > 0 || len(manifestValue.LegacyConfigLanes) > 0

	allSources := make([]configRestoreSource, 0, len(manifestValue.ConfigCaptures))
	for index, capture := range manifestValue.ConfigCaptures {
		if capture.SourceInstance.Evidence == nil {
			return invalidConfigRestoreInputWithCapture(
				capture.CaptureID, fmt.Sprintf("configCaptures[%d].sourceInstance.evidence", index), "", "source evidence is missing",
			)
		}
		payloadRoot, envErr := resolveConfigPayloadRoot(
			request.ManifestPath, capture.CaptureID, fmt.Sprintf("configCaptures[%d].payloadRoot", index), capture.PayloadRoot,
		)
		if envErr != nil {
			return envErr
		}
		evidence := capture.SourceInstance.Evidence
		allSources = append(allSources, configRestoreSource{
			source: planner.SourceCapture{
				CaptureID: capture.CaptureID, ModuleID: capture.ModuleID, ConfigSetID: capture.ConfigSetID,
				Instance: planner.SourceInstance{
					ID: capture.SourceInstance.ID, DetectorID: capture.SourceInstance.DetectorID,
					RawVersion: capture.SourceInstance.RawVersion, NormalizedVersion: capture.SourceInstance.NormalizedVersion,
					Evidence: planner.InstanceEvidence{
						Type: evidence.Type, AppID: evidence.AppID, Backend: evidence.Backend,
						Platform: evidence.Platform, Ref: evidence.Ref, Driver: evidence.Driver,
					},
				},
				Generation: capture.SourceGeneration, GenerationFingerprint: capture.SourceGenerationFingerprint,
				ModuleRevision: capture.CaptureModule.ContentHash, CaptureModuleSchemaVersion: capture.CaptureModule.SchemaVersion,
			},
			payloadRoot:     payloadRoot,
			payloadManifest: clonePayloadManifest(capture.PayloadManifest),
		})
	}
	sort.Slice(allSources, func(left, right int) bool {
		leftSource := allSources[left].source
		rightSource := allSources[right].source
		if leftSource.CaptureID != rightSource.CaptureID {
			return leftSource.CaptureID < rightSource.CaptureID
		}
		if leftSource.ModuleID != rightSource.ModuleID {
			return leftSource.ModuleID < rightSource.ModuleID
		}
		return leftSource.ConfigSetID < rightSource.ConfigSetID
	})
	selectedCaptureIDs := make(map[string]struct{}, len(allSources))
	for index := range allSources {
		allSources[index].selected = configRestoreFilterIncludes(filter, allSources[index].source.ModuleID)
		inputs.generationSources = append(inputs.generationSources, allSources[index])
		if allSources[index].selected {
			selectedCaptureIDs[allSources[index].source.CaptureID] = struct{}{}
		}
	}
	for captureID, targetID := range mappings {
		if _, selected := selectedCaptureIDs[captureID]; selected {
			inputs.targetMappings[captureID] = targetID
		}
	}

	lanesByID := make(map[string]configRestoreLegacyLane, len(manifestValue.LegacyConfigLanes))
	for index, lane := range manifestValue.LegacyConfigLanes {
		payloadRoot, envErr := resolveConfigPayloadRoot(
			request.ManifestPath, lane.CaptureID, fmt.Sprintf("legacyConfigLanes[%d].payloadRoot", index), lane.PayloadRoot,
		)
		if envErr != nil {
			return envErr
		}
		lanesByID[lane.CaptureID] = configRestoreLegacyLane{
			captureID: lane.CaptureID, moduleID: lane.ModuleID, configSetID: "legacy",
			payloadRoot: payloadRoot, restoreEntries: []manifest.RestoreEntry{},
		}
	}
	for index, entry := range manifestValue.Restore {
		if entry.LegacyCaptureID == "" && entry.FromModule == "" {
			if root, overlaps := ordinaryV2RestoreConfigPayloadRoot(entry.Source, manifestValue); overlaps {
				return invalidConfigRestoreInput(
					fmt.Sprintf("restore[%d].source", index), entry.Source,
					fmt.Sprintf("anonymous ordinary restore source overlaps config payload root %q", root),
				)
			}
			inputs.ordinaryRestores = append(inputs.ordinaryRestores, cloneConfigRestoreEntry(entry))
			continue
		}
		lane, exists := lanesByID[entry.LegacyCaptureID]
		if entry.LegacyCaptureID == "" || !exists || entry.FromModule != lane.moduleID {
			return invalidConfigRestoreInputWithCapture(
				entry.LegacyCaptureID, fmt.Sprintf("restore[%d].legacyCaptureId", index), entry.LegacyCaptureID,
				"manifest v2 flat restore is not associated with one explicit validated legacy lane",
			)
		}
		lane.restoreEntries = append(lane.restoreEntries, cloneConfigRestoreEntry(entry))
		lanesByID[entry.LegacyCaptureID] = lane
	}
	allLanes := make([]configRestoreLegacyLane, 0, len(lanesByID))
	for _, lane := range lanesByID {
		if len(lane.restoreEntries) == 0 {
			return invalidConfigRestoreInputWithCapture(lane.captureID, "legacyConfigLanes", lane.captureID, "explicit legacy lane has no associated restore entries")
		}
		allLanes = append(allLanes, lane)
	}
	sort.Slice(allLanes, func(left, right int) bool {
		if allLanes[left].captureID != allLanes[right].captureID {
			return allLanes[left].captureID < allLanes[right].captureID
		}
		return allLanes[left].moduleID < allLanes[right].moduleID
	})
	for index := range allLanes {
		allLanes[index].selected = configRestoreFilterIncludes(filter, allLanes[index].moduleID)
		inputs.legacyLanes = append(inputs.legacyLanes, allLanes[index])
	}
	return nil
}

func ordinaryV2RestoreConfigPayloadRoot(source string, manifestValue *manifest.Manifest) (string, bool) {
	if manifestValue == nil || strings.TrimSpace(source) == "" {
		return "", false
	}
	normalizedSource := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(source), `\`, "/"))
	normalizedSource = strings.TrimPrefix(normalizedSource, "./")
	normalizedSource = strings.TrimSuffix(path.Clean(normalizedSource), "/")
	roots := make([]string, 0, len(manifestValue.ConfigCaptures)+len(manifestValue.LegacyConfigLanes))
	for _, capture := range manifestValue.ConfigCaptures {
		roots = append(roots, capture.PayloadRoot)
	}
	for _, lane := range manifestValue.LegacyConfigLanes {
		roots = append(roots, lane.PayloadRoot)
	}
	for _, root := range roots {
		normalizedRoot := strings.ToLower(strings.TrimSuffix(path.Clean(strings.ReplaceAll(root, `\`, "/")), "/"))
		if normalizedSource == normalizedRoot || strings.HasPrefix(normalizedSource, normalizedRoot+"/") ||
			strings.HasPrefix(normalizedRoot, normalizedSource+"/") {
			return root, true
		}
	}
	return "", false
}

func resolveConfigPayloadRoot(manifestPath, captureID, field, portableRoot string) (string, *envelope.Error) {
	manifestDir, err := filepath.Abs(filepath.Dir(manifestPath))
	if err != nil {
		return "", invalidConfigRestoreInputWithCapture(captureID, field, portableRoot, err.Error())
	}
	resolved, err := safepath.Resolve(manifestDir, portableRoot)
	if err == nil {
		err = safepath.ValidateRoot(resolved)
	}
	if err != nil {
		return "", invalidConfigRestoreInputWithCapture(captureID, field, portableRoot, err.Error())
	}
	return resolved, nil
}

func parseConfigRestoreFilter(value string) map[string]struct{} {
	result := make(map[string]struct{})
	for _, item := range strings.Split(value, ",") {
		item = strings.TrimSpace(item)
		if item != "" {
			result[item] = struct{}{}
		}
	}
	return result
}

func configRestoreFilterIncludes(filter map[string]struct{}, moduleID string) bool {
	if len(filter) == 0 {
		return true
	}
	if _, exists := filter[moduleID]; exists {
		return true
	}
	if strings.HasPrefix(moduleID, "apps.") {
		_, exists := filter[strings.TrimPrefix(moduleID, "apps.")]
		return exists
	}
	return false
}

func clonePayloadManifest(entries []manifest.PayloadManifestEntry) []manifest.PayloadManifestEntry {
	cloned := make([]manifest.PayloadManifestEntry, len(entries))
	copy(cloned, entries)
	return cloned
}

func cloneConfigRestoreEntries(entries []manifest.RestoreEntry) []manifest.RestoreEntry {
	cloned := make([]manifest.RestoreEntry, len(entries))
	for index := range entries {
		cloned[index] = cloneConfigRestoreEntry(entries[index])
	}
	return cloned
}

func cloneConfigRestoreEntry(entry manifest.RestoreEntry) manifest.RestoreEntry {
	cloned := entry
	cloned.Exclude = append([]string(nil), entry.Exclude...)
	if cloned.Exclude == nil {
		cloned.Exclude = []string{}
	}
	return cloned
}

func invalidConfigRestoreInput(field, value, reason string) *envelope.Error {
	return invalidConfigRestoreInputWithCapture("", field, value, reason)
}

func invalidConfigRestoreInputWithCapture(captureID, field, value, reason string) *envelope.Error {
	return envelope.NewError(envelope.ErrManifestValidationError, "Invalid configuration restore input.").
		WithDetail(configRestoreInputErrorDetail{CaptureID: captureID, Field: field, Value: value, Reason: reason}).
		WithRemediation("Use a manifest loaded and validated by Endstate, and keep every payload root inside the manifest directory without links or traversal.")
}
