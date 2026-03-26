// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"context"
	"os/exec"
	"testing"
)

func TestRunDoctor_ReturnsChecks(t *testing.T) {
	result, err := RunDoctor(DoctorFlags{})
	if err != nil {
		t.Fatalf("RunDoctor failed: %v", err)
	}

	dr, ok := result.(*DoctorResult)
	if !ok {
		t.Fatalf("expected *DoctorResult, got %T", result)
	}

	if len(dr.Checks) == 0 {
		t.Error("expected at least one check")
	}

	// Verify summary matches checks
	expectedTotal := dr.Summary.Pass + dr.Summary.Fail + dr.Summary.Warn
	if dr.Summary.Total != expectedTotal {
		t.Errorf("summary total=%d doesn't match pass+fail+warn=%d", dr.Summary.Total, expectedTotal)
	}
}

func TestRunDoctor_SummaryComputation(t *testing.T) {
	// Test that summary correctly counts pass/fail/warn
	result, _ := RunDoctor(DoctorFlags{})
	dr := result.(*DoctorResult)

	pass, fail, warn := 0, 0, 0
	for _, c := range dr.Checks {
		switch c.Status {
		case "pass":
			pass++
		case "fail":
			fail++
		case "warn":
			warn++
		default:
			t.Errorf("unexpected status %q for check %q", c.Status, c.Name)
		}
	}

	if dr.Summary.Pass != pass {
		t.Errorf("summary.pass=%d, counted=%d", dr.Summary.Pass, pass)
	}
	if dr.Summary.Fail != fail {
		t.Errorf("summary.fail=%d, counted=%d", dr.Summary.Fail, fail)
	}
	if dr.Summary.Warn != warn {
		t.Errorf("summary.warn=%d, counted=%d", dr.Summary.Warn, warn)
	}
}

func TestDoctorCheck_EngineVersion(t *testing.T) {
	check := checkEngineVersion()
	if check.Name != "engine-version" {
		t.Errorf("expected name=engine-version, got %q", check.Name)
	}
	if check.Status != "pass" {
		t.Errorf("expected status=pass, got %q", check.Status)
	}
	// Version should be non-empty (falls back to "0.0.0-dev")
	if check.Message == "" {
		t.Error("expected non-empty version message")
	}
}

func TestDoctorCheck_ProfilesDir(t *testing.T) {
	check := checkProfilesDir()
	if check.Name != "profiles-dir" {
		t.Errorf("expected name=profiles-dir, got %q", check.Name)
	}
	// Status can be "pass" or "warn" depending on environment
	if check.Status != "pass" && check.Status != "warn" {
		t.Errorf("expected status pass or warn, got %q", check.Status)
	}
}

func TestDoctorCheck_StateDir(t *testing.T) {
	check := checkStateDir()
	if check.Name != "state-dir" {
		t.Errorf("expected name=state-dir, got %q", check.Name)
	}
	// Status can be "pass" or "fail" depending on environment
	if check.Status != "pass" && check.Status != "fail" {
		t.Errorf("expected status pass or fail, got %q", check.Status)
	}
}

func TestCheckWinget_WithMockSuccess(t *testing.T) {
	// Save and restore the original ExecCommandContext
	orig := ExecCommandContext
	defer func() { ExecCommandContext = orig }()

	// Mock ExecCommandContext to simulate winget --version returning "v1.9.0"
	ExecCommandContext = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		// Use "echo" to simulate winget output
		return exec.CommandContext(ctx, "cmd", "/C", "echo v1.9.0")
	}

	checks := checkWinget()
	if len(checks) != 2 {
		t.Fatalf("expected 2 checks for successful winget, got %d", len(checks))
	}

	if checks[0].Name != "winget" {
		t.Errorf("expected first check name=winget, got %q", checks[0].Name)
	}
	if checks[0].Status != "pass" {
		t.Errorf("expected first check status=pass, got %q", checks[0].Status)
	}
	if checks[0].Message != "winget available" {
		t.Errorf("expected message='winget available', got %q", checks[0].Message)
	}

	if checks[1].Name != "winget-version" {
		t.Errorf("expected second check name=winget-version, got %q", checks[1].Name)
	}
	if checks[1].Status != "pass" {
		t.Errorf("expected second check status=pass, got %q", checks[1].Status)
	}
	if checks[1].Message != "v1.9.0" {
		t.Errorf("expected version message='v1.9.0', got %q", checks[1].Message)
	}
}

func TestCheckWinget_WithMockFailure(t *testing.T) {
	orig := ExecCommandContext
	defer func() { ExecCommandContext = orig }()

	// Mock ExecCommandContext to simulate winget not found
	ExecCommandContext = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "nonexistent-binary-that-does-not-exist-12345")
	}

	checks := checkWinget()
	if len(checks) != 1 {
		t.Fatalf("expected 1 check for missing winget, got %d", len(checks))
	}

	if checks[0].Name != "winget" {
		t.Errorf("expected check name=winget, got %q", checks[0].Name)
	}
	if checks[0].Status != "fail" {
		t.Errorf("expected check status=fail, got %q", checks[0].Status)
	}
	if checks[0].Message != "winget not found" {
		t.Errorf("expected message='winget not found', got %q", checks[0].Message)
	}
}

func TestRunDoctor_AllChecksHaveValidStatus(t *testing.T) {
	result, err := RunDoctor(DoctorFlags{})
	if err != nil {
		t.Fatalf("RunDoctor failed: %v", err)
	}

	dr := result.(*DoctorResult)
	validStatuses := map[string]bool{"pass": true, "fail": true, "warn": true}

	for _, check := range dr.Checks {
		if !validStatuses[check.Status] {
			t.Errorf("check %q has invalid status %q", check.Name, check.Status)
		}
		if check.Name == "" {
			t.Error("check has empty name")
		}
		if check.Message == "" {
			t.Error("check has empty message")
		}
	}
}
