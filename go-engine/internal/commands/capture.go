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
	"github.com/Artexis10/endstate/go-engine/internal/driver"
	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/events"
	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/realizer"
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
	Pin              bool
	Drivers          []string // repeatable explicit package drivers; empty uses platform capture defaults
	Events           string   // "jsonl" or ""
}

// CaptureResult is the data payload for the capture command.
// The shape matches the PowerShell Invoke-CaptureCore return value so the GUI
// can consume it without transformation.
type CaptureResult struct {
	AppsIncluded         []CaptureApp          `json:"appsIncluded"`
	ConfigModules        []CaptureModuleResult `json:"configModules"`
	ConfigModuleMap      map[string]string     `json:"configModuleMap"`
	PackageModuleMap     map[string][]string   `json:"packageModuleMap"`
	Warnings             []CommandWarning      `json:"warnings,omitempty"`
	OutputPath           string                `json:"outputPath"`
	OutputFormat         string                `json:"outputFormat"` // "zip" or "jsonc"
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
	DisplayName    string   `json:"displayName"`
	WingetRefs     []string `json:"wingetRefs"`
	ChocolateyRefs []string `json:"chocolateyRefs"`
	AppID          string   `json:"appId"`
	ID             string   `json:"id"`
	Paths          []string `json:"paths"`
	FilesCaptured  int      `json:"filesCaptured"`
	Status         string   `json:"status"` // "captured" or "skipped"
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

// listInstalledFn enumerates installed packages (winget list) to build the
// display-name and installed-version maps in one pass. It defaults to
// snapshot.TakeSnapshot and can be replaced in tests.
var listInstalledFn = snapshot.TakeSnapshot

// resolveRepoRootFn returns the repo root path. It defaults to
// config.ResolveRepoRoot and can be replaced in tests to avoid filesystem
// dependency on the repo-root marker (.release-please-manifest.json).
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
	ID      string            `json:"id"`
	Refs    map[string]string `json:"refs"`
	Driver  string            `json:"driver,omitempty"`
	Version string            `json:"version,omitempty"`
	Name    string            `json:"_name,omitempty"`
	Source  string            `json:"-"`
}

type enumeratedCapturePackage struct {
	Driver  string
	Package driver.InstalledPackage
}

type legacyWingetCaptureEnumerator struct {
	structuredEvents bool
}

// resolveCaptureEnumeratorFn is the capture-specific lazy driver seam. Winget
// keeps the long-standing snapshot injection points used by command tests;
// other drivers resolve through the platform registry.
var resolveCaptureEnumeratorFn = func(name string, structuredEvents bool) (driver.InstalledEnumerator, error) {
	if strings.EqualFold(name, "winget") {
		return legacyWingetCaptureEnumerator{structuredEvents: structuredEvents}, nil
	}
	d, err := selectDriver(captureGOOSFn(), name)
	if err != nil {
		return nil, err
	}
	enumerator, ok := d.(driver.InstalledEnumerator)
	if !ok {
		return nil, fmt.Errorf("driver %s does not support installed-package enumeration", name)
	}
	return enumerator, nil
}

func (legacy legacyWingetCaptureEnumerator) EnumerateInstalled() ([]driver.InstalledPackage, error) {
	type snapshotResult struct {
		apps []snapshot.SnapshotApp
		err  error
	}
	exportCh := make(chan snapshotResult, 1)
	listCh := make(chan snapshotResult, 1)
	go func() {
		apps, err := takeSnapshotFn()
		exportCh <- snapshotResult{apps: apps, err: err}
	}()
	go func() {
		apps, err := listInstalledFn()
		listCh <- snapshotResult{apps: apps, err: err}
	}()

	exported := <-exportCh
	listed := <-listCh
	if exported.err != nil {
		return nil, exported.err
	}
	if len(exported.apps) == 0 {
		if !legacy.structuredEvents {
			fmt.Fprintf(os.Stderr, "Warning: winget returned 0 packages, retrying after %v...\n", snapshotRetryDelay)
		}
		time.Sleep(snapshotRetryDelay)
		if retry, err := takeSnapshotFn(); err == nil && len(retry) > 0 {
			exported.apps = retry
		}
	}

	evidence := make(map[string]snapshot.SnapshotApp, len(listed.apps))
	if listed.err == nil {
		for _, app := range listed.apps {
			evidence[app.ID] = app
		}
	}
	packages := make([]driver.InstalledPackage, 0, len(exported.apps))
	for _, app := range exported.apps {
		listedApp := evidence[app.ID]
		name := listedApp.Name
		if name == "" {
			name = app.Name
		}
		version := listedApp.Version
		if version == "" {
			version = app.Version
		}
		packages = append(packages, driver.InstalledPackage{Ref: app.ID, DisplayName: name, Version: version})
	}
	return packages, nil
}

