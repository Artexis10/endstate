// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package modules

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/manifest"
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
	// Identity evidence is preserved even when strict module parsing or
	// validation fails, so capture can scope diagnostics to package and path
	// instances without treating the rejected module as executable authority.
	WingetRefs         []string              `json:"wingetRefs,omitempty"`
	ChocolateyRefs     []string              `json:"chocolateyRefs,omitempty"`
	InstanceDetectors  []InstanceDetectorDef `json:"instanceDetectors,omitempty"`
	AssociationUnknown bool                  `json:"associationUnknown,omitempty"`
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
// A directory loaded on its own is the apps tier.
func LoadCatalogWithDiagnostics(modulesRoot string) (map[string]*Module, []CatalogDiagnostic, error) {
	return LoadTierCatalogWithDiagnostics(modulesRoot, AppsTier)
}

// LoadTierCatalogWithDiagnostics loads one catalog tier directory and marks
// every module it loads with that tier.
func LoadTierCatalogWithDiagnostics(modulesRoot, tier string) (map[string]*Module, []CatalogDiagnostic, error) {
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
			moduleID, wingetRefs, chocolateyRefs, detectors, associationUnknown := catalogDiagnosticIdentity(data, tier, entry.Name())
			diagnostics = append(diagnostics, CatalogDiagnostic{
				Code:               DiagnosticInvalidJSON,
				Severity:           "error",
				ModuleID:           moduleID,
				FilePath:           moduleFile,
				Message:            fmt.Sprintf("invalid JSON in %s: %v", moduleFile, err),
				WingetRefs:         wingetRefs,
				ChocolateyRefs:     chocolateyRefs,
				InstanceDetectors:  detectors,
				AssociationUnknown: associationUnknown,
			})
			continue
		}

		// Validate required fields.
		if err := validateModule(mod, moduleFile); err != nil {
			diagnostics = append(diagnostics, CatalogDiagnostic{
				Code:              DiagnosticCode(err),
				Severity:          "error",
				ModuleID:          mod.ID,
				FilePath:          moduleFile,
				Message:           err.Error(),
				WingetRefs:        append([]string(nil), mod.Matches.Winget...),
				ChocolateyRefs:    append([]string(nil), mod.Matches.Chocolatey...),
				InstanceDetectors: diagnosticInstanceDetectors(mod),
			})
			continue
		}

		// Check for duplicate IDs.
		if _, exists := catalog[mod.ID]; exists {
			diagnostics = append(diagnostics, CatalogDiagnostic{
				Code:              DiagnosticDuplicateModuleID,
				Severity:          "error",
				ModuleID:          mod.ID,
				FilePath:          moduleFile,
				Message:           fmt.Sprintf("duplicate config module id %q found at %s, skipping", mod.ID, moduleFile),
				WingetRefs:        append([]string(nil), mod.Matches.Winget...),
				ChocolateyRefs:    append([]string(nil), mod.Matches.Chocolatey...),
				InstanceDetectors: diagnosticInstanceDetectors(mod),
			})
			continue
		}

		// Set metadata fields.
		mod.FilePath = moduleFile
		mod.ModuleDir = filepath.Dir(moduleFile)
		mod.Tier = tier

		catalog[mod.ID] = mod
	}

	return catalog, diagnostics, nil
}

func catalogDiagnosticIdentity(data []byte, tier, directoryName string) (string, []string, []string, []InstanceDetectorDef, bool) {
	type diagnosticConfigIdentity struct {
		InstanceDetectors []InstanceDetectorDef `json:"instanceDetectors"`
	}
	identity := struct {
		ID      string                    `json:"id"`
		Matches MatchCriteria             `json:"matches"`
		Config  *diagnosticConfigIdentity `json:"config"`
	}{}
	moduleID := tier + "." + directoryName
	if err := json.Unmarshal(manifest.StripJsoncComments(data), &identity); err != nil {
		return moduleID, nil, nil, nil, true
	}
	if strings.TrimSpace(identity.ID) != "" {
		moduleID = identity.ID
	}
	var detectors []InstanceDetectorDef
	if identity.Config != nil {
		detectors = append([]InstanceDetectorDef(nil), identity.Config.InstanceDetectors...)
	}
	return moduleID,
		append([]string(nil), identity.Matches.Winget...),
		append([]string(nil), identity.Matches.Chocolatey...),
		detectors,
		false
}

// ParseAndValidateModuleJSON applies the same parse and production validation
// steps used by catalog loading to caller-supplied, already-safe module bytes.
func ParseAndValidateModuleJSON(data []byte, filePath string) (*Module, error) {
	mod, err := ParseModuleJSON(data)
	if err != nil {
		return nil, err
	}
	if err := validateModule(mod, filePath); err != nil {
		return nil, err
	}
	mod.FilePath = filePath
	mod.ModuleDir = filepath.Dir(filePath)
	return mod, nil
}

func diagnosticInstanceDetectors(mod *Module) []InstanceDetectorDef {
	if mod == nil || mod.Config == nil {
		return nil
	}
	return append([]InstanceDetectorDef(nil), mod.Config.InstanceDetectors...)
}

// AppsTier is the catalog tier whose modules configure installed
// applications. Other tiers, such as Windows settings, carry configuration
// that is not an application.
const AppsTier = "apps"

// CatalogTier is one directory under repoRoot/modules that holds module
// directories. Its name is the tier of every module loaded from it.
type CatalogTier struct {
	Name string
	Dir  string
}

