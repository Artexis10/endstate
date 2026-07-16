// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package modules

import (
	"fmt"
	"os"
	"path/filepath"
)

// CatalogDiagnostic is a structured explanation for a module that the catalog
// could not load. LoadCatalogWithDiagnostics exposes these to commands and
// envelopes while LoadCatalog retains the existing warning-based API.
type CatalogDiagnostic struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
	ModuleID string `json:"moduleId,omitempty"`
	FilePath string `json:"filePath"`
	Message  string `json:"message"`
}

// LoadCatalog scans modulesRoot for */module.jsonc files, parses each with
// JSONC comment stripping, validates required fields, and returns a map keyed
// by module ID. Invalid modules are skipped with a warning to stderr. A missing
// modulesRoot directory returns an empty map without error.
func LoadCatalog(modulesRoot string) (map[string]*Module, error) {
	catalog, diagnostics, err := LoadCatalogWithDiagnostics(modulesRoot)
	for _, diagnostic := range diagnostics {
		fmt.Fprintf(os.Stderr, "Warning: %s\n", diagnostic.Message)
	}
	return catalog, err
}

// LoadCatalogWithDiagnostics loads the same backward-compatible catalog as
// LoadCatalog and also returns a structured diagnostic for every skipped
// module. A missing modulesRoot retains the historical empty-catalog behavior.
func LoadCatalogWithDiagnostics(modulesRoot string) (map[string]*Module, []CatalogDiagnostic, error) {
	catalog := make(map[string]*Module)
	var diagnostics []CatalogDiagnostic

	// If modules directory doesn't exist, return empty catalog.
	info, err := os.Stat(modulesRoot)
	if err != nil || !info.IsDir() {
		return catalog, diagnostics, nil
	}

	// Scan for subdirectories containing module.jsonc.
	entries, err := os.ReadDir(modulesRoot)
	if err != nil {
		return catalog, diagnostics, nil
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		moduleFile := filepath.Join(modulesRoot, entry.Name(), "module.jsonc")
		data, err := os.ReadFile(moduleFile)
		if err != nil {
			// No module.jsonc in this directory — skip silently.
			continue
		}

		mod, err := ParseModuleJSON(data)
		if err != nil {
			diagnostics = append(diagnostics, CatalogDiagnostic{
				Code:     DiagnosticInvalidJSON,
				Severity: "error",
				FilePath: moduleFile,
				Message:  fmt.Sprintf("invalid JSON in %s: %v", moduleFile, err),
			})
			continue
		}

		// Validate required fields.
		if err := validateModule(mod, moduleFile); err != nil {
			diagnostics = append(diagnostics, CatalogDiagnostic{
				Code:     DiagnosticCode(err),
				Severity: "error",
				ModuleID: mod.ID,
				FilePath: moduleFile,
				Message:  err.Error(),
			})
			continue
		}

		// Check for duplicate IDs.
		if _, exists := catalog[mod.ID]; exists {
			diagnostics = append(diagnostics, CatalogDiagnostic{
				Code:     DiagnosticDuplicateModuleID,
				Severity: "error",
				ModuleID: mod.ID,
				FilePath: moduleFile,
				Message:  fmt.Sprintf("duplicate config module id %q found at %s, skipping", mod.ID, moduleFile),
			})
			continue
		}

		// Set metadata fields.
		mod.FilePath = moduleFile
		mod.ModuleDir = filepath.Dir(moduleFile)

		catalog[mod.ID] = mod
	}

	return catalog, diagnostics, nil
}

// GetCatalog resolves the modules directory as repoRoot/modules/apps and
// calls LoadCatalog.
func GetCatalog(repoRoot string) (map[string]*Module, error) {
	modulesDir := filepath.Join(repoRoot, "modules", "apps")
	return LoadCatalog(modulesDir)
}

// GetCatalogWithDiagnostics resolves the production catalog directory and
// returns structured skip diagnostics.
func GetCatalogWithDiagnostics(repoRoot string) (map[string]*Module, []CatalogDiagnostic, error) {
	modulesDir := filepath.Join(repoRoot, "modules", "apps")
	return LoadCatalogWithDiagnostics(modulesDir)
}

// validateModule checks that a module has all required fields.
func validateModule(mod *Module, filePath string) error {
	if mod.ID == "" {
		return validationError(mod, filePath, DiagnosticInvalidID, "missing or empty 'id' field")
	}
	if mod.DisplayName == "" {
		return validationError(mod, filePath, DiagnosticInvalidID, "missing or empty 'displayName' field")
	}

	// matches must have at least one matcher.
	hasWinget := len(mod.Matches.Winget) > 0
	hasExe := len(mod.Matches.Exe) > 0
	hasUninstall := len(mod.Matches.UninstallDisplayName) > 0
	hasPathExists := len(mod.Matches.PathExists) > 0

	if !hasWinget && !hasExe && !hasUninstall && !hasPathExists {
		return validationError(mod, filePath, DiagnosticInvalidID, "matches must have at least one of: winget, exe, uninstallDisplayName, pathExists")
	}

	return validateModuleV2(mod, filePath)
}
