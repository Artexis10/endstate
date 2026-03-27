// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

// Package planner computes a declarative plan by detecting each app in a
// manifest against the current system state via the driver interface.
package planner

import (
	"os"

	"github.com/Artexis10/endstate/go-engine/internal/config"
	"github.com/Artexis10/endstate/go-engine/internal/driver"
	"github.com/Artexis10/endstate/go-engine/internal/manifest"
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
	CurrentStatus string `json:"currentStatus"`
	PlannedAction string `json:"plannedAction"`
	DisplayName   string `json:"displayName,omitempty"`
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

	// Batch-detect all winget apps in one call for performance.
	var wingetRefs []string
	for _, app := range mf.Apps {
		ref := resolveRef(app)
		if ref != "" {
			wingetRefs = append(wingetRefs, ref)
		}
	}

	var batchResults map[string]driver.DetectResult
	if bd, ok := drv.(driver.BatchDetector); ok && len(wingetRefs) > 0 {
		batchResults, _ = bd.DetectBatch(wingetRefs)
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

		// Use batch results if available; fall back to per-ref Detect.
		var installed bool
		var displayName string
		if br, ok := batchResults[ref]; ok {
			installed = br.Installed
			displayName = br.DisplayName
		} else {
			installed, displayName, _ = drv.Detect(ref)
		}

		action := PlanAction{
			Type:        "app",
			ID:          app.ID,
			Ref:         ref,
			Driver:      drv.Name(),
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
	expanded := config.ExpandWindowsEnvVars(p)
	expanded = os.ExpandEnv(expanded)
	return expanded
}

// resolveRef returns the platform-specific package ref for an app. It prefers
// app.Refs["windows"]; if absent it falls back to the first available ref in
// map iteration order. Returns "" if the map is empty or all refs are blank.
func resolveRef(app manifest.App) string {
	if ref, ok := app.Refs["windows"]; ok && ref != "" {
		return ref
	}
	for _, ref := range app.Refs {
		if ref != "" {
			return ref
		}
	}
	return ""
}
