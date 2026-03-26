// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package planner

import (
	"os"
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

func (d *testDriver) Detect(ref string) (bool, string, error) {
	if d.installed[ref] {
		return true, ref + " Name", nil
	}
	return false, "", nil
}

func (d *testDriver) DetectBatch(refs []string) (map[string]driver.DetectResult, error) {
	results := make(map[string]driver.DetectResult, len(refs))
	for _, ref := range refs {
		if d.installed[ref] {
			results[ref] = driver.DetectResult{Installed: true, DisplayName: ref + " Name"}
		} else {
			results[ref] = driver.DetectResult{Installed: false}
		}
	}
	return results, nil
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

// ---------------------------------------------------------------------------
// Gap tests ported from Pester: Plan.Tests.ps1 and Planner.Tests.ps1
// ---------------------------------------------------------------------------

// TestComputePlan_Deterministic verifies that the same inputs produce the same
// plan output. (Pester: Plan.Deterministic.HashAndRunId - "Should produce
// identical plan for same inputs")
func TestComputePlan_Deterministic(t *testing.T) {
	drv := &testDriver{installed: map[string]bool{"Test.App2": true}}

	mf := &manifest.Manifest{
		Apps: []manifest.App{
			{ID: "test-app-1", Refs: map[string]string{"windows": "Test.App1"}},
			{ID: "test-app-2", Refs: map[string]string{"windows": "Test.App2"}},
			{ID: "test-app-3", Refs: map[string]string{"windows": "Test.App3"}},
		},
	}

	plan1, err := ComputePlan(mf, drv)
	if err != nil {
		t.Fatalf("first ComputePlan failed: %v", err)
	}
	plan2, err := ComputePlan(mf, drv)
	if err != nil {
		t.Fatalf("second ComputePlan failed: %v", err)
	}

	if plan1.Summary.Total != plan2.Summary.Total {
		t.Errorf("total mismatch: %d vs %d", plan1.Summary.Total, plan2.Summary.Total)
	}
	if plan1.Summary.ToInstall != plan2.Summary.ToInstall {
		t.Errorf("toInstall mismatch: %d vs %d", plan1.Summary.ToInstall, plan2.Summary.ToInstall)
	}
	if plan1.Summary.AlreadyPresent != plan2.Summary.AlreadyPresent {
		t.Errorf("alreadyPresent mismatch: %d vs %d", plan1.Summary.AlreadyPresent, plan2.Summary.AlreadyPresent)
	}
	if len(plan1.Actions) != len(plan2.Actions) {
		t.Fatalf("action count mismatch: %d vs %d", len(plan1.Actions), len(plan2.Actions))
	}
	for i := range plan1.Actions {
		if plan1.Actions[i].ID != plan2.Actions[i].ID {
			t.Errorf("action[%d].ID mismatch: %q vs %q", i, plan1.Actions[i].ID, plan2.Actions[i].ID)
		}
		if plan1.Actions[i].PlannedAction != plan2.Actions[i].PlannedAction {
			t.Errorf("action[%d].PlannedAction mismatch: %q vs %q", i, plan1.Actions[i].PlannedAction, plan2.Actions[i].PlannedAction)
		}
	}
}

// TestComputePlan_ActionOrderMatchesManifest verifies that plan actions
// preserve the order of apps as declared in the manifest.
// (Pester: Plan.Deterministic.HashAndRunId - "Should produce stable action
// list order")
func TestComputePlan_ActionOrderMatchesManifest(t *testing.T) {
	drv := &testDriver{installed: map[string]bool{}}

	mf := &manifest.Manifest{
		Apps: []manifest.App{
			{ID: "test-app-1", Refs: map[string]string{"windows": "Test.App1"}},
			{ID: "test-app-2", Refs: map[string]string{"windows": "Test.App2"}},
			{ID: "test-app-3", Refs: map[string]string{"windows": "Test.App3"}},
		},
	}

	plan, err := ComputePlan(mf, drv)
	if err != nil {
		t.Fatalf("ComputePlan failed: %v", err)
	}

	if len(plan.Actions) != 3 {
		t.Fatalf("expected 3 actions, got %d", len(plan.Actions))
	}

	expectedIDs := []string{"test-app-1", "test-app-2", "test-app-3"}
	for i, wantID := range expectedIDs {
		if plan.Actions[i].ID != wantID {
			t.Errorf("action[%d].ID = %q, want %q", i, plan.Actions[i].ID, wantID)
		}
	}
}

// TestComputePlan_ActionFields verifies that each action has the required
// fields populated: Type, ID, Ref, Driver, CurrentStatus, PlannedAction.
// (Pester: Plan.Structure - "Should have type/driver/id/ref/status field on
// app actions")
func TestComputePlan_ActionFields(t *testing.T) {
	drv := &testDriver{installed: map[string]bool{}}

	mf := &manifest.Manifest{
		Apps: []manifest.App{
			{ID: "my-app", Refs: map[string]string{"windows": "My.App"}},
		},
	}

	plan, err := ComputePlan(mf, drv)
	if err != nil {
		t.Fatalf("ComputePlan failed: %v", err)
	}

	if len(plan.Actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(plan.Actions))
	}

	action := plan.Actions[0]
	if action.Type != "app" {
		t.Errorf("Type = %q, want %q", action.Type, "app")
	}
	if action.ID != "my-app" {
		t.Errorf("ID = %q, want %q", action.ID, "my-app")
	}
	if action.Ref != "My.App" {
		t.Errorf("Ref = %q, want %q", action.Ref, "My.App")
	}
	if action.Driver != "test" {
		t.Errorf("Driver = %q, want %q", action.Driver, "test")
	}
	if action.CurrentStatus == "" {
		t.Error("CurrentStatus should not be empty")
	}
	if action.PlannedAction == "" {
		t.Error("PlannedAction should not be empty")
	}
}

// TestComputePlan_ThreeAppsMixed verifies mixed install/skip classification
// with three apps where only some are installed.
// (Pester: Planner.SkipLogic - "Should correctly classify mixed install/skip
// scenario")
func TestComputePlan_ThreeAppsMixed(t *testing.T) {
	t.Run("two installed one missing", func(t *testing.T) {
		drv := &testDriver{installed: map[string]bool{
			"Test.App1": true,
			"Test.App3": true,
		}}

		mf := &manifest.Manifest{
			Apps: []manifest.App{
				{ID: "app-1", Refs: map[string]string{"windows": "Test.App1"}},
				{ID: "app-2", Refs: map[string]string{"windows": "Test.App2"}},
				{ID: "app-3", Refs: map[string]string{"windows": "Test.App3"}},
			},
		}

		plan, err := ComputePlan(mf, drv)
		if err != nil {
			t.Fatalf("ComputePlan failed: %v", err)
		}

		if plan.Summary.ToInstall != 1 {
			t.Errorf("expected toInstall=1, got %d", plan.Summary.ToInstall)
		}
		if plan.Summary.AlreadyPresent != 2 {
			t.Errorf("expected alreadyPresent=2, got %d", plan.Summary.AlreadyPresent)
		}
	})

	t.Run("one installed two missing", func(t *testing.T) {
		drv := &testDriver{installed: map[string]bool{
			"Test.App2": true,
		}}

		mf := &manifest.Manifest{
			Apps: []manifest.App{
				{ID: "app-1", Refs: map[string]string{"windows": "Test.App1"}},
				{ID: "app-2", Refs: map[string]string{"windows": "Test.App2"}},
				{ID: "app-3", Refs: map[string]string{"windows": "Test.App3"}},
			},
		}

		plan, err := ComputePlan(mf, drv)
		if err != nil {
			t.Fatalf("ComputePlan failed: %v", err)
		}

		if plan.Summary.ToInstall != 2 {
			t.Errorf("expected toInstall=2, got %d", plan.Summary.ToInstall)
		}
		if plan.Summary.AlreadyPresent != 1 {
			t.Errorf("expected alreadyPresent=1, got %d", plan.Summary.AlreadyPresent)
		}
	})
}

// TestComputePlan_SummaryTotalEqualsSumOfParts verifies that the total count
// equals the sum of all classification counts.
// (Pester: Planner.SummaryCountsAccuracy - "Should have total actions equal
// to sum of all types")
func TestComputePlan_SummaryTotalEqualsSumOfParts(t *testing.T) {
	drv := &testDriver{installed: map[string]bool{"Test.App2": true}}

	mf := &manifest.Manifest{
		Apps: []manifest.App{
			{ID: "app-1", Refs: map[string]string{"windows": "Test.App1"}},
			{ID: "app-2", Refs: map[string]string{"windows": "Test.App2"}},
			{ID: "app-3", Refs: map[string]string{"windows": "Test.App3"}},
		},
	}

	plan, err := ComputePlan(mf, drv)
	if err != nil {
		t.Fatalf("ComputePlan failed: %v", err)
	}

	sum := plan.Summary.ToInstall + plan.Summary.AlreadyPresent + plan.Summary.Skipped
	if plan.Summary.Total != sum {
		t.Errorf("Total=%d does not equal ToInstall(%d)+AlreadyPresent(%d)+Skipped(%d)=%d",
			plan.Summary.Total, plan.Summary.ToInstall, plan.Summary.AlreadyPresent, plan.Summary.Skipped, sum)
	}
	if plan.Summary.Total != len(plan.Actions) {
		t.Errorf("Total=%d does not equal len(Actions)=%d", plan.Summary.Total, len(plan.Actions))
	}
}

// TestComputePlan_FallbackRef verifies that when no "windows" ref exists, the
// planner falls back to another platform ref.
// (Pester implicitly tests this via multi-platform ref handling)
func TestComputePlan_FallbackRef(t *testing.T) {
	drv := &testDriver{installed: map[string]bool{"cross-plat-brew": true}}

	mf := &manifest.Manifest{
		Apps: []manifest.App{
			{ID: "cross", Refs: map[string]string{"macos": "cross-plat-brew"}},
		},
	}

	plan, err := ComputePlan(mf, drv)
	if err != nil {
		t.Fatalf("ComputePlan failed: %v", err)
	}

	// Should fall back to the macos ref since no windows ref exists
	if len(plan.Actions) != 1 {
		t.Fatalf("expected 1 action (fallback ref), got %d", len(plan.Actions))
	}
	if plan.Actions[0].Ref != "cross-plat-brew" {
		t.Errorf("expected ref=%q from fallback, got %q", "cross-plat-brew", plan.Actions[0].Ref)
	}
	if plan.Summary.AlreadyPresent != 1 {
		t.Errorf("expected alreadyPresent=1, got %d", plan.Summary.AlreadyPresent)
	}
}

// TestComputePlan_WindowsRefPreferred verifies that when both "windows" and
// another platform ref exist, "windows" is preferred.
func TestComputePlan_WindowsRefPreferred(t *testing.T) {
	drv := &testDriver{installed: map[string]bool{"Win.App": true}}

	mf := &manifest.Manifest{
		Apps: []manifest.App{
			{ID: "multi", Refs: map[string]string{
				"windows": "Win.App",
				"linux":   "linux-app",
			}},
		},
	}

	plan, err := ComputePlan(mf, drv)
	if err != nil {
		t.Fatalf("ComputePlan failed: %v", err)
	}

	if len(plan.Actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(plan.Actions))
	}
	if plan.Actions[0].Ref != "Win.App" {
		t.Errorf("expected ref=%q (windows preferred), got %q", "Win.App", plan.Actions[0].Ref)
	}
}

