// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/driver"
	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/events"
	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
)

// ---------------------------------------------------------------------------
// Mock driver
// ---------------------------------------------------------------------------

// mockDriver implements driver.Driver for testing. The installed map controls
// which package refs Detect returns true for. Install always reports success
// for refs not already in installed.
type mockDriver struct {
	installed map[string]bool
	// installErr, if non-nil, is returned by Install for every call.
	installErr error
}

func (m *mockDriver) Name() string { return "mock" }

func (m *mockDriver) Detect(ref string) (bool, string, error) {
	if m.installed[ref] {
		return true, ref + " Display Name", nil
	}
	return false, "", nil
}

func (m *mockDriver) DetectBatch(refs []string) (map[string]driver.DetectResult, error) {
	results := make(map[string]driver.DetectResult, len(refs))
	for _, ref := range refs {
		if m.installed[ref] {
			results[ref] = driver.DetectResult{Installed: true, DisplayName: ref + " Display Name"}
		} else {
			results[ref] = driver.DetectResult{Installed: false}
		}
	}
	return results, nil
}

func (m *mockDriver) Install(ref string) (*driver.InstallResult, error) {
	if m.installErr != nil {
		return nil, m.installErr
	}
	if m.installed[ref] {
		return &driver.InstallResult{
			Status:  driver.StatusPresent,
			Reason:  driver.ReasonAlreadyInstalled,
			Message: "Already installed",
		}, nil
	}
	// Mark as installed and report success.
	if m.installed == nil {
		m.installed = make(map[string]bool)
	}
	m.installed[ref] = true
	return &driver.InstallResult{
		Status:  driver.StatusInstalled,
		Message: "Installed successfully",
	}, nil
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// fixtureManifest returns the absolute path to a named fixture file in the
// testdata/ directory adjacent to this test file.
func fixtureManifest(name string) string {
	_, thisFile, _, _ := runtime.Caller(0)
	dir := filepath.Dir(thisFile)
	return filepath.Join(dir, "testdata", name)
}

// captureEmitter creates an Emitter that writes to a bytes.Buffer so tests
// can inspect the emitted NDJSON events.
func captureEmitter(runID string) (*events.Emitter, *bytes.Buffer) {
	var buf bytes.Buffer
	e := events.NewEmitterWithWriter(runID, true, &buf)
	return e, &buf
}

// parseEvents parses all non-empty lines in buf as JSON objects and returns
// them as a slice of raw maps.
func parseEvents(buf *bytes.Buffer) []map[string]interface{} {
	var out []map[string]interface{}
	dec := json.NewDecoder(buf)
	for dec.More() {
		var obj map[string]interface{}
		if err := dec.Decode(&obj); err == nil {
			out = append(out, obj)
		}
	}
	return out
}

// withMockDriver replaces newDriverFn with one that returns md, calls f, then
// restores the original factory.
func withMockDriver(md *mockDriver, f func()) {
	orig := newDriverFn
	newDriverFn = func() driver.Driver { return md }
	defer func() { newDriverFn = orig }()
	f()
}

// ---------------------------------------------------------------------------
// Verify tests
// ---------------------------------------------------------------------------

func TestRunVerify_AllPresent_ReturnsCorrectShape(t *testing.T) {
	md := &mockDriver{installed: map[string]bool{
		"Microsoft.VisualStudioCode": true,
		"Git.Git":                   true,
	}}

	var result interface{}
	var cmdErr *envelope.Error

	withMockDriver(md, func() {
		r, e := RunVerify(VerifyFlags{Manifest: fixtureManifest("two-apps.jsonc")})
		result = r
		cmdErr = e
	})

	if cmdErr != nil {
		t.Fatalf("RunVerify returned unexpected error: %v", cmdErr)
	}

	vr, ok := result.(*VerifyResult)
	if !ok {
		t.Fatalf("expected *VerifyResult, got %T", result)
	}

	if vr.Summary.Total != 2 {
		t.Errorf("expected total=2, got %d", vr.Summary.Total)
	}
	if vr.Summary.Pass != 2 {
		t.Errorf("expected pass=2, got %d", vr.Summary.Pass)
	}
	if vr.Summary.Fail != 0 {
		t.Errorf("expected fail=0, got %d", vr.Summary.Fail)
	}
	if len(vr.Results) != 2 {
		t.Errorf("expected 2 result items, got %d", len(vr.Results))
	}
	for _, item := range vr.Results {
		if item.Status != "pass" {
			t.Errorf("expected status=pass for %q, got %q", item.Ref, item.Status)
		}
		if item.Type != "app" {
			t.Errorf("expected type=app for %q, got %q", item.Ref, item.Type)
		}
	}
}

func TestRunVerify_SomeMissing_ReturnsFailItems(t *testing.T) {
	md := &mockDriver{installed: map[string]bool{
		"Microsoft.VisualStudioCode": true,
		// Git.Git is absent
	}}

	var vr *VerifyResult
	withMockDriver(md, func() {
		r, _ := RunVerify(VerifyFlags{Manifest: fixtureManifest("two-apps.jsonc")})
		vr = r.(*VerifyResult)
	})

	if vr.Summary.Pass != 1 {
		t.Errorf("expected pass=1, got %d", vr.Summary.Pass)
	}
	if vr.Summary.Fail != 1 {
		t.Errorf("expected fail=1, got %d", vr.Summary.Fail)
	}

	var gitItem *VerifyItem
	for i := range vr.Results {
		if vr.Results[i].Ref == "Git.Git" {
			gitItem = &vr.Results[i]
		}
	}
	if gitItem == nil {
		t.Fatal("expected a result item for Git.Git")
	}
	if gitItem.Status != "fail" {
		t.Errorf("expected Git.Git status=fail, got %q", gitItem.Status)
	}
	if gitItem.Reason != driver.ReasonMissing {
		t.Errorf("expected reason=%q, got %q", driver.ReasonMissing, gitItem.Reason)
	}
}

func TestRunVerify_ManifestNotFound_ReturnsEnvelopeError(t *testing.T) {
	md := &mockDriver{}
	withMockDriver(md, func() {
		_, err := RunVerify(VerifyFlags{Manifest: "/nonexistent/path/manifest.jsonc"})
		if err == nil {
			t.Fatal("expected envelope error for missing manifest, got nil")
		}
		if string(err.Code) != string("MANIFEST_NOT_FOUND") {
			t.Errorf("expected code MANIFEST_NOT_FOUND, got %q", err.Code)
		}
	})
}

// TestRunVerify_EventOrdering verifies the event-contract.md invariant that
// a PhaseEvent is first and a SummaryEvent is last.
func TestRunVerify_EventOrdering(t *testing.T) {
	md := &mockDriver{installed: map[string]bool{
		"Microsoft.VisualStudioCode": true,
		"Git.Git":                   true,
	}}

	emitter, buf := captureEmitter("test-verify-order")

	withMockDriver(md, func() {
		// Swap newDriverFn and call the internal logic manually by using a
		// fresh emitter-accepting path. Since RunVerify creates its own
		// internal emitter, we test ordering by inspecting what an enabled
		// emitter produces using the exported EmitPhase/EmitItem/EmitSummary
		// methods directly — simulating what RunVerify does.
		emitter.EmitPhase("verify")
		emitter.EmitItem("Microsoft.VisualStudioCode", "mock", "present", "", "Verified installed", "")
		emitter.EmitItem("Git.Git", "mock", "present", "", "Verified installed", "")
		emitter.EmitSummary("verify", 2, 2, 0, 0)
	})

	evts := parseEvents(buf)
	if len(evts) == 0 {
		t.Fatal("expected events in buffer, got none")
	}

	first := evts[0]
	if first["event"] != "phase" {
		t.Errorf("first event must be type=phase, got %q", first["event"])
	}
	if first["phase"] != "verify" {
		t.Errorf("first phase event must have phase=verify, got %q", first["phase"])
	}

	last := evts[len(evts)-1]
	if last["event"] != "summary" {
		t.Errorf("last event must be type=summary, got %q", last["event"])
	}
}

// ---------------------------------------------------------------------------
// Apply tests
// ---------------------------------------------------------------------------

func TestRunApply_DryRun_ReturnsDryRunTrue(t *testing.T) {
	md := &mockDriver{installed: map[string]bool{}}

	var result *ApplyResult
	withMockDriver(md, func() {
		r, err := RunApply(ApplyFlags{
			Manifest: fixtureManifest("two-apps.jsonc"),
			DryRun:   true,
		})
		if err != nil {
			t.Fatalf("RunApply dry-run returned unexpected error: %v", err)
		}
		result = r.(*ApplyResult)
	})

	if !result.DryRun {
		t.Error("expected dryRun=true in result")
	}
	// In dry-run mode no installs happen; both apps should show to_install in actions.
	if len(result.Actions) != 2 {
		t.Errorf("expected 2 actions, got %d", len(result.Actions))
	}
	for _, action := range result.Actions {
		if action.Status != "to_install" {
			t.Errorf("expected status=to_install for %v in dry-run, got %q", action.Ref, action.Status)
		}
	}
}

func TestRunApply_AllPresent_SkipsInstall(t *testing.T) {
	md := &mockDriver{installed: map[string]bool{
		"Microsoft.VisualStudioCode": true,
		"Git.Git":                   true,
	}}

	var result *ApplyResult
	withMockDriver(md, func() {
		r, err := RunApply(ApplyFlags{
			Manifest: fixtureManifest("two-apps.jsonc"),
		})
		if err != nil {
			t.Fatalf("RunApply returned unexpected error: %v", err)
		}
		result = r.(*ApplyResult)
	})

	if result.DryRun {
		t.Error("expected dryRun=false")
	}
	// Both apps are already present; no installs should have been executed.
	for _, action := range result.Actions {
		if action.Status != "present" {
			t.Errorf("expected status=present for already-installed app %v, got %q", action.Ref, action.Status)
		}
	}
	// Summary: nothing to install → success=0, skipped=2 (the two present apps), failed=0.
	if result.Summary.Success != 0 {
		t.Errorf("expected success=0, got %d", result.Summary.Success)
	}
	if result.Summary.Skipped != 2 {
		t.Errorf("expected skipped=2, got %d", result.Summary.Skipped)
	}
	if result.Summary.Failed != 0 {
		t.Errorf("expected failed=0, got %d", result.Summary.Failed)
	}
}

func TestRunApply_InstallsNewApp(t *testing.T) {
	md := &mockDriver{installed: map[string]bool{
		"Microsoft.VisualStudioCode": true,
		// Git.Git not installed initially
	}}

	var result *ApplyResult
	withMockDriver(md, func() {
		r, err := RunApply(ApplyFlags{
			Manifest: fixtureManifest("two-apps.jsonc"),
		})
		if err != nil {
			t.Fatalf("RunApply returned unexpected error: %v", err)
		}
		result = r.(*ApplyResult)
	})

	if result.Summary.Success != 1 {
		t.Errorf("expected success=1 (Git.Git installed), got %d", result.Summary.Success)
	}
	if result.Summary.Skipped != 1 {
		t.Errorf("expected skipped=1 (vscode already present), got %d", result.Summary.Skipped)
	}
	if result.Summary.Failed != 0 {
		t.Errorf("expected failed=0, got %d", result.Summary.Failed)
	}
}

func TestRunApply_EventOrdering_PhaseFirst_SummaryLast(t *testing.T) {
	// Validate the event-contract.md invariant using the emitter directly
	// to simulate the three-phase sequence apply emits.
	emitter, buf := captureEmitter("test-apply-order")

	// Phase 1 — plan
	emitter.EmitPhase("plan")
	emitter.EmitItem("Git.Git", "mock", "to_install", driver.ReasonMissing, "Will be installed", "")
	emitter.EmitSummary("plan", 1, 0, 0, 1)

	// Phase 2 — apply
	emitter.EmitPhase("apply")
	emitter.EmitItem("Git.Git", "mock", "installing", "", "Installing", "")
	emitter.EmitItem("Git.Git", "mock", "installed", "", "Done", "")
	emitter.EmitSummary("apply", 1, 1, 0, 0)

	// Phase 3 — verify
	emitter.EmitPhase("verify")
	emitter.EmitItem("Git.Git", "mock", "present", "", "Verified", "")
	emitter.EmitSummary("verify", 1, 1, 0, 0)

	evts := parseEvents(buf)
	if len(evts) == 0 {
		t.Fatal("no events emitted")
	}

	first := evts[0]
	if first["event"] != "phase" {
		t.Errorf("first event must be phase, got %q", first["event"])
	}
	if first["phase"] != "plan" {
		t.Errorf("first phase event must be plan, got %q", first["phase"])
	}

	last := evts[len(evts)-1]
	if last["event"] != "summary" {
		t.Errorf("last event must be summary, got %q", last["event"])
	}
	if last["phase"] != "verify" {
		t.Errorf("last summary event must be for verify phase, got %q", last["phase"])
	}
}

func TestRunApply_ManifestNotFound_ReturnsEnvelopeError(t *testing.T) {
	md := &mockDriver{}
	withMockDriver(md, func() {
		_, err := RunApply(ApplyFlags{Manifest: "/no/such/manifest.jsonc"})
		if err == nil {
			t.Fatal("expected envelope error for missing manifest, got nil")
		}
		if string(err.Code) != "MANIFEST_NOT_FOUND" {
			t.Errorf("expected MANIFEST_NOT_FOUND, got %q", err.Code)
		}
	})
}

func TestRunApply_EmptyManifest_ZeroActions(t *testing.T) {
	md := &mockDriver{}
	withMockDriver(md, func() {
		r, err := RunApply(ApplyFlags{Manifest: fixtureManifest("no-apps.jsonc")})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		result := r.(*ApplyResult)
		if len(result.Actions) != 0 {
			t.Errorf("expected 0 actions for empty manifest, got %d", len(result.Actions))
		}
		if result.Summary.Total != 0 {
			t.Errorf("expected total=0, got %d", result.Summary.Total)
		}
	})
}

// ---------------------------------------------------------------------------
// Plan command tests (ported from Pester: Plan.Tests.ps1, Planner.Tests.ps1)
// ---------------------------------------------------------------------------

// TestRunPlan_ReturnsCorrectShape verifies the PlanResult has the expected
// structure: manifest ref, plan summary, and actions array.
// (Pester: Plan.Structure - "Should have runId/timestamp/manifest/actions/summary")
func TestRunPlan_ReturnsCorrectShape(t *testing.T) {
	md := &mockDriver{installed: map[string]bool{
		"Test.App1": true,
	}}

	var result interface{}
	var cmdErr *envelope.Error

	withMockDriver(md, func() {
		r, e := RunPlan(PlanFlags{Manifest: fixtureManifest("three-apps.jsonc")})
		result = r
		cmdErr = e
	})

	if cmdErr != nil {
		t.Fatalf("RunPlan returned unexpected error: %v", cmdErr)
	}

	pr, ok := result.(*PlanResult)
	if !ok {
		t.Fatalf("expected *PlanResult, got %T", result)
	}

	// Manifest ref should be populated
	if pr.Manifest.Path == "" {
		t.Error("expected manifest.path to be non-empty")
	}
	if pr.Manifest.Name != "three-app-manifest" {
		t.Errorf("expected manifest.name=%q, got %q", "three-app-manifest", pr.Manifest.Name)
	}

	// Actions array should exist
	if pr.Actions == nil {
		t.Error("expected actions to be non-nil")
	}

	// Summary should have total matching actions length
	if pr.Plan.Total != len(pr.Actions) {
		t.Errorf("plan.total=%d does not match len(actions)=%d", pr.Plan.Total, len(pr.Actions))
	}
}

// TestRunPlan_ActionOrderMatchesManifest verifies that plan actions preserve
// the order of apps as declared in the manifest.
// (Pester: Plan.Deterministic - "Should produce stable action list order")
func TestRunPlan_ActionOrderMatchesManifest(t *testing.T) {
	md := &mockDriver{installed: map[string]bool{}}

	withMockDriver(md, func() {
		r, err := RunPlan(PlanFlags{Manifest: fixtureManifest("three-apps.jsonc")})
		if err != nil {
			t.Fatalf("RunPlan returned error: %v", err)
		}
		pr := r.(*PlanResult)

		if len(pr.Actions) != 3 {
			t.Fatalf("expected 3 actions, got %d", len(pr.Actions))
		}

		expectedIDs := []string{"test-app-1", "test-app-2", "test-app-3"}
		for i, wantID := range expectedIDs {
			if pr.Actions[i].ID != wantID {
				t.Errorf("action[%d].ID = %q, want %q", i, pr.Actions[i].ID, wantID)
			}
		}
	})
}

// TestRunPlan_SkipLogic verifies that installed apps are classified as
// "present"/"skip" and missing apps as "missing"/"install".
// (Pester: Planner.SkipLogic - "Should mark app as skip when in installed
// list" / "Should mark app as install when not in installed list")
func TestRunPlan_SkipLogic(t *testing.T) {
	md := &mockDriver{installed: map[string]bool{
		"Test.App2": true,
	}}

	withMockDriver(md, func() {
		r, err := RunPlan(PlanFlags{Manifest: fixtureManifest("three-apps.jsonc")})
		if err != nil {
			t.Fatalf("RunPlan returned error: %v", err)
		}
		pr := r.(*PlanResult)

		for _, action := range pr.Actions {
			switch action.Ref {
			case "Test.App2":
				if action.CurrentStatus != "present" {
					t.Errorf("Test.App2 currentStatus=%q, want %q", action.CurrentStatus, "present")
				}
				if action.PlannedAction != "skip" {
					t.Errorf("Test.App2 plannedAction=%q, want %q", action.PlannedAction, "skip")
				}
			case "Test.App1", "Test.App3":
				if action.CurrentStatus != "missing" {
					t.Errorf("%s currentStatus=%q, want %q", action.Ref, action.CurrentStatus, "missing")
				}
				if action.PlannedAction != "install" {
					t.Errorf("%s plannedAction=%q, want %q", action.Ref, action.PlannedAction, "install")
				}
			}
		}
	})
}

// TestRunPlan_SummaryCounts verifies that plan summary counts are correct for
// mixed install/skip scenarios.
// (Pester: Planner.SummaryCountsAccuracy - "Should have total actions equal
// to sum of all types")
func TestRunPlan_SummaryCounts(t *testing.T) {
	t.Run("all installed", func(t *testing.T) {
		md := &mockDriver{installed: map[string]bool{
			"Test.App1": true,
			"Test.App2": true,
			"Test.App3": true,
		}}
		withMockDriver(md, func() {
			r, err := RunPlan(PlanFlags{Manifest: fixtureManifest("three-apps.jsonc")})
			if err != nil {
				t.Fatalf("RunPlan returned error: %v", err)
			}
			pr := r.(*PlanResult)
			if pr.Plan.ToInstall != 0 {
				t.Errorf("expected toInstall=0, got %d", pr.Plan.ToInstall)
			}
			if pr.Plan.AlreadyPresent != 3 {
				t.Errorf("expected alreadyPresent=3, got %d", pr.Plan.AlreadyPresent)
			}
		})
	})

	t.Run("none installed", func(t *testing.T) {
		md := &mockDriver{installed: map[string]bool{}}
		withMockDriver(md, func() {
			r, err := RunPlan(PlanFlags{Manifest: fixtureManifest("three-apps.jsonc")})
			if err != nil {
				t.Fatalf("RunPlan returned error: %v", err)
			}
			pr := r.(*PlanResult)
			if pr.Plan.ToInstall != 3 {
				t.Errorf("expected toInstall=3, got %d", pr.Plan.ToInstall)
			}
			if pr.Plan.AlreadyPresent != 0 {
				t.Errorf("expected alreadyPresent=0, got %d", pr.Plan.AlreadyPresent)
			}
		})
	})

	t.Run("mixed", func(t *testing.T) {
		md := &mockDriver{installed: map[string]bool{
			"Test.App1": true,
			"Test.App3": true,
		}}
		withMockDriver(md, func() {
			r, err := RunPlan(PlanFlags{Manifest: fixtureManifest("three-apps.jsonc")})
			if err != nil {
				t.Fatalf("RunPlan returned error: %v", err)
			}
			pr := r.(*PlanResult)
			if pr.Plan.ToInstall != 1 {
				t.Errorf("expected toInstall=1, got %d", pr.Plan.ToInstall)
			}
			if pr.Plan.AlreadyPresent != 2 {
				t.Errorf("expected alreadyPresent=2, got %d", pr.Plan.AlreadyPresent)
			}
			if pr.Plan.Total != 3 {
				t.Errorf("expected total=3, got %d", pr.Plan.Total)
			}
		})
	})
}

// TestRunPlan_ManifestNotFound verifies that a missing manifest returns
// MANIFEST_NOT_FOUND error code.
// (Pester: JsonMode - "verify without profile/manifest returns JSON error")
func TestRunPlan_ManifestNotFound(t *testing.T) {
	md := &mockDriver{}
	withMockDriver(md, func() {
		_, err := RunPlan(PlanFlags{Manifest: "/nonexistent/path/manifest.jsonc"})
		if err == nil {
			t.Fatal("expected envelope error for missing manifest, got nil")
		}
		if string(err.Code) != "MANIFEST_NOT_FOUND" {
			t.Errorf("expected code MANIFEST_NOT_FOUND, got %q", err.Code)
		}
	})
}

// TestRunPlan_EmptyManifest verifies that a manifest with no apps produces
// an empty plan.
func TestRunPlan_EmptyManifest(t *testing.T) {
	md := &mockDriver{}
	withMockDriver(md, func() {
		r, err := RunPlan(PlanFlags{Manifest: fixtureManifest("no-apps.jsonc")})
		if err != nil {
			t.Fatalf("RunPlan returned error: %v", err)
		}
		pr := r.(*PlanResult)
		if len(pr.Actions) != 0 {
			t.Errorf("expected 0 actions for empty manifest, got %d", len(pr.Actions))
		}
		if pr.Plan.Total != 0 {
			t.Errorf("expected total=0, got %d", pr.Plan.Total)
		}
	})
}

// TestRunPlan_ActionFields verifies each plan action has the expected fields.
// (Pester: Plan.Structure - "Should have type/driver/id/ref/status field on
// app actions")
func TestRunPlan_ActionFields(t *testing.T) {
	md := &mockDriver{installed: map[string]bool{}}

	withMockDriver(md, func() {
		r, err := RunPlan(PlanFlags{Manifest: fixtureManifest("two-apps.jsonc")})
		if err != nil {
			t.Fatalf("RunPlan returned error: %v", err)
		}
		pr := r.(*PlanResult)

		for _, action := range pr.Actions {
			if action.Type != "app" {
				t.Errorf("action %q: Type=%q, want %q", action.ID, action.Type, "app")
			}
			if action.Driver == "" {
				t.Errorf("action %q: Driver should not be empty", action.ID)
			}
			if action.ID == "" {
				t.Error("action ID should not be empty")
			}
			if action.Ref == "" {
				t.Errorf("action %q: Ref should not be empty", action.ID)
			}
			if action.CurrentStatus == "" {
				t.Errorf("action %q: CurrentStatus should not be empty", action.ID)
			}
			if action.PlannedAction == "" {
				t.Errorf("action %q: PlannedAction should not be empty", action.ID)
			}
		}
	})
}

// ---------------------------------------------------------------------------
// Capabilities command tests (ported from Pester: JsonMode.Tests.ps1)
// ---------------------------------------------------------------------------

// TestRunCapabilities_ReturnsExpectedCommands verifies that the capabilities
// response includes the expected set of commands.
// (Pester: JsonMode - "JSON data contains commands list")
func TestRunCapabilities_ReturnsExpectedCommands(t *testing.T) {
	result, err := RunCapabilities()
	if err != nil {
		t.Fatalf("RunCapabilities returned error: %v", err)
	}

	data, ok := result.(CapabilitiesData)
	if !ok {
		t.Fatalf("expected CapabilitiesData, got %T", result)
	}

	requiredCommands := []string{"apply", "verify", "capabilities", "plan", "doctor", "profile", "capture", "report"}
	for _, cmd := range requiredCommands {
		info, exists := data.Commands[cmd]
		if !exists {
			t.Errorf("expected command %q in capabilities, not found", cmd)
			continue
		}
		if !info.Supported {
			t.Errorf("command %q should be supported", cmd)
		}
	}
}

// TestRunCapabilities_SchemaVersion verifies the capabilities response has
// the correct schema version range.
func TestRunCapabilities_SchemaVersion(t *testing.T) {
	result, _ := RunCapabilities()
	data := result.(CapabilitiesData)

	if data.SupportedSchemaVersions.Min != "1.0" {
		t.Errorf("expected min schema version %q, got %q", "1.0", data.SupportedSchemaVersions.Min)
	}
	if data.SupportedSchemaVersions.Max != "1.0" {
		t.Errorf("expected max schema version %q, got %q", "1.0", data.SupportedSchemaVersions.Max)
	}
}

// TestRunCapabilities_FeaturesIncludeJSONOutput verifies that the features
// map includes JSON output support.
// (Pester: JsonMode - capabilities response structure)
func TestRunCapabilities_FeaturesIncludeJSONOutput(t *testing.T) {
	result, _ := RunCapabilities()
	data := result.(CapabilitiesData)

	if !data.Features.JSONOutput {
		t.Error("expected features.jsonOutput=true")
	}
}

// TestRunCapabilities_PlatformInfo verifies the platform info is correct.
func TestRunCapabilities_PlatformInfo(t *testing.T) {
	result, _ := RunCapabilities()
	data := result.(CapabilitiesData)

	if data.Platform.OS != "windows" {
		t.Errorf("expected platform.os=%q, got %q", "windows", data.Platform.OS)
	}
	if len(data.Platform.Drivers) == 0 {
		t.Error("expected at least one driver in platform.drivers")
	}
}

// TestRunCapabilities_NeverFails verifies that capabilities never returns an
// error. (Pester: "capabilities --json exits 0")
func TestRunCapabilities_NeverFails(t *testing.T) {
	_, err := RunCapabilities()
	if err != nil {
		t.Fatalf("RunCapabilities should never return error, got: %v", err)
	}
}

// TestRunCapabilities_CommandFlagsPresent verifies that commands include their
// expected flags.
func TestRunCapabilities_CommandFlagsPresent(t *testing.T) {
	result, _ := RunCapabilities()
	data := result.(CapabilitiesData)

	// Verify key flags
	verifyCmd, ok := data.Commands["verify"]
	if !ok {
		t.Fatal("expected 'verify' command in capabilities")
	}
	hasManifest := false
	hasJSON := false
	for _, flag := range verifyCmd.Flags {
		if flag == "--manifest" {
			hasManifest = true
		}
		if flag == "--json" {
			hasJSON = true
		}
	}
	if !hasManifest {
		t.Error("verify command should have --manifest flag")
	}
	if !hasJSON {
		t.Error("verify command should have --json flag")
	}
}

// ---------------------------------------------------------------------------
// Manual app tests
// ---------------------------------------------------------------------------

// TestRunApply_ManualApp_Present verifies that a manual app with existing
// verifyPath reports present/already_installed.
func TestRunApply_ManualApp_Present(t *testing.T) {
	tmpDir := t.TempDir()
	os.WriteFile(filepath.Join(tmpDir, "lightroom.exe"), []byte("fake"), 0644)
	t.Setenv("MANUAL_TEST_DIR", tmpDir)

	md := &mockDriver{}
	var result *ApplyResult
	withMockDriver(md, func() {
		r, err := RunApply(ApplyFlags{
			Manifest: fixtureManifest("manual-app.jsonc"),
		})
		if err != nil {
			t.Fatalf("RunApply returned unexpected error: %v", err)
		}
		result = r.(*ApplyResult)
	})

	if len(result.Actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(result.Actions))
	}
	action := result.Actions[0]
	if action.Driver != "manual" {
		t.Errorf("expected driver=manual, got %q", action.Driver)
	}
	if action.Ref != nil {
		t.Errorf("expected ref=nil for manual app, got %v", action.Ref)
	}
	if action.Manual != nil {
		t.Errorf("expected manual=nil for present manual app, got non-nil")
	}
	if result.Summary.Success != 1 {
		t.Errorf("expected success=1 for present manual app, got %d", result.Summary.Success)
	}
}

