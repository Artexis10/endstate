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
	// Only limits the capture to a comma-separated selection. Bare tokens are
	// captured app IDs; tokens prefixed "apps." are config module IDs. Mirrors
	// apply --only so a selection reads the same on both sides.
	//
	// Under a selection, config modules attach only when a selected app matches
	// them BY PACKAGE REF, or when the module is named outright. A module that
	// merely has a path on this filesystem is not part of the selection — see
	// modules.MatchModulesForAppsSelective.
	Only string
}

// captureSelection is a parsed --only value for capture.
type captureSelection struct {
	appIDs    []string
	moduleIDs []string
}

// active reports whether a selection was supplied.
func (s captureSelection) active() bool {
	return len(s.appIDs) > 0 || len(s.moduleIDs) > 0
}

// parseCaptureOnly splits a --only value into app and module selections.
//
// Bare tokens are app IDs; "apps."-prefixed tokens are config module IDs. The
// prefix is the canonical module ID form throughout the catalog and in a
// manifest's configModules, and app IDs (produced by wingetIDToManifestID) never
// contain it, so the two namespaces cannot collide.
//
// Note the deliberate asymmetry with --restore-filter, which also accepts a bare
// short module ID: here bare always means app. Accepting bare module IDs would
// make "vscode" ambiguous between an app and a module.
func parseCaptureOnly(only string) captureSelection {
	var sel captureSelection
	for _, token := range parseOnlyIDs(only) {
		if strings.HasPrefix(token, "apps.") {
			sel.moduleIDs = append(sel.moduleIDs, token)
			continue
		}
		sel.appIDs = append(sel.appIDs, token)
	}
	return sel
}

// validateCaptureOnly parses and validates --only against the captured app set,
// returning the selection and the filtered apps. Mirrors validateOnly's posture:
// every rejection happens before anything is written.
func validateCaptureOnly(only string, captured []capturedApp) (captureSelection, []capturedApp, *envelope.Error) {
	if only == "" {
		return captureSelection{}, captured, nil
	}

	sel := parseCaptureOnly(only)

	if !sel.active() {
		return sel, nil, envelope.NewError(
			envelope.ErrManifestValidationError,
			"--only requires at least one id; the provided value is empty after normalisation").
			WithRemediation("Provide one or more comma-separated ids, e.g. --only git-git,apps.vscode.")
	}

	// A module-only selection yields a manifest with no apps, which collides with
	// the zero-apps capture failure and leaves nothing for module matching to work
	// from. Reject it explicitly rather than failing later with a worse message.
	if len(sel.appIDs) == 0 {
		return sel, nil, envelope.NewError(
			envelope.ErrManifestValidationError,
			"--only selected config modules but no apps; a capture must contain at least one app").
			WithRemediation("Add at least one app id, e.g. --only git-git,apps.vscode.")
	}

	allow := make(map[string]bool, len(sel.appIDs))
	for _, id := range sel.appIDs {
		allow[id] = true
	}
	matched := make(map[string]bool, len(sel.appIDs))
	var filtered []capturedApp
	for _, app := range captured {
		if allow[app.ID] {
			filtered = append(filtered, app)
			matched[app.ID] = true
		}
	}

	var unknown []string
	for _, id := range sel.appIDs {
		if !matched[id] {
			unknown = append(unknown, id)
		}
	}
	if len(unknown) > 0 {
		return sel, nil, envelope.NewError(
			envelope.ErrManifestValidationError,
			fmt.Sprintf("--only references app ids that were not detected on this machine: %s", strings.Join(unknown, ", "))).
			WithRemediation("Run capture without --only to see the detected app ids, then select from those.")
	}

	if len(filtered) == 0 {
		return sel, nil, envelope.NewError(
			envelope.ErrManifestValidationError,
			"--only produced an empty app selection; no apps would be captured").
			WithRemediation("Provide ids matching at least one detected app.")
	}

	return sel, filtered, nil
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
	BundleSchemaVersion  string                `json:"bundleSchemaVersion,omitempty"`
	ManifestVersion      int                   `json:"manifestVersion,omitempty"`
	CaptureWarnings      []string              `json:"captureWarnings"`
	ConfigCapture        *CaptureConfigSummary `json:"configCapture,omitempty"`

	// Manifest identifies the manifest that was produced (kept for tooling
	// and test compatibility).
	Manifest CaptureManifest `json:"manifest"`
}

