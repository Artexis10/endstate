// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/bundle"
	"github.com/Artexis10/endstate/go-engine/internal/config"
	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/events"
	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/snapshot"
)

// CaptureFlags holds parsed CLI flags for the capture command.
type CaptureFlags struct {
	Manifest         string // existing manifest to update
	Out              string // output path
	Profile          string // profile name
	Name             string // manifest name
	Sanitize         bool
	Discover         bool
	Update           bool
	IncludeRuntimes  bool
	IncludeStoreApps bool
	Minimize         bool
	Events           string // "jsonl" or ""
}

// CaptureResult is the data payload for the capture command.
// The shape matches the PowerShell Invoke-CaptureCore return value so the GUI
// can consume it without transformation.
type CaptureResult struct {
	AppsIncluded         []CaptureApp          `json:"appsIncluded"`
	ConfigModules        []CaptureModuleResult `json:"configModules"`
	ConfigModuleMap      map[string]string     `json:"configModuleMap"`
	OutputPath           string                `json:"outputPath"`
	OutputFormat         string                `json:"outputFormat"`       // "zip" or "jsonc"
	ConfigsIncluded      []string              `json:"configsIncluded"`
	ConfigsSkipped       []string              `json:"configsSkipped"`
	ConfigsCaptureErrors []string              `json:"configsCaptureErrors"`
	Sanitized            bool                  `json:"sanitized"`
	IsExample            interface{}           `json:"isExample"`
	Counts               CaptureCountsFull     `json:"counts"`

	// Manifest identifies the manifest that was produced (kept for tooling
	// and test compatibility).
	Manifest CaptureManifest `json:"manifest"`
}

// CaptureApp is a single entry in AppsIncluded.
type CaptureApp struct {
	Source string `json:"source"`
	Name   string `json:"name,omitempty"`
	ID     string `json:"id"`
}

// CaptureModuleResult holds per-module capture details for ConfigModules.
type CaptureModuleResult struct {
	DisplayName   string   `json:"displayName"`
	WingetRefs    []string `json:"wingetRefs"`
	AppID         string   `json:"appId"`
	ID            string   `json:"id"`
	Paths         []string `json:"paths"`
	FilesCaptured int      `json:"filesCaptured"`
	Status        string   `json:"status"` // "captured" or "skipped"
}

// CaptureCountsFull aggregates filtering and capture statistics.
type CaptureCountsFull struct {
	FilteredRuntimes       int `json:"filteredRuntimes"`
	Included               int `json:"included"`
	TotalFound             int `json:"totalFound"`
	SensitiveExcludedCount int `json:"sensitiveExcludedCount"`
	FilteredStoreApps      int `json:"filteredStoreApps"`
	Skipped                int `json:"skipped"`
}

// CaptureManifest identifies the manifest that was produced.
type CaptureManifest struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// snapshotRetryDelay is the pause before retrying a snapshot when the first
// attempt returns zero packages (winget lock contention). Tests override this
// to avoid slow sleeps.
var snapshotRetryDelay = 2 * time.Second

// takeSnapshotFn is the function used to enumerate winget-managed packages. It
// defaults to snapshot.WingetExport (authoritative list via `winget export`)
// and can be replaced in tests to inject fake data.
var takeSnapshotFn = snapshot.WingetExport

// getDisplayNameMapFn is the function used to obtain the winget display-name
// map. It defaults to snapshot.GetDisplayNameMap and can be replaced in tests.
var getDisplayNameMapFn = snapshot.GetDisplayNameMap

// resolveRepoRootFn returns the repo root path. It defaults to
// config.ResolveRepoRoot and can be replaced in tests to avoid filesystem
// dependency on a VERSION file.
var resolveRepoRootFn = config.ResolveRepoRoot

// resolveProfileDirFn returns the profiles directory path. It defaults to
// config.ProfileDir and can be replaced in tests to avoid writing to the real
// user profile directory.
var resolveProfileDirFn = config.ProfileDir

// loadModuleCatalogFn loads the module catalog from the given repo root. It
// defaults to modules.GetCatalog and can be replaced in tests.
var loadModuleCatalogFn = func(repoRoot string) (map[string]*modules.Module, error) {
	return modules.GetCatalog(repoRoot)
}

// capturedApp is an internal representation of a captured application entry
// before it is written to the output manifest.
type capturedApp struct {
	ID   string            `json:"id"`
	Refs map[string]string `json:"refs"`
	Name string            `json:"_name,omitempty"`
}