// CatalogTierDirs lists every tier directory under repoRoot/modules in name
// order. Files directly under modules/ are not tiers. A missing modules
// directory yields no tiers, which keeps the historical empty-catalog result.
func CatalogTierDirs(repoRoot string) ([]CatalogTier, error) {
	modulesRoot := filepath.Join(repoRoot, "modules")
	entries, err := os.ReadDir(modulesRoot)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	tiers := make([]CatalogTier, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			tiers = append(tiers, CatalogTier{Name: entry.Name(), Dir: filepath.Join(modulesRoot, entry.Name())})
		}
	}
	sort.Slice(tiers, func(left, right int) bool { return tiers[left].Name < tiers[right].Name })
	return tiers, nil
}

// GetCatalog loads every catalog tier under repoRoot/modules and prints a
// warning for each module it skips.
func GetCatalog(repoRoot string) (map[string]*Module, error) {
	catalog, diagnostics, err := GetCatalogWithDiagnostics(repoRoot)
	for _, diagnostic := range diagnostics {
		fmt.Fprintf(os.Stderr, "Warning: %s\n", diagnostic.Message)
	}
	return catalog, err
}

// GetCatalogWithDiagnostics loads every catalog tier under repoRoot/modules
// into one catalog and returns structured skip diagnostics. A module ID that
// an earlier tier already loaded is skipped with a duplicate diagnostic.
func GetCatalogWithDiagnostics(repoRoot string) (map[string]*Module, []CatalogDiagnostic, error) {
	catalog := make(map[string]*Module)
	var diagnostics []CatalogDiagnostic
	tiers, err := CatalogTierDirs(repoRoot)
	if err != nil {
		return catalog, diagnostics, nil
	}
	for _, tier := range tiers {
		tierCatalog, tierDiagnostics, err := LoadTierCatalogWithDiagnostics(tier.Dir, tier.Name)
		if err != nil {
			return nil, nil, err
		}
		diagnostics = append(diagnostics, tierDiagnostics...)
		moduleIDs := make([]string, 0, len(tierCatalog))
		for moduleID := range tierCatalog {
			moduleIDs = append(moduleIDs, moduleID)
		}
		sort.Strings(moduleIDs)
		for _, moduleID := range moduleIDs {
			mod := tierCatalog[moduleID]
			if _, exists := catalog[moduleID]; exists {
				diagnostics = append(diagnostics, CatalogDiagnostic{
					Code: DiagnosticDuplicateModuleID, Severity: "error", ModuleID: moduleID, FilePath: mod.FilePath,
					Message:           fmt.Sprintf("duplicate config module id %q found at %s, skipping", moduleID, mod.FilePath),
					WingetRefs:        append([]string(nil), mod.Matches.Winget...),
					ChocolateyRefs:    append([]string(nil), mod.Matches.Chocolatey...),
					InstanceDetectors: diagnosticInstanceDetectors(mod),
				})
				continue
			}
			catalog[moduleID] = mod
		}
	}
	return catalog, diagnostics, nil
}

// validateModule checks that a module has all required fields.
func validateModule(mod *Module, filePath string) error {
	if mod.ID == "" {
		return validationError(mod, filePath, DiagnosticInvalidID, "missing or empty 'id' field")
	}
	if mod.DisplayName == "" {
		return validationError(mod, filePath, DiagnosticInvalidID, "missing or empty 'displayName' field")
	}

	// matches must declare at least one matcher, and a module that actually does
	// something must declare one the engine consults.
	//
	// exe and uninstallDisplayName satisfy the first rule but not the second.
	// Neither takes part in matching: matchModulesForApps reads only winget,
	// chocolatey and pathExists, exe supplies the planner's running-process
	// guard, and uninstallDisplayName only breaks precedence ties between modules
	// that already matched. Accepting them as sufficient let seven modules load
	// cleanly, appear in the catalog, and never attach to anything — the silent
	// failure behind Artexis10/endstate#208.
	//
	// The stricter rule is scoped to modules carrying capture or restore work,
	// because those are the only ones that can strand a user. Inert stubs that
	// declare neither are left alone; whether they should exist at all is a
	// catalog question, not a validation one. Schema-v2 modules are exempt
	// throughout, being discovered via config.instanceDetectors instead.
	hasWinget := len(mod.Matches.Winget) > 0
	hasChocolatey := len(mod.Matches.Chocolatey) > 0
	hasExe := len(mod.Matches.Exe) > 0
	hasUninstall := len(mod.Matches.UninstallDisplayName) > 0
	hasPathExists := len(mod.Matches.PathExists) > 0
	hasDetectors := mod.Config != nil && len(mod.Config.InstanceDetectors) > 0

	if !hasWinget && !hasChocolatey && !hasExe && !hasUninstall && !hasPathExists && !hasDetectors {
		return validationError(mod, filePath, DiagnosticInvalidID, "matches must have at least one of: winget, chocolatey, exe, uninstallDisplayName, pathExists")
	}

	capturesSomething := mod.Capture.HasCaptureWork()
	restoresSomething := len(mod.Restore) > 0

	if (capturesSomething || restoresSomething) && !hasWinget && !hasChocolatey && !hasPathExists && !hasDetectors {
		return validationError(mod, filePath, DiagnosticInvalidID, "a module with capture or restore work must match on winget, chocolatey or pathExists; exe and uninstallDisplayName do not participate in matching, so the module could never attach")
	}

	return validateModuleV2(mod, filePath)
}