func captureDriverNames(flags CaptureFlags) []string {
	registered := platformBackendsFor(captureGOOSFn()).DriverNames()
	if len(flags.Drivers) == 0 {
		return registered
	}
	selected := make(map[string]bool, len(flags.Drivers))
	for _, name := range flags.Drivers {
		name = strings.ToLower(strings.TrimSpace(name))
		if name != "" {
			selected[name] = true
		}
	}
	ordered := make([]string, 0, len(selected))
	for _, name := range registered {
		if selected[name] {
			ordered = append(ordered, name)
			delete(selected, name)
		}
	}
	unknown := make([]string, 0, len(selected))
	for name := range selected {
		unknown = append(unknown, name)
	}
	sort.Strings(unknown)
	return append(ordered, unknown...)
}

func includesCaptureDriver(flags CaptureFlags, wanted string) bool {
	for _, name := range captureDriverNames(flags) {
		if name == wanted {
			return true
		}
	}
	return false
}

func enumerateWindowsCapturePackages(flags CaptureFlags) ([]enumeratedCapturePackage, []CommandWarning, *envelope.Error) {
	explicit := len(flags.Drivers) > 0
	if explicit {
		for _, name := range flags.Drivers {
			if strings.TrimSpace(name) == "" {
				return nil, nil, envelope.NewError(envelope.ErrCaptureFailed, "Capture driver name must not be empty.")
			}
		}
	}
	var packages []enumeratedCapturePackage
	var warnings []CommandWarning
	seenIdentities := make(map[string]bool)
	for _, name := range captureDriverNames(flags) {
		enumerator, err := resolveCaptureEnumeratorFn(name, flags.Events == "jsonl")
		if err == nil {
			var installed []driver.InstalledPackage
			installed, err = enumerator.EnumerateInstalled()
			if err == nil {
				sort.SliceStable(installed, func(i, j int) bool {
					left, right := strings.ToLower(installed[i].Ref), strings.ToLower(installed[j].Ref)
					if left != right {
						return left < right
					}
					return installed[i].Ref < installed[j].Ref
				})
				for _, pkg := range installed {
					if strings.TrimSpace(pkg.Ref) == "" {
						continue
					}
					identity := captureIdentity(name, pkg.Ref)
					if seenIdentities[identity] {
						continue
					}
					seenIdentities[identity] = true
					packages = append(packages, enumeratedCapturePackage{Driver: name, Package: pkg})
				}
				continue
			}
		}

		if !explicit && name != "winget" {
			warnings = append(warnings, CommandWarning{
				Code:    "optional_driver_unavailable",
				Message: fmt.Sprintf("Optional capture driver %s is unavailable: %v", name, err),
				Driver:  name,
			})
			continue
		}
		if name == "winget" {
			var execErr *exec.Error
			if errors.As(err, &execErr) && errors.Is(execErr.Err, exec.ErrNotFound) {
				return nil, warnings, envelope.NewError(
					envelope.ErrWingetNotAvailable,
					"winget is not installed or not available on PATH.",
				).WithRemediation("Install winget from https://aka.ms/winget or ensure it is on your PATH.")
			}
		}
		return nil, warnings, envelope.NewError(
			envelope.ErrCaptureFailed,
			fmt.Sprintf("Failed to enumerate installed packages with %s: %v", name, err),
		)
	}
	return packages, warnings, nil
}

func countCapturePackages(packages []enumeratedCapturePackage, driverName string) int {
	count := 0
	for _, item := range packages {
		if item.Driver == driverName {
			count++
		}
	}
	return count
}

func effectiveCaptureDriver(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return "winget"
	}
	return name
}

func captureIdentity(driverName, ref string) string {
	return effectiveCaptureDriver(driverName) + "\x00" + strings.TrimSpace(ref)
}

