// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/driver"
	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/realizer"
	"github.com/Artexis10/endstate/go-engine/internal/realizer/nix"
)

type configRestoreEvidenceSourceFunc func(context.Context, configRestoreDetectionRequest) (configRestoreDetectionEvidence, error)

func (function configRestoreEvidenceSourceFunc) Snapshot(
	ctx context.Context,
	request configRestoreDetectionRequest,
) (configRestoreDetectionEvidence, error) {
	return function(ctx, request)
}

// newStandaloneConfigRestoreEvidenceSource follows the same host-backend
// precedence as apply: a whole-set realizer is authoritative when available,
// with Homebrew queried alongside it, then the platform driver is used as the
// fallback. Filesystem-only detection is the honest last resort.
func newStandaloneConfigRestoreEvidenceSource(manifestApps []manifest.App) configRestoreEvidenceSource {
	if backend, err := newRealizerFn(); err == nil && backend != nil {
		var brewBackend driver.Driver
		if candidate, brewErr := newBrewDriverFn(); brewErr == nil {
			brewBackend = candidate
		}
		return newRealizerConfigRestoreEvidenceSource(backend, brewBackend, manifestApps)
	}
	if lanes, _, err := resolvePackageDriverLanes(manifestApps); err == nil && len(lanes) > 0 {
		return newDriverLaneConfigRestoreEvidenceSource(lanes)
	}
	if backend, err := newDriverFn(); err == nil && backend != nil {
		return newDriverConfigRestoreEvidenceSource(backend, manifestApps)
	}
	return newFilesystemConfigRestoreEvidenceSource()
}

func newDriverConfigRestoreEvidenceSource(
	backend driver.Driver,
	manifestApps []manifest.App,
) configRestoreEvidenceSource {
	apps := cloneConfigRestoreEvidenceApps(manifestApps)
	return configRestoreEvidenceSourceFunc(func(
		ctx context.Context,
		request configRestoreDetectionRequest,
	) (configRestoreDetectionEvidence, error) {
		if err := ctx.Err(); err != nil {
			return configRestoreDetectionEvidence{}, err
		}
		packagesByModule := make(map[string][]modules.PackageEvidence, len(request.Modules))
		failedModules := make(map[string]struct{})
		moduleIDs := make([]string, 0, len(request.Modules))
		for moduleID := range request.Modules {
			moduleIDs = append(moduleIDs, moduleID)
		}
		sort.Strings(moduleIDs)
		for _, moduleID := range moduleIDs {
			module := request.Modules[moduleID]
			current, err := detectDriverConfigRestoreApps(backend, configRestoreEvidenceAppsForModule(module, apps))
			if err != nil {
				packagesByModule[moduleID] = []modules.PackageEvidence{}
				failedModules[moduleID] = struct{}{}
				continue
			}
			packagesByModule[moduleID] = capturePackageEvidence(module, current)
		}
		return configRestoreDetectionEvidence{
			PackagesByModule: packagesByModule, FailedModules: failedModules, Glob: filepath.Glob,
		}, nil
	})
}

