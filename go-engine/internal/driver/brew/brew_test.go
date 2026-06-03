// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package brew

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/driver"
)

// ---------------------------------------------------------------------------
// Helper-process shim (mirrors winget's fake command runner).
//
// Tests replace BrewDriver.ExecCommand with a fake that spawns THIS test binary
// with GO_WANT_HELPER_PROCESS=1 and encodes the desired exit code and optional
// stdout/stderr via environment variables. TestHelperProcess runs inside that
// re-invocation and emulates what brew would do.
//
// Because brew uses ordinary POSIX exit codes (no Windows HRESULTs), every code
// path here is exercisable on Linux/macOS alike — unlike winget's HRESULT cases.
// ---------------------------------------------------------------------------

// TestHelperProcess is the entry point for the fake-brew subprocess.
func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}

	stdout := os.Getenv("FAKE_STDOUT")
	stderr := os.Getenv("FAKE_STDERR")
	if stdout != "" {
		fmt.Fprint(os.Stdout, stdout)
	}
	if stderr != "" {
		fmt.Fprint(os.Stderr, stderr)
	}

	code := 0
	if s := os.Getenv("FAKE_EXIT_CODE"); s != "" {
		if parsed, err := strconv.Atoi(s); err == nil {
			code = parsed
		}
	}
	os.Exit(code)
}

// fakeCommand returns an ExecCommand func that re-invokes the test binary as a
// helper process exiting with exitCode and writing the supplied stdout/stderr.
func fakeCommand(exitCode int, stdout, stderr string) func(string, ...string) *exec.Cmd {
	return func(name string, args ...string) *exec.Cmd {
		cs := append([]string{"-test.run=TestHelperProcess", "--"}, args...)
		cmd := exec.Command(os.Args[0], cs...)
		cmd.Env = append(os.Environ(),
			"GO_WANT_HELPER_PROCESS=1",
			fmt.Sprintf("FAKE_EXIT_CODE=%d", exitCode),
			fmt.Sprintf("FAKE_STDOUT=%s", stdout),
			fmt.Sprintf("FAKE_STDERR=%s", stderr),
		)
		return cmd
	}
}

// scriptedCommand drives a different (exitCode, stdout, stderr) response per
// brew subcommand, so a test can simulate e.g. `brew list` (detect) returning
// absent while `brew install` returns success. The key is args[0] (the brew
// subcommand: "list", "install", "uninstall"). It also records each argv.
type scriptedResponse struct {
	exitCode int
	stdout   string
	stderr   string
}

func scriptedCommand(bySubcommand map[string]scriptedResponse, calls *[][]string) func(string, ...string) *exec.Cmd {
	return func(name string, args ...string) *exec.Cmd {
		if calls != nil {
			*calls = append(*calls, append([]string{name}, args...))
		}
		sub := ""
		if len(args) > 0 {
			sub = args[0]
		}
		resp := bySubcommand[sub]
		cs := append([]string{"-test.run=TestHelperProcess", "--"}, args...)
		cmd := exec.Command(os.Args[0], cs...)
		cmd.Env = append(os.Environ(),
			"GO_WANT_HELPER_PROCESS=1",
			fmt.Sprintf("FAKE_EXIT_CODE=%d", resp.exitCode),
			fmt.Sprintf("FAKE_STDOUT=%s", resp.stdout),
			fmt.Sprintf("FAKE_STDERR=%s", resp.stderr),
		)
		return cmd
	}
}

// ---------------------------------------------------------------------------
// Name / parseRef
// ---------------------------------------------------------------------------

func TestName(t *testing.T) {
	if New().Name() != "brew" {
		t.Errorf("expected driver name %q, got %q", "brew", New().Name())
	}
}

