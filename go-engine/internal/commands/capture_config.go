// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/bundle"
	"github.com/Artexis10/endstate/go-engine/internal/config"
	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
)

const (
	CapturePlanningUnknownGeneration   = modules.GenerationUnknown
	CapturePlanningAmbiguousGeneration = modules.GenerationAmbiguous
	CapturePlanningNoCapture           = "capture_not_declared"
	CapturePlanningDiscoveryFailed     = "instance_discovery_failed"
	CapturePlanningSelectionFailed     = "generation_selection_failed"
	CaptureConfigStatusCaptured        = "captured"
	CaptureConfigStatusSkipped         = "skipped"
	CaptureConfigStatusFailed          = "failed"
)

var loadCaptureModuleCatalogFn = modules.GetCatalogWithDiagnostics
var createCaptureBundleFn = bundle.CreateCaptureBundle

// CaptureConfigSummary extends the established configCapture.modules shape.
// Generation-aware captures additionally emit non-null configSets and
// diagnostics arrays; schema-v1 JSON omits those generation-only fields.
type CaptureConfigSummary struct {
	Modules         []CaptureConfigModule            `json:"modules"`
	ConfigSets      []CaptureConfigSetResult         `json:"configSets"`
	Counts          CaptureConfigCounts              `json:"counts"`
	Diagnostics     []bundle.CaptureBundleDiagnostic `json:"diagnostics"`
	generationAware bool
}

// MarshalJSON keeps the established schema-v1 configCapture.modules shape
// while adding generation fields only when a schema-v2 module or diagnostic
// actually participated in planning.
func (summary CaptureConfigSummary) MarshalJSON() ([]byte, error) {
	modulesMetadata := summary.Modules
	if modulesMetadata == nil {
		modulesMetadata = []CaptureConfigModule{}
	}
	if !summary.generationAware {
		return json.Marshal(struct {
			Modules []CaptureConfigModule `json:"modules"`
		}{Modules: modulesMetadata})
	}
	configSets := summary.ConfigSets
	if configSets == nil {
		configSets = []CaptureConfigSetResult{}
	}
	diagnostics := summary.Diagnostics
	if diagnostics == nil {
		diagnostics = []bundle.CaptureBundleDiagnostic{}
	}
	return json.Marshal(struct {
		Modules     []CaptureConfigModule            `json:"modules"`
		ConfigSets  []CaptureConfigSetResult         `json:"configSets"`
		Counts      CaptureConfigCounts              `json:"counts"`
		Diagnostics []bundle.CaptureBundleDiagnostic `json:"diagnostics"`
	}{Modules: modulesMetadata, ConfigSets: configSets, Counts: summary.Counts, Diagnostics: diagnostics})
}

// CaptureConfigModule preserves the existing declaration-oriented module
// metadata used by GUI consumers.
type CaptureConfigModule struct {
	ID          string   `json:"id"`
	DisplayName string   `json:"displayName"`
	Entries     int      `json:"entries"`
	Files       []string `json:"files"`
}

// CaptureConfigSetResult is one source instance/config-set outcome. Reason is
// deliberately present as either a stable code or JSON null.
type CaptureConfigSetResult struct {
	CaptureID                   string                        `json:"captureId"`
	ModuleID                    string                        `json:"moduleId"`
	ConfigSetID                 string                        `json:"configSetId"`
	DisplayName                 string                        `json:"displayName"`
	SourceInstance              manifest.ConfigSourceInstance `json:"sourceInstance"`
	SourceGeneration            string                        `json:"sourceGeneration"`
	SourceGenerationFingerprint string                        `json:"sourceGenerationFingerprint"`
	CaptureModuleRevision       string                        `json:"captureModuleRevision"`
	FilesCaptured               int                           `json:"filesCaptured"`
	Status                      string                        `json:"status"`
	Reason                      *string                       `json:"reason"`
}

type CaptureConfigCounts struct {
	Total    int `json:"total"`
	Captured int `json:"captured"`
	Skipped  int `json:"skipped"`
	Failed   int `json:"failed"`
}

type captureConfigFinalizeRequest struct {
	Flags        CaptureFlags
	ManifestPath string
	Apps         []manifest.App
}

