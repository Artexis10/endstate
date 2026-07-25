// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationharness

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/safepath"
	"github.com/Artexis10/endstate/go-engine/internal/validationmatrix"
)

type selection struct {
	request  Request
	catalog  *validationmatrix.Catalog
	module   *modules.Module
	record   validationmatrix.ValidationRecord
	scenario validationmatrix.Scenario
	fixture  fixtureDefinitions
}

func compileSelection(request Request, now time.Time) (*selection, *Failure) {
	if request.EnginePath == "" || !filepath.IsAbs(request.EnginePath) || filepath.Clean(request.EnginePath) != request.EnginePath {
		return nil, fail(CodeInvalidEngine, "selection", "engine", "engine must be a canonical absolute path")
	}
	info, err := os.Stat(request.EnginePath)
	if err != nil || !info.Mode().IsRegular() {
		return nil, fail(CodeInvalidEngine, "selection", "engine", "engine must be an existing regular file")
	}
	if failure := validateResultPath(request.ResultPath); failure != nil {
		return nil, failure
	}
	if request.RepoRoot == "" || !filepath.IsAbs(request.RepoRoot) || filepath.Clean(request.RepoRoot) != request.RepoRoot {
		return nil, fail(CodeScenarioSelection, "selection", "repoRoot", "repository root must be a canonical absolute path")
	}
	catalog, err := validationmatrix.LoadCatalog(request.RepoRoot, now)
	if err != nil {
		return nil, fail(CodeScenarioSelection, "selection", "catalog", validationmatrix.ErrorCode(err))
	}
	mod := catalog.Modules[request.ModuleID]
	if mod == nil {
		return nil, fail(CodeScenarioSelection, "selection", "module", "module is not declared by the production catalog")
	}
	record, ok := catalog.Records[request.ModuleID]
	if !ok || record.ModuleRevision != mod.Revision {
		return nil, fail(CodeScenarioSelection, "selection", "moduleRevision", "sidecar revision does not match the selected module")
	}
	scenario, failure := selectDeclaredScenario(catalog, mod, record, request.ScenarioID)
	if failure != nil {
		return nil, failure
	}
	fixture, failure := compileFixtureDefinitionsAt(request.RepoRoot, mod, scenario)
	if failure != nil {
		return nil, failure
	}
	return &selection{request: request, catalog: catalog, module: mod, record: record, scenario: scenario, fixture: fixture}, nil
}

func selectDeclaredScenario(catalog *validationmatrix.Catalog, mod *modules.Module, record validationmatrix.ValidationRecord, scenarioID string) (validationmatrix.Scenario, *Failure) {
	if catalog == nil || mod == nil || catalog.Modules[mod.ID] != mod || record.ModuleID != mod.ID || record.ModuleRevision != mod.Revision {
		return validationmatrix.Scenario{}, fail(CodeScenarioSelection, "selection", "module", "module object is not the selected production catalog authority")
	}
	var matches []validationmatrix.Scenario
	for _, scenario := range record.Synthetic.Scenarios {
		if scenario.ID == scenarioID {
			matches = append(matches, scenario)
		}
	}
	if len(matches) != 1 {
		return validationmatrix.Scenario{}, fail(CodeScenarioSelection, "selection", "scenario", "scenario selection must resolve exactly one declaration")
	}
	if matches[0].Mode != validationmatrix.ScenarioConfigRoundtripV1 {
		return validationmatrix.Scenario{}, fail(CodeUnsupportedFixture, "selection", "scenario.mode", "Task 7A supports config-roundtrip-v1 only")
	}
	return matches[0], nil
}

func validateResultPath(path string) *Failure {
	if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path || !strings.EqualFold(filepath.Ext(path), ".json") {
		return fail(CodeInvalidResultPath, "selection", "result", "result path must be a canonical absolute JSON path")
	}
	parent := filepath.Dir(path)
	info, err := os.Stat(parent)
	if err != nil || !info.IsDir() {
		return fail(CodeInvalidResultPath, "selection", "result", "result parent must be an existing directory")
	}
	if err := safepath.ValidateRoot(parent); err != nil {
		return fail(CodeInvalidResultPath, "selection", "result", "result parent must not traverse links")
	}
	temp := filepath.Clean(os.TempDir())
	relative, err := filepath.Rel(temp, parent)
	if err != nil || relative == "." || relative == ".." || filepath.IsAbs(relative) || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return fail(CodeInvalidResultPath, "selection", "result", "result must be owned by a strict temporary descendant")
	}
	if existing, err := os.Lstat(path); err == nil && !existing.Mode().IsRegular() {
		return fail(CodeInvalidResultPath, "selection", "result", "existing result must be a regular file")
	} else if err != nil && !os.IsNotExist(err) {
		return fail(CodeInvalidResultPath, "selection", "result", "result path cannot be inspected")
	}
	return nil
}