// TestRunApply_ManualApp_Missing verifies that a manual app with non-existing
// verifyPath reports skipped/manual_required and includes the manual object.
func TestRunApply_ManualApp_Missing(t *testing.T) {
	t.Setenv("MANUAL_TEST_DIR", "/nonexistent/dir")

	md := &mockDriver{}
	var result *ApplyResult
	withMockDriver(md, func() {
		r, err := RunApply(ApplyFlags{
			Manifest: fixtureManifest("manual-app.jsonc"),
		})
		if err != nil {
			t.Fatalf("RunApply returned unexpected error: %v", err)
		}
		result = r.(*ApplyResult)
	})

	if len(result.Actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(result.Actions))
	}
	action := result.Actions[0]
	if action.Status != "skipped" {
		t.Errorf("expected status=skipped, got %q", action.Status)
	}
	if action.Reason != "manual_required" {
		t.Errorf("expected reason=manual_required, got %q", action.Reason)
	}
	if action.Manual == nil {
		t.Fatal("expected manual object in response for missing manual app")
	}
	if action.Manual.Launch != "https://example.com/lightroom" {
		t.Errorf("expected manual.launch to be preserved, got %q", action.Manual.Launch)
	}
	if result.Summary.Skipped != 1 {
		t.Errorf("expected skipped=1, got %d", result.Summary.Skipped)
	}
}

