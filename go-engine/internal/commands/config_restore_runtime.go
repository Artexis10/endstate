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

// loadConfigRestoreCatalogFn is the single injectable disk-read seam for a
// command-scoped config restore runtime.
var loadConfigRestoreCatalogFn configRestoreCatalogLoader = modules.GetCatalogWithDiagnostics

// configCatalogSnapshot owns the trusted, in-memory compatibility resolver for
// one command. The source catalog map is deliberately not retained.
type configCatalogSnapshot struct {
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
	inputs, envErr := buildConfigRestoreInputs(request)
	runtime := &configRestoreRuntime{
		inputs: inputs,
		catalog: configCatalogSnapshot{
			diagnostics: []modules.CatalogDiagnostic{},
		},
	}
	if envErr != nil || !inputs.hasConfigPayloads {
		return runtime, envErr
	}

	catalog, diagnostics, err := loadConfigRestoreCatalogFn(request.RepoRoot)
	if err != nil {
		return nil, envelope.NewError(envelope.ErrInternalError, "Failed to load the configuration module catalog.").
			WithDetail(map[string]string{"reason": err.Error()}).
			WithRemediation("Verify the Endstate repository root and module catalog, then retry.")
	}
	runtime.catalog = configCatalogSnapshot{
		resolver:    planner.NewCompatibilityResolver(catalog, diagnostics),
		diagnostics: cloneConfigCatalogDiagnostics(diagnostics),
	}
	return runtime, nil
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