// newDriverLaneConfigRestoreEvidenceSource combines authoritative package
// driver lanes without allowing one lane to stand in for another. Each
// Snapshot call re-queries every available lane and isolates a lane failure to
// modules that actually declare ownership of an app routed through that lane.
func newDriverLaneConfigRestoreEvidenceSource(
	lanes []packageDriverLane,
) configRestoreEvidenceSource {
	type laneSource struct {
		name   string
		source configRestoreEvidenceSource
		apps   []manifest.App
		err    error
	}
	sources := make([]laneSource, 0, len(lanes))
	for _, lane := range lanes {
		laneApps := make([]manifest.App, 0, len(lane.apps))
		for _, routed := range lane.apps {
			if routed != nil {
				laneApps = append(laneApps, routed.app)
			}
		}
		entry := laneSource{name: lane.name, apps: cloneConfigRestoreEvidenceApps(laneApps), err: lane.err}
		if lane.drv != nil && lane.err == nil {
			entry.source = newDriverConfigRestoreEvidenceSource(lane.drv, laneApps)
		} else if entry.err == nil {
			entry.err = errors.New("package driver is unavailable")
		}
		sources = append(sources, entry)
	}

	return configRestoreEvidenceSourceFunc(func(
		ctx context.Context,
		request configRestoreDetectionRequest,
	) (configRestoreDetectionEvidence, error) {
		if err := ctx.Err(); err != nil {
			return configRestoreDetectionEvidence{}, err
		}
		combined := configRestoreDetectionEvidence{
			PackagesByModule: make(map[string][]modules.PackageEvidence, len(request.Modules)),
			FailedModules:    make(map[string]struct{}),
			Glob:             filepath.Glob,
		}
		for moduleID := range request.Modules {
			combined.PackagesByModule[moduleID] = []modules.PackageEvidence{}
		}
		for _, lane := range sources {
			if lane.source == nil {
				for moduleID, module := range request.Modules {
					if len(configRestoreEvidenceAppsForModule(module, lane.apps)) > 0 {
						combined.FailedModules[moduleID] = struct{}{}
					}
				}
				continue
			}
			evidence, err := lane.source.Snapshot(ctx, request)
			if err != nil {
				return configRestoreDetectionEvidence{}, fmt.Errorf("%s package detection: %w", lane.name, err)
			}
			for moduleID, packages := range evidence.PackagesByModule {
				combined.PackagesByModule[moduleID] = append(combined.PackagesByModule[moduleID], packages...)
			}
			for moduleID := range evidence.FailedModules {
				combined.FailedModules[moduleID] = struct{}{}
			}
		}
		for moduleID := range combined.PackagesByModule {
			sort.Slice(combined.PackagesByModule[moduleID], func(left, right int) bool {
				leftEvidence := combined.PackagesByModule[moduleID][left]
				rightEvidence := combined.PackagesByModule[moduleID][right]
				leftKey := leftEvidence.Backend + "\x00" + leftEvidence.Ref + "\x00" + leftEvidence.AppID
				rightKey := rightEvidence.Backend + "\x00" + rightEvidence.Ref + "\x00" + rightEvidence.AppID
				return leftKey < rightKey
			})
		}
		return combined, nil
	})
}

func configRestoreEvidenceAppsForModule(module *modules.Module, apps []manifest.App) []manifest.App {
	selected := make([]manifest.App, 0, len(apps))
	for _, app := range apps {
		if _, _, matched := matchedPackageRef(module, app); matched {
			selected = append(selected, app)
		}
	}
	return selected
}

func newFilesystemConfigRestoreEvidenceSource() configRestoreEvidenceSource {
	return configRestoreEvidenceSourceFunc(func(
		ctx context.Context,
		request configRestoreDetectionRequest,
	) (configRestoreDetectionEvidence, error) {
		if err := ctx.Err(); err != nil {
			return configRestoreDetectionEvidence{}, err
		}
		packagesByModule := make(map[string][]modules.PackageEvidence, len(request.Modules))
		for moduleID := range request.Modules {
			packagesByModule[moduleID] = []modules.PackageEvidence{}
		}
		return configRestoreDetectionEvidence{PackagesByModule: packagesByModule, Glob: filepath.Glob}, nil
	})
}