// ---------------------------------------------------------------------------
// Manual app tests
// ---------------------------------------------------------------------------

// TestComputePlan_ManualAppIncluded verifies that an app with manual.verifyPath
// but no refs.windows enters the plan.
func TestComputePlan_ManualAppIncluded(t *testing.T) {
	// Create a temp file to simulate the verifyPath existing.
	tmpDir := t.TempDir()
	tmpFile := tmpDir + "/app.exe"
	os.WriteFile(tmpFile, []byte("fake"), 0644)

	drv := &testDriver{installed: map[string]bool{}}

	mf := &manifest.Manifest{
		Apps: []manifest.App{
			{
				ID: "manual-app",
				Manual: &manifest.ManualApp{
					VerifyPath: tmpFile,
				},
			},
		},
	}

	plan, err := ComputePlan(mf, drv)
	if err != nil {
		t.Fatalf("ComputePlan failed: %v", err)
	}

	if len(plan.Actions) != 1 {
		t.Fatalf("expected 1 action for manual app, got %d", len(plan.Actions))
	}
	if plan.Actions[0].Driver != "manual" {
		t.Errorf("expected driver=manual, got %q", plan.Actions[0].Driver)
	}
	if plan.Actions[0].CurrentStatus != "present" {
		t.Errorf("expected currentStatus=present (file exists), got %q", plan.Actions[0].CurrentStatus)
	}
	if plan.Summary.AlreadyPresent != 1 {
		t.Errorf("expected alreadyPresent=1, got %d", plan.Summary.AlreadyPresent)
	}
}

