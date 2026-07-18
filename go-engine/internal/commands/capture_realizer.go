// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/bundle"
	"github.com/Artexis10/endstate/go-engine/internal/driver"
	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/events"
	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/provision"
	"github.com/Artexis10/endstate/go-engine/internal/realizer"
	"github.com/Artexis10/endstate/go-engine/internal/realizer/nix"
)

// listGenerationsFn reads the provisioning generations (newest-first). It
// defaults to provision.List and is replaced in tests to make home-manager
// flake recovery hermetic.
var listGenerationsFn = provision.List

// captureGOOSFn reports the host OS for the capture lane. It defaults to
// runtime.GOOS and is replaced in tests so the brew capture lane (darwin-only)
// can be exercised on any host.
var captureGOOSFn = func() string { return runtime.GOOS }

// brewEnumerator is the capture-lane capability the brew driver implements
// (EnumerateInstalled). The realizer capture path type-asserts the resolved
// brew driver to this interface; a driver that does not implement it (or an
// unavailable brew) yields no brew apps.
type brewEnumerator interface {
	EnumerateInstalled() ([]driver.InstalledPackage, error)
}

type realizerCaptureSelection struct {
	nix      bool
	brew     bool
	explicit bool
}

func resolveRealizerCaptureSelection(flags CaptureFlags, goos string) (realizerCaptureSelection, *envelope.Error) {
	if len(flags.Drivers) == 0 {
		return realizerCaptureSelection{nix: true, brew: goos == "darwin"}, nil
	}

	supported := make(map[string]bool)
	for _, name := range platformBackendsFor(goos).SupportedNames() {
		supported[name] = true
	}
	selection := realizerCaptureSelection{explicit: true}
	for _, requested := range flags.Drivers {
		name := strings.ToLower(strings.TrimSpace(requested))
		if name == "" {
			return realizerCaptureSelection{}, envelope.NewError(
				envelope.ErrCaptureFailed,
				"Capture driver name must not be empty.",
			)
		}
		if !supported[name] {
			return realizerCaptureSelection{}, envelope.NewError(
				envelope.ErrCaptureFailed,
				fmt.Sprintf("Capture driver %s is not supported on %s.", name, goos),
			)
		}
		switch name {
		case "nix":
			selection.nix = true
		case "brew":
			selection.brew = true
		}
	}
	return selection, nil
}

// runCaptureRealizer is the capture path for a realizer backend (Nix on
// linux/darwin). It reads the installed set via Current() and emits each element
// as a manifest app keyed by the host OS, using the same manifest shape, output-
// path resolution, and write/verify invariants as the winget path.
//
// It is package-scoped: no config modules, manual apps, or config bundle are
// synthesized (those are the winget app catalog). The emitted ref is the
// element's bare attr (its Name) — NOT its AttrPath — so apply's
// ResolveInstallable expands it against the pin and the manifest stays portable
// across realizer hosts (no system tuple baked in). A capture therefore
// round-trips: apply of the produced manifest re-installs the same set.
func runCaptureRealizer(flags CaptureFlags, r realizer.Realizer, emitter *events.Emitter) (interface{}, *envelope.Error) {
	// --- 1. Emit phase event (first event per event-contract.md) ---
	emitter.EmitPhase("capture")
	selection, selectionErr := resolveRealizerCaptureSelection(flags, captureGOOSFn())
	if selectionErr != nil {
		return nil, selectionErr
	}
	return runCaptureRealizerSelected(flags, r, emitter, selection)
}

