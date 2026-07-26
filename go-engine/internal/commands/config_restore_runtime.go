// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"path/filepath"
	"sort"

	"github.com/Artexis10/endstate/go-engine/internal/bundle"
	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/planner"
)

type configRestoreCatalogLoader func(string) (map[string]*modules.Module, []modules.CatalogDiagnostic, error)

// configRestoreCatalogSource is the read-only catalog boundary used while a
// restore-capable command is prepared. Production uses the disk-backed
// function adapter below; tests and future prepared command wrappers can pass
// an owned source without mutating package-global state.
type configRestoreCatalogSource interface {
	LoadConfigRestoreCatalog(string) (map[string]*modules.Module, []modules.CatalogDiagnostic, error)
}

func (loader configRestoreCatalogLoader) LoadConfigRestoreCatalog(
	repoRoot string,
) (map[string]*modules.Module, []modules.CatalogDiagnostic, error) {
	return loader(repoRoot)
}

// loadConfigRestoreCatalogFn is the single injectable disk-read seam for a
// command-scoped config restore runtime.
var loadConfigRestoreCatalogFn configRestoreCatalogLoader = modules.GetCatalogWithDiagnostics

// configCatalogSnapshot owns one trusted in-memory catalog for a command. Its
// module declarations are never exposed directly: command consumers receive
// defensive copies while compatibility resolution uses the same pinned facts.
type configCatalogSnapshot struct {
	modules     map[string]*modules.Module
	resolver    *planner.CompatibilityResolver
	diagnostics []modules.CatalogDiagnostic
}

type configRestoreRuntime struct {
	inputs  configRestoreInputs
	catalog configCatalogSnapshot
}

// newConfigRestoreRuntime builds the immutable manifest snapshot first, then
// loads exactly one trusted catalog snapshot when explicit config lanes are
// present. Pure config-free input remains a no-I/O path.
func newConfigRestoreRuntime(request configRestoreBuildRequest) (*configRestoreRuntime, *envelope.Error) {
	return newConfigRestoreRuntimeWithCatalogSource(request, configRestoreCatalogLoader(loadConfigRestoreCatalogFn))
}

func newConfigRestoreRuntimeWithCatalogSource(
	request configRestoreBuildRequest,
	catalogSource configRestoreCatalogSource,
) (*configRestoreRuntime, *envelope.Error) {
	inputs, envErr := buildConfigRestoreInputs(request)
	markInvalidConfigCaptureSnapshots(request, &inputs)
	runtime := newConfigRestoreRuntimeFromInputs(inputs, emptyConfigCatalogSnapshot())
	if envErr != nil || len(inputs.generationSources) == 0 {
		return runtime, envErr
	}

	if catalogSource == nil {
		return nil, envelope.NewError(envelope.ErrInternalError, "Failed to load the configuration module catalog.").
			WithDetail(map[string]string{"reason": "catalog source is nil"}).
			WithRemediation("Verify the Endstate repository root and module catalog, then retry.")
	}
	catalog, diagnostics, err := catalogSource.LoadConfigRestoreCatalog(request.RepoRoot)
	if err != nil {
		return nil, envelope.NewError(envelope.ErrInternalError, "Failed to load the configuration module catalog.").
			WithDetail(map[string]string{"reason": err.Error()}).
			WithRemediation("Verify the Endstate repository root and module catalog, then retry.")
	}
	return newConfigRestoreRuntimeFromInputs(inputs, newConfigCatalogSnapshot(catalog, diagnostics)), nil
}

// newConfigRestoreRuntimeWithCatalogSnapshot prepares config planning from a
// catalog already pinned by the outer command. This is the no-reload seam used
// when Apply and Verify later share one snapshot inside Rebuild.
func newConfigRestoreRuntimeWithCatalogSnapshot(
	request configRestoreBuildRequest,
	catalog configCatalogSnapshot,
) (*configRestoreRuntime, *envelope.Error) {
	inputs, envErr := buildConfigRestoreInputs(request)
	markInvalidConfigCaptureSnapshots(request, &inputs)
	if envErr != nil || len(inputs.generationSources) == 0 {
		return newConfigRestoreRuntimeFromInputs(inputs, emptyConfigCatalogSnapshot()), envErr
	}
	if catalog.resolver == nil {
		return nil, envelope.NewError(envelope.ErrInternalError, "Configuration module catalog is not prepared.").
			WithRemediation("Prepare one trusted catalog snapshot before configuration planning.")
	}
	return newConfigRestoreRuntimeFromInputs(inputs, catalog), nil
}