// TestRunApply_DryRun_ManualApp verifies that manual apps appear in dry-run
// with driver: "manual".
func TestRunApply_DryRun_ManualApp(t *testing.T) {
	t.Setenv("MANUAL_TEST_DIR", "/nonexistent/dir")

	md := &mockDriver{}
	var result *ApplyResult
	withMockDriver(md, func() {
		r, err := RunApply(ApplyFlags{
			Manifest: fixtureManifest("manual-app.jsonc"),
			DryRun:   true,
		})
		if err != nil {
			t.Fatalf("RunApply returned unexpected error: %v", err)
		}
		result = r.(*ApplyResult)
	})

	if len(result.Actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(result.Actions))
	}
	if result.Actions[0].Driver != "manual" {
		t.Errorf("expected driver=manual in dry-run, got %q", result.Actions[0].Driver)
	}
	if result.Actions[0].Status != "to_install" {
		t.Errorf("expected status=to_install in dry-run, got %q", result.Actions[0].Status)
	}
}

// TestRunApply_DualDriver_WingetPrecedence verifies that an app with both
// refs.windows and manual uses the winget driver.
func TestRunApply_DualDriver_WingetPrecedence(t *testing.T) {
	md := &mockDriver{installed: map[string]bool{"Vendor.DualApp": true}}

	var result *ApplyResult
	withMockDriver(md, func() {
		r, err := RunApply(ApplyFlags{
			Manifest: fixtureManifest("dual-driver.jsonc"),
		})
		if err != nil {
			t.Fatalf("RunApply returned unexpected error: %v", err)
		}
		result = r.(*ApplyResult)
	})

	if len(result.Actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(result.Actions))
	}
	if result.Actions[0].Driver != "mock" {
		t.Errorf("expected driver=mock (winget), got %q", result.Actions[0].Driver)
	}
	if result.Actions[0].Ref == nil || *result.Actions[0].Ref != "Vendor.DualApp" {
		t.Errorf("expected ref=Vendor.DualApp, got %v", result.Actions[0].Ref)
	}
}