func deterministicCaptureID(base, driverName string, used map[string]bool) string {
	if used == nil {
		used = make(map[string]bool)
	}
	base = strings.TrimSpace(base)
	if base == "" {
		base = "package"
	}
	if !used[strings.ToLower(base)] {
		used[strings.ToLower(base)] = true
		return base
	}
	suffixBase := base + "-" + effectiveCaptureDriver(driverName)
	candidate := suffixBase
	for n := 2; used[strings.ToLower(candidate)]; n++ {
		candidate = fmt.Sprintf("%s-%d", suffixBase, n)
	}
	used[strings.ToLower(candidate)] = true
	return candidate
}

func assignDeterministicCaptureIDs(apps []capturedApp, used map[string]bool) {
	if used == nil {
		used = make(map[string]bool, len(apps))
	}
	for i := range apps {
		apps[i].ID = deterministicCaptureID(apps[i].ID, effectiveCaptureDriver(apps[i].Driver), used)
	}
}

func possibleDuplicateWarnings(apps []capturedApp) []CommandWarning {
	type prior struct{ driverName string }
	seen := make(map[string][]prior)
	var warnings []CommandWarning
	for _, app := range apps {
		name := strings.TrimSpace(app.Name)
		if name == "" {
			continue
		}
		driverName := effectiveCaptureDriver(app.Driver)
		key := strings.ToLower(name)
		duplicate := false
		for _, earlier := range seen[key] {
			if earlier.driverName != driverName {
				duplicate = true
				break
			}
		}
		if duplicate {
			ref := app.Refs["windows"]
			warnings = append(warnings, CommandWarning{
				Code:    "possible_duplicate",
				Message: fmt.Sprintf("%s reports the same display name %q as another package driver; both entries were kept", driverName, name),
				Driver:  driverName,
				Ref:     ref,
			})
		}
		seen[key] = append(seen[key], prior{driverName: driverName})
	}
	return warnings
}

func captureHasAnyVersion(apps []capturedApp) bool {
	for _, app := range apps {
		if app.Version != "" {
			return true
		}
	}
	return false
}

// cleanApp is the sanitized version of capturedApp without underscore-prefixed fields.
type cleanApp struct {
	ID      string            `json:"id"`
	Refs    map[string]string `json:"refs"`
	Driver  string            `json:"driver,omitempty"`
	Version string            `json:"version,omitempty"`
}