// cleanApp is the sanitized version of capturedApp without underscore-prefixed fields.
type cleanApp struct {
	ID   string            `json:"id"`
	Refs map[string]string `json:"refs"`
}

// captureManifestOutput is the manifest structure written to disk.
type captureManifestOutput struct {
	Version  int         `json:"version"`
	Name     string      `json:"name,omitempty"`
	Captured string      `json:"captured,omitempty"`
	Apps     interface{} `json:"apps"`
}

// RunCapture executes the capture command with the provided flags.
//
// The algorithm:
//  1. Emit phase("capture")
//  2. Enumerate winget-managed packages via winget export
//  3. Convert snapshot apps to manifest app entries
//  4. Filter runtime packages and store IDs
//  5. If --update and --manifest: merge with existing manifest
//  6. If --sanitize: strip underscore fields, sort by id
//  7. Determine output path and write manifest (.jsonc)
//  8. Verify file exists and is non-empty (INV-CAPTURE-2)
//  9. Load module catalog and match against captured apps
//  10. Non-sanitized: create zip bundle, populate config module fields
//  11. Emit artifact and summary events
func RunCapture(flags CaptureFlags) (interface{}, *envelope.Error) {
	runID := buildRunID("capture")
	emitter := events.NewEmitter(runID, flags.Events == "jsonl")

	// --- 1. Emit phase event (first event per event-contract.md) ---
	emitter.EmitPhase("capture")

	// --- 2. Enumerate winget-managed packages and resolve display names ---
	// Both calls spawn winget and are slow; run them concurrently.
	type snapshotResult struct {
		apps []snapshot.SnapshotApp
		err  error
	}
	type nameMapResult struct {
		nameMap map[string]string
		err     error
	}

	snapCh := make(chan snapshotResult, 1)
	nameCh := make(chan nameMapResult, 1)

	go func() {
		apps, err := takeSnapshotFn()
		snapCh <- snapshotResult{apps, err}
	}()
	go func() {
		nameMap, err := getDisplayNameMapFn()
		nameCh <- nameMapResult{nameMap, err}
	}()

	snapRes := <-snapCh
	nameRes := <-nameCh

	if snapRes.err != nil {
		var execErr *exec.Error
		if errors.As(snapRes.err, &execErr) && errors.Is(execErr.Err, exec.ErrNotFound) {
			return nil, envelope.NewError(
				envelope.ErrWingetNotAvailable,
				"winget is not installed or not available on PATH.",
			).WithRemediation("Install winget from https://aka.ms/winget or ensure it is on your PATH.")
		}
		return nil, envelope.NewError(
			envelope.ErrCaptureFailed,
			fmt.Sprintf("Failed to take system snapshot: %v", snapRes.err),
		)
	}

	snapshotApps := snapRes.apps

	// Guard: winget sometimes returns empty results due to database lock
	// contention. Retry once after a brief pause before failing.
	if len(snapshotApps) == 0 {
		fmt.Fprintf(os.Stderr, "Warning: winget returned 0 packages, retrying after %v...\n", snapshotRetryDelay)
		time.Sleep(snapshotRetryDelay)
		retryApps, retryErr := takeSnapshotFn()
		if retryErr == nil && len(retryApps) > 0 {
			snapshotApps = retryApps
		}
	}

	// If still empty after retry, fail explicitly. A machine with winget
	// should always have at least a few packages. Discover mode is exempt
	// because it may legitimately find nothing on a fresh machine.
	if len(snapshotApps) == 0 && !flags.Discover {
		return nil, envelope.NewError(
			envelope.ErrCaptureFailed,
			"Winget returned no packages after retry. This usually means another winget operation is still running.",
		).WithRemediation("Wait a few seconds and try again. Run 'winget list' in a terminal to verify winget is working.")
	}

	// Display name map — failure is non-fatal.
	displayNameMap := make(map[string]string)
	if nameRes.err == nil {
		displayNameMap = nameRes.nameMap
	}

	totalFound := len(snapshotApps)

	// --- 3. Convert and filter snapshot apps ---
	var captured []capturedApp
	filteredRuntimes := 0
	filteredStore := 0
	skipped := 0

	for _, sApp := range snapshotApps {
		// Filter runtime packages unless --include-runtimes.
		if !flags.IncludeRuntimes && snapshot.IsRuntimePackage(sApp.ID) {
			filteredRuntimes++
			skipped++
			continue
		}

		// Filter store IDs unless --include-store-apps.
		if !flags.IncludeStoreApps && snapshot.IsStoreID(sApp.ID) {
			filteredStore++
			skipped++
			continue
		}

		appID := wingetIDToManifestID(sApp.ID)

		app := capturedApp{
			ID: appID,
			Refs: map[string]string{
				"windows": sApp.ID,
			},
			Name: sApp.Name,
		}

		captured = append(captured, app)
	}

	// --- 4. Emit item events for each included app ---
	for _, app := range captured {
		wingetID := app.Refs["windows"]
		name := displayNameMap[wingetID]
		if name == "" {
			name = app.Name
		}
		emitter.EmitItem(wingetID, "winget", "captured", "", fmt.Sprintf("Captured %s", name), name)
	}

	// --- 5. If --update and --manifest: merge with existing manifest ---
	if flags.Update && flags.Manifest != "" {
		existingMf, loadErr := loadManifest(flags.Manifest)
		if loadErr != nil {
			return nil, loadErr
		}

		// Build set of existing windows refs for dedup.
		existingRefs := make(map[string]bool)
		for _, app := range existingMf.Apps {
			if ref, ok := app.Refs["windows"]; ok {
				existingRefs[ref] = true
			}
		}

		// Convert existing apps to capturedApp format.
		var merged []capturedApp
		for _, app := range existingMf.Apps {
			merged = append(merged, capturedApp{
				ID:   app.ID,
				Refs: app.Refs,
			})
		}

		// Append newly discovered apps that aren't already present.
		for _, app := range captured {
			winRef := app.Refs["windows"]
			if !existingRefs[winRef] {
				merged = append(merged, app)
			}
		}

		captured = merged
	}

	included := len(captured)

	// --- 6. Sanitize ---
	var outputApps interface{}
	if flags.Sanitize {
		sorted := make([]cleanApp, len(captured))
		for i, app := range captured {
			sorted[i] = cleanApp{
				ID:   app.ID,
				Refs: app.Refs,
			}
		}
		sort.Slice(sorted, func(i, j int) bool {
			return sorted[i].ID < sorted[j].ID
		})
		outputApps = sorted
	} else {
		sort.Slice(captured, func(i, j int) bool {
			return captured[i].ID < captured[j].ID
		})
		outputApps = captured
	}

	// --- 7. Determine output path ---
	outputPath := resolveOutputPath(flags)

	// Determine manifest name.
	manifestName := "captured"
	if flags.Name != "" {
		manifestName = flags.Name
	} else if flags.Profile != "" {
		manifestName = flags.Profile
	}

	// Build the output manifest.
	capturedTimestamp := time.Now().UTC().Format(time.RFC3339)
	outManifest := captureManifestOutput{
		Version:  1,
		Name:     manifestName,
		Captured: capturedTimestamp,
		Apps:     outputApps,
	}

	// Write manifest as pretty-printed JSON (JSONC-compatible).
	data, marshalErr := json.MarshalIndent(outManifest, "", "  ")
	if marshalErr != nil {
		return nil, envelope.NewError(
			envelope.ErrCaptureFailed,
			fmt.Sprintf("Failed to marshal manifest: %v", marshalErr),
		)
	}

	// Ensure parent directory exists.
	dir := filepath.Dir(outputPath)
	if dir != "" && dir != "." {
		if mkdirErr := os.MkdirAll(dir, 0755); mkdirErr != nil {
			return nil, envelope.NewError(
				envelope.ErrManifestWriteFailed,
				fmt.Sprintf("Failed to create output directory: %v", mkdirErr),
			).WithRemediation("Check directory permissions and ensure the path is writable.")
		}
	}

	if writeErr := os.WriteFile(outputPath, data, 0644); writeErr != nil {
		return nil, envelope.NewError(
			envelope.ErrManifestWriteFailed,
			fmt.Sprintf("Failed to write manifest file: %v", writeErr),
		).WithRemediation("Check directory permissions and ensure the path is writable.")
	}

	// --- 8. INV-CAPTURE-2: Verify file exists and is non-empty ---
	fileInfo, statErr := os.Stat(outputPath)
	if statErr != nil || fileInfo.Size() == 0 {
		return nil, envelope.NewError(
			envelope.ErrManifestWriteFailed,
			"Manifest file is empty or does not exist after write.",
		).WithRemediation("Check disk space and directory permissions.")
	}

	// Resolve to absolute path for the artifact event.
	absPath, absErr := filepath.Abs(outputPath)
	if absErr != nil {
		absPath = outputPath
	}

	// --- 9. Build appsIncluded (reuses displayNameMap from step 2) ---
	appsIncluded := buildAppsIncluded(captured, displayNameMap)

	// --- 10. Module matching and optional bundle creation ---
	configModules := []CaptureModuleResult{}
	configModuleMap := map[string]string{}
	configsIncluded := []string{}
	configsSkipped := []string{}
	configsCaptureErrors := []string{}

	outputFormat := "jsonc"
	finalOutputPath := absPath

	if !flags.Sanitize {
		repoRoot := resolveRepoRootFn()
		if repoRoot != "" {
			catalog, catalogErr := loadModuleCatalogFn(repoRoot)
			if catalogErr == nil && len(catalog) > 0 {
				// Build manifest.App slice for matching.
				manifestApps := make([]manifest.App, 0, len(captured))
				for _, ca := range captured {
					manifestApps = append(manifestApps, manifest.App{
						ID:   ca.ID,
						Refs: ca.Refs,
					})
				}

				matchedModules := modules.MatchModulesForApps(catalog, manifestApps)

				if len(matchedModules) > 0 {
					// Build configModuleMap from matched modules.
					configModuleMap = buildConfigModuleMap(matchedModules)

					// Determine zip output path.
					profileName := manifestName
					if flags.Profile != "" {
						profileName = flags.Profile
					}
					profilesDir := resolveProfileDirFn()
					zipOutputPath := filepath.Join(profilesDir, profileName+".zip")

					version := config.ReadVersion(repoRoot)

					// Create zip bundle.
					bundleErr := bundle.CreateBundle(absPath, matchedModules, zipOutputPath, version)

					if bundleErr == nil {
						// Bundle succeeded: zip is the deliverable.
						// Remove intermediate .jsonc (zip is the canonical output).
						os.Remove(absPath)
						outputFormat = "zip"
						finalOutputPath = zipOutputPath
					} else {
						// Bundle failed: keep the .jsonc as fallback.
						configsCaptureErrors = append(configsCaptureErrors,
							fmt.Sprintf("Bundle creation failed: %v", bundleErr))
					}

					// Build per-module results using a fresh staging pass so we
					// have accurate file counts and paths.
					configModules, configsIncluded, configsSkipped, configsCaptureErrors =
						buildConfigModuleResults(matchedModules, configsCaptureErrors)
				}
			}
		}
	}

	// Ensure slice fields are never null in JSON output.
	if configModules == nil {
		configModules = []CaptureModuleResult{}
	}
	if configsIncluded == nil {
		configsIncluded = []string{}
	}
	if configsSkipped == nil {
		configsSkipped = []string{}
	}
	if configsCaptureErrors == nil {
		configsCaptureErrors = []string{}
	}

	// --- 11. Emit artifact and summary events ---
	emitter.EmitArtifact("capture", "manifest", finalOutputPath)
	emitter.EmitSummary("capture", totalFound, included, skipped, 0)

	return &CaptureResult{
		AppsIncluded:         appsIncluded,
		ConfigModules:        configModules,
		ConfigModuleMap:      configModuleMap,
		OutputPath:           finalOutputPath,
		OutputFormat:         outputFormat,
		ConfigsIncluded:      configsIncluded,
		ConfigsSkipped:       configsSkipped,
		ConfigsCaptureErrors: configsCaptureErrors,
		Sanitized:            flags.Sanitize,
		IsExample:            false,
		Counts: CaptureCountsFull{
			FilteredRuntimes:       filteredRuntimes,
			Included:               included,
			TotalFound:             totalFound,
			SensitiveExcludedCount: 0,
			FilteredStoreApps:      filteredStore,
			Skipped:                skipped,
		},
		Manifest: CaptureManifest{
			Name: manifestName,
			Path: finalOutputPath,
		},
	}, nil
}