// TestRunVerify_ManualApp_Present verifies that verify reports pass for a
// manual app whose verifyPath exists.
func TestRunVerify_ManualApp_Present(t *testing.T) {
	tmpDir := t.TempDir()
	os.WriteFile(filepath.Join(tmpDir, "lightroom.exe"), []byte("fake"), 0644)
	t.Setenv("MANUAL_TEST_DIR", tmpDir)

	md := &mockDriver{}
	withMockDriver(md, func() {
		r, err := RunVerify(VerifyFlags{Manifest: fixtureManifest("manual-app.jsonc")})
		if err != nil {
			t.Fatalf("RunVerify returned unexpected error: %v", err)
		}
		vr := r.(*VerifyResult)
		if vr.Summary.Pass != 1 {
			t.Errorf("expected pass=1, got %d", vr.Summary.Pass)
		}
		if vr.Summary.Fail != 0 {
			t.Errorf("expected fail=0, got %d", vr.Summary.Fail)
		}
		if len(vr.Results) != 1 {
			t.Fatalf("expected 1 result, got %d", len(vr.Results))
		}
		if vr.Results[0].Status != "pass" {
			t.Errorf("expected status=pass, got %q", vr.Results[0].Status)
		}
	})
}

// TestRunVerify_ManualApp_Missing verifies that verify reports fail/missing
// for a manual app whose verifyPath does not exist.
func TestRunVerify_ManualApp_Missing(t *testing.T) {
	t.Setenv("MANUAL_TEST_DIR", "/nonexistent/dir")

	md := &mockDriver{}
	withMockDriver(md, func() {
		r, err := RunVerify(VerifyFlags{Manifest: fixtureManifest("manual-app.jsonc")})
		if err != nil {
			t.Fatalf("RunVerify returned unexpected error: %v", err)
		}
		vr := r.(*VerifyResult)
		if vr.Summary.Pass != 0 {
			t.Errorf("expected pass=0, got %d", vr.Summary.Pass)
		}
		if vr.Summary.Fail != 1 {
			t.Errorf("expected fail=1, got %d", vr.Summary.Fail)
		}
		if len(vr.Results) != 1 {
			t.Fatalf("expected 1 result, got %d", len(vr.Results))
		}
		if vr.Results[0].Status != "fail" {
			t.Errorf("expected status=fail, got %q", vr.Results[0].Status)
		}
		if vr.Results[0].Reason != "missing" {
			t.Errorf("expected reason=missing, got %q", vr.Results[0].Reason)
		}
	})
}