func runCaptureRealizerSelected(flags CaptureFlags, r realizer.Realizer, emitter *events.Emitter, selection realizerCaptureSelection) (interface{}, *envelope.Error) {
	driverName := platformBackendsFor(captureGOOSFn()).RealizerName()
	if driverName == "" {
		driverName = "nix"
	}
	if r != nil {
		driverName = r.Name()
	}

	// --- 2. Read the installed set ---
	emitter.EmitProgress("capture", "inventory")
	cur := realizer.Set{Elements: map[string]realizer.Element{}}
	if selection.nix {
		if r == nil {
			return nil, envelope.NewError(envelope.ErrCaptureFailed, "Explicit capture driver nix is unavailable.")
		}
		var cerr error
		cur, cerr = r.Current()
		if cerr != nil {
			if rerr, ok := cerr.(*realizer.Error); ok && isSystemic(rerr.Code) {
				return nil, realizerEnvelopeError(rerr)
			}
			// Non-systemic read issue: treat as empty (capture an empty set),
			// mirroring runVerifyRealizer.
		}
	}

	// --- 3. Convert the set to captured apps (deterministic order) ---
	names := make([]string, 0, len(cur.Elements))
	for name := range cur.Elements {
		names = append(names, name)
	}
	sort.Strings(names)

	goos := captureGOOSFn()
	captured := make([]capturedApp, 0, len(names))
	for _, name := range names {
		el := cur.Elements[name]
		ref := el.Name // bare attr -> apply's ResolveInstallable expands against the pin
		ver := nix.StorePathVersion(el.Name, el.StorePaths)
		captured = append(captured, capturedApp{
			ID:               name,
			Refs:             map[string]string{goos: ref},
			Name:             name,
			Version:          ver,
			Installed:        true,
			InstalledVersion: ver,
			Backend:          driverName,
		})
		emitter.EmitItem(ref, driverName, "present", "detected", fmt.Sprintf("Detected %s", name), name)
	}

	// --- 3b. Brew capture lane (darwin-only) ---
	// Enumerate installed brew formulae + casks and emit them as driver:"brew"
	// apps (casks as cask: refs under the darwin key). Package identity never
	// crosses manager boundaries; colliding manifest IDs get stable suffixes. The brew
	// driver is resolved additively; a non-darwin host or an unavailable/non-
	// enumerating brew yields no brew apps (the realizer set stands unchanged).
	if selection.brew && captureGOOSFn() == "darwin" {
		if d, berr := newBrewDriverFn(); berr == nil {
			if be, ok := d.(brewEnumerator); ok {
				brewApps, eerr := be.EnumerateInstalled()
				if eerr == nil {
					usedIDs := make(map[string]bool, len(captured)+len(brewApps))
					for _, app := range captured {
						usedIDs[strings.ToLower(app.ID)] = true
					}
					for _, ba := range brewApps {
						name := ba.DisplayName
						if name == "" {
							name = strings.TrimPrefix(ba.Ref, "cask:")
						}
						captured = append(captured, capturedApp{
							ID:               deterministicCaptureID(name, "brew", usedIDs),
							Refs:             map[string]string{"darwin": ba.Ref},
							Driver:           "brew",
							Name:             name,
							Version:          ba.Version,
							Installed:        true,
							InstalledVersion: ba.Version,
							Backend:          "brew",
						})
						emitter.EmitItem(ba.Ref, "brew", "present", "detected", fmt.Sprintf("Detected %s", name), name)
					}
				}
				if eerr != nil && selection.explicit {
					return nil, envelope.NewError(
						envelope.ErrCaptureFailed,
						fmt.Sprintf("Failed to enumerate installed packages with brew: %v", eerr),
					)
				}
				// An enumeration error is best-effort: brew capture is skipped, the
				// realizer-captured set stands (package capture is unaffected).
			} else if selection.explicit {
				return nil, envelope.NewError(envelope.ErrCaptureFailed, "Explicit capture driver brew does not support installed-package enumeration.")
			}
		} else if selection.explicit {
			return nil, envelope.NewError(
				envelope.ErrCaptureFailed,
				fmt.Sprintf("Explicit capture driver brew is unavailable: %v", berr),
			)
		}
	}

	// Apply --only after BOTH lanes have contributed (realizer above, brew just
	// now) and before the --update merge, matching the Windows path: a selection
	// narrows what this run discovered, it never truncates an existing manifest.
	appSelection, selectedApps, onlyErr := validateCaptureOnly(flags.Only, captured)
	if onlyErr != nil {
		return nil, onlyErr
	}
	captured = selectedApps

	// --- 4. If --update and --manifest: merge with existing manifest (host-keyed) ---
	if flags.Update && flags.Manifest != "" {
		existingMf, loadErr := loadManifest(flags.Manifest)
		if loadErr != nil {
			return nil, loadErr
		}
		// KNOWN PRE-EXISTING LIMITATION (LOW): --update dedups by host ref, not by
		// app id. An app whose id is unchanged but whose host ref changed is treated
		// as a new entry (and a renamed app keeps the old entry). This predates the
		// brew two-lane work and is intentionally left unchanged here; revisit if
		// ref churn causes duplicate/stale merged entries.
		existingRefs := make(map[string]bool)
		for _, app := range existingMf.Apps {
			if ref, ok := app.Refs[goos]; ok {
				existingRefs[realizerCaptureIdentity(app.Driver, ref, driverName)] = true
			}
		}
		currentlyDetected := make(map[string]capturedApp, len(captured))
		for _, app := range captured {
			if ref := app.Refs[goos]; ref != "" {
				currentlyDetected[realizerCaptureIdentity(app.Driver, ref, driverName)] = app
			}
		}
		merged := make([]capturedApp, 0, len(existingMf.Apps)+len(captured))
		usedIDs := make(map[string]bool, len(existingMf.Apps)+len(captured))
		for _, app := range existingMf.Apps {
			// Preserve the existing app's driver (e.g. "brew") on re-read so a
			// previously-captured brew app round-trips through --update unchanged,
			// while attaching current installed evidence only when the backend
			// actually enumerated the same host ref in this run.
			mergedApp := capturedApp{ID: app.ID, Refs: app.Refs, Driver: app.Driver, Version: app.Version, Source: app.Source}
			identity := realizerCaptureIdentity(app.Driver, app.Refs[goos], driverName)
			if detected, ok := currentlyDetected[identity]; ok {
				mergedApp.Name = detected.Name
				mergedApp.Installed = detected.Installed
				mergedApp.InstalledVersion = detected.InstalledVersion
				mergedApp.Backend = detected.Backend
				mergedApp.Source = detected.Source
			}
			merged = append(merged, mergedApp)
			usedIDs[strings.ToLower(strings.TrimSpace(app.ID))] = true
		}
		for _, app := range captured {
			if !existingRefs[realizerCaptureIdentity(app.Driver, app.Refs[goos], driverName)] {
				collisionDriver := strings.TrimSpace(app.Driver)
				if collisionDriver == "" {
					collisionDriver = strings.TrimSpace(app.Source)
				}
				if collisionDriver == "" {
					collisionDriver = driverName
				}
				app.ID = deterministicCaptureID(app.ID, collisionDriver, usedIDs)
				merged = append(merged, app)
			}
		}
		captured = merged
	}

	included := len(captured)

	// --- 5. Sanitize / sort by id ---
	var outputApps interface{}
	if flags.Sanitize {
		sorted := make([]cleanApp, len(captured))
		for i, app := range captured {
			sorted[i] = cleanApp{ID: app.ID, Refs: app.Refs, Driver: app.Driver, Version: app.Version}
		}
		sort.Slice(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })
		outputApps = sorted
	} else {
		sort.Slice(captured, func(i, j int) bool { return captured[i].ID < captured[j].ID })
		outputApps = captured
	}

	// --- 6. Determine output path and manifest name ---
	outputPath := resolveOutputPath(flags)
	manifestName := "captured"
	if flags.Name != "" {
		manifestName = flags.Name
	} else if flags.Profile != "" {
		manifestName = flags.Profile
	}

	outManifest := captureManifestOutput{
		Version:     1,
		Name:        manifestName,
		Captured:    time.Now().UTC().Format(time.RFC3339),
		Apps:        outputApps,
		HomeManager: recoverHomeManager(flags),
	}

	// --- 7. Write manifest as pretty-printed JSON (same as the winget path) ---
	data, marshalErr := json.MarshalIndent(outManifest, "", "  ")
	if marshalErr != nil {
		return nil, envelope.NewError(
			envelope.ErrCaptureFailed,
			fmt.Sprintf("Failed to marshal manifest: %v", marshalErr),
		)
	}
	if dir := filepath.Dir(outputPath); dir != "" && dir != "." {
		if mkdirErr := os.MkdirAll(dir, 0755); mkdirErr != nil {
			return nil, envelope.NewError(
				envelope.ErrManifestWriteFailed,
				fmt.Sprintf("Failed to create output directory: %v", mkdirErr),
			).WithRemediation("Check directory permissions and ensure the path is writable.")
		}
	}
	if flags.Sanitize {
		emitter.EmitProgress("capture", "packaging")
	}
	if writeErr := os.WriteFile(outputPath, data, 0644); writeErr != nil {
		return nil, envelope.NewError(
			envelope.ErrManifestWriteFailed,
			fmt.Sprintf("Failed to write manifest file: %v", writeErr),
		).WithRemediation("Check directory permissions and ensure the path is writable.")
	}

	// --- 8. INV-CAPTURE-2: verify file exists and is non-empty ---
	if fileInfo, statErr := os.Stat(outputPath); statErr != nil || fileInfo.Size() == 0 {
		return nil, envelope.NewError(
			envelope.ErrManifestWriteFailed,
			"Manifest file is empty or does not exist after write.",
		).WithRemediation("Check disk space and directory permissions.")
	}

	absPath, absErr := filepath.Abs(outputPath)
	if absErr != nil {
		absPath = outputPath
	}

	// --- 9. Build appsIncluded (package-scoped; source is the backend name) ---
	appsIncluded := make([]CaptureApp, 0, len(captured))
	for _, ca := range captured {
		source := ca.Source
		if source == "" {
			source = ca.Backend
		}
		if source == "" {
			source = ca.Driver
		}
		if source == "" {
			source = driverName
		}
		// The realizer path's ref and manifest id coincide, but both are emitted so
		// clients can read manifestId uniformly across capture paths.
		appsIncluded = append(appsIncluded, CaptureApp{Source: source, ID: ca.ID, ManifestID: ca.ID, Name: ca.Name})
	}

	// --- 10. Plan config capture and publish one canonical artifact ---
	finalization, finalizeErr := finalizeCaptureConfig(captureConfigFinalizeRequest{
		Flags: flags, ManifestPath: absPath,
		Apps:      buildModuleMatchApps(captured),
		Selection: appSelection,
		OnStage: func(stage bundle.Stage) {
			emitter.EmitProgress("capture", string(stage))
		},
	})
	if finalizeErr != nil {
		return nil, envelope.NewError(
			envelope.ErrCaptureFailed,
			fmt.Sprintf("Failed to create capture bundle: %v", finalizeErr),
		)
	}

	// The realizer path collects no other CommandWarnings, so this is the whole
	// set rather than an append.
	var warnings []CommandWarning
	if finalization.CatalogUnavailable {
		warnings = append(warnings, captureCatalogUnavailableWarning())
	}

	// --- 11. Emit artifact and summary events ---
	emitter.EmitArtifact("capture", "manifest", finalization.OutputPath)
	emitter.EmitSummary("capture", included, included, 0, 0)

	return &CaptureResult{
		AppsIncluded:         appsIncluded,
		ConfigModules:        finalization.ConfigModules,
		ConfigModuleMap:      finalization.ConfigModuleMap,
		PackageModuleMap:     finalization.PackageModuleMap,
		OutputPath:           finalization.OutputPath,
		OutputFormat:         finalization.OutputFormat,
		ConfigsIncluded:      finalization.ConfigsIncluded,
		ConfigsSkipped:       finalization.ConfigsSkipped,
		ConfigsCaptureErrors: finalization.ConfigsCaptureErrors,
		Sanitized:            flags.Sanitize,
		IsExample:            false,
		Counts: CaptureCountsFull{
			Included:   included,
			TotalFound: included,
		},
		Manifest: CaptureManifest{
			Name: manifestName,
			Path: finalization.OutputPath,
		},
		BundleSchemaVersion: generationBundleSchemaVersion(finalization),
		ManifestVersion:     generationManifestVersion(finalization),
		CaptureWarnings:     finalization.CaptureWarnings,
		ConfigCapture:       captureConfigResultSummary(finalization),
		Warnings:            warnings,
	}, nil
}

