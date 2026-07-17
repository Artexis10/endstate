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
		var brewBackendErr error
		if candidate, brewErr := newBrewDriverFn(); brewErr == nil {
			brewBackend = candidate
		} else {
			brewBackendErr = brewErr
		}
		return newRealizerConfigRestoreEvidenceSourceWithBrewError(
			backend, brewBackend, brewBackendErr, manifestApps,
		)
	}
	if lanes, _, err := resolvePackageDriverLanes(manifestApps); err == nil && len(lanes) > 0 {
		return newDriverLaneConfigRestoreEvidenceSource(lanes)
	}
	if backend, err := newDriverFn(); err == nil && backend != nil {
		return newDriverConfigRestoreEvidenceSource(backend, manifestApps)
	}
	return newFilesystemConfigRestoreEvidenceSource()
}

type configRestoreEvidenceLane struct {
	name   string
	source configRestoreEvidenceSource
	apps   []manifest.App
	err    error
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
		failures := []configRestoreDetectionFailure{}
		moduleIDs := make([]string, 0, len(request.Modules))
		for moduleID := range request.Modules {
			moduleIDs = append(moduleIDs, moduleID)
		}
		sort.Strings(moduleIDs)
		for _, moduleID := range moduleIDs {
			module := request.Modules[moduleID]
			selectedApps := configRestoreEvidenceAppsForModule(module, apps)
			current, err := detectDriverConfigRestoreApps(backend, selectedApps)
			if err != nil {
				packagesByModule[moduleID] = []modules.PackageEvidence{}
				failedModules[moduleID] = struct{}{}
				ref := ""
				var detectionErr *configRestoreDriverDetectionError
				if errors.As(err, &detectionErr) {
					ref = detectionErr.Ref
				}
				failures = append(failures, configRestoreDetectionFailure{
					ModuleID: moduleID, Driver: backend.Name(), Ref: ref, Detail: err.Error(),
				})
				continue
			}
			packagesByModule[moduleID] = capturePackageEvidence(module, current)
		}
		return configRestoreDetectionEvidence{
			PackagesByModule: packagesByModule, FailedModules: failedModules,
			Failures: normalizedConfigRestoreDetectionFailures(failures), Glob: filepath.Glob,
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
	sources := make([]configRestoreEvidenceLane, 0, len(lanes))
	for _, lane := range lanes {
		laneApps := make([]manifest.App, 0, len(lane.apps))
		for _, routed := range lane.apps {
			if routed != nil {
				laneApps = append(laneApps, routed.app)
			}
		}
		entry := configRestoreEvidenceLane{name: lane.name, apps: cloneConfigRestoreEvidenceApps(laneApps), err: lane.err}
		if lane.drv != nil && lane.err == nil {
			entry.source = newDriverConfigRestoreEvidenceSource(lane.drv, laneApps)
		} else if entry.err == nil {
			entry.err = errors.New("package driver is unavailable")
		}
		sources = append(sources, entry)
	}
	return newCompositeConfigRestoreEvidenceSource(sources)
}

func newCompositeConfigRestoreEvidenceSource(
	sources []configRestoreEvidenceLane,
) configRestoreEvidenceSource {
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
			Failures:         []configRestoreDetectionFailure{},
			Glob:             filepath.Glob,
		}
		for moduleID := range request.Modules {
			combined.PackagesByModule[moduleID] = []modules.PackageEvidence{}
		}
		for _, lane := range sources {
			if lane.source == nil {
				for moduleID, module := range request.Modules {
					for _, app := range lane.apps {
						ref, matched := matchedUnavailableConfigRestoreLaneRef(module, app, lane.name)
						if !matched {
							continue
						}
						combined.FailedModules[moduleID] = struct{}{}
						detail := "package driver is unavailable"
						if lane.err != nil {
							detail = lane.err.Error()
						}
						combined.Failures = append(combined.Failures, configRestoreDetectionFailure{
							ModuleID: moduleID, Driver: lane.name, Ref: ref, Detail: detail,
						})
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
			combined.Failures = append(combined.Failures, evidence.Failures...)
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
		combined.Failures = normalizedConfigRestoreDetectionFailures(combined.Failures)
		return combined, nil
	})
}

func matchedUnavailableConfigRestoreLaneRef(
	module *modules.Module,
	app manifest.App,
	driverName string,
) (string, bool) {
	if module == nil {
		return "", false
	}
	var declaredRefs []string
	switch strings.ToLower(strings.TrimSpace(driverName)) {
	case "winget":
		declaredRefs = module.Matches.Winget
	case "chocolatey":
		declaredRefs = module.Matches.Chocolatey
	}
	if len(declaredRefs) > 0 {
		platforms := make([]string, 0, len(app.Refs))
		for platform := range app.Refs {
			platforms = append(platforms, platform)
		}
		sort.Strings(platforms)
		for _, platform := range platforms {
			ref := strings.TrimSpace(app.Refs[platform])
			for _, declared := range declaredRefs {
				if ref != "" && strings.EqualFold(strings.TrimSpace(declared), ref) {
					return ref, true
				}
			}
		}
	}
	if app.Driver == "" && app.Backend == "" && strings.EqualFold(driverName, "nix") {
		app.Backend = "nix"
	}
	_, ref, matched := matchedPackageRef(module, app)
	return ref, matched
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
	return newRealizerConfigRestoreEvidenceSourceWithBrewError(backend, brewBackend, nil, manifestApps)
}

func newRealizerConfigRestoreEvidenceSourceWithBrewError(
	backend realizer.Realizer,
	brewBackend driver.Driver,
	brewBackendErr error,
	manifestApps []manifest.App,
) configRestoreEvidenceSource {
	brewApps, unsupportedApps, realizerApps := partitionRealizerLanes(manifestApps)
	sources := make([]configRestoreEvidenceLane, 0, 2+len(unsupportedApps))
	if len(realizerApps) > 0 {
		sources = append(sources, configRestoreEvidenceLane{
			name: backend.Name(), source: newNixConfigRestoreEvidenceSource(backend, realizerApps),
			apps: cloneConfigRestoreEvidenceApps(realizerApps),
		})
	}
	if len(brewApps) > 0 {
		brewLane := configRestoreEvidenceLane{name: "brew", apps: cloneConfigRestoreEvidenceApps(brewApps), err: brewBackendErr}
		if brewBackend != nil {
			brewLane.source = newDriverConfigRestoreEvidenceSource(brewBackend, brewApps)
		} else if brewLane.err == nil {
			brewLane.err = errors.New("brew package driver is unavailable")
		}
		sources = append(sources, brewLane)
	}
	sources = append(sources, unsupportedConfigRestoreEvidenceLanes(unsupportedApps)...)
	return newCompositeConfigRestoreEvidenceSource(sources)
}

func newBrewOnlyConfigRestoreEvidenceSource(
	brewBackend driver.Driver,
	realizerApps []manifest.App,
	brewApps []manifest.App,
	unsupportedApps []manifest.App,
) configRestoreEvidenceSource {
	sources := make([]configRestoreEvidenceLane, 0, 2+len(unsupportedApps))
	if len(realizerApps) > 0 {
		sources = append(sources, configRestoreEvidenceLane{
			name: "nix", apps: cloneConfigRestoreEvidenceApps(realizerApps),
			err: errors.New("nix package realizer is unavailable"),
		})
	}
	if len(brewApps) > 0 {
		brewLane := configRestoreEvidenceLane{name: "brew", apps: cloneConfigRestoreEvidenceApps(brewApps)}
		if brewBackend != nil {
			brewLane.source = newDriverConfigRestoreEvidenceSource(brewBackend, brewApps)
		} else {
			brewLane.err = errors.New("brew package driver is unavailable")
		}
		sources = append(sources, brewLane)
	}
	sources = append(sources, unsupportedConfigRestoreEvidenceLanes(unsupportedApps)...)
	return newCompositeConfigRestoreEvidenceSource(sources)
}

func unsupportedConfigRestoreEvidenceLanes(apps []manifest.App) []configRestoreEvidenceLane {
	byDriver := make(map[string][]manifest.App)
	for _, app := range apps {
		driverName := strings.ToLower(strings.TrimSpace(app.Driver))
		if driverName == "" {
			driverName = "unknown"
		}
		byDriver[driverName] = append(byDriver[driverName], app)
	}
	driverNames := make([]string, 0, len(byDriver))
	for driverName := range byDriver {
		driverNames = append(driverNames, driverName)
	}
	sort.Strings(driverNames)
	lanes := make([]configRestoreEvidenceLane, 0, len(driverNames))
	for _, driverName := range driverNames {
		lanes = append(lanes, configRestoreEvidenceLane{
			name: driverName, apps: cloneConfigRestoreEvidenceApps(byDriver[driverName]),
			err: fmt.Errorf("%s package driver is unsupported on the selected realizer host", driverName),
		})
	}
	return lanes
}

func newNixConfigRestoreEvidenceSource(
	backend realizer.Realizer,
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
		type failedApp struct {
			app    manifest.App
			ref    string
			detail string
		}
		failedApps := []failedApp{}
		for index := range detected {
			app := &detected[index]
			app.Installed = false
			app.InstalledVersion = ""
			ref := app.Refs[runtime.GOOS]
			if ref == "" {
				continue
			}
			app.Backend = backend.Name()
			if currentErr != nil {
				failedApps = append(failedApps, failedApp{app: *app, ref: ref, detail: currentErr.Error()})
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
		failures := []configRestoreDetectionFailure{}
		for moduleID, module := range request.Modules {
			packagesByModule[moduleID] = capturePackageEvidence(module, detected)
			for _, failure := range failedApps {
				if _, _, matched := matchedPackageRef(module, failure.app); matched {
					failedModules[moduleID] = struct{}{}
					failures = append(failures, configRestoreDetectionFailure{
						ModuleID: moduleID, Driver: backend.Name(), Ref: failure.ref, Detail: failure.detail,
					})
				}
			}
		}
		return configRestoreDetectionEvidence{
			PackagesByModule: packagesByModule, FailedModules: failedModules,
			Failures: normalizedConfigRestoreDetectionFailures(failures), Glob: filepath.Glob,
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

type configRestoreDriverDetectionError struct {
	Ref string
	Err error
}

func (detectionErr *configRestoreDriverDetectionError) Error() string {
	if detectionErr == nil || detectionErr.Err == nil {
		return "package detection failed"
	}
	return detectionErr.Err.Error()
}

func (detectionErr *configRestoreDriverDetectionError) Unwrap() error {
	if detectionErr == nil {
		return nil
	}
	return detectionErr.Err
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
				return nil, &configRestoreDriverDetectionError{Ref: ref, Err: err}
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