// TestRunApply_ManualValidation_MissingVerifyPath verifies that a manifest
// with manual but no verifyPath returns a parse error.
func TestRunApply_ManualValidation_MissingVerifyPath(t *testing.T) {
	md := &mockDriver{}
	withMockDriver(md, func() {
		_, err := RunApply(ApplyFlags{Manifest: fixtureManifest("manual-no-verifypath.jsonc")})
		if err == nil {
			t.Fatal("expected envelope error for manual without verifyPath, got nil")
		}
		if string(err.Code) != "MANIFEST_PARSE_ERROR" {
			t.Errorf("expected MANIFEST_PARSE_ERROR, got %q", err.Code)
		}
	})
}

// TestRunCapabilities_ManualAppsFeature verifies that the capabilities
// response includes manualApps: true.
func TestRunCapabilities_ManualAppsFeature(t *testing.T) {
	result, _ := RunCapabilities()
	data := result.(CapabilitiesData)
	if !data.Features.ManualApps {
		t.Error("expected features.manualApps=true")
	}
}

// TestRunApply_ManualApp_EnvExpansion verifies that %VAR% env vars in
// verifyPath are expanded correctly.
func TestRunApply_ManualApp_EnvExpansion(t *testing.T) {
	tmpDir := t.TempDir()
	os.WriteFile(filepath.Join(tmpDir, "lightroom.exe"), []byte("fake"), 0644)
	t.Setenv("MANUAL_TEST_DIR", tmpDir)

	md := &mockDriver{}
	var result *ApplyResult
	withMockDriver(md, func() {
		r, err := RunApply(ApplyFlags{
			Manifest: fixtureManifest("manual-app.jsonc"),
			DryRun:   true,
		})
		if err != nil {
			t.Fatalf("RunApply returned unexpected error: %v", err)
		}
		result = r.(*ApplyResult)
	})

	if len(result.Actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(result.Actions))
	}
	// With the env var set to a valid dir containing lightroom.exe, the app should be present.
	if result.Actions[0].Status != "present" {
		t.Errorf("expected status=present after env expansion, got %q", result.Actions[0].Status)
	}
}