type captureConfigFinalization struct {
	OutputPath           string
	OutputFormat         string
	BundleSchemaVersion  string
	ManifestVersion      int
	ConfigModules        []CaptureModuleResult
	ConfigModuleMap      map[string]string
	PackageModuleMap     map[string][]string
	ConfigsIncluded      []string
	ConfigsSkipped       []string
	ConfigsCaptureErrors []string
	CaptureWarnings      []string
	ConfigCapture        CaptureConfigSummary
	SensitiveExcluded    int
}

// captureConfigPlanning pins every capture decision to one catalog snapshot.
// Candidates retain the portable facts required to report refusals without
// manufacturing an executable collection plan.
type captureConfigPlanning struct {
	Modules                []*modules.Module
	LegacyModules          []*modules.Module
	GenerationPlans        []bundle.ConfigSetCapturePlan
	PreplanningDiagnostics []bundle.CaptureBundleDiagnostic
	Candidates             []captureConfigCandidate
}

type captureConfigCandidate struct {
	CaptureID   string
	Module      *modules.Module
	Set         *modules.ConfigSetDef
	Generation  *modules.GenerationDef
	Instance    modules.ConfigInstance
	DisplayName string
}

// planCaptureConfig strictly partitions schema-v1 and schema-v2 behavior.
// Legacy modules use only the established matcher. Generation-aware modules
// are considered by detector eligibility, even without an application match,
// so path-only instances remain discoverable.
func planCaptureConfig(catalog map[string]*modules.Module, apps []manifest.App, catalogDiagnostics []modules.CatalogDiagnostic) captureConfigPlanning {
	planning := captureConfigPlanning{
		Modules:                []*modules.Module{},
		LegacyModules:          []*modules.Module{},
		GenerationPlans:        []bundle.ConfigSetCapturePlan{},
		PreplanningDiagnostics: []bundle.CaptureBundleDiagnostic{},
		Candidates:             []captureConfigCandidate{},
	}

	legacyCatalog := make(map[string]*modules.Module)
	generationModules := make([]*modules.Module, 0)
	for _, mod := range catalog {
		if mod == nil {
			continue
		}
		switch mod.EffectiveSchemaVersion() {
		case 1:
			legacyCatalog[mod.ID] = mod
		case 2:
			if modules.IsGenerationCaptureEligible(mod) {
				generationModules = append(generationModules, mod)
			}
		}
	}
	planning.LegacyModules = nonNilModules(matchModulesForAppsFn(legacyCatalog, apps))
	sort.Slice(planning.LegacyModules, func(left, right int) bool { return planning.LegacyModules[left].ID < planning.LegacyModules[right].ID })
	sort.Slice(generationModules, func(left, right int) bool { return generationModules[left].ID < generationModules[right].ID })
	planning.Modules = append(planning.Modules, planning.LegacyModules...)
	relevantModuleIDs := make(map[string]struct{}, len(planning.LegacyModules))
	for _, mod := range planning.LegacyModules {
		relevantModuleIDs[mod.ID] = struct{}{}
	}

	for _, mod := range generationModules {
		candidateStart := len(planning.Candidates)
		diagnosticStart := len(planning.PreplanningDiagnostics)
		instances, err := modules.DiscoverInstances(mod, capturePackageEvidence(mod, apps), modules.DiscoveryOptions{})
		if err != nil {
			planning.PreplanningDiagnostics = append(planning.PreplanningDiagnostics, bundle.CaptureBundleDiagnostic{
				ModuleID: mod.ID,
				Status:   bundle.CaptureBundleStatusFailed,
				Code:     CapturePlanningDiscoveryFailed,
				Detail:   err.Error(),
			})
		} else {
			for instanceIndex := range instances {
				instance := instances[instanceIndex]
				for setIndex := range mod.Config.Sets {
					set := &mod.Config.Sets[setIndex]
					candidate := captureConfigCandidate{
						CaptureID: bundle.CaptureID(mod.ID, set.ID, instance.ID),
						Module:    mod,
						Set:       set,
						Instance:  instance,
						DisplayName: func() string {
							if set.DisplayName != "" {
								return set.DisplayName
							}
							return set.ID
						}(),
					}
					generation, selectErr := modules.SelectGeneration(set, instance.Version)
					if selectErr != nil {
						code := modules.GenerationMatchCode(selectErr)
						status := bundle.CaptureBundleStatusSkipped
						if code == "" {
							code = CapturePlanningSelectionFailed
							status = bundle.CaptureBundleStatusFailed
						}
						planning.Candidates = append(planning.Candidates, candidate)
						planning.PreplanningDiagnostics = append(planning.PreplanningDiagnostics, planningDiagnostic(candidate, status, code, selectErr.Error()))
						continue
					}
					candidate.Generation = generation
					planning.Candidates = append(planning.Candidates, candidate)
					if generation.Capture == nil {
						planning.PreplanningDiagnostics = append(planning.PreplanningDiagnostics, planningDiagnostic(
							candidate,
							bundle.CaptureBundleStatusSkipped,
							CapturePlanningNoCapture,
							fmt.Sprintf("config set %q generation %q has no capture declaration", set.ID, generation.ID),
						))
						continue
					}
					planning.GenerationPlans = append(planning.GenerationPlans, bundle.ConfigSetCapturePlan{
						Module: mod, Set: set, Generation: generation, Instance: instance,
					})
				}
			}
		}
		if len(planning.Candidates) > candidateStart || len(planning.PreplanningDiagnostics) > diagnosticStart {
			planning.Modules = append(planning.Modules, mod)
			relevantModuleIDs[mod.ID] = struct{}{}
		}
	}

	for _, diagnostic := range catalogDiagnostics {
		_, relevant := relevantModuleIDs[diagnostic.ModuleID]
		if !relevant {
			relevant = captureCatalogDiagnosticIsRelevant(diagnostic, apps)
		}
		if !relevant {
			continue
		}
		planning.PreplanningDiagnostics = append(planning.PreplanningDiagnostics, bundle.CaptureBundleDiagnostic{
			ModuleID: diagnostic.ModuleID,
			Status:   bundle.CaptureBundleStatusFailed,
			Code:     diagnostic.Code,
			Detail:   diagnostic.Message,
		})
	}
	sort.Slice(planning.Modules, func(left, right int) bool { return planning.Modules[left].ID < planning.Modules[right].ID })

	sort.Slice(planning.GenerationPlans, func(left, right int) bool {
		return capturePlanID(planning.GenerationPlans[left]) < capturePlanID(planning.GenerationPlans[right])
	})
	sort.Slice(planning.Candidates, func(left, right int) bool {
		return planning.Candidates[left].CaptureID < planning.Candidates[right].CaptureID
	})
	sort.SliceStable(planning.PreplanningDiagnostics, func(left, right int) bool {
		return capturePlanningDiagnosticKey(planning.PreplanningDiagnostics[left]) < capturePlanningDiagnosticKey(planning.PreplanningDiagnostics[right])
	})
	return planning
}

