// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package modules

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Artexis10/endstate/go-engine/internal/manifest"
)

// LoadCatalog scans modulesRoot for */module.jsonc files, parses each with
// JSONC comment stripping, validates required fields, and returns a map keyed
// by module ID. Invalid modules are skipped with a warning to stderr. A missing
// modulesRoot directory returns an empty map without error.
func LoadCatalog(modulesRoot string) (map[string]*Module, error) {
	catalog := make(map[string]*Module)

	// If modules directory doesn't exist, return empty catalog.
	info, err := os.Stat(modulesRoot)
	if err != nil || !info.IsDir() {
		return catalog, nil
	}

	// Scan for subdirectories containing module.jsonc.
	entries, err := os.ReadDir(modulesRoot)
	if err != nil {
		return catalog, nil
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

		clean := manifest.StripJsoncComments(data)

		var mod Module
		if err := json.Unmarshal(clean, &mod); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: invalid JSON in %s: %v\n", moduleFile, err)
			continue
		}

		// Validate required fields.
		if err := validateModule(&mod, moduleFile); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: %v\n", err)
			continue
		}

		// Check for duplicate IDs.
		if _, exists := catalog[mod.ID]; exists {
			fmt.Fprintf(os.Stderr, "Warning: duplicate config module id %q found at %s, skipping\n", mod.ID, moduleFile)
			continue
		}

		// Set metadata fields.
		mod.FilePath = moduleFile
		mod.ModuleDir = filepath.Dir(moduleFile)

		catalog[mod.ID] = &mod
	}

	return catalog, nil
}

// GetCatalog resolves the modules directory as repoRoot/modules/apps and
// calls LoadCatalog.
func GetCatalog(repoRoot string) (map[string]*Module, error) {
	modulesDir := filepath.Join(repoRoot, "modules", "apps")
	return LoadCatalog(modulesDir)
}

// validateModule checks that a module has all required fields.
func validateModule(mod *Module, filePath string) error {
	if mod.ID == "" {
		return fmt.Errorf("invalid config module at %s: missing or empty 'id' field", filePath)
	}
	if mod.DisplayName == "" {
		return fmt.Errorf("invalid config module at %s: missing or empty 'displayName' field", filePath)
	}

	// matches must have at least one matcher.
	hasWinget := len(mod.Matches.Winget) > 0
	hasExe := len(mod.Matches.Exe) > 0
	hasUninstall := len(mod.Matches.UninstallDisplayName) > 0
	hasPathExists := len(mod.Matches.PathExists) > 0

	if !hasWinget && !hasExe && !hasUninstall && !hasPathExists {
		return fmt.Errorf("invalid config module at %s: matches must have at least one of: winget, exe, uninstallDisplayName, pathExists", filePath)
	}

	return nil
}
