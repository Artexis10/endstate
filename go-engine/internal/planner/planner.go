// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

// Package planner computes a declarative plan by detecting each app in a
// manifest against the current system state via the driver interface.
package planner

import (
	"os"
	"runtime"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/config"
	"github.com/Artexis10/endstate/go-engine/internal/driver"
	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/packagesource"
)

// statFn is the filesystem stat function used for manual app verification.
// It defaults to os.Stat and can be replaced in tests.
var statFn = os.Stat

// Plan is the result of computing a manifest against the current system state.
type Plan struct {
	Actions []PlanAction `json:"actions"`
	Summary PlanSummary  `json:"summary"`
}

// PlanAction describes what would happen for a single app entry.
type PlanAction struct {
	Type          string `json:"type"`
	ID            string `json:"id"`
	Ref           string `json:"ref"`
	Driver        string `json:"driver"`
	Source        string `json:"source,omitempty"`
	CurrentStatus string `json:"currentStatus"`
	PlannedAction string `json:"plannedAction"`
	DisplayName   string `json:"displayName,omitempty"`
	Message       string `json:"message,omitempty"`
}

// PlanSummary aggregates the plan outcome counts.
type PlanSummary struct {
	Total          int `json:"total"`
	ToInstall      int `json:"toInstall"`
	AlreadyPresent int `json:"alreadyPresent"`
	Skipped        int `json:"skipped"`
}

// ComputePlan builds a plan by detecting each app in the manifest using the
// provided driver. Apps without a resolvable ref or manual.verifyPath are
// silently skipped.
func ComputePlan(mf *manifest.Manifest, drv driver.Driver) (*Plan, error) {
	var plan Plan
	isWinget := strings.EqualFold(strings.TrimSpace(drv.Name()), "winget")

	// Batch-detect all winget apps in one call for performance.
	var wingetRefs []string
	for _, app := range mf.Apps {
		ref := resolveRef(app)
		if ref != "" {
			wingetRefs = append(wingetRefs, ref)
		}
	}

	var batchResults map[string]driver.DetectResult
	var sourceBatchResults map[string]map[string]driver.DetectResult
	var sourceBatchErrors map[string]error
	var batchErr error
	batchUsed := false
	if sourceBD, ok := drv.(driver.SourceBatchDetector); ok && isWinget && len(wingetRefs) > 0 {
		batchUsed = true
		sourceBatchResults = map[string]map[string]driver.DetectResult{}
		sourceBatchErrors = map[string]error{}
		refsBySource := map[string][]string{}
		var sourceOrder []string
		for _, app := range mf.Apps {
			ref := resolveRef(app)
			if ref == "" {
				continue
			}
			source := packagesource.ResolveWinget(ref, app.Source)
			if _, exists := refsBySource[source]; !exists {
				sourceOrder = append(sourceOrder, source)
			}
			refsBySource[source] = append(refsBySource[source], ref)
		}
		for _, source := range sourceOrder {
			results, err := sourceBD.DetectBatchSource(refsBySource[source], source)
			sourceBatchResults[source], sourceBatchErrors[source] = results, err
		}
	} else if bd, ok := drv.(driver.BatchDetector); ok && len(wingetRefs) > 0 {
		batchUsed = true
		batchResults, batchErr = bd.DetectBatch(wingetRefs)
	}

	for _, app := range mf.Apps {
		ref := resolveRef(app)
		isManual := ref == "" && app.Manual != nil && app.Manual.VerifyPath != ""

		if ref == "" && !isManual {
			continue
		}

		if isManual {
			expanded := expandVerifyPath(app.Manual.VerifyPath)
			_, err := statFn(expanded)
			exists := err == nil

			action := PlanAction{
				Type:        "app",
				ID:          app.ID,
				Driver:      "manual",
				DisplayName: resolveDisplayName("", app),
			}
			if exists {
				action.CurrentStatus = "present"
				action.PlannedAction = "skip"
				plan.Summary.AlreadyPresent++
			} else {
				action.CurrentStatus = "missing"
				action.PlannedAction = "install"
				plan.Summary.ToInstall++
			}
			plan.Actions = append(plan.Actions, action)
			continue
		}

		if batchErr != nil {
			plan.Actions = append(plan.Actions, PlanAction{
				Type:          "app",
				ID:            app.ID,
				Ref:           ref,
				Driver:        drv.Name(),
				CurrentStatus: "failed",
				PlannedAction: "skip",
				DisplayName:   resolveDisplayName("", app),
				Message:       batchErr.Error(),
			})
			plan.Summary.Skipped++
			continue
		}
		source := ""
		if isWinget {
			source = packagesource.ResolveWinget(ref, app.Source)
		}
		if err := sourceBatchErrors[source]; err != nil {
			plan.Actions = append(plan.Actions, PlanAction{Type: "app", ID: app.ID, Ref: ref, Driver: drv.Name(), Source: source, CurrentStatus: "failed", PlannedAction: "skip", DisplayName: resolveDisplayName("", app), Message: err.Error()})
			plan.Summary.Skipped++
			continue
		}

		// Use batch results if available; otherwise detect this ref directly.
		var installed bool
		var displayName string
		var detectErr error
		if batchUsed {
			results := batchResults
			if sourceBatchResults != nil {
				results = sourceBatchResults[source]
			}
			if br, ok := results[ref]; ok {
				installed = br.Installed
				displayName = br.DisplayName
			}
		} else if sourceDriver, ok := drv.(driver.SourceDriver); ok && isWinget {
			installed, displayName, detectErr = sourceDriver.DetectSource(ref, source)
		} else {
			installed, displayName, detectErr = drv.Detect(ref)
		}
		if detectErr != nil {
			plan.Actions = append(plan.Actions, PlanAction{
				Type:          "app",
				ID:            app.ID,
				Ref:           ref,
				Driver:        drv.Name(),
				CurrentStatus: "failed",
				PlannedAction: "skip",
				DisplayName:   resolveDisplayName("", app),
				Message:       detectErr.Error(),
			})
			plan.Summary.Skipped++
			continue
		}

		action := PlanAction{
			Type:        "app",
			ID:          app.ID,
			Ref:         ref,
			Driver:      drv.Name(),
			Source:      source,
			DisplayName: resolveDisplayName(displayName, app),
		}

		if installed {
			action.CurrentStatus = "present"
			action.PlannedAction = "skip"
			plan.Summary.AlreadyPresent++
		} else {
			action.CurrentStatus = "missing"
			action.PlannedAction = "install"
			plan.Summary.ToInstall++
		}

		plan.Actions = append(plan.Actions, action)
	}

	plan.Summary.Total = len(plan.Actions)
	return &plan, nil
}

// resolveDisplayName returns the best available human-readable name for an
// app. Resolution order: (1) resolved display name from detection, (2)
// manifest displayName, (3) manifest id.
func resolveDisplayName(resolved string, app manifest.App) string {
	if resolved != "" {
		return resolved
	}
	if app.DisplayName != "" {
		return app.DisplayName
	}
	return app.ID
}

// expandVerifyPath expands Windows-style %VAR% and Go-style $VAR environment
// variables in a verify path.
func expandVerifyPath(p string) string {
	expanded := config.ExpandEnvVars(p)
	expanded = os.ExpandEnv(expanded)
	return expanded
}

// resolveRef returns the package ref for an app on the host platform via the
// shared manifest.ResolveRef resolver (prefers refs[GOOS], else first non-empty).
func resolveRef(app manifest.App) string {
	return manifest.ResolveRef(app, runtime.GOOS)
}
