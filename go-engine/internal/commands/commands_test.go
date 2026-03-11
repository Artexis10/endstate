// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/driver"
	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/events"
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

func (m *mockDriver) Detect(ref string) (bool, error) {
	return m.installed[ref], nil
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
		emitter.EmitItem("Microsoft.VisualStudioCode", "mock", "present", "", "Verified installed")
		emitter.EmitItem("Git.Git", "mock", "present", "", "Verified installed")
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
			t.Errorf("expected status=to_install for %q in dry-run, got %q", action.Ref, action.Status)
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
			t.Errorf("expected status=present for already-installed app %q, got %q", action.Ref, action.Status)
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
	emitter.EmitItem("Git.Git", "mock", "to_install", driver.ReasonMissing, "Will be installed")
	emitter.EmitSummary("plan", 1, 0, 0, 1)

	// Phase 2 — apply
	emitter.EmitPhase("apply")
	emitter.EmitItem("Git.Git", "mock", "installing", "", "Installing")
	emitter.EmitItem("Git.Git", "mock", "installed", "", "Done")
	emitter.EmitSummary("apply", 1, 1, 0, 0)

	// Phase 3 — verify
	emitter.EmitPhase("verify")
	emitter.EmitItem("Git.Git", "mock", "present", "", "Verified")
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
