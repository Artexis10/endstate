// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package winget

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/driver"
	"github.com/Artexis10/endstate/go-engine/internal/snapshot"
)

// capturingCommand records the argv handed to winget (into *captured) and then
// re-invokes the helper process with the given exit code, so a test can assert
// the flags passed AND drive the simulated winget exit. Reuses TestHelperProcess
// defined in winget_test.go.
func capturingCommand(exitCode int, captured *[]string) func(string, ...string) *exec.Cmd {
	return func(name string, args ...string) *exec.Cmd {
		if captured != nil {
			*captured = append([]string{name}, args...)
		}
		cs := append([]string{"-test.run=TestHelperProcess", "--"}, args...)
		cmd := exec.Command(os.Args[0], cs...)
		cmd.Env = append(os.Environ(),
			"GO_WANT_HELPER_PROCESS=1",
			fmt.Sprintf("FAKE_EXIT_CODE=%d", exitCode),
		)
		return cmd
	}
}

func hasFlagValue(args []string, flag, value string) bool {
	for i, a := range args {
		if a == flag && i+1 < len(args) && args[i+1] == value {
			return true
		}
	}
	return false
}

// InstallVersion passes `--version <v>` and, on success, reports the pinned
// version in the message.
func TestInstallVersion_PassesVersionFlag(t *testing.T) {
	var got []string
	d := &WingetDriver{ExecCommand: capturingCommand(0, &got)}

	res, err := d.InstallVersion("Vendor.App", "1.2.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !hasFlagValue(got, "--id", "Vendor.App") {
		t.Fatalf("expected --id Vendor.App in args: %v", got)
	}
	if !hasFlagValue(got, "--version", "1.2.0") {
		t.Fatalf("expected --version 1.2.0 in args: %v", got)
	}
	if res.Status != driver.StatusInstalled {
		t.Fatalf("Status = %q, want installed", res.Status)
	}
	if !strings.Contains(res.Message, "1.2.0") {
		t.Fatalf("message should surface the pinned version: %q", res.Message)
	}
}

// An unavailable pinned version (generic non-zero exit, not the already-installed
// HRESULT) is a per-item install failure — never a silent different-version
// install.
func TestInstallVersion_UnavailableIsInstallFailed(t *testing.T) {
	d := &WingetDriver{ExecCommand: capturingCommand(1, nil)}

	res, err := d.InstallVersion("Vendor.App", "9.9.9")
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

// The unpinned Install path must NOT pass --version (regression guard for the
// shared-impl refactor) and keeps its historical success message.
func TestInstall_Unpinned_NoVersionFlag(t *testing.T) {
	var got []string
	d := &WingetDriver{ExecCommand: capturingCommand(0, &got)}

	res, err := d.Install("Vendor.App")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, a := range got {
		if a == "--version" {
			t.Fatalf("unpinned Install must not pass --version: %v", got)
		}
	}
	if res.Message != "Installed successfully" {
		t.Fatalf("unpinned success message changed: %q", res.Message)
	}
}

// ReinstallVersion passes BOTH --version and --force so an installed-but-drifted
// package is changed to the declared version (the apply --repin path).
func TestReinstallVersion_PassesVersionAndForce(t *testing.T) {
	var got []string
	d := &WingetDriver{ExecCommand: capturingCommand(0, &got)}

	res, err := d.ReinstallVersion("Vendor.App", "1.2.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !hasFlagValue(got, "--version", "1.2.0") {
		t.Fatalf("expected --version 1.2.0 in args: %v", got)
	}
	forced := false
	for _, a := range got {
		if a == "--force" {
			forced = true
		}
	}
	if !forced {
		t.Fatalf("ReinstallVersion must pass --force: %v", got)
	}
	if res.Status != driver.StatusInstalled {
		t.Fatalf("Status = %q, want installed", res.Status)
	}
}

// DetectBatch propagates the snapshot's parsed Version column into
// DetectResult.Version (capture), keyed case-insensitively by winget Id.
func TestDetectBatch_CapturesVersion(t *testing.T) {
	orig := takeSnapshotFn
	takeSnapshotFn = func() ([]snapshot.SnapshotApp, error) {
		return []snapshot.SnapshotApp{
			{Name: "Ripgrep", ID: "BurntSushi.ripgrep.MSVC", Version: "14.1.0"},
		}, nil
	}
	defer func() { takeSnapshotFn = orig }()

	d := New()
	results, err := d.DetectBatch([]string{"BurntSushi.ripgrep.MSVC", "Not.Installed"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	rg := results["BurntSushi.ripgrep.MSVC"]
	if !rg.Installed || rg.Version != "14.1.0" {
		t.Fatalf("ripgrep result = %+v, want installed with version 14.1.0", rg)
	}
	if absent := results["Not.Installed"]; absent.Installed || absent.Version != "" {
		t.Fatalf("absent ref should carry no version: %+v", absent)
	}
}