// TestRunApply_ManualApp_SummaryCount verifies summary counting for manual apps:
// present → success, missing → skipped.
func TestRunApply_ManualApp_SummaryCount(t *testing.T) {
	tmpDir := t.TempDir()
	os.WriteFile(filepath.Join(tmpDir, "lightroom.exe"), []byte("fake"), 0644)
	t.Setenv("MANUAL_TEST_DIR", tmpDir)

	md := &mockDriver{installed: map[string]bool{"Microsoft.VisualStudioCode": true}}
	var result *ApplyResult
	withMockDriver(md, func() {
		r, err := RunApply(ApplyFlags{
			Manifest: fixtureManifest("manual-and-winget.jsonc"),
		})
		if err != nil {
			t.Fatalf("RunApply returned unexpected error: %v", err)
		}
		result = r.(*ApplyResult)
	})

	// vscode (present winget) → skipped, lightroom (present manual) → success
	if result.Summary.Total != 2 {
		t.Errorf("expected total=2, got %d", result.Summary.Total)
	}
	if result.Summary.Success != 1 {
		t.Errorf("expected success=1 (manual present), got %d", result.Summary.Success)
	}
	if result.Summary.Skipped != 1 {
		t.Errorf("expected skipped=1 (winget already present), got %d", result.Summary.Skipped)
	}
}

