// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package bootstrap

import (
	"errors"
	"strings"
	"testing"
)

// Probe partitions a needed set into present (detected working) and absent
// (needs install), preserving input order.
func TestProbe_PartitionsPresentAndAbsent(t *testing.T) {
	bs := &Bootstrapper{
		Detect: func(b Backend) (bool, error) { return b == BackendBrew, nil }, // brew present, nix absent
	}
	absent, present := bs.Probe([]Backend{BackendBrew, BackendNix})
	if len(present) != 1 || present[0] != BackendBrew {
		t.Fatalf("present = %v, want [brew]", present)
	}
	if len(absent) != 1 || absent[0] != BackendNix {
		t.Fatalf("absent = %v, want [nix]", absent)
	}
}

// A detect error is treated as absent: we would rather offer to install than
// wrongly assume a backend is present and then hard-fail mid-run.
func TestProbe_DetectErrorTreatedAsAbsent(t *testing.T) {
	bs := &Bootstrapper{
		Detect: func(b Backend) (bool, error) { return false, errors.New("boom") },
	}
	absent, present := bs.Probe([]Backend{BackendBrew})
	if len(present) != 0 {
		t.Fatalf("present = %v, want []", present)
	}
	if len(absent) != 1 || absent[0] != BackendBrew {
		t.Fatalf("absent = %v, want [brew]", absent)
	}
}

// Install then a passing verify yields OutcomeInstalled, and verify runs only
// after install.
func TestProvision_InstallThenVerifySucceeds(t *testing.T) {
	var calls []string
	bs := &Bootstrapper{
		Install: func(b Backend) error { calls = append(calls, "install:"+string(b)); return nil },
		Verify:  func(b Backend) (bool, error) { calls = append(calls, "verify:"+string(b)); return true, nil },
	}
	out := bs.Provision([]Backend{BackendBrew})
	if out[BackendBrew] != OutcomeInstalled {
		t.Fatalf("outcome = %v, want Installed", out[BackendBrew])
	}
	if strings.Join(calls, ",") != "install:brew,verify:brew" {
		t.Fatalf("calls = %v, want [install:brew verify:brew]", calls)
	}
}

// An install error is OutcomeInstallFailed and verify is never attempted.
func TestProvision_InstallErrorSkipsVerify(t *testing.T) {
	verifyCalled := false
	bs := &Bootstrapper{
		Install: func(b Backend) error { return errors.New("install failed") },
		Verify:  func(b Backend) (bool, error) { verifyCalled = true; return true, nil },
	}
	out := bs.Provision([]Backend{BackendBrew})
	if out[BackendBrew] != OutcomeInstallFailed {
		t.Fatalf("outcome = %v, want InstallFailed", out[BackendBrew])
	}
	if verifyCalled {
		t.Fatal("verify must not be called after an install error")
	}
}

// Installer exits 0 but the verify probe reports not-working → VerifyFailed
// (the backend is unavailable, never used half-configured).
func TestProvision_VerifyFalseIsVerifyFailed(t *testing.T) {
	bs := &Bootstrapper{
		Install: func(b Backend) error { return nil },
		Verify:  func(b Backend) (bool, error) { return false, nil },
	}
	out := bs.Provision([]Backend{BackendBrew})
	if out[BackendBrew] != OutcomeVerifyFailed {
		t.Fatalf("outcome = %v, want VerifyFailed", out[BackendBrew])
	}
}

// A verify probe error is also VerifyFailed (unavailable).
func TestProvision_VerifyErrorIsVerifyFailed(t *testing.T) {
	bs := &Bootstrapper{
		Install: func(b Backend) error { return nil },
		Verify:  func(b Backend) (bool, error) { return false, errors.New("probe error") },
	}
	out := bs.Provision([]Backend{BackendBrew})
	if out[BackendBrew] != OutcomeVerifyFailed {
		t.Fatalf("outcome = %v, want VerifyFailed", out[BackendBrew])
	}
}

// Each backend in a combined set gets its own independent outcome.
func TestProvision_MultipleBackendsIndependentOutcomes(t *testing.T) {
	bs := &Bootstrapper{
		Install: func(b Backend) error {
			if b == BackendNix {
				return errors.New("nix install failed")
			}
			return nil
		},
		Verify: func(b Backend) (bool, error) { return true, nil },
	}
	out := bs.Provision([]Backend{BackendBrew, BackendNix})
	if out[BackendBrew] != OutcomeInstalled {
		t.Fatalf("brew outcome = %v, want Installed", out[BackendBrew])
	}
	if out[BackendNix] != OutcomeInstallFailed {
		t.Fatalf("nix outcome = %v, want InstallFailed", out[BackendNix])
	}
}

// Only a freshly-installed-and-verified backend is "available".
func TestOutcome_AvailableOnlyWhenInstalled(t *testing.T) {
	if !OutcomeInstalled.Available() {
		t.Fatal("Installed must be available")
	}
	if OutcomeInstallFailed.Available() || OutcomeVerifyFailed.Available() {
		t.Fatal("failed outcomes must not be available")
	}
}

// The inspectable details for the consent event come from InstallerCommand:
// they must name the OFFICIAL installer for each backend.
func TestInstallerCommand_NamesOfficialInstaller(t *testing.T) {
	brewCmd := InstallerCommand(BackendBrew)
	if !strings.Contains(brewCmd, "install.sh") || !strings.Contains(strings.ToLower(brewCmd), "homebrew") {
		t.Fatalf("brew installer command %q must reference the official Homebrew install.sh", brewCmd)
	}
	nixCmd := InstallerCommand(BackendNix)
	if !strings.Contains(strings.ToLower(nixCmd), "determinate") {
		t.Fatalf("nix installer command %q must reference the Determinate installer", nixCmd)
	}
}

// The plain-language consent message must NOT name any backend product — the
// concepts stay invisible. (Product names live only in the inspectable Details.)
func TestConsentMessage_NamesNoBackendProduct(t *testing.T) {
	for _, set := range [][]Backend{{BackendBrew}, {BackendNix}, {BackendBrew, BackendNix}} {
		msg := strings.ToLower(ConsentMessage(set))
		for _, banned := range []string{"nix", "homebrew", "brew", "determinate"} {
			if strings.Contains(msg, banned) {
				t.Fatalf("consent message for %v must not name product %q: %q", set, banned, msg)
			}
		}
		if msg == "" {
			t.Fatalf("consent message for %v is empty", set)
		}
	}
}

// New wires the real seams (so production uses real detect/install/verify), but
// no test ever calls Provision on it — the real installer must never run in
// `go test`.
func TestNew_WiresRealSeams(t *testing.T) {
	bs := New()
	if bs.Detect == nil || bs.Install == nil || bs.Verify == nil {
		t.Fatal("New must wire all three real seams")
	}
}