func captureCatalogDiagnosticIsRelevant(diagnostic modules.CatalogDiagnostic, apps []manifest.App) bool {
	if diagnostic.AssociationUnknown {
		return true
	}
	if len(diagnostic.InstanceDetectors) > 0 {
		partialModule := &modules.Module{
			ModuleSchemaVersion: 2,
			ID:                  diagnostic.ModuleID,
			Config: &modules.ConfigDef{
				InstanceDetectors: append([]modules.InstanceDetectorDef(nil), diagnostic.InstanceDetectors...),
			},
		}
		instances, err := modules.DiscoverInstances(partialModule, nil, modules.DiscoveryOptions{})
		if err != nil || len(instances) > 0 {
			return true
		}
	}
	shortID := moduleDirName(diagnostic.ModuleID)
	for _, app := range apps {
		if !app.Installed {
			continue
		}
		if shortID != "" && strings.EqualFold(app.ID, shortID) {
			return true
		}
		windowsRef := app.Refs["windows"]
		for _, diagnosticRef := range diagnostic.WingetRefs {
			if windowsRef != "" && strings.EqualFold(windowsRef, diagnosticRef) {
				return true
			}
		}
	}
	return false
}

// finalizeCaptureConfig is the single config planning/publication seam used
// by every package backend. The caller has already written and verified the
// manifest at ManifestPath. Sanitized capture returns it untouched; every
// other capture publishes exactly one zip, including install-only output.
func finalizeCaptureConfig(request captureConfigFinalizeRequest) (*captureConfigFinalization, error) {
	empty := emptyCaptureConfigFinalization(request.ManifestPath)
	if request.Flags.Sanitize {
		return empty, nil
	}

	repoRoot := resolveRepoRootFn()
	catalog := map[string]*modules.Module{}
	diagnostics := []modules.CatalogDiagnostic{}
	if repoRoot != "" {
		loaded, loadedDiagnostics, err := loadCaptureModuleCatalogFn(repoRoot)
		if err != nil {
			return nil, fmt.Errorf("load capture module catalog: %w", err)
		}
		catalog = loaded
		diagnostics = loadedDiagnostics
	}
	planning := planCaptureConfig(catalog, request.Apps, diagnostics)
	outputPath, err := captureBundleOutputPath(request.Flags, request.ManifestPath)
	if err != nil {
		return nil, err
	}
	bundleResult, err := createCaptureBundleFn(bundle.CaptureBundleRequest{
		ManifestPath:           request.ManifestPath,
		OutputPath:             outputPath,
		EndstateVersion:        config.ReadVersion(repoRoot),
		Modules:                planning.Modules,
		GenerationPlans:        planning.GenerationPlans,
		PreplanningDiagnostics: planning.PreplanningDiagnostics,
	})
	if err != nil {
		return nil, err
	}
	if !sameHostPath(request.ManifestPath, outputPath) {
		if err := os.Remove(request.ManifestPath); err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("remove intermediate capture manifest: %w", err)
		}
	}

	configSummary := buildCaptureConfigSummary(planning, bundleResult)
	moduleResults, included, skipped, captureErrors, sensitiveExcluded := buildCaptureModuleResults(planning, bundleResult, configSummary.ConfigSets)
	return &captureConfigFinalization{
		OutputPath:           outputPath,
		OutputFormat:         "zip",
		BundleSchemaVersion:  bundleResult.BundleSchemaVersion,
		ManifestVersion:      bundleResult.ManifestVersion,
		ConfigModules:        moduleResults,
		ConfigModuleMap:      buildConfigModuleMap(planning.Modules),
		PackageModuleMap:     buildPackageModuleMap(planning.Modules),
		ConfigsIncluded:      included,
		ConfigsSkipped:       skipped,
		ConfigsCaptureErrors: captureErrors,
		CaptureWarnings:      nonNilCommandStrings(bundleResult.CaptureWarnings),
		ConfigCapture:        configSummary,
		SensitiveExcluded:    sensitiveExcluded,
	}, nil
}

