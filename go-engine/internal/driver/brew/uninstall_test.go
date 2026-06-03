// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package brew

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/driver"
)

func TestUninstall_FormulaRemoved(t *testing.T) {
	var calls [][]string
	d := &BrewDriver{ExecCommand: scriptedCommand(map[string]scriptedResponse{
		"uninstall": {exitCode: 0, stdout: "Uninstalling /opt/homebrew/Cellar/ripgrep/14.1.0... (12 files)"},
	}, &calls)}

	res, err := d.Uninstall("ripgrep")
	if err != nil {
		t.Fatalf("Uninstall returned unexpected error: %v", err)
	}
	if res.Status != driver.StatusUninstalled {
		t.Errorf("Status = %q, want %q", res.Status, driver.StatusUninstalled)
	}
	joined := strings.Join(calls[0], " ")
	if !strings.Contains(joined, "uninstall ripgrep") {
		t.Errorf("unexpected brew argv: %q", joined)
	}
	// Non-destructive: must never pass --zap.
	if strings.Contains(joined, "--zap") {
		t.Errorf("Uninstall must NEVER pass --zap (non-destructive): %q", joined)
	}
}

func TestUninstall_CaskRemoved(t *testing.T) {
	var calls [][]string
	d := &BrewDriver{ExecCommand: scriptedCommand(map[string]scriptedResponse{
		"uninstall": {exitCode: 0, stdout: "Uninstalling Cask firefox..."},
	}, &calls)}

	res, err := d.Uninstall("cask:firefox")
	if err != nil {
		t.Fatalf("Uninstall returned unexpected error: %v", err)
	}
	if res.Status != driver.StatusUninstalled {
		t.Errorf("Status = %q, want %q", res.Status, driver.StatusUninstalled)
	}
	joined := strings.Join(calls[0], " ")
	if !strings.Contains(joined, "uninstall --cask firefox") {
		t.Errorf("cask uninstall must pass --cask: %q", joined)
	}
	if strings.Contains(joined, "--zap") {
		t.Errorf("Uninstall must NEVER pass --zap (non-destructive): %q", joined)
	}
}

func TestUninstall_AlreadyAbsentViaOutput(t *testing.T) {
	// ASSUMPTION: brew uninstall of an absent formula exits non-zero with a
	// "No such keg" style message; the output substring is the absent signal.
	d := &BrewDriver{ExecCommand: fakeCommand(1, "", "Error: No such keg: /opt/homebrew/Cellar/ripgrep")}
	res, err := d.Uninstall("ripgrep")
	if err != nil {
		t.Fatalf("Uninstall returned unexpected error: %v", err)
	}
	if res.Status != driver.StatusAbsent {
		t.Errorf("Status = %q, want %q (already-absent is a no-op)", res.Status, driver.StatusAbsent)
	}
}

func TestUninstall_AlreadyAbsentCaskViaOutput(t *testing.T) {
	d := &BrewDriver{ExecCommand: fakeCommand(1, "", "Error: Cask 'firefox' is not installed.")}
	res, err := d.Uninstall("cask:firefox")
	if err != nil {
		t.Fatalf("Uninstall returned unexpected error: %v", err)
	}
	if res.Status != driver.StatusAbsent {
		t.Errorf("Status = %q, want %q", res.Status, driver.StatusAbsent)
	}
}

func TestUninstall_Failed(t *testing.T) {
	// Non-zero exit with NO already-absent substring → a genuine failure.
	d := &BrewDriver{ExecCommand: fakeCommand(1, "", "Error: Refusing to uninstall because it is required by other formulae")}
	res, err := d.Uninstall("openssl")
	if err != nil {
		t.Fatalf("Uninstall returned unexpected error: %v", err)
	}
	if res.Status != driver.StatusFailed {
		t.Errorf("Status = %q, want %q", res.Status, driver.StatusFailed)
	}
}

func TestUninstall_MissingBinary(t *testing.T) {
	d := &BrewDriver{ExecCommand: func(name string, args ...string) *exec.Cmd {
		return exec.Command("endstate-no-such-brew-binary-xyz")
	}}
	_, err := d.Uninstall("ripgrep")
	if err != ErrBrewNotAvailable {
		t.Fatalf("expected ErrBrewNotAvailable, got %v", err)
	}
}