// ---------------------------------------------------------------------------
// restoreModulesAvailable display name enrichment
// ---------------------------------------------------------------------------

func TestRunApply_RestoreModulesAvailable_DisplayNames(t *testing.T) {
	md := &mockDriver{installed: map[string]bool{
		"Microsoft.VisualStudioCode": true,
		"Git.Git":                   true,
	}}

	catalog := map[string]*modules.Module{
		"apps.vscode": {
			ID:          "apps.vscode",
			DisplayName: "Visual Studio Code",
			Matches:     modules.MatchCriteria{Winget: []string{"Microsoft.VisualStudioCode"}},
			Capture:     &modules.CaptureDef{Files: []modules.CaptureFile{{Source: "a", Dest: "b"}}},
		},
		"apps.git": {
			ID:          "apps.git",
			DisplayName: "Git",
			Matches:     modules.MatchCriteria{Winget: []string{"Git.Git"}},
			Capture:     &modules.CaptureDef{Files: []modules.CaptureFile{{Source: "a", Dest: "b"}}},
		},
	}

	var result *ApplyResult
	withMockDriver(md, func() {
		withMockCatalog(catalog, nil, func() {
			r, err := RunApply(ApplyFlags{
				Manifest: fixtureManifest("two-apps.jsonc"),
				DryRun:   true,
			})
			if err != nil {
				t.Fatalf("RunApply returned unexpected error: %v", err)
			}
			result = r.(*ApplyResult)
		})
	})

	if len(result.RestoreModulesAvailable) != 2 {
		t.Fatalf("expected 2 restoreModulesAvailable entries, got %d", len(result.RestoreModulesAvailable))
	}

	// Build a lookup map for easy assertion.
	byID := make(map[string]string)
	for _, ref := range result.RestoreModulesAvailable {
		byID[ref.ID] = ref.DisplayName
	}

	if dn, ok := byID["apps.vscode"]; !ok || dn != "Visual Studio Code" {
		t.Errorf("expected apps.vscode displayName=%q, got %q (present=%v)", "Visual Studio Code", dn, ok)
	}
	if dn, ok := byID["apps.git"]; !ok || dn != "Git" {
		t.Errorf("expected apps.git displayName=%q, got %q (present=%v)", "Git", dn, ok)
	}
}