// buildAppsIncluded converts internal captured apps to the CaptureApp slice
// expected by the GUI. Display names are looked up in displayNameMap (winget
// ID -> display name). If a display name is not in the map, the name captured
// at snapshot time (_name field) is used.
func buildAppsIncluded(apps []capturedApp, displayNameMap map[string]string) []CaptureApp {
	result := make([]CaptureApp, 0, len(apps))
	for _, ca := range apps {
		wingetID := ca.Refs["windows"]
		if wingetID == "" {
			wingetID = ca.ID
		}
		entry := CaptureApp{
			Source: "winget",
			ID:     wingetID,
		}
		if name, ok := displayNameMap[wingetID]; ok && name != "" {
			entry.Name = name
		} else if ca.Name != "" {
			entry.Name = ca.Name
		}
		result = append(result, entry)
	}
	return result
}

// buildConfigModuleMap builds a winget-ref to module-ID map from matched modules.
// Modules without winget refs are keyed by their module ID.
func buildConfigModuleMap(matchedModules []*modules.Module) map[string]string {
	m := make(map[string]string, len(matchedModules))
	for _, mod := range matchedModules {
		if len(mod.Matches.Winget) > 0 {
			for _, wingetRef := range mod.Matches.Winget {
				m[wingetRef] = mod.ID
			}
		} else {
			m[mod.ID] = mod.ID
		}
	}
	return m
}