// TestComputePlan_ManualAppMissing verifies that a manual app with a non-existent
// verifyPath shows as missing/install in the plan.
func TestComputePlan_ManualAppMissing(t *testing.T) {
	drv := &testDriver{installed: map[string]bool{}}

	mf := &manifest.Manifest{
		Apps: []manifest.App{
			{
				ID: "manual-missing",
				Manual: &manifest.ManualApp{
					VerifyPath: "/nonexistent/path/app.exe",
				},
			},
		},
	}

	plan, err := ComputePlan(mf, drv)
	if err != nil {
		t.Fatalf("ComputePlan failed: %v", err)
	}

	if len(plan.Actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(plan.Actions))
	}
	if plan.Actions[0].CurrentStatus != "missing" {
		t.Errorf("expected currentStatus=missing, got %q", plan.Actions[0].CurrentStatus)
	}
	if plan.Actions[0].PlannedAction != "install" {
		t.Errorf("expected plannedAction=install, got %q", plan.Actions[0].PlannedAction)
	}
	if plan.Summary.ToInstall != 1 {
		t.Errorf("expected toInstall=1, got %d", plan.Summary.ToInstall)
	}
}

// TestComputePlan_NoRefNoManual verifies that an app with neither refs nor
// manual is excluded from the plan.
func TestComputePlan_NoRefNoManual(t *testing.T) {
	drv := &testDriver{installed: map[string]bool{}}

	mf := &manifest.Manifest{
		Apps: []manifest.App{
			{ID: "excluded-app", Refs: map[string]string{}},
		},
	}

	plan, err := ComputePlan(mf, drv)
	if err != nil {
		t.Fatalf("ComputePlan failed: %v", err)
	}

	if len(plan.Actions) != 0 {
		t.Errorf("expected 0 actions for app with no ref and no manual, got %d", len(plan.Actions))
	}
}