// markInvalidConfigCaptureSnapshots verifies immutable capture-time module
// evidence at the command boundary. The snapshot is never parsed as current
// restore authority; a verification failure is isolated to its config set so
// other valid sets can still be planned and restored.
func markInvalidConfigCaptureSnapshots(request configRestoreBuildRequest, inputs *configRestoreInputs) {
	if inputs == nil || request.Manifest == nil || len(inputs.generationSources) == 0 {
		return
	}
	bundleRoot := filepath.Dir(request.ManifestPath)
	invalidCaptureIDs := make(map[string]struct{})
	for _, capture := range request.Manifest.ConfigCaptures {
		if err := bundle.VerifyModuleSnapshot(bundleRoot, capture); err != nil {
			invalidCaptureIDs[capture.CaptureID] = struct{}{}
		}
	}
	for index := range inputs.generationSources {
		if _, invalid := invalidCaptureIDs[inputs.generationSources[index].source.CaptureID]; invalid {
			inputs.generationSources[index].source.PayloadIntegrityFailed = true
		}
	}
}

func newConfigRestoreRuntimeFromInputs(
	inputs configRestoreInputs,
	catalog configCatalogSnapshot,
) *configRestoreRuntime {
	return &configRestoreRuntime{inputs: inputs, catalog: catalog}
}

func emptyConfigCatalogSnapshot() configCatalogSnapshot {
	return configCatalogSnapshot{
		modules:     map[string]*modules.Module{},
		diagnostics: []modules.CatalogDiagnostic{},
	}
}

func newConfigCatalogSnapshot(
	catalog map[string]*modules.Module,
	diagnostics []modules.CatalogDiagnostic,
) configCatalogSnapshot {
	pinnedCatalog := cloneConfigModuleCatalog(catalog)
	pinnedDiagnostics := cloneConfigCatalogDiagnostics(diagnostics)
	return configCatalogSnapshot{
		modules:     pinnedCatalog,
		resolver:    planner.NewCompatibilityResolver(pinnedCatalog, pinnedDiagnostics),
		diagnostics: pinnedDiagnostics,
	}
}

// ModuleCatalog returns a caller-owned copy of the command's pinned catalog.
// Mutating it cannot affect later detection, planning, or command consumers.
func (snapshot configCatalogSnapshot) ModuleCatalog() map[string]*modules.Module {
	return cloneConfigModuleCatalog(snapshot.modules)
}

func (snapshot configCatalogSnapshot) modulesFor(moduleIDs []string) map[string]*modules.Module {
	selected := make(map[string]*modules.Module, len(moduleIDs))
	for _, moduleID := range moduleIDs {
		if module := snapshot.modules[moduleID]; module != nil {
			selected[moduleID] = cloneConfigModule(module)
		}
	}
	return selected
}

func cloneConfigCatalogDiagnostics(values []modules.CatalogDiagnostic) []modules.CatalogDiagnostic {
	cloned := make([]modules.CatalogDiagnostic, len(values))
	copy(cloned, values)
	for index := range cloned {
		cloned[index].WingetRefs = append([]string(nil), values[index].WingetRefs...)
		cloned[index].ChocolateyRefs = append([]string(nil), values[index].ChocolateyRefs...)
		cloned[index].InstanceDetectors = append(
			[]modules.InstanceDetectorDef(nil),
			values[index].InstanceDetectors...,
		)
	}
	sort.Slice(cloned, func(left, right int) bool {
		leftDiagnostic := cloned[left]
		rightDiagnostic := cloned[right]
		if leftDiagnostic.ModuleID != rightDiagnostic.ModuleID {
			return leftDiagnostic.ModuleID < rightDiagnostic.ModuleID
		}
		if leftDiagnostic.FilePath != rightDiagnostic.FilePath {
			return leftDiagnostic.FilePath < rightDiagnostic.FilePath
		}
		if leftDiagnostic.Code != rightDiagnostic.Code {
			return leftDiagnostic.Code < rightDiagnostic.Code
		}
		if leftDiagnostic.Severity != rightDiagnostic.Severity {
			return leftDiagnostic.Severity < rightDiagnostic.Severity
		}
		return leftDiagnostic.Message < rightDiagnostic.Message
	})
	return cloned
}

func cloneConfigModuleCatalog(values map[string]*modules.Module) map[string]*modules.Module {
	cloned := make(map[string]*modules.Module, len(values))
	for moduleID, module := range values {
		if module != nil {
			cloned[moduleID] = cloneConfigModule(module)
		}
	}
	return cloned
}

