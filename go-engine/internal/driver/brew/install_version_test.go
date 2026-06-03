// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package brew

import (
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/driver"
)

// InstallVersion honors a versioned-formula ref (e.g. "node@20") by passing the
// version-bearing NAME through to brew unchanged — brew has no general
// `--version` flag, so the name IS the version selector.
func TestInstallVersion_VersionedFormulaRefPassthrough(t *testing.T) {
	var calls [][]string
	d := &BrewDriver{ExecCommand: scriptedCommand(map[string]scriptedResponse{
		"list":    {exitCode: 1}, // absent → proceed to install
		"install": {exitCode: 0},
	}, &calls)}

	res, err := d.InstallVersion("node@20", "20")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Status != driver.StatusInstalled {
		t.Fatalf("Status = %q, want installed", res.Status)
	}
	// The versioned-formula token must reach brew install verbatim.
	sawTarget := false
	for _, c := range calls {
		if len(c) > 1 && c[1] == "install" && strings.Contains(strings.Join(c, " "), "node@20") {
			sawTarget = true
		}
		// brew has no --version flag; we must never emit one.
		if strings.Contains(strings.Join(c, " "), "--version") {
			t.Fatalf("brew install must not pass --version (unsupported): %v", c)
		}
	}
	if !sawTarget {
		t.Fatalf("expected `brew install node@20`; calls=%v", calls)
	}
}

// A bare ref with a separately-declared version installs latest; the version is
// advisory and surfaced in the message, NOT passed as an unsupported flag.
func TestInstallVersion_BareRefVersionIsAdvisory(t *testing.T) {
	var calls [][]string
	d := &BrewDriver{ExecCommand: scriptedCommand(map[string]scriptedResponse{
		"list":    {exitCode: 1},
		"install": {exitCode: 0},
	}, &calls)}

	res, err := d.InstallVersion("ripgrep", "14.1.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Status != driver.StatusInstalled {
		t.Fatalf("Status = %q, want installed", res.Status)
	}
	if !strings.Contains(res.Message, "14.1.0") || !strings.Contains(res.Message, "advisory") {
		t.Fatalf("advisory-version message should surface the requested version and weakness: %q", res.Message)
	}
	for _, c := range calls {
		if strings.Contains(strings.Join(c, " "), "--version") {
			t.Fatalf("must not pass --version: %v", c)
		}
	}
}

// The plain Install path keeps its historical success message and passes no
// version anywhere (regression guard for the shared-impl refactor).
func TestInstall_Unpinned_PlainMessage(t *testing.T) {
	d := &BrewDriver{ExecCommand: scriptedCommand(map[string]scriptedResponse{
		"list":    {exitCode: 1},
		"install": {exitCode: 0},
	}, nil)}

	res, err := d.Install("ripgrep")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Message != "Installed successfully" {
		t.Fatalf("unpinned success message changed: %q", res.Message)
	}
}

// ReinstallVersion skips the detect-before-install short-circuit and passes
// --force, so an already-present (drifted) package is reinstalled rather than
// reported as already installed.
func TestReinstallVersion_ForcesAndSkipsShortCircuit(t *testing.T) {
	var calls [][]string
	d := &BrewDriver{ExecCommand: scriptedCommand(map[string]scriptedResponse{
		"list":    {exitCode: 0}, // present — Install would short-circuit, Reinstall must NOT
		"install": {exitCode: 0},
	}, &calls)}

	res, err := d.ReinstallVersion("node@20", "20")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Status != driver.StatusInstalled {
		t.Fatalf("Status = %q, want installed (reinstall must run despite present)", res.Status)
	}
	forced, sawInstall := false, false
	for _, c := range calls {
		joined := strings.Join(c, " ")
		if len(c) > 1 && c[1] == "install" {
			sawInstall = true
			if strings.Contains(joined, "--force") {
				forced = true
			}
		}
	}
	if !sawInstall {
		t.Fatalf("ReinstallVersion must spawn `brew install` even when present: %v", calls)
	}
	if !forced {
		t.Fatalf("ReinstallVersion must pass --force: %v", calls)
	}
}

// A failed install via InstallVersion classifies as failed/install_failed and
// surfaces the requested version in the message.
func TestInstallVersion_FailureSurfacesVersion(t *testing.T) {
	d := &BrewDriver{ExecCommand: scriptedCommand(map[string]scriptedResponse{
		"list":    {exitCode: 1},
		"install": {exitCode: 1, stderr: "Error: No available formula"},
	}, nil)}

	res, err := d.InstallVersion("ripgrep", "9.9.9")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Status != driver.StatusFailed || res.Reason != driver.ReasonInstallFailed {
		t.Fatalf("want failed/install_failed, got %q/%q", res.Status, res.Reason)
	}
	if !strings.Contains(res.Message, "9.9.9") {
		t.Fatalf("failure message should surface the requested version: %q", res.Message)
	}
}