func TestParseRef(t *testing.T) {
	tests := []struct {
		ref      string
		wantName string
		wantCask bool
	}{
		{"node", "node", false},
		{"node@20", "node@20", false},
		{"cask:firefox", "firefox", true},
		{"CASK:visual-studio-code", "visual-studio-code", true},
		{"  ripgrep  ", "ripgrep", false},
		{"cask:  google-chrome ", "google-chrome", true},
		{"cask:", "", true},
	}
	for _, tc := range tests {
		gotName, gotCask := parseRef(tc.ref)
		if gotName != tc.wantName || gotCask != tc.wantCask {
			t.Errorf("parseRef(%q) = (%q, %v), want (%q, %v)", tc.ref, gotName, gotCask, tc.wantName, tc.wantCask)
		}
	}
}

// ---------------------------------------------------------------------------
// Detect
// ---------------------------------------------------------------------------

func TestDetect_FormulaInstalled(t *testing.T) {
	var calls [][]string
	d := &BrewDriver{ExecCommand: scriptedCommand(map[string]scriptedResponse{
		"list": {exitCode: 0},
	}, &calls)}
	got, name, err := d.Detect("ripgrep")
	if err != nil {
		t.Fatalf("Detect returned unexpected error: %v", err)
	}
	if !got {
		t.Error("expected Detect to return true for exit code 0")
	}
	if name != "ripgrep" {
		t.Errorf("expected display name %q, got %q", "ripgrep", name)
	}
	joined := strings.Join(calls[0], " ")
	if strings.Contains(joined, "--cask") {
		t.Errorf("formula detect must not pass --cask: %q", joined)
	}
	if !strings.Contains(joined, "list ripgrep") {
		t.Errorf("unexpected brew argv: %q", joined)
	}
}

func TestDetect_FormulaNotInstalled(t *testing.T) {
	d := &BrewDriver{ExecCommand: fakeCommand(1, "", "Error: No such keg")}
	got, name, err := d.Detect("not-installed")
	if err != nil {
		t.Fatalf("Detect returned unexpected error: %v", err)
	}
	if got {
		t.Error("expected Detect to return false for non-zero exit code")
	}
	if name != "" {
		t.Errorf("expected empty display name for missing package, got %q", name)
	}
}

func TestDetect_CaskInstalled(t *testing.T) {
	var calls [][]string
	d := &BrewDriver{ExecCommand: scriptedCommand(map[string]scriptedResponse{
		"list": {exitCode: 0},
	}, &calls)}
	got, name, err := d.Detect("cask:firefox")
	if err != nil {
		t.Fatalf("Detect returned unexpected error: %v", err)
	}
	if !got {
		t.Error("expected Detect to return true for installed cask")
	}
	if name != "firefox" {
		t.Errorf("expected display name %q, got %q", "firefox", name)
	}
	joined := strings.Join(calls[0], " ")
	if !strings.Contains(joined, "list --cask firefox") {
		t.Errorf("cask detect must pass --cask: %q", joined)
	}
}

func TestDetect_CaskNotInstalled(t *testing.T) {
	d := &BrewDriver{ExecCommand: fakeCommand(1, "", "Error: Cask 'firefox' is not installed.")}
	got, _, err := d.Detect("cask:firefox")
	if err != nil {
		t.Fatalf("Detect returned unexpected error: %v", err)
	}
	if got {
		t.Error("expected Detect to return false for absent cask")
	}
}