func cloneConfigModule(value *modules.Module) *modules.Module {
	if value == nil {
		return nil
	}
	cloned := *value
	cloned.Matches = cloneConfigModuleMatches(value.Matches)
	cloned.Verify = cloneConfigSlice(value.Verify)
	cloned.Restore = cloneConfigModuleRestoreDefs(value.Restore)
	cloned.Capture = cloneConfigModuleCaptureDef(value.Capture)
	if value.Secrets != nil {
		secrets := *value.Secrets
		secrets.Files = cloneConfigSlice(value.Secrets.Files)
		cloned.Secrets = &secrets
	}
	cloned.Config = cloneConfigModuleConfigDef(value.Config)
	if value.Curation != nil {
		curation := *value.Curation
		curation.SnapshotRoots = cloneConfigSlice(value.Curation.SnapshotRoots)
		curation.ExcludePatterns = cloneConfigSlice(value.Curation.ExcludePatterns)
		if value.Curation.Seed != nil {
			seed := *value.Curation.Seed
			curation.Seed = &seed
		}
		cloned.Curation = &curation
	}
	return &cloned
}

func cloneConfigModuleMatches(value modules.MatchCriteria) modules.MatchCriteria {
	return modules.MatchCriteria{
		Winget:               cloneConfigSlice(value.Winget),
		Chocolatey:           cloneConfigSlice(value.Chocolatey),
		Exe:                  cloneConfigSlice(value.Exe),
		UninstallDisplayName: cloneConfigSlice(value.UninstallDisplayName),
		PathExists:           cloneConfigSlice(value.PathExists),
	}
}

func cloneConfigModuleConfigDef(value *modules.ConfigDef) *modules.ConfigDef {
	if value == nil {
		return nil
	}
	cloned := *value
	cloned.InstanceDetectors = cloneConfigSlice(value.InstanceDetectors)
	cloned.Sets = nil
	if value.Sets != nil {
		cloned.Sets = make([]modules.ConfigSetDef, len(value.Sets))
	}
	for setIndex := range value.Sets {
		set := value.Sets[setIndex]
		set.Generations = nil
		if value.Sets[setIndex].Generations != nil {
			set.Generations = make([]modules.GenerationDef, len(value.Sets[setIndex].Generations))
		}
		for generationIndex := range value.Sets[setIndex].Generations {
			generation := value.Sets[setIndex].Generations[generationIndex]
			generation.Matches = cloneConfigSlice(generation.Matches)
			generation.AcceptsSourceFingerprints = cloneConfigSlice(generation.AcceptsSourceFingerprints)
			generation.Capture = cloneConfigModuleCaptureDef(generation.Capture)
			generation.Restore = cloneConfigModuleRestoreDefs(generation.Restore)
			generation.Validate = cloneConfigSlice(generation.Validate)
			set.Generations[generationIndex] = generation
		}
		set.Migrations = nil
		if value.Sets[setIndex].Migrations != nil {
			set.Migrations = make([]modules.MigrationEdgeDef, len(value.Sets[setIndex].Migrations))
		}
		for edgeIndex := range value.Sets[setIndex].Migrations {
			edge := value.Sets[setIndex].Migrations[edgeIndex]
			edge.Operations = cloneConfigSlice(edge.Operations)
			for operationIndex := range edge.Operations {
				edge.Operations[operationIndex].Value = cloneConfigModuleJSONValue(edge.Operations[operationIndex].Value)
			}
			edge.Validate = cloneConfigSlice(edge.Validate)
			set.Migrations[edgeIndex] = edge
		}
		cloned.Sets[setIndex] = set
	}
	return &cloned
}

func cloneConfigModuleRestoreDefs(values []modules.RestoreDef) []modules.RestoreDef {
	cloned := cloneConfigSlice(values)
	for index := range cloned {
		cloned[index].Exclude = cloneConfigSlice(values[index].Exclude)
	}
	return cloned
}

func cloneConfigModuleCaptureDef(value *modules.CaptureDef) *modules.CaptureDef {
	if value == nil {
		return nil
	}
	cloned := *value
	cloned.Files = cloneConfigSlice(value.Files)
	cloned.RegistryKeys = cloneConfigSlice(value.RegistryKeys)
	cloned.RegistryValues = cloneConfigSlice(value.RegistryValues)
	cloned.ExcludeGlobs = cloneConfigSlice(value.ExcludeGlobs)
	return &cloned
}

func cloneConfigSlice[T any](values []T) []T {
	if values == nil {
		return nil
	}
	cloned := make([]T, len(values))
	copy(cloned, values)
	return cloned
}

func cloneConfigModuleJSONValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		cloned := make(map[string]any, len(typed))
		for key, item := range typed {
			cloned[key] = cloneConfigModuleJSONValue(item)
		}
		return cloned
	case []any:
		cloned := make([]any, len(typed))
		for index, item := range typed {
			cloned[index] = cloneConfigModuleJSONValue(item)
		}
		return cloned
	default:
		return value
	}
}
