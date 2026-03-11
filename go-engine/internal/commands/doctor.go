// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/config"
	"github.com/Artexis10/endstate/go-engine/internal/envelope"
)

// DoctorFlags holds the parsed CLI flags for the doctor command.
type DoctorFlags struct {
	// Events controls streaming event output. "jsonl" enables it; "" disables.
	Events string
}

// DoctorResult is the data payload for the doctor command JSON envelope.
type DoctorResult struct {
	Checks  []DoctorCheck `json:"checks"`
	Summary DoctorSummary `json:"summary"`
}

// DoctorCheck represents a single diagnostic check result.
type DoctorCheck struct {
	Name    string `json:"name"`
	Status  string `json:"status"` // "pass", "fail", "warn"
	Message string `json:"message"`
	Detail  string `json:"detail,omitempty"`
}

// DoctorSummary aggregates pass/fail/warn counts across all checks.
type DoctorSummary struct {
	Total int `json:"total"`
	Pass  int `json:"pass"`
	Fail  int `json:"fail"`
	Warn  int `json:"warn"`
}

// ExecCommandContext is the function used to create exec.Cmd instances. It
// defaults to exec.CommandContext and can be replaced in tests to inject stubs.
var ExecCommandContext = exec.CommandContext

// RunDoctor executes the doctor command, running a series of environment health
// checks and returning the results. It never returns a non-nil *envelope.Error;
// individual check failures are encoded in the DoctorResult payload.
func RunDoctor(flags DoctorFlags) (interface{}, *envelope.Error) {
	var checks []DoctorCheck

	// 1. Check winget available (returns 1-2 checks)
	checks = append(checks, checkWinget()...)

	// 2. Check PowerShell available
	checks = append(checks, checkPowerShell())

	// 3. Check profiles directory
	checks = append(checks, checkProfilesDir())

	// 4. Check state directory writable
	checks = append(checks, checkStateDir())

	// 5. Check engine version
	checks = append(checks, checkEngineVersion())

	// Build summary
	summary := DoctorSummary{Total: len(checks)}
	for _, c := range checks {
		switch c.Status {
		case "pass":
			summary.Pass++
		case "fail":
			summary.Fail++
		case "warn":
			summary.Warn++
		}
	}

	return &DoctorResult{Checks: checks, Summary: summary}, nil
}

// checkWinget verifies that winget is available and returns its version.
// Returns 1 check on failure, 2 checks (availability + version) on success.
func checkWinget() []DoctorCheck {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := ExecCommandContext(ctx, "winget", "--version")
	output, err := cmd.Output()
	if err != nil {
		return []DoctorCheck{
			{
				Name:    "winget",
				Status:  "fail",
				Message: "winget not found",
			},
		}
	}

	version := strings.TrimSpace(string(output))
	return []DoctorCheck{
		{
			Name:    "winget",
			Status:  "pass",
			Message: "winget available",
		},
		{
			Name:    "winget-version",
			Status:  "pass",
			Message: version,
			Detail:  version,
		},
	}
}

// checkPowerShell verifies that PowerShell is available and returns its version.
func checkPowerShell() DoctorCheck {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := ExecCommandContext(ctx, "powershell", "-Command", "$PSVersionTable.PSVersion.ToString()")
	output, err := cmd.Output()
	if err != nil {
		return DoctorCheck{
			Name:    "powershell",
			Status:  "warn",
			Message: "PowerShell not found",
		}
	}

	version := strings.TrimSpace(string(output))
	return DoctorCheck{
		Name:    "powershell",
		Status:  "pass",
		Message: "PowerShell " + version,
	}
}

// checkProfilesDir verifies that the profiles directory exists.
func checkProfilesDir() DoctorCheck {
	dir := config.ProfileDir()
	if dir == "" {
		return DoctorCheck{
			Name:    "profiles-dir",
			Status:  "warn",
			Message: "Profiles directory not found",
		}
	}

	if _, err := os.Stat(dir); err != nil {
		return DoctorCheck{
			Name:    "profiles-dir",
			Status:  "warn",
			Message: "Profiles directory not found",
			Detail:  dir,
		}
	}

	return DoctorCheck{
		Name:    "profiles-dir",
		Status:  "pass",
		Message: "Profiles directory exists",
		Detail:  dir,
	}
}

// checkStateDir verifies that the state directory is writable by creating and
// removing a temporary file.
func checkStateDir() DoctorCheck {
	root := config.ResolveRepoRoot()
	if root == "" {
		return DoctorCheck{
			Name:    "state-dir",
			Status:  "fail",
			Message: "State directory not writable",
			Detail:  "cannot resolve repo root",
		}
	}

	stateDir := filepath.Join(root, "state")

	// Ensure the state directory exists (create if needed).
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		return DoctorCheck{
			Name:    "state-dir",
			Status:  "fail",
			Message: "State directory not writable",
			Detail:  stateDir,
		}
	}

	// Try to write and remove a temp file.
	tmpFile := filepath.Join(stateDir, ".doctor-probe")
	if err := os.WriteFile(tmpFile, []byte("probe"), 0644); err != nil {
		return DoctorCheck{
			Name:    "state-dir",
			Status:  "fail",
			Message: "State directory not writable",
			Detail:  stateDir,
		}
	}
	os.Remove(tmpFile)

	return DoctorCheck{
		Name:    "state-dir",
		Status:  "pass",
		Message: "State directory writable",
		Detail:  stateDir,
	}
}

// checkEngineVersion reads the engine version from the VERSION file.
func checkEngineVersion() DoctorCheck {
	version := config.ReadVersion(config.ResolveRepoRoot())
	return DoctorCheck{
		Name:    "engine-version",
		Status:  "pass",
		Message: version,
		Detail:  version,
	}
}
