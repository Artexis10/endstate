// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"errors"
	"runtime"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/bootstrap"
	"github.com/Artexis10/endstate/go-engine/internal/driver"
	"github.com/Artexis10/endstate/go-engine/internal/events"
	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/packagesource"
	"github.com/Artexis10/endstate/go-engine/internal/planner"
)

// newNamedDriverFn is the command-layer seam for authoritative explicit
// package-driver selection. The default lane keeps using newDriverFn so the
// mature single-driver test and production paths retain their existing seam.
var newNamedDriverFn func(string) (driver.Driver, error) = func(name string) (driver.Driver, error) {
	return selectDriver(runtime.GOOS, name)
}

type routedDriverApp struct {
	index      int
	app        manifest.App
	ref        string
	driverName string
	source     string
	laneKey    string
	drv        driver.Driver
	err        error
	failed     bool
	isManual   bool
}

type packageDriverLane struct {
	name   string
	source string
	key    string
	drv    driver.Driver
	err    error
	failed bool
	apps   []*routedDriverApp
}

type driverLaneDetection struct {
	results   map[string]driver.DetectResult
	err       error
	batchUsed bool
}

func detectPackageDriverLanes(lanes []packageDriverLane) map[string]driverLaneDetection {
	detections := make(map[string]driverLaneDetection, len(lanes))
	for _, lane := range lanes {
		if lane.err != nil || lane.drv == nil {
			detections[lane.key] = driverLaneDetection{err: lane.err}
			continue
		}
		if len(lane.apps) == 0 {
			detections[lane.key] = driverLaneDetection{}
			continue
		}
		refs := make([]string, 0, len(lane.apps))
		for _, route := range lane.apps {
			refs = append(refs, route.ref)
		}
		if sourceBD, ok := lane.drv.(driver.SourceBatchDetector); ok && lane.source != "" {
			results, err := sourceBD.DetectBatchSource(refs, lane.source)
			detections[lane.key] = driverLaneDetection{results: results, err: err, batchUsed: true}
		} else if bd, ok := lane.drv.(driver.BatchDetector); ok {
			results, err := bd.DetectBatch(refs)
			detections[lane.key] = driverLaneDetection{results: results, err: err, batchUsed: true}
		} else {
			detections[lane.key] = driverLaneDetection{}
		}
	}
	return detections
}

// resolvePackageDriverLanes resolves each package app exactly once and groups
// it by authoritative driver in first-manifest-appearance order. Omitted
// drivers share the platform default lane; explicit names never fall back.
type driverLaneOverride struct {
	err    error
	failed bool
}

func resolvePackageDriverLanes(apps []manifest.App) ([]packageDriverLane, []*routedDriverApp, error) {
	return resolvePackageDriverLanesWithOverrides(apps, nil)
}

func resolvePackageDriverLanesWithOverrides(apps []manifest.App, overrides map[string]driverLaneOverride) ([]packageDriverLane, []*routedDriverApp, error) {
	lanes := make([]packageDriverLane, 0)
	byKey := make(map[string]int)
	resolvedDrivers := make(map[string]driver.Driver)
	routed := make([]*routedDriverApp, 0, len(apps))
	var defaultDriver driver.Driver
	var defaultErr error
	defaultResolved := false

	addLane := func(name, source string, drv driver.Driver, err error, failed bool) int {
		key := name + "\x00" + source
		if i, ok := byKey[key]; ok {
			return i
		}
		byKey[key] = len(lanes)
		lanes = append(lanes, packageDriverLane{name: name, source: source, key: key, drv: drv, err: err, failed: failed})
		return len(lanes) - 1
	}

	for i, app := range apps {
		ref := resolveAppRef(app)
		isManual := ref == "" && app.Manual != nil && app.Manual.VerifyPath != ""
		if ref == "" && !isManual {
			continue
		}
		if isManual {
			routed = append(routed, &routedDriverApp{index: i, app: app, isManual: true, driverName: "manual"})
			continue
		}

		requested := strings.ToLower(strings.TrimSpace(app.Driver))
		var drv driver.Driver
		var err error
		failed := false
		name := requested
		if requested == "" {
			if !defaultResolved {
				defaultDriver, defaultErr = newDriverFn()
				defaultResolved = true
			}
			drv, err = defaultDriver, defaultErr
			if err != nil {
				// Preserve the legacy default-lane behavior: inability to construct
				// the host default is a command-level infrastructure error.
				return nil, nil, err
			}
			name = strings.ToLower(strings.TrimSpace(drv.Name()))
		} else if override, ok := overrides[requested]; ok {
			err, failed = override.err, override.failed
		} else if existing, ok := resolvedDrivers[requested]; ok {
			drv = existing
		} else {
			drv, err = newNamedDriverFn(requested)
			if drv != nil {
				resolvedDrivers[requested] = drv
			}
		}

		if drv != nil {
			name = strings.ToLower(strings.TrimSpace(drv.Name()))
		}
		source := ""
		if name == "winget" {
			source = packagesource.ResolveWinget(ref, app.Source)
		}
		laneIndex := addLane(name, source, drv, err, failed)
		if lanes[laneIndex].drv == nil && drv != nil {
			lanes[laneIndex].drv = drv
		}
		route := &routedDriverApp{
			index:      i,
			app:        app,
			ref:        ref,
			driverName: name,
			source:     source,
			laneKey:    lanes[laneIndex].key,
			drv:        lanes[laneIndex].drv,
			err:        lanes[laneIndex].err,
			failed:     lanes[laneIndex].failed,
		}
		lanes[laneIndex].apps = append(lanes[laneIndex].apps, route)
		routed = append(routed, route)
	}
	return lanes, routed, nil
}