func generationBundleSchemaVersion(finalization *captureConfigFinalization) string {
	if finalization == nil || finalization.ManifestVersion != 2 {
		return ""
	}
	return finalization.BundleSchemaVersion
}

func generationManifestVersion(finalization *captureConfigFinalization) int {
	if finalization == nil || finalization.ManifestVersion != 2 {
		return 0
	}
	return finalization.ManifestVersion
}

func captureConfigResultSummary(finalization *captureConfigFinalization) *CaptureConfigSummary {
	if finalization == nil {
		return nil
	}
	summary := finalization.ConfigCapture
	if len(summary.Modules) == 0 && !summary.generationAware {
		return nil
	}
	return &summary
}

func emptyCaptureConfigFinalization(manifestPath string) *captureConfigFinalization {
	return &captureConfigFinalization{
		OutputPath:           manifestPath,
		OutputFormat:         "jsonc",
		ManifestVersion:      1,
		ConfigModules:        []CaptureModuleResult{},
		ConfigModuleMap:      map[string]string{},
		PackageModuleMap:     map[string][]string{},
		ConfigsIncluded:      []string{},
		ConfigsSkipped:       []string{},
		ConfigsCaptureErrors: []string{},
		CaptureWarnings:      []string{},
		ConfigCapture: CaptureConfigSummary{
			Modules: []CaptureConfigModule{}, ConfigSets: []CaptureConfigSetResult{}, Diagnostics: []bundle.CaptureBundleDiagnostic{},
		},
	}
}

func captureBundleOutputPath(flags CaptureFlags, manifestPath string) (string, error) {
	if flags.Profile != "" {
		profilesDir := resolveProfileDirFn()
		if profilesDir != "" {
			return filepath.Abs(filepath.Join(profilesDir, flags.Profile+".zip"))
		}
	}
	if flags.Out != "" {
		absolute, err := filepath.Abs(flags.Out)
		if err != nil {
			return "", fmt.Errorf("resolve capture output: %w", err)
		}
		extension := filepath.Ext(absolute)
		if strings.EqualFold(extension, ".zip") {
			return absolute, nil
		}
		if extension == "" {
			return absolute + ".zip", nil
		}
		return strings.TrimSuffix(absolute, extension) + ".zip", nil
	}
	absolute, err := filepath.Abs(manifestPath)
	if err != nil {
		return "", fmt.Errorf("resolve capture manifest: %w", err)
	}
	extension := filepath.Ext(absolute)
	if extension == "" {
		return absolute + ".zip", nil
	}
	return strings.TrimSuffix(absolute, extension) + ".zip", nil
}