func newRealizerConfigRestoreEvidenceSource(
	backend realizer.Realizer,
	brewBackend driver.Driver,
	manifestApps []manifest.App,
) configRestoreEvidenceSource {
	apps := cloneConfigRestoreEvidenceApps(manifestApps)
	return configRestoreEvidenceSourceFunc(func(
		ctx context.Context,
		request configRestoreDetectionRequest,
	) (configRestoreDetectionEvidence, error) {
		if err := ctx.Err(); err != nil {
			return configRestoreDetectionEvidence{}, err
		}
		current, currentErr := backend.Current()
		detected := cloneConfigRestoreEvidenceApps(apps)
		failedApps := []manifest.App{}
		for index := range detected {
			app := &detected[index]
			app.Installed = false
			app.InstalledVersion = ""
			ref := app.Refs[runtime.GOOS]
			if strings.EqualFold(app.Driver, "brew") || isCaskRef(ref) {
				if brewBackend == nil || ref == "" {
					continue
				}
				installed, _, detectErr := brewBackend.Detect(ref)
				if detectErr != nil {
					failedApps = append(failedApps, *app)
					continue
				}
				app.Installed, app.Backend = installed, brewBackend.Name()
				if batch, ok := brewBackend.(driver.BatchDetector); ok {
					if results, batchErr := batch.DetectBatch([]string{ref}); batchErr == nil {
						app.InstalledVersion = results[ref].Version
					}
				}
				continue
			}
			if ref == "" {
				continue
			}
			app.Backend = backend.Name()
			if currentErr != nil {
				failedApps = append(failedApps, *app)
				continue
			}
			if !presentInSet(current, ref) {
				continue
			}
			app.Installed = true
			if element, ok := realizerElementForRef(current, ref); ok {
				app.InstalledVersion = nix.StorePathVersion(element.Name, element.StorePaths)
			}
		}
		packagesByModule := make(map[string][]modules.PackageEvidence, len(request.Modules))
		failedModules := make(map[string]struct{})
		for moduleID, module := range request.Modules {
			packagesByModule[moduleID] = capturePackageEvidence(module, detected)
			for _, failedApp := range failedApps {
				if _, _, matched := matchedPackageRef(module, failedApp); matched {
					failedModules[moduleID] = struct{}{}
					break
				}
			}
		}
		return configRestoreDetectionEvidence{
			PackagesByModule: packagesByModule, FailedModules: failedModules, Glob: filepath.Glob,
		}, nil
	})
}

func realizerElementForRef(set realizer.Set, ref string) (realizer.Element, bool) {
	leaf := leafAttr(ref)
	if element, ok := set.Elements[leaf]; ok {
		return element, true
	}
	for _, element := range set.Elements {
		if element.Name == leaf || leafAttr(element.AttrPath) == leaf {
			return element, true
		}
	}
	return realizer.Element{}, false
}

func detectDriverConfigRestoreApps(backend driver.Driver, apps []manifest.App) ([]manifest.App, error) {
	current := cloneConfigRestoreEvidenceApps(apps)
	refs := make([]string, 0, len(current))
	seen := make(map[string]struct{}, len(current))
	for _, app := range current {
		ref := resolveAppRef(app)
		if ref == "" {
			continue
		}
		if _, exists := seen[ref]; exists {
			continue
		}
		seen[ref] = struct{}{}
		refs = append(refs, ref)
	}
	sort.Strings(refs)
	batch := map[string]driver.DetectResult{}
	if detector, ok := backend.(driver.BatchDetector); ok && len(refs) > 0 {
		var err error
		batch, err = detector.DetectBatch(refs)
		if err != nil {
			batch = map[string]driver.DetectResult{}
		}
	}
	for index := range current {
		current[index].Installed = false
		current[index].InstalledVersion = ""
		ref := resolveAppRef(current[index])
		if ref == "" {
			continue
		}
		if detected, exists := batch[ref]; exists {
			current[index].Installed = detected.Installed
			current[index].InstalledVersion = detected.Version
		} else {
			installed, _, err := backend.Detect(ref)
			if err != nil {
				return nil, err
			}
			current[index].Installed = installed
		}
		current[index].Backend = backend.Name()
	}
	return current, nil
}

func cloneConfigRestoreEvidenceApps(values []manifest.App) []manifest.App {
	cloned := make([]manifest.App, len(values))
	for index, value := range values {
		cloned[index] = value
		cloned[index].Refs = make(map[string]string, len(value.Refs))
		for platform, ref := range value.Refs {
			cloned[index].Refs[platform] = ref
		}
	}
	return cloned
}