func computeDriverLanePlan(mf *manifest.Manifest) (*planner.Plan, []CommandWarning, error) {
	return computeDriverLanePlanWithOverrides(mf, nil)
}

func computeDriverLanePlanWithOverrides(mf *manifest.Manifest, overrides map[string]driverLaneOverride) (*planner.Plan, []CommandWarning, error) {
	lanes, routed, err := resolvePackageDriverLanesWithOverrides(mf.Apps, overrides)
	if err != nil {
		return nil, nil, err
	}
	warnings := possibleDuplicatePackageWarnings(routed)
	actionsByIndex := make(map[int]planner.PlanAction, len(routed))

	for _, lane := range lanes {
		if lane.err != nil || lane.drv == nil {
			for _, route := range lane.apps {
				actionsByIndex[route.index] = planner.PlanAction{
					Type:          "app",
					ID:            route.app.ID,
					Ref:           route.ref,
					Driver:        route.driverName,
					Source:        route.source,
					CurrentStatus: driver.StatusSkipped,
					PlannedAction: "skip",
					DisplayName:   resolveItemDisplayName("", route.app, route.ref),
					Message:       lane.err.Error(),
				}
			}
			continue
		}

		laneManifest := *mf
		laneManifest.Apps = make([]manifest.App, 0, len(lane.apps))
		for _, route := range lane.apps {
			laneManifest.Apps = append(laneManifest.Apps, route.app)
		}
		lanePlan, planErr := planner.ComputePlan(&laneManifest, lane.drv)
		if planErr != nil {
			return nil, nil, planErr
		}
		for i, action := range lanePlan.Actions {
			actionsByIndex[lane.apps[i].index] = action
		}
	}

	for _, route := range routed {
		if !route.isManual {
			continue
		}
		expanded, exists := checkVerifyPath(route.app.Manual.VerifyPath)
		action := planner.PlanAction{
			Type:        "app",
			ID:          route.app.ID,
			Driver:      "manual",
			DisplayName: resolveItemDisplayName("", route.app, route.app.ID),
		}
		if exists {
			action.CurrentStatus = "present"
			action.PlannedAction = "skip"
		} else {
			action.CurrentStatus = "missing"
			action.PlannedAction = "install"
			action.Message = "Not found at " + expanded
		}
		actionsByIndex[route.index] = action
	}

	plan := &planner.Plan{}
	for i := range mf.Apps {
		action, ok := actionsByIndex[i]
		if !ok {
			continue
		}
		plan.Actions = append(plan.Actions, action)
		switch {
		case action.CurrentStatus == "present":
			plan.Summary.AlreadyPresent++
		case action.PlannedAction == "install":
			plan.Summary.ToInstall++
		default:
			plan.Summary.Skipped++
		}
	}
	plan.Summary.Total = len(plan.Actions)
	return plan, warnings, nil
}

// packageDriverReadOnlyOverrides probes optional selected package managers for
// plan/verify without installing or asking for mutating consent. An absent
// Chocolatey lane is visible as unavailable/skipped and is never mistaken for
// every package being absent.
func packageDriverReadOnlyOverrides(mf *manifest.Manifest, emitter *events.Emitter) map[string]driverLaneOverride {
	needed := false
	for _, app := range mf.Apps {
		if strings.EqualFold(strings.TrimSpace(app.Driver), string(bootstrap.BackendChocolatey)) && resolveAppRef(app) != "" {
			needed = true
			break
		}
	}
	if !needed {
		return nil
	}
	available, bootstrapErr := bootstrapBackendsFn([]bootstrap.Backend{bootstrap.BackendChocolatey}, false, Consent{}, emitter)
	if bootstrapErr != nil {
		return map[string]driverLaneOverride{"chocolatey": {err: errors.New(bootstrapErr.Message), failed: true}}
	}
	if available[bootstrap.BackendChocolatey] {
		return nil
	}
	return map[string]driverLaneOverride{
		"chocolatey": {err: errors.New("Chocolatey is unavailable; read-only commands will not install package backends")},
	}
}