func buildCaptureConfigSummary(planning captureConfigPlanning, result *bundle.CaptureBundleResult) CaptureConfigSummary {
	summary := CaptureConfigSummary{
		Modules:         buildCaptureConfigModules(planning.Modules),
		ConfigSets:      []CaptureConfigSetResult{},
		Diagnostics:     []bundle.CaptureBundleDiagnostic{},
		generationAware: capturePlanningIsGenerationAware(planning),
	}
	summary.Diagnostics = append(summary.Diagnostics, result.Diagnostics...)
	candidates := make(map[string]captureConfigCandidate, len(planning.Candidates))
	for _, candidate := range planning.Candidates {
		candidates[candidate.CaptureID] = candidate
	}
	completed := make(map[string]struct{}, len(result.ConfigCaptures))
	for _, capture := range result.ConfigCaptures {
		candidate := candidates[capture.CaptureID]
		displayName := candidate.DisplayName
		if displayName == "" {
			displayName = capture.ConfigSetID
		}
		summary.ConfigSets = append(summary.ConfigSets, CaptureConfigSetResult{
			CaptureID: capture.CaptureID, ModuleID: capture.ModuleID, ConfigSetID: capture.ConfigSetID, DisplayName: displayName,
			SourceInstance: capture.SourceInstance, SourceGeneration: capture.SourceGeneration, SourceGenerationFingerprint: capture.SourceGenerationFingerprint,
			CaptureModuleRevision: capture.CaptureModule.ContentHash, FilesCaptured: len(capture.PayloadManifest), Status: CaptureConfigStatusCaptured,
		})
		completed[capture.CaptureID] = struct{}{}
	}
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.CaptureID == "" {
			continue
		}
		if _, exists := completed[diagnostic.CaptureID]; exists {
			continue
		}
		candidate, exists := candidates[diagnostic.CaptureID]
		if !exists {
			continue
		}
		reason := diagnostic.Code
		row := CaptureConfigSetResult{
			CaptureID: diagnostic.CaptureID, ModuleID: diagnostic.ModuleID, ConfigSetID: diagnostic.ConfigSetID,
			DisplayName: candidate.DisplayName, SourceInstance: portableSourceInstance(candidate.Instance),
			CaptureModuleRevision: candidate.Module.Revision, Status: diagnostic.Status, Reason: &reason,
		}
		if candidate.Generation != nil {
			row.SourceGeneration = candidate.Generation.ID
			row.SourceGenerationFingerprint = candidate.Generation.Fingerprint
		}
		summary.ConfigSets = append(summary.ConfigSets, row)
	}
	sort.Slice(summary.ConfigSets, func(left, right int) bool {
		return summary.ConfigSets[left].CaptureID < summary.ConfigSets[right].CaptureID
	})
	summary.Counts.Total = len(summary.ConfigSets)
	for _, row := range summary.ConfigSets {
		switch row.Status {
		case CaptureConfigStatusCaptured:
			summary.Counts.Captured++
		case CaptureConfigStatusSkipped:
			summary.Counts.Skipped++
		case CaptureConfigStatusFailed:
			summary.Counts.Failed++
		}
	}
	return summary
}

func capturePlanningIsGenerationAware(planning captureConfigPlanning) bool {
	for _, mod := range planning.Modules {
		if mod != nil && mod.EffectiveSchemaVersion() == 2 {
			return true
		}
	}
	return len(planning.PreplanningDiagnostics) > 0
}

