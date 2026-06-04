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
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/driver/brew"
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
	EnumerateInstalled() ([]brew.InstalledApp, error)
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
	driverName := r.Name()

	// --- 1. Emit phase event (first event per event-contract.md) ---
	emitter.EmitPhase("capture")

	// --- 2. Read the installed set ---
	cur, cerr := r.Current()
	if cerr != nil {
		if rerr, ok := cerr.(*realizer.Error); ok && isSystemic(rerr.Code) {
			return nil, realizerEnvelopeError(rerr)
		}
		// Non-systemic read issue: treat as empty (capture an empty set),
		// mirroring runVerifyRealizer.
	}

	// --- 3. Convert the set to captured apps (deterministic order) ---
	names := make([]string, 0, len(cur.Elements))
	for name := range cur.Elements {
		names = append(names, name)
	}
	sort.Strings(names)

	goos := captureGOOSFn()
	captured := make([]capturedApp, 0, len(names))
	capturedIDs := make(map[string]bool, len(names))
	for _, name := range names {
		el := cur.Elements[name]
		ref := el.Name // bare attr -> apply's ResolveInstallable expands against the pin
		ver := nix.StorePathVersion(el.Name, el.StorePaths)
		captured = append(captured, capturedApp{
			ID:      name,
			Refs:    map[string]string{goos: ref},
			Name:    name,
			Version: ver,
		})
		capturedIDs[name] = true
		emitter.EmitItem(ref, driverName, "captured", "", fmt.Sprintf("Captured %s", name), name)
	}

	// --- 3b. Brew capture lane (darwin-only) ---
	// Enumerate installed brew formulae + casks and emit them as driver:"brew"
	// apps (casks as cask: refs under the darwin key), deduped by ID against the
	// realizer-captured set (a colliding ID keeps the realizer entry). The brew
	// driver is resolved additively; a non-darwin host or an unavailable/non-
	// enumerating brew yields no brew apps (the realizer set stands unchanged).
	if captureGOOSFn() == "darwin" {
		if d, berr := newBrewDriverFn(); berr == nil {
			if be, ok := d.(brewEnumerator); ok {
				if brewApps, eerr := be.EnumerateInstalled(); eerr == nil {
					for _, ba := range brewApps {
						if capturedIDs[ba.Name] {
							continue // realizer already captured this ID; do not duplicate
						}
						captured = append(captured, capturedApp{
							ID:      ba.Name,
							Refs:    map[string]string{"darwin": ba.Ref},
							Driver:  "brew",
							Name:    ba.Name,
							Version: ba.Version,
						})
						capturedIDs[ba.Name] = true
						emitter.EmitItem(ba.Ref, "brew", "captured", "", fmt.Sprintf("Captured %s", ba.Name), ba.Name)
					}
				}
				// An enumeration error is best-effort: brew capture is skipped, the
				// realizer-captured set stands (package capture is unaffected).
			}
		}
	}

	// --- 4. If --update and --manifest: merge with existing manifest (host-keyed) ---
	if flags.Update && flags.Manifest != "" {
		existingMf, loadErr := loadManifest(flags.Manifest)
		if loadErr != nil {
			return nil, loadErr
		}
		existingRefs := make(map[string]bool)
		for _, app := range existingMf.Apps {
			if ref, ok := app.Refs[goos]; ok {
				existingRefs[ref] = true
			}
		}
		merged := make([]capturedApp, 0, len(existingMf.Apps)+len(captured))
		for _, app := range existingMf.Apps {
			// Preserve the existing app's driver (e.g. "brew") on re-read so a
			// previously-captured brew app round-trips through --update unchanged.
			merged = append(merged, capturedApp{ID: app.ID, Refs: app.Refs, Driver: app.Driver, Version: app.Version})
		}
		for _, app := range captured {
			if !existingRefs[app.Refs[goos]] {
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
		appsIncluded = append(appsIncluded, CaptureApp{Source: driverName, ID: ca.ID, Name: ca.Name})
	}

	// --- 10. Emit artifact and summary events ---
	emitter.EmitArtifact("capture", "manifest", absPath)
	emitter.EmitSummary("capture", included, included, 0, 0)

	// No config modules / bundle on the realizer path (packages only).
	return &CaptureResult{
		AppsIncluded:         appsIncluded,
		ConfigModules:        []CaptureModuleResult{},
		ConfigModuleMap:      map[string]string{},
		OutputPath:           absPath,
		OutputFormat:         "jsonc",
		ConfigsIncluded:      []string{},
		ConfigsSkipped:       []string{},
		ConfigsCaptureErrors: []string{},
		Sanitized:            flags.Sanitize,
		IsExample:            false,
		Counts: CaptureCountsFull{
			Included:   included,
			TotalFound: included,
		},
		Manifest: CaptureManifest{
			Name: manifestName,
			Path: absPath,
		},
	}, nil
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