func TestDetect_MissingBinary(t *testing.T) {
	d := &BrewDriver{ExecCommand: func(name string, args ...string) *exec.Cmd {
		return exec.Command("endstate-no-such-brew-binary-xyz")
	}}
	_, _, err := d.Detect("ripgrep")
	if err != ErrBrewNotAvailable {
		t.Fatalf("expected ErrBrewNotAvailable, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Install
// ---------------------------------------------------------------------------

func TestInstall_FreshFormula(t *testing.T) {
	var calls [][]string
	d := &BrewDriver{ExecCommand: scriptedCommand(map[string]scriptedResponse{
		"list":    {exitCode: 1, stderr: "Error: No such keg"}, // detect-before-install: absent
		"install": {exitCode: 0, stdout: "🍺  /opt/homebrew/Cellar/ripgrep/14.1.0"},
	}, &calls)}

	res, err := d.Install("ripgrep")
	if err != nil {
		t.Fatalf("Install returned unexpected error: %v", err)
	}
	if res.Status != driver.StatusInstalled {
		t.Errorf("expected status %q, got %q", driver.StatusInstalled, res.Status)
	}
	if res.Reason != "" {
		t.Errorf("expected empty reason, got %q", res.Reason)
	}
	// Verify install actually spawned (detect said absent).
	sawInstall := false
	for _, c := range calls {
		if len(c) > 1 && c[1] == "install" {
			sawInstall = true
			if strings.Contains(strings.Join(c, " "), "--cask") {
				t.Errorf("formula install must not pass --cask: %v", c)
			}
		}
	}
	if !sawInstall {
		t.Errorf("expected `brew install` to be spawned; calls=%v", calls)
	}
}

func TestInstall_FreshCask(t *testing.T) {
	var calls [][]string
	d := &BrewDriver{ExecCommand: scriptedCommand(map[string]scriptedResponse{
		"list":    {exitCode: 1, stderr: "Error: Cask 'firefox' is not installed."},
		"install": {exitCode: 0, stdout: "🍺  firefox was successfully installed!"},
	}, &calls)}

	res, err := d.Install("cask:firefox")
	if err != nil {
		t.Fatalf("Install returned unexpected error: %v", err)
	}
	if res.Status != driver.StatusInstalled {
		t.Errorf("expected status %q, got %q", driver.StatusInstalled, res.Status)
	}
	sawCaskInstall := false
	for _, c := range calls {
		if len(c) > 1 && c[1] == "install" && strings.Contains(strings.Join(c, " "), "--cask firefox") {
			sawCaskInstall = true
		}
	}
	if !sawCaskInstall {
		t.Errorf("expected `brew install --cask firefox`; calls=%v", calls)
	}
}

func TestInstall_AlreadyInstalledShortCircuits(t *testing.T) {
	var calls [][]string
	d := &BrewDriver{ExecCommand: scriptedCommand(map[string]scriptedResponse{
		"list": {exitCode: 0}, // detect-before-install: present
		// install intentionally not scripted — it must NOT be called.
	}, &calls)}

	res, err := d.Install("ripgrep")
	if err != nil {
		t.Fatalf("Install returned unexpected error: %v", err)
	}
	if res.Status != driver.StatusPresent {
		t.Errorf("expected status %q, got %q", driver.StatusPresent, res.Status)
	}
	if res.Reason != driver.ReasonAlreadyInstalled {
		t.Errorf("expected reason %q, got %q", driver.ReasonAlreadyInstalled, res.Reason)
	}
	for _, c := range calls {
		if len(c) > 1 && c[1] == "install" {
			t.Errorf("Install must NOT spawn `brew install` when already present: %v", calls)
		}
	}
}

func TestInstall_Failed(t *testing.T) {
	d := &BrewDriver{ExecCommand: scriptedCommand(map[string]scriptedResponse{
		"list":    {exitCode: 1}, // absent → proceed to install
		"install": {exitCode: 1, stderr: "Error: No available formula with the name \"no-such-pkg\"."},
	}, nil)}

	res, err := d.Install("no-such-pkg")
	if err != nil {
		t.Fatalf("Install returned unexpected error: %v", err)
	}
	if res.Status != driver.StatusFailed {
		t.Errorf("expected status %q, got %q", driver.StatusFailed, res.Status)
	}
	if res.Reason != driver.ReasonInstallFailed {
		t.Errorf("expected reason %q, got %q", driver.ReasonInstallFailed, res.Reason)
	}
}

func TestInstall_MissingBinary(t *testing.T) {
	// Detect runs first; a missing binary there surfaces ErrBrewNotAvailable
	// out of Install (infrastructure failure, not a per-package failure).
	d := &BrewDriver{ExecCommand: func(name string, args ...string) *exec.Cmd {
		return exec.Command("endstate-no-such-brew-binary-xyz")
	}}
	_, err := d.Install("ripgrep")
	if err != ErrBrewNotAvailable {
		t.Fatalf("expected ErrBrewNotAvailable, got %v", err)
	}
}