// buildConfigModuleResults builds the CaptureModuleResult slice and collects
// per-module file counts by running CollectConfigFiles against each matched
// module in a temporary staging directory.
func buildConfigModuleResults(matchedModules []*modules.Module, existingErrors []string) (
	results []CaptureModuleResult,
	included []string,
	skipped []string,
	captureErrors []string,
) {
	captureErrors = existingErrors

	stagingDir, mkdirErr := os.MkdirTemp("", "endstate-capture-meta-")
	if mkdirErr != nil {
		captureErrors = append(captureErrors, fmt.Sprintf("failed to create staging dir: %v", mkdirErr))
		for _, mod := range matchedModules {
			dirName := moduleDirName(mod.ID)
			skipped = append(skipped, dirName)
			results = append(results, CaptureModuleResult{
				ID:          mod.ID,
				AppID:       dirName,
				DisplayName: mod.DisplayName,
				WingetRefs:  safeStringSlice(mod.Matches.Winget),
				Paths:       []string{},
				Status:      "skipped",
			})
		}
		return
	}
	defer os.RemoveAll(stagingDir)

	includedSet := make(map[string]bool)
	moduleFileCounts := make(map[string]int)
	moduleFilePaths := make(map[string][]string)
	moduleErrors := make(map[string]bool)

	for _, mod := range matchedModules {
		dirName := moduleDirName(mod.ID)

		collected, collectErr := bundle.CollectConfigFiles(mod, stagingDir)
		if collectErr != nil {
			captureErrors = append(captureErrors, fmt.Sprintf("module %s: %v", mod.ID, collectErr))
			moduleErrors[dirName] = true
			moduleFileCounts[dirName] = 0
			moduleFilePaths[dirName] = []string{}
			continue
		}

		if len(collected) > 0 {
			includedSet[dirName] = true
			moduleFileCounts[dirName] = len(collected)
			moduleFilePaths[dirName] = collected
		} else {
			moduleFileCounts[dirName] = 0
			moduleFilePaths[dirName] = []string{}
		}
	}

	for _, mod := range matchedModules {
		dirName := moduleDirName(mod.ID)

		status := "skipped"
		if moduleErrors[dirName] {
			status = "error"
		} else if includedSet[dirName] {
			status = "captured"
			included = append(included, dirName)
		} else {
			skipped = append(skipped, dirName)
		}

		paths := moduleFilePaths[dirName]
		if paths == nil {
			paths = []string{}
		}

		results = append(results, CaptureModuleResult{
			ID:            mod.ID,
			AppID:         dirName,
			DisplayName:   mod.DisplayName,
			WingetRefs:    safeStringSlice(mod.Matches.Winget),
			Paths:         paths,
			FilesCaptured: moduleFileCounts[dirName],
			Status:        status,
		})
	}

	return
}