// TestComputePlan_DualDriver verifies that an app with both refs.windows and
// manual uses the winget driver (refs.windows takes precedence).
func TestComputePlan_DualDriver(t *testing.T) {
	drv := &testDriver{installed: map[string]bool{"Vendor.DualApp": true}}

	mf := &manifest.Manifest{
		Apps: []manifest.App{
			{
				ID:   "dual-app",
				Refs: map[string]string{"windows": "Vendor.DualApp"},
				Manual: &manifest.ManualApp{
					VerifyPath: "/some/path/app.exe",
				},
			},
		},
	}

	plan, err := ComputePlan(mf, drv)
	if err != nil {
		t.Fatalf("ComputePlan failed: %v", err)
	}

	if len(plan.Actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(plan.Actions))
	}
	if plan.Actions[0].Driver != "test" {
		t.Errorf("expected driver=test (winget), got %q", plan.Actions[0].Driver)
	}
	if plan.Actions[0].Ref != "Vendor.DualApp" {
		t.Errorf("expected ref=Vendor.DualApp, got %q", plan.Actions[0].Ref)
	}
}

// TestComputePlan_ManualEnvExpansion verifies that environment variables in
// verifyPath are expanded correctly.
func TestComputePlan_ManualEnvExpansion(t *testing.T) {
	tmpDir := t.TempDir()
	os.WriteFile(tmpDir+"/expanded.exe", []byte("fake"), 0644)

	t.Setenv("ENDSTATE_MANUAL_TEST_DIR", tmpDir)

	drv := &testDriver{installed: map[string]bool{}}

	mf := &manifest.Manifest{
		Apps: []manifest.App{
			{
				ID: "env-manual",
				Manual: &manifest.ManualApp{
					VerifyPath: "%ENDSTATE_MANUAL_TEST_DIR%\\expanded.exe",
				},
			},
		},
	}

	plan, err := ComputePlan(mf, drv)
	if err != nil {
		t.Fatalf("ComputePlan failed: %v", err)
	}

	if len(plan.Actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(plan.Actions))
	}
	if plan.Actions[0].CurrentStatus != "present" {
		t.Errorf("expected currentStatus=present after env expansion, got %q", plan.Actions[0].CurrentStatus)
	}
}