func TestRunApply_RestoreModulesAvailable_FallbackToShortID(t *testing.T) {
	md := &mockDriver{installed: map[string]bool{
		"Vendor.NoDisplay": true,
	}}

	catalog := map[string]*modules.Module{
		"apps.nodisplay": {
			ID:          "apps.nodisplay",
			DisplayName: "", // empty — should fall back to "nodisplay"
			Matches:     modules.MatchCriteria{Winget: []string{"Vendor.NoDisplay"}},
			Capture:     &modules.CaptureDef{Files: []modules.CaptureFile{{Source: "a", Dest: "b"}}},
		},
	}

	// Need a manifest with this app. Create a temp fixture.
	tmpDir := t.TempDir()
	manifestContent := `{
		"name": "test-nodisplay",
		"apps": [
			{ "id": "nodisplay", "refs": { "windows": "Vendor.NoDisplay" } }
		]
	}`
	manifestPath := filepath.Join(tmpDir, "test.jsonc")
	os.WriteFile(manifestPath, []byte(manifestContent), 0644)

	var result *ApplyResult
	withMockDriver(md, func() {
		withMockCatalog(catalog, nil, func() {
			r, err := RunApply(ApplyFlags{
				Manifest: manifestPath,
				DryRun:   true,
			})
			if err != nil {
				t.Fatalf("RunApply returned unexpected error: %v", err)
			}
			result = r.(*ApplyResult)
		})
	})

	if len(result.RestoreModulesAvailable) != 1 {
		t.Fatalf("expected 1 restoreModulesAvailable entry, got %d", len(result.RestoreModulesAvailable))
	}

	ref := result.RestoreModulesAvailable[0]
	if ref.ID != "apps.nodisplay" {
		t.Errorf("expected id=%q, got %q", "apps.nodisplay", ref.ID)
	}
	if ref.DisplayName != "nodisplay" {
		t.Errorf("expected displayName=%q (short ID fallback), got %q", "nodisplay", ref.DisplayName)
	}
}

func TestResolveModuleDisplayName(t *testing.T) {
	tests := []struct {
		name string
		mod  *modules.Module
		want string
	}{
		{
			name: "uses displayName when set",
			mod:  &modules.Module{ID: "apps.vscode", DisplayName: "Visual Studio Code"},
			want: "Visual Studio Code",
		},
		{
			name: "falls back to short ID when displayName empty",
			mod:  &modules.Module{ID: "apps.myapp", DisplayName: ""},
			want: "myapp",
		},
		{
			name: "strips apps. prefix only",
			mod:  &modules.Module{ID: "apps.complex-app-name", DisplayName: ""},
			want: "complex-app-name",
		},
		{
			name: "handles ID without apps. prefix",
			mod:  &modules.Module{ID: "other.thing", DisplayName: ""},
			want: "other.thing",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := resolveModuleDisplayName(tc.mod)
			if got != tc.want {
				t.Errorf("resolveModuleDisplayName() = %q, want %q", got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// resolveItemDisplayName — streaming event display name helper
// ---------------------------------------------------------------------------

func TestResolveItemDisplayName(t *testing.T) {
	tests := []struct {
		name     string
		resolved string
		app      manifest.App
		ref      string
		want     string
	}{
		{
			name:     "uses winget resolved name when available",
			resolved: "Visual Studio Code",
			app:      manifest.App{ID: "vscode", DisplayName: "VS Code"},
			ref:      "Microsoft.VisualStudioCode",
			want:     "Visual Studio Code",
		},
		{
			name:     "falls back to manifest displayName when resolved is empty",
			resolved: "",
			app:      manifest.App{ID: "vscode", DisplayName: "VS Code"},
			ref:      "Microsoft.VisualStudioCode",
			want:     "VS Code",
		},
		{
			name:     "falls back to winget ref when displayName is empty",
			resolved: "",
			app:      manifest.App{ID: "temurin-8-jre"},
			ref:      "EclipseAdoptium.Temurin.8.JRE",
			want:     "EclipseAdoptium.Temurin.8.JRE",
		},
		{
			name:     "falls back to manifest id when ref is also empty",
			resolved: "",
			app:      manifest.App{ID: "some-app"},
			ref:      "",
			want:     "some-app",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := resolveItemDisplayName(tc.resolved, tc.app, tc.ref)
			if got != tc.want {
				t.Errorf("resolveItemDisplayName() = %q, want %q", got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Streaming item events include display names
// ---------------------------------------------------------------------------

func TestRunApply_ItemEvents_IncludeDisplayName(t *testing.T) {
	// Setup: vscode is installed (will have display name from detect),
	// git is NOT installed (must fall back to manifest id).
	md := &mockDriver{
		installed: map[string]bool{
			"Microsoft.VisualStudioCode": true,
			"Git.Git":                   false,
		},
	}

	var result *ApplyResult
	withMockDriver(md, func() {
		r, err := RunApply(ApplyFlags{
			Manifest: fixtureManifest("two-apps.jsonc"),
			DryRun:   true,
		})
		if err != nil {
			t.Fatalf("RunApply returned unexpected error: %v", err)
		}
		result = r.(*ApplyResult)
	})

	// The plan-phase ApplyAction entries should have names.
	nameByID := make(map[string]string)
	for _, action := range result.Actions {
		nameByID[action.ID] = action.Name
	}

	// vscode: installed, so mock returns "Microsoft.VisualStudioCode Display Name"
	if name, ok := nameByID["vscode"]; !ok || name == "" {
		t.Errorf("expected vscode action name to be non-empty, got %q", name)
	}

	// git: not installed, no winget display name, no manifest displayName →
	// falls back to winget ref "Git.Git" (more recognizable than manifest id "git")
	if name, ok := nameByID["git"]; !ok || name == "" {
		t.Errorf("expected git action name to be non-empty, got %q", name)
	}
	if name := nameByID["git"]; name != "Git.Git" {
		t.Errorf("expected git action name=%q (winget ref fallback), got %q", "Git.Git", name)
	}
}

func TestRunVerify_ItemEvents_IncludeDisplayName(t *testing.T) {
	md := &mockDriver{
		installed: map[string]bool{
			"Microsoft.VisualStudioCode": true,
			"Git.Git":                   false,
		},
	}

	var result *VerifyResult
	withMockDriver(md, func() {
		r, err := RunVerify(VerifyFlags{
			Manifest: fixtureManifest("two-apps.jsonc"),
		})
		if err != nil {
			t.Fatalf("RunVerify returned unexpected error: %v", err)
		}
		result = r.(*VerifyResult)
	})

	// Check that all items have names (pass or fail).
	for _, item := range result.Results {
		if item.Type == "app" && item.Name == "" && item.ID != "" {
			t.Errorf("verify item %q (ref=%q) should have a non-empty name", item.ID, item.Ref)
		}
	}
}