// captureManifestOutput is the manifest structure written to disk.
type captureManifestOutput struct {
	Version     int                         `json:"version"`
	Name        string                      `json:"name,omitempty"`
	Captured    string                      `json:"captured,omitempty"`
	Apps        interface{}                 `json:"apps"`
	HomeManager *manifest.HomeManagerConfig `json:"homeManager,omitempty"`
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

	// An explicit selection on a realizer host is authoritative. Resolve it
	// before the legacy realizer-first dispatch so --driver brew can skip Nix
	// entirely and unsupported selections cannot silently fall back to Nix.
	if len(flags.Drivers) > 0 && platformBackendsFor(captureGOOSFn()).RealizerName() != "" {
		emitter.EmitPhase("capture")
		selection, selectionErr := resolveRealizerCaptureSelection(flags, captureGOOSFn())
		if selectionErr != nil {
			return nil, selectionErr
		}

		var rz realizer.Realizer
		if selection.nix {
			var rerr error
			rz, rerr = newRealizerFn()
			if rerr != nil || rz == nil {
				return nil, envelope.NewError(
					envelope.ErrCaptureFailed,
					fmt.Sprintf("Explicit capture driver nix is unavailable: %v", rerr),
				)
			}
		}
		return runCaptureRealizerSelected(flags, rz, emitter, selection)
	}

	// --- 0. Realizer path (whole-set, e.g. Nix on linux/darwin) ---
	// On Windows newRealizerFn returns ErrNoRealizer and control falls through to
	// the winget capture path below, byte-identical to prior behavior.
	if rz, rerr := newRealizerFn(); rerr == nil {
		return runCaptureRealizer(flags, rz, emitter)
	}

	// --- 1. Emit phase event (first event per event-contract.md) ---
	emitter.EmitPhase("capture")

	// --- 2. Enumerate selected package-manager ledgers ---
	enumerated, warnings, enumErr := enumerateWindowsCapturePackages(flags)
	if enumErr != nil {
		return nil, enumErr
	}
	totalFound := len(enumerated)

	// Preserve Winget's empty-ledger guard. Other explicitly selected drivers
	// may legitimately enumerate an empty package ledger.
	if includesCaptureDriver(flags, "winget") && countCapturePackages(enumerated, "winget") == 0 && !flags.Discover {
		return nil, envelope.NewError(
			envelope.ErrCaptureFailed,
			"Winget returned no packages after retry. This usually means another winget operation is still running.",
		).WithRemediation("Wait a few seconds and try again. Run 'winget list' in a terminal to verify winget is working.")
	}

	// --- 3. Convert and filter package records ---
	var captured []capturedApp
	filteredRuntimes := 0
	filteredStore := 0
	skipped := 0
	for _, item := range enumerated {
		pkg := item.Package
		// Filter runtime packages unless --include-runtimes.
		if item.Driver == "winget" && !flags.IncludeRuntimes && snapshot.IsRuntimePackage(pkg.Ref) {
			filteredRuntimes++
			skipped++
			continue
		}

		// Filter store IDs unless --include-store-apps.
		if item.Driver == "winget" && !flags.IncludeStoreApps && snapshot.IsStoreID(pkg.Ref) {
			filteredStore++
			skipped++
			continue
		}
		app := capturedApp{
			ID: wingetIDToManifestID(pkg.Ref),
			Refs: map[string]string{
				"windows": pkg.Ref,
			},
			Name:   pkg.DisplayName,
			Source: item.Driver,
		}
		if item.Driver != "winget" {
			app.Driver = item.Driver
		}

		if flags.Pin {
			app.Version = pkg.Version
		}
		captured = append(captured, app)
	}
	assignDeterministicCaptureIDs(captured, nil)
	warnings = append(warnings, possibleDuplicateWarnings(captured)...)
	if flags.Pin && !captureHasAnyVersion(captured) && flags.Events != "jsonl" {
		fmt.Fprintln(os.Stderr, "Warning: --pin requested but installed-package enumeration exposed no versions; the manifest will be written without pins.")
	}

	// --- 4. Emit item events for each included app ---
	for _, app := range captured {
		ref := app.Refs["windows"]
		emitter.EmitItem(ref, app.Source, "captured", "", fmt.Sprintf("Captured %s", app.Name), app.Name)
	}

	// --- 5. If --update and --manifest: merge with existing manifest ---
	if flags.Update && flags.Manifest != "" {
		existingMf, loadErr := loadManifest(flags.Manifest)
		if loadErr != nil {
			return nil, loadErr
		}

		// Identity is the selected driver plus manager-specific ref. An omitted
		// app.driver is the legacy Winget default.
		existingRefs := make(map[string]bool)
		for _, app := range existingMf.Apps {
			if ref, ok := app.Refs["windows"]; ok {
				existingRefs[captureIdentity(app.Driver, ref)] = true
			}
		}
		versionMap := make(map[string]string, len(captured))
		for _, app := range captured {
			versionMap[captureIdentity(app.Driver, app.Refs["windows"])] = app.Version
		}

		// Convert existing apps to capturedApp format, preserving declared
		// driver and version through the merge (parity with the realizer merge
		// path — dropping them would silently blank declared state).
		var merged []capturedApp
		for _, app := range existingMf.Apps {
			ca := capturedApp{
				ID:      app.ID,
				Refs:    app.Refs,
				Driver:  app.Driver,
				Version: app.Version,
				Source:  effectiveCaptureDriver(app.Driver),
			}
			// Under --pin, refresh an existing app's version from the installed-
			// apps snapshot only when it exposes a non-empty version — absence of
			// evidence never blanks a declared pin.
			if flags.Pin {
				if ver := versionMap[captureIdentity(app.Driver, app.Refs["windows"])]; ver != "" {
					ca.Version = ver
				}
			}
			merged = append(merged, ca)
		}

		// Append newly discovered apps that aren't already present.
		usedIDs := make(map[string]bool, len(merged))
		for _, app := range merged {
			usedIDs[strings.ToLower(strings.TrimSpace(app.ID))] = true
		}
		for _, app := range captured {
			identity := captureIdentity(app.Driver, app.Refs["windows"])
			if !existingRefs[identity] {
				app.ID = deterministicCaptureID(app.ID, effectiveCaptureDriver(app.Driver), usedIDs)
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
				ID:      app.ID,
				Refs:    app.Refs,
				Driver:  app.Driver,
				Version: app.Version,
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

	// --- 9. Build appsIncluded with package-manager provenance ---
	appsIncluded := buildAppsIncluded(captured, nil)

	// --- 10. Module matching and optional bundle creation ---
	configModules := []CaptureModuleResult{}
	configModuleMap := map[string]string{}
	packageModuleMap := map[string][]string{}
	configsIncluded := []string{}
	configsSkipped := []string{}
	configsCaptureErrors := []string{}
	configSecretsExcluded := 0

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
						ID:     ca.ID,
						Refs:   ca.Refs,
						Driver: ca.Driver,
					})
				}

				matchedModules := modules.MatchModulesForApps(catalog, manifestApps)

				if len(matchedModules) > 0 {
					// Build configModuleMap from matched modules.
					configModuleMap = buildConfigModuleMap(matchedModules)
					packageModuleMap = buildPackageModuleMap(matchedModules)

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
					configModules, configsIncluded, configsSkipped, configsCaptureErrors, configSecretsExcluded =
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
		PackageModuleMap:     packageModuleMap,
		Warnings:             warnings,
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
			SensitiveExcludedCount: configSecretsExcluded,
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
			Source: effectiveCaptureDriver(ca.Driver),
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

// buildPackageModuleMap exposes driver-qualified package-to-module ownership.
// Values are slices because multiple matched modules may intentionally attach
// configuration to the same package identity.
func buildPackageModuleMap(matchedModules []*modules.Module) map[string][]string {
	result := make(map[string][]string)
	for _, mod := range matchedModules {
		for _, ref := range mod.Matches.Winget {
			key := "winget:" + ref
			result[key] = append(result[key], mod.ID)
		}
		for _, ref := range mod.Matches.Chocolatey {
			key := "chocolatey:" + ref
			result[key] = append(result[key], mod.ID)
		}
	}
	for key := range result {
		sort.Strings(result[key])
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
		} else if len(mod.Matches.Chocolatey) == 0 {
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
	sensitiveExcluded int,
) {
	captureErrors = existingErrors

	stagingDir, mkdirErr := os.MkdirTemp("", "endstate-capture-meta-")
	if mkdirErr != nil {
		captureErrors = append(captureErrors, fmt.Sprintf("failed to create staging dir: %v", mkdirErr))
		for _, mod := range matchedModules {
			dirName := moduleDirName(mod.ID)
			skipped = append(skipped, dirName)
			results = append(results, CaptureModuleResult{
				ID:             mod.ID,
				AppID:          dirName,
				DisplayName:    mod.DisplayName,
				WingetRefs:     safeStringSlice(mod.Matches.Winget),
				ChocolateyRefs: safeStringSlice(mod.Matches.Chocolatey),
				Paths:          []string{},
				Status:         "skipped",
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

		fileCollected, secretsN, collectErr := bundle.CollectConfigFiles(mod, stagingDir)
		sensitiveExcluded += secretsN
		if collectErr != nil {
			captureErrors = append(captureErrors, fmt.Sprintf("module %s: %v", mod.ID, collectErr))
			moduleErrors[dirName] = true
			moduleFileCounts[dirName] = 0
			moduleFilePaths[dirName] = []string{}
			continue
		}

		regCollected, regErr := bundle.CollectRegistryKeys(mod, stagingDir)
		if regErr != nil {
			captureErrors = append(captureErrors, fmt.Sprintf("module %s registry: %v", mod.ID, regErr))
			// Don't mark the whole module as errored — file collection may have succeeded.
		}

		regValuesCollected, regValErr := bundle.CollectRegistryValues(mod, stagingDir)
		if regValErr != nil {
			captureErrors = append(captureErrors, fmt.Sprintf("module %s registry values: %v", mod.ID, regValErr))
			// Don't mark the whole module as errored — other collection may have succeeded.
		}

		collected := append(fileCollected, regCollected...)
		collected = append(collected, regValuesCollected...)

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
			ID:             mod.ID,
			AppID:          dirName,
			DisplayName:    mod.DisplayName,
			WingetRefs:     safeStringSlice(mod.Matches.Winget),
			ChocolateyRefs: safeStringSlice(mod.Matches.Chocolatey),
			Paths:          paths,
			FilesCaptured:  moduleFileCounts[dirName],
			Status:         status,
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