// moduleDirName strips the "apps." prefix from a module ID to get the
// directory name used under configs/ in the bundle.
func moduleDirName(moduleID string) string {
	if strings.HasPrefix(moduleID, "apps.") {
		return moduleID[5:]
	}
	return moduleID
}

// safeStringSlice returns s as-is if non-nil, or an empty slice.
func safeStringSlice(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// resolveOutputPath determines the output file path based on flags.
//
// Priority:
//  1. --profile: <ProfileDir>/<profile>.jsonc
//  2. --out: use as-is
//  3. Default: captured-<timestamp>.jsonc in current directory
func resolveOutputPath(flags CaptureFlags) string {
	if flags.Profile != "" {
		profileDir := resolveProfileDirFn()
		if profileDir != "" {
			return filepath.Join(profileDir, flags.Profile+".jsonc")
		}
	}

	if flags.Out != "" {
		return flags.Out
	}

	timestamp := time.Now().UTC().Format("20060102-150405")
	return fmt.Sprintf("captured-%s.jsonc", timestamp)
}

// wingetIDToManifestID converts a winget package ID to a manifest app ID.
// The ID is lowercased and dots are replaced with hyphens.
// Example: "Microsoft.VisualStudioCode" -> "microsoft-visualstudiocode"
func wingetIDToManifestID(wingetID string) string {
	return strings.ToLower(strings.ReplaceAll(wingetID, ".", "-"))
}
