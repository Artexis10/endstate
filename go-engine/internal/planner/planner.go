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
				Type:   "app",
				ID:     app.ID,
				Driver: "manual",
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

		installed, displayName, _ := drv.Detect(ref)

		action := PlanAction{
			Type:        "app",
			ID:          app.ID,
			Ref:         ref,
			Driver:      drv.Name(),
			DisplayName: displayName,
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