func buildCaptureConfigModules(values []*modules.Module) []CaptureConfigModule {
	result := make([]CaptureConfigModule, 0, len(values))
	for _, mod := range values {
		files := make([]string, 0)
		entries := 0
		if mod.EffectiveSchemaVersion() == 1 {
			entries = len(mod.Restore)
			if mod.Capture != nil {
				for _, captureFile := range mod.Capture.Files {
					files = append(files, filepath.ToSlash(captureFile.Dest))
				}
			}
		} else if mod.Config != nil {
			for _, set := range mod.Config.Sets {
				for _, generation := range set.Generations {
					entries += len(generation.Restore)
					if generation.Capture != nil {
						for _, captureFile := range generation.Capture.Files {
							files = append(files, filepath.ToSlash(captureFile.Dest))
						}
					}
				}
			}
		}
		files = uniqueSortedStrings(files)
		result = append(result, CaptureConfigModule{ID: mod.ID, DisplayName: mod.DisplayName, Entries: entries, Files: files})
	}
	sort.Slice(result, func(left, right int) bool { return result[left].ID < result[right].ID })
	return result
}

func buildCaptureModuleResults(planning captureConfigPlanning, result *bundle.CaptureBundleResult, configSets []CaptureConfigSetResult) (
	[]CaptureModuleResult, []string, []string, []string, int,
) {
	legacy := make(map[string]bundle.LegacyModuleCaptureResult, len(result.LegacyModules))
	for _, moduleResult := range result.LegacyModules {
		legacy[moduleResult.ModuleID] = moduleResult
	}
	setRows := make(map[string][]CaptureConfigSetResult)
	for _, row := range configSets {
		setRows[row.ModuleID] = append(setRows[row.ModuleID], row)
	}
	captures := make(map[string][]manifest.ConfigCapture)
	for _, capture := range result.ConfigCaptures {
		captures[capture.ModuleID] = append(captures[capture.ModuleID], capture)
	}

	moduleResults := make([]CaptureModuleResult, 0, len(planning.Modules))
	included := []string{}
	skipped := []string{}
	errors := []string{}
	sensitiveExcluded := result.SensitiveExcluded
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Status == CaptureConfigStatusFailed {
			errors = append(errors, diagnostic.Detail)
		}
	}
	for _, mod := range planning.Modules {
		paths := []string{}
		filesCaptured := 0
		status := CaptureConfigStatusSkipped
		if legacyResult, exists := legacy[mod.ID]; exists {
			paths = append(paths, legacyResult.Paths...)
			filesCaptured += legacyResult.FilesCaptured
			sensitiveExcluded += legacyResult.SecretsExcluded
			switch legacyResult.Status {
			case bundle.LegacyCaptureStatusCaptured:
				status = CaptureConfigStatusCaptured
			case bundle.LegacyCaptureStatusFailed:
				status = "error"
			}
		}
		for _, capture := range captures[mod.ID] {
			for _, entry := range capture.PayloadManifest {
				paths = append(paths, path.Join(capture.PayloadRoot, entry.RelativePath))
			}
			filesCaptured += len(capture.PayloadManifest)
			status = CaptureConfigStatusCaptured
		}
		if status != CaptureConfigStatusCaptured {
			for _, row := range setRows[mod.ID] {
				if row.Status == CaptureConfigStatusFailed {
					status = "error"
					break
				}
			}
		}
		shortID := moduleDirName(mod.ID)
		switch status {
		case CaptureConfigStatusCaptured:
			included = append(included, shortID)
		case CaptureConfigStatusSkipped:
			skipped = append(skipped, shortID)
		}
		moduleResults = append(moduleResults, CaptureModuleResult{
			ID: mod.ID, AppID: shortID, DisplayName: mod.DisplayName, WingetRefs: safeStringSlice(mod.Matches.Winget),
			Paths: uniqueSortedStrings(paths), FilesCaptured: filesCaptured, Status: status,
		})
	}
	for _, legacyResult := range result.LegacyModules {
		if legacyResult.Status != bundle.LegacyCaptureStatusFailed {
			continue
		}
		prefix := "module " + legacyResult.ModuleID
		for _, warning := range result.CaptureWarnings {
			if strings.Contains(warning, prefix) {
				errors = append(errors, warning)
			}
		}
	}
	sort.Slice(moduleResults, func(left, right int) bool { return moduleResults[left].ID < moduleResults[right].ID })
	sort.Strings(included)
	sort.Strings(skipped)
	sort.Strings(errors)
	return moduleResults, included, skipped, errors, sensitiveExcluded
}