// ---------------------------------------------------------------------------
// Batch detection tests
// ---------------------------------------------------------------------------

// testDriverNoBatch implements only Driver (no BatchDetector), forcing
// per-ref Detect fallback.
type testDriverNoBatch struct {
	installed map[string]bool
}

func (d *testDriverNoBatch) Name() string { return "test-nobatch" }

func (d *testDriverNoBatch) Detect(ref string) (bool, string, error) {
	if d.installed[ref] {
		return true, ref + " Name", nil
	}
	return false, "", nil
}

func (d *testDriverNoBatch) Install(ref string) (*driver.InstallResult, error) {
	return nil, nil
}

// TestComputePlan_BatchDetectMatchesFallback verifies that batch detection
// produces the same plan as per-ref Detect.
func TestComputePlan_BatchDetectMatchesFallback(t *testing.T) {
	installedSet := map[string]bool{
		"Microsoft.VisualStudioCode": true,
		// Git.Git is missing
	}

	mf := &manifest.Manifest{
		Apps: []manifest.App{
			{ID: "vscode", Refs: map[string]string{"windows": "Microsoft.VisualStudioCode"}},
			{ID: "git", Refs: map[string]string{"windows": "Git.Git"}},
		},
	}

	// Plan with batch-capable driver.
	batchDrv := &testDriver{installed: installedSet}
	planBatch, err := ComputePlan(mf, batchDrv)
	if err != nil {
		t.Fatalf("ComputePlan (batch) failed: %v", err)
	}

	// Plan with non-batch driver (fallback).
	noBatchDrv := &testDriverNoBatch{installed: installedSet}
	planFallback, err := ComputePlan(mf, noBatchDrv)
	if err != nil {
		t.Fatalf("ComputePlan (fallback) failed: %v", err)
	}

	// Verify identical results.
	if planBatch.Summary.Total != planFallback.Summary.Total {
		t.Errorf("total mismatch: batch=%d fallback=%d", planBatch.Summary.Total, planFallback.Summary.Total)
	}
	if planBatch.Summary.ToInstall != planFallback.Summary.ToInstall {
		t.Errorf("toInstall mismatch: batch=%d fallback=%d", planBatch.Summary.ToInstall, planFallback.Summary.ToInstall)
	}
	if planBatch.Summary.AlreadyPresent != planFallback.Summary.AlreadyPresent {
		t.Errorf("alreadyPresent mismatch: batch=%d fallback=%d", planBatch.Summary.AlreadyPresent, planFallback.Summary.AlreadyPresent)
	}
	if len(planBatch.Actions) != len(planFallback.Actions) {
		t.Fatalf("action count mismatch: batch=%d fallback=%d", len(planBatch.Actions), len(planFallback.Actions))
	}
	for i := range planBatch.Actions {
		if planBatch.Actions[i].CurrentStatus != planFallback.Actions[i].CurrentStatus {
			t.Errorf("action[%d] status mismatch: batch=%q fallback=%q",
				i, planBatch.Actions[i].CurrentStatus, planFallback.Actions[i].CurrentStatus)
		}
		if planBatch.Actions[i].PlannedAction != planFallback.Actions[i].PlannedAction {
			t.Errorf("action[%d] planned mismatch: batch=%q fallback=%q",
				i, planBatch.Actions[i].PlannedAction, planFallback.Actions[i].PlannedAction)
		}
	}
}