// CaptureApp is a single entry in AppsIncluded.
type CaptureApp struct {
	Source string `json:"source"`
	Name   string `json:"name,omitempty"`
	// ID is the package reference (e.g. "Git.Git"), kept as-is because clients
	// match evidence on it.
	ID string `json:"id"`
	// ManifestID is the app's id in the written manifest (e.g. "git-git") — the
	// token `--only` and `apply --only` match. It differs from ID, so without it
	// a client cannot turn capture output into a selection: nothing in the
	// envelope showed the value the user has to pass.
	ManifestID string `json:"manifestId,omitempty"`
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

// matchModulesForAppsFn is the narrow matching boundary used after capture.
// Tests replace it to inspect the runtime evidence supplied to module matching.
var matchModulesForAppsFn = modules.MatchModulesForApps

// capturedApp is an internal representation of a captured application entry
// before it is written to the output manifest.
type capturedApp struct {
	ID               string            `json:"id"`
	Refs             map[string]string `json:"refs"`
	Driver           string            `json:"driver,omitempty"`
	Version          string            `json:"version,omitempty"`
	Name             string            `json:"_name,omitempty"`
	Installed        bool              `json:"-"`
	InstalledVersion string            `json:"-"`
	Backend          string            `json:"-"`
	Source           string            `json:"-"`
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
			evidence[wingetEvidenceKey(app.ID)] = app
		}
	}
	packages := make([]driver.InstalledPackage, 0, len(exported.apps))
	for _, app := range exported.apps {
		listedApp := evidence[wingetEvidenceKey(app.ID)]
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
	driverName = effectiveCaptureDriver(driverName)
	ref = strings.TrimSpace(ref)
	if driverName == "winget" || driverName == "chocolatey" {
		ref = strings.ToLower(ref)
	}
	return driverName + "\x00" + ref
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
			Name:             pkg.DisplayName,
			Installed:        true,
			InstalledVersion: pkg.Version,
			Backend:          item.Driver,
			Source:           item.Driver,
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

	// Apply --only here, after IDs are final and before everything downstream.
	//
	// After ID assignment because the dedup suffixing above produces the IDs a
	// user actually sees and selects by. Before the --update merge below because
	// "capture --only git-git --update" must ADD git-git to an existing manifest,
	// not truncate that manifest to git-git — filtering the newly discovered set
	// pre-merge gives the former, post-merge would silently destroy the rest.
	//
	// One insertion point then scopes duplicate warnings, pin warnings, item
	// events, counts, the manifest write, and module matching alike.
	totalDetected := len(captured)
	selection, selectedApps, onlyErr := validateCaptureOnly(flags.Only, captured)
	if onlyErr != nil {
		return nil, onlyErr
	}
	captured = selectedApps
	// Deselected apps count as skipped so totalFound == included + skipped still
	// holds. totalFound stays pre-filter: it reports what is on the machine, which
	// is what makes the subset visible as a subset.
	skipped += totalDetected - len(captured)

	warnings = append(warnings, possibleDuplicateWarnings(captured)...)
	if flags.Pin && !captureHasAnyVersion(captured) && flags.Events != "jsonl" {
		fmt.Fprintln(os.Stderr, "Warning: --pin requested but installed-package enumeration exposed no versions; the manifest will be written without pins.")
	}

	// --- 4. Emit item events for each included app ---
	for _, app := range captured {
		ref := app.Refs["windows"]
		emitter.EmitItem(ref, app.Source, "present", "detected", fmt.Sprintf("Captured %s", app.Name), app.Name)
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
		currentlyDetected := make(map[string]capturedApp, len(captured))
		for _, app := range captured {
			if ref := app.Refs["windows"]; ref != "" {
				currentlyDetected[captureIdentity(app.Driver, ref)] = app
			}
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
			// Desired-only entries remain serialized but carry no installed
			// evidence. A current export match supplies runtime evidence and,
			// under --pin, may refresh the declared pin without blanking it.
			if detected, ok := currentlyDetected[captureIdentity(app.Driver, app.Refs["windows"])]; ok {
				ca.Name = detected.Name
				ca.Installed = detected.Installed
				ca.InstalledVersion = detected.InstalledVersion
				ca.Backend = detected.Backend
				ca.Source = detected.Source
				if flags.Pin && detected.InstalledVersion != "" {
					ca.Version = detected.InstalledVersion
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

	// --- 10. Plan config capture and publish one canonical artifact ---
	finalization, finalizeErr := finalizeCaptureConfig(captureConfigFinalizeRequest{
		Flags: flags, ManifestPath: absPath,
		Apps:      buildModuleMatchApps(captured),
		Selection: selection,
	})
	if finalizeErr != nil {
		if selErr, ok := asCaptureSelectionError(finalizeErr); ok {
			return nil, selErr
		}
		return nil, envelope.NewError(
			envelope.ErrCaptureFailed,
			fmt.Sprintf("Failed to create capture bundle: %v", finalizeErr),
		)
	}

	if finalization.CatalogUnavailable {
		warnings = append(warnings, captureCatalogUnavailableWarning())
	}

	// --- 11. Emit artifact and summary events ---
	emitter.EmitArtifact("capture", "manifest", finalization.OutputPath)
	emitter.EmitSummary("capture", totalFound, included, skipped, 0)

	return &CaptureResult{
		AppsIncluded:         appsIncluded,
		ConfigModules:        finalization.ConfigModules,
		ConfigModuleMap:      finalization.ConfigModuleMap,
		PackageModuleMap:     finalization.PackageModuleMap,
		Warnings:             warnings,
		OutputPath:           finalization.OutputPath,
		OutputFormat:         finalization.OutputFormat,
		ConfigsIncluded:      finalization.ConfigsIncluded,
		ConfigsSkipped:       finalization.ConfigsSkipped,
		ConfigsCaptureErrors: finalization.ConfigsCaptureErrors,
		Sanitized:            flags.Sanitize,
		IsExample:            false,
		Counts: CaptureCountsFull{
			FilteredRuntimes:       filteredRuntimes,
			Included:               included,
			TotalFound:             totalFound,
			SensitiveExcludedCount: finalization.SensitiveExcluded,
			FilteredStoreApps:      filteredStore,
			Skipped:                skipped,
		},
		Manifest: CaptureManifest{
			Name: manifestName,
			Path: finalization.OutputPath,
		},
		BundleSchemaVersion: generationBundleSchemaVersion(finalization),
		ManifestVersion:     generationManifestVersion(finalization),
		CaptureWarnings:     finalization.CaptureWarnings,
		ConfigCapture:       captureConfigResultSummary(finalization),
	}, nil
}

// buildModuleMatchApps keeps desired manifest pins separate from runtime
// installed-version evidence. Matchers see the current detected version and
// backend, while desired-only update entries remain explicitly not installed.
func buildModuleMatchApps(apps []capturedApp) []manifest.App {
	result := make([]manifest.App, 0, len(apps))
	for _, app := range apps {
		driver := app.Driver
		installedVersion := ""
		if app.Installed {
			installedVersion = app.InstalledVersion
			if driver == "" {
				driver = app.Backend
			}
		}
		result = append(result, manifest.App{
			ID:               app.ID,
			Refs:             app.Refs,
			Driver:           driver,
			Version:          installedVersion,
			Installed:        app.Installed,
			InstalledVersion: installedVersion,
			Backend:          app.Backend,
			DisplayName:      app.Name,
		})
	}
	return result
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
			Source:     ca.Source,
			ID:         wingetID,
			ManifestID: ca.ID,
		}
		if entry.Source == "" {
			entry.Source = ca.Backend
		}
		if entry.Source == "" {
			entry.Source = effectiveCaptureDriver(ca.Driver)
		}
		name := displayNameMap[wingetEvidenceKey(wingetID)]
		if name == "" {
			// Keep this helper compatible with direct callers that supply an
			// exact-cased map rather than capture's normalized evidence map.
			name = displayNameMap[wingetID]
		}
		if name != "" {
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
	chocolateyOwners := make(map[string]map[string]struct{})
	for _, mod := range matchedModules {
		for _, ref := range mod.Matches.Winget {
			key := "winget:" + ref
			result[key] = append(result[key], mod.ID)
		}
		for _, ref := range mod.Matches.Chocolatey {
			key := "chocolatey:" + strings.ToLower(strings.TrimSpace(ref))
			if chocolateyOwners[key] == nil {
				chocolateyOwners[key] = make(map[string]struct{})
			}
			chocolateyOwners[key][mod.ID] = struct{}{}
		}
	}
	for key, moduleIDs := range chocolateyOwners {
		for moduleID := range moduleIDs {
			result[key] = append(result[key], moduleID)
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

// wingetEvidenceKey normalizes only identity joins. Callers keep the original
// package ref for output and evidence because Winget identity is
// case-insensitive even though its display casing is useful provenance.
func wingetEvidenceKey(ref string) string {
	return strings.ToLower(strings.TrimSpace(ref))
}