func portableSourceInstance(instance modules.ConfigInstance) manifest.ConfigSourceInstance {
	evidence := instance.Evidence
	return manifest.ConfigSourceInstance{
		ID: instance.ID, DetectorID: instance.DetectorID, RawVersion: instance.Version.Raw, NormalizedVersion: instance.Version.Normalized,
		Evidence: &manifest.ConfigSourceInstanceEvidence{
			Type: evidence.Type, AppID: evidence.AppID, Backend: evidence.Backend, Platform: evidence.Platform,
			Ref: evidence.Ref, Driver: evidence.Driver,
		},
	}
}

func sameHostPath(left, right string) bool {
	leftAbs, leftErr := filepath.Abs(left)
	rightAbs, rightErr := filepath.Abs(right)
	return leftErr == nil && rightErr == nil && strings.EqualFold(filepath.Clean(leftAbs), filepath.Clean(rightAbs))
}

func nonNilCommandStrings(values []string) []string {
	result := make([]string, len(values))
	copy(result, values)
	return result
}

func uniqueSortedStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func capturePackageEvidence(mod *modules.Module, apps []manifest.App) []modules.PackageEvidence {
	evidence := make([]modules.PackageEvidence, 0)
	for _, app := range apps {
		if !app.Installed {
			continue
		}
		platform, ref, matched := matchedPackageRef(mod, app)
		if !matched {
			continue
		}
		backend := strings.ToLower(strings.TrimSpace(app.Backend))
		if backend == "" {
			backend = strings.ToLower(strings.TrimSpace(app.Driver))
		}
		if backend == "" && platform == "windows" {
			backend = "winget"
		}
		if backend == "" || strings.TrimSpace(ref) == "" {
			continue
		}
		evidence = append(evidence, modules.PackageEvidence{
			AppID:      app.ID,
			Backend:    backend,
			Platform:   platform,
			Ref:        ref,
			Driver:     app.Driver,
			RawVersion: app.InstalledVersion,
		})
	}
	sort.Slice(evidence, func(left, right int) bool {
		leftKey := evidence[left].Backend + "\x00" + evidence[left].Ref + "\x00" + evidence[left].AppID
		rightKey := evidence[right].Backend + "\x00" + evidence[right].Ref + "\x00" + evidence[right].AppID
		return leftKey < rightKey
	})
	return evidence
}

func matchedPackageRef(mod *modules.Module, app manifest.App) (platform, ref string, matched bool) {
	if mod == nil {
		return "", "", false
	}
	if windowsRef := app.Refs["windows"]; windowsRef != "" {
		for _, declared := range mod.Matches.Winget {
			if strings.EqualFold(strings.TrimSpace(declared), strings.TrimSpace(windowsRef)) {
				return "windows", windowsRef, true
			}
		}
	}
	if app.ID != moduleDirName(mod.ID) {
		return "", "", false
	}
	platforms := make([]string, 0, len(app.Refs))
	for candidate, candidateRef := range app.Refs {
		if strings.TrimSpace(candidateRef) != "" {
			platforms = append(platforms, candidate)
		}
	}
	sort.Strings(platforms)
	if len(platforms) == 0 {
		return "", "", false
	}
	return platforms[0], app.Refs[platforms[0]], true
}

func planningDiagnostic(candidate captureConfigCandidate, status, code, detail string) bundle.CaptureBundleDiagnostic {
	return bundle.CaptureBundleDiagnostic{
		CaptureID:   candidate.CaptureID,
		ModuleID:    candidate.Module.ID,
		ConfigSetID: candidate.Set.ID,
		InstanceID:  candidate.Instance.ID,
		Status:      status,
		Code:        code,
		Detail:      detail,
	}
}

func capturePlanID(plan bundle.ConfigSetCapturePlan) string {
	if plan.Module == nil || plan.Set == nil {
		return ""
	}
	return bundle.CaptureID(plan.Module.ID, plan.Set.ID, plan.Instance.ID)
}

func capturePlanningDiagnosticKey(diagnostic bundle.CaptureBundleDiagnostic) string {
	return strings.Join([]string{diagnostic.CaptureID, diagnostic.ModuleID, diagnostic.ConfigSetID, diagnostic.InstanceID, diagnostic.Code}, "\x00")
}

func nonNilModules(values []*modules.Module) []*modules.Module {
	if values == nil {
		return []*modules.Module{}
	}
	return append([]*modules.Module(nil), values...)
}
