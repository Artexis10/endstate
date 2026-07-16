// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"sort"

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
	if envErr != nil || len(inputs.generationSources) == 0 {
		return newConfigRestoreRuntimeFromInputs(inputs, emptyConfigCatalogSnapshot()), envErr
	}
	if catalog.resolver == nil {
		return nil, envelope.NewError(envelope.ErrInternalError, "Configuration module catalog is not prepared.").
			WithRemediation("Prepare one trusted catalog snapshot before configuration planning.")
	}
	return newConfigRestoreRuntimeFromInputs(inputs, catalog), nil
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
	cloned.Verify = append([]modules.VerifyDef(nil), value.Verify...)
	cloned.Restore = cloneConfigModuleRestoreDefs(value.Restore)
	cloned.Capture = cloneConfigModuleCaptureDef(value.Capture)
	if value.Secrets != nil {
		secrets := *value.Secrets
		secrets.Files = append([]string(nil), value.Secrets.Files...)
		cloned.Secrets = &secrets
	}
	cloned.Config = cloneConfigModuleConfigDef(value.Config)
	return &cloned
}

func cloneConfigModuleMatches(value modules.MatchCriteria) modules.MatchCriteria {
	return modules.MatchCriteria{
		Winget:               append([]string(nil), value.Winget...),
		Exe:                  append([]string(nil), value.Exe...),
		UninstallDisplayName: append([]string(nil), value.UninstallDisplayName...),
		PathExists:           append([]string(nil), value.PathExists...),
	}
}

func cloneConfigModuleConfigDef(value *modules.ConfigDef) *modules.ConfigDef {
	if value == nil {
		return nil
	}
	cloned := *value
	cloned.InstanceDetectors = append([]modules.InstanceDetectorDef(nil), value.InstanceDetectors...)
	cloned.Sets = make([]modules.ConfigSetDef, len(value.Sets))
	for setIndex := range value.Sets {
		set := value.Sets[setIndex]
		set.Generations = make([]modules.GenerationDef, len(value.Sets[setIndex].Generations))
		for generationIndex := range value.Sets[setIndex].Generations {
			generation := value.Sets[setIndex].Generations[generationIndex]
			generation.Matches = append([]modules.VersionSelectorDef(nil), generation.Matches...)
			generation.AcceptsSourceFingerprints = append([]string(nil), generation.AcceptsSourceFingerprints...)
			generation.Capture = cloneConfigModuleCaptureDef(generation.Capture)
			generation.Restore = cloneConfigModuleRestoreDefs(generation.Restore)
			generation.Validate = append([]modules.ValidationDef(nil), generation.Validate...)
			set.Generations[generationIndex] = generation
		}
		set.Migrations = make([]modules.MigrationEdgeDef, len(value.Sets[setIndex].Migrations))
		for edgeIndex := range value.Sets[setIndex].Migrations {
			edge := value.Sets[setIndex].Migrations[edgeIndex]
			edge.Operations = append([]modules.MigrationOperationDef(nil), edge.Operations...)
			for operationIndex := range edge.Operations {
				edge.Operations[operationIndex].Value = cloneConfigModuleJSONValue(edge.Operations[operationIndex].Value)
			}
			edge.Validate = append([]modules.ValidationDef(nil), edge.Validate...)
			set.Migrations[edgeIndex] = edge
		}
		cloned.Sets[setIndex] = set
	}
	return &cloned
}

func cloneConfigModuleRestoreDefs(values []modules.RestoreDef) []modules.RestoreDef {
	cloned := append([]modules.RestoreDef(nil), values...)
	for index := range cloned {
		cloned[index].Exclude = append([]string(nil), values[index].Exclude...)
	}
	return cloned
}

func cloneConfigModuleCaptureDef(value *modules.CaptureDef) *modules.CaptureDef {
	if value == nil {
		return nil
	}
	cloned := *value
	cloned.Files = append([]modules.CaptureFile(nil), value.Files...)
	cloned.RegistryKeys = append([]modules.CaptureRegistryKey(nil), value.RegistryKeys...)
	cloned.RegistryValues = append([]modules.CaptureRegistryValue(nil), value.RegistryValues...)
	cloned.ExcludeGlobs = append([]string(nil), value.ExcludeGlobs...)
	return &cloned
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
