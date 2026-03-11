// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package planner

import (
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/driver"
	"github.com/Artexis10/endstate/go-engine/internal/manifest"
)

// ---------------------------------------------------------------------------
// Test driver
// ---------------------------------------------------------------------------

type testDriver struct {
	installed map[string]bool
}

func (d *testDriver) Name() string { return "test" }

func (d *testDriver) Detect(ref string) (bool, error) {
	return d.installed[ref], nil
}

func (d *testDriver) Install(ref string) (*driver.InstallResult, error) {
	return nil, nil
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestComputePlan_AllPresent(t *testing.T) {
	drv := &testDriver{installed: map[string]bool{
		"Microsoft.VisualStudioCode": true,
		"Git.Git":                   true,
	}}

	mf := &manifest.Manifest{
		Apps: []manifest.App{
			{ID: "vscode", Refs: map[string]string{"windows": "Microsoft.VisualStudioCode"}},
			{ID: "git", Refs: map[string]string{"windows": "Git.Git"}},
		},
	}

	plan, err := ComputePlan(mf, drv)
	if err != nil {
		t.Fatalf("ComputePlan failed: %v", err)
	}

	if plan.Summary.Total != 2 {
		t.Errorf("expected total=2, got %d", plan.Summary.Total)
	}
	if plan.Summary.ToInstall != 0 {
		t.Errorf("expected toInstall=0, got %d", plan.Summary.ToInstall)
	}
	if plan.Summary.AlreadyPresent != 2 {
		t.Errorf("expected alreadyPresent=2, got %d", plan.Summary.AlreadyPresent)
	}

	for _, action := range plan.Actions {
		if action.CurrentStatus != "present" {
			t.Errorf("expected currentStatus=present for %q, got %q", action.ID, action.CurrentStatus)
		}
		if action.PlannedAction != "skip" {
			t.Errorf("expected plannedAction=skip for %q, got %q", action.ID, action.PlannedAction)
		}
	}
}

func TestComputePlan_AllMissing(t *testing.T) {
	drv := &testDriver{installed: map[string]bool{}}

	mf := &manifest.Manifest{
		Apps: []manifest.App{
			{ID: "vscode", Refs: map[string]string{"windows": "Microsoft.VisualStudioCode"}},
			{ID: "git", Refs: map[string]string{"windows": "Git.Git"}},
		},
	}

	plan, err := ComputePlan(mf, drv)
	if err != nil {
		t.Fatalf("ComputePlan failed: %v", err)
	}

	if plan.Summary.Total != 2 {
		t.Errorf("expected total=2, got %d", plan.Summary.Total)
	}
	if plan.Summary.ToInstall != 2 {
		t.Errorf("expected toInstall=2, got %d", plan.Summary.ToInstall)
	}
	if plan.Summary.AlreadyPresent != 0 {
		t.Errorf("expected alreadyPresent=0, got %d", plan.Summary.AlreadyPresent)
	}

	for _, action := range plan.Actions {
		if action.CurrentStatus != "missing" {
			t.Errorf("expected currentStatus=missing for %q, got %q", action.ID, action.CurrentStatus)
		}
		if action.PlannedAction != "install" {
			t.Errorf("expected plannedAction=install for %q, got %q", action.ID, action.PlannedAction)
		}
	}
}

func TestComputePlan_Mixed(t *testing.T) {
	drv := &testDriver{installed: map[string]bool{
		"Microsoft.VisualStudioCode": true,
		// Git.Git is missing
	}}

	mf := &manifest.Manifest{
		Apps: []manifest.App{
			{ID: "vscode", Refs: map[string]string{"windows": "Microsoft.VisualStudioCode"}},
			{ID: "git", Refs: map[string]string{"windows": "Git.Git"}},
		},
	}

	plan, err := ComputePlan(mf, drv)
	if err != nil {
		t.Fatalf("ComputePlan failed: %v", err)
	}

	if plan.Summary.Total != 2 {
		t.Errorf("expected total=2, got %d", plan.Summary.Total)
	}
	if plan.Summary.ToInstall != 1 {
		t.Errorf("expected toInstall=1, got %d", plan.Summary.ToInstall)
	}
	if plan.Summary.AlreadyPresent != 1 {
		t.Errorf("expected alreadyPresent=1, got %d", plan.Summary.AlreadyPresent)
	}

	// Verify each action's status.
	for _, action := range plan.Actions {
		switch action.ID {
		case "vscode":
			if action.CurrentStatus != "present" {
				t.Errorf("expected vscode currentStatus=present, got %q", action.CurrentStatus)
			}
			if action.PlannedAction != "skip" {
				t.Errorf("expected vscode plannedAction=skip, got %q", action.PlannedAction)
			}
		case "git":
			if action.CurrentStatus != "missing" {
				t.Errorf("expected git currentStatus=missing, got %q", action.CurrentStatus)
			}
			if action.PlannedAction != "install" {
				t.Errorf("expected git plannedAction=install, got %q", action.PlannedAction)
			}
		}
	}
}

func TestComputePlan_EmptyManifest(t *testing.T) {
	drv := &testDriver{installed: map[string]bool{}}

	mf := &manifest.Manifest{
		Apps: []manifest.App{},
	}

	plan, err := ComputePlan(mf, drv)
	if err != nil {
		t.Fatalf("ComputePlan failed: %v", err)
	}

	if plan.Summary.Total != 0 {
		t.Errorf("expected total=0, got %d", plan.Summary.Total)
	}
	if len(plan.Actions) != 0 {
		t.Errorf("expected 0 actions, got %d", len(plan.Actions))
	}
}

func TestComputePlan_NoWindowsRef(t *testing.T) {
	drv := &testDriver{installed: map[string]bool{}}

	mf := &manifest.Manifest{
		Apps: []manifest.App{
			{ID: "no-ref", Refs: map[string]string{}},
		},
	}

	plan, err := ComputePlan(mf, drv)
	if err != nil {
		t.Fatalf("ComputePlan failed: %v", err)
	}

	if plan.Summary.Total != 0 {
		t.Errorf("expected total=0 (app with no ref should be skipped), got %d", plan.Summary.Total)
	}
	if len(plan.Actions) != 0 {
		t.Errorf("expected 0 actions, got %d", len(plan.Actions))
	}
}