func realizerCaptureIdentity(driverName, ref, defaultDriver string) string {
	driverName = strings.ToLower(strings.TrimSpace(driverName))
	if driverName == "" {
		driverName = strings.ToLower(defaultDriver)
	}
	return driverName + "\x00" + strings.TrimSpace(ref)
}

// recoverHomeManager returns the home-manager configuration to record in the
// captured manifest, or nil if none is known. The flake is recovered from the
// engine's own provisioning history (home-manager does not persist the source
// flakeref in a live install): the most-recent generation whose HomeManager is
// set — provision.List is newest-first, and a later package-only apply records
// HomeManager=nil, so the first match is the config still in effect. The read is
// best-effort: an error or empty history simply yields nil (the field is
// omitted; package capture is unaffected). On --update with no flake in history,
// an existing manifest's homeManager block is preserved rather than dropped.
func recoverHomeManager(flags CaptureFlags) *manifest.HomeManagerConfig {
	if gens, err := listGenerationsFn(); err == nil {
		for _, g := range gens {
			if g.HomeManager == nil {
				continue
			}
			// Prefer the user's declared catalog settings, then the declared config
			// path (a config/settings apply records the machine-local generated flake
			// in Flake but the portable input in Settings/Config); fall back to a
			// directly-declared flake. Secrets compose with the generated modes, so
			// their REFERENCES (path/env/backend — never material) ride along; capture
			// is reference-only by construction (HomeGenRef.Secrets holds no material).
			if g.HomeManager.Settings != nil {
				return &manifest.HomeManagerConfig{Settings: g.HomeManager.Settings, Secrets: g.HomeManager.Secrets}
			}
			if g.HomeManager.Config != "" {
				return &manifest.HomeManagerConfig{Config: g.HomeManager.Config, Secrets: g.HomeManager.Secrets}
			}
			if g.HomeManager.Flake != "" {
				return &manifest.HomeManagerConfig{Flake: g.HomeManager.Flake}
			}
		}
	}
	if flags.Update && flags.Manifest != "" {
		if mf, loadErr := loadManifest(flags.Manifest); loadErr == nil && mf.HomeManager != nil {
			return mf.HomeManager
		}
	}
	return nil
}
