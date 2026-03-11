// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package winget

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/driver"
)

// ---------------------------------------------------------------------------
// Helper-process shim
//
// Tests replace WingetDriver.ExecCommand with fakeCommand, which spawns THIS
// test binary with GO_WANT_HELPER_PROCESS=1 and encodes the desired exit code
// (and optional stdout text) via environment variables. TestHelperProcess runs
// inside that re-invocation and emulates what winget would do.
// ---------------------------------------------------------------------------

// TestHelperProcess is the entry point for the fake-winget subprocess.
// It must be a Test* function so the test binary includes it; the guard at the
// top ensures it is a no-op during a normal test run.
func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}

	// Read configuration written by fakeCommand.
	exitCodeStr := os.Getenv("FAKE_EXIT_CODE")
	stdout := os.Getenv("FAKE_STDOUT")
	stderr := os.Getenv("FAKE_STDERR")

	if stdout != "" {
		fmt.Fprint(os.Stdout, stdout)
	}
	if stderr != "" {
		fmt.Fprint(os.Stderr, stderr)
	}

	code := 0
	if exitCodeStr != "" {
		parsed, err := strconv.Atoi(exitCodeStr)
		if err == nil {
			code = parsed
		}
	}
	os.Exit(code)
}

// fakeCommand returns an *exec.Cmd that re-invokes the test binary as a
// helper process. The helper process exits with exitCode and writes the
// supplied stdout/stderr strings.
func fakeCommand(exitCode int, stdout, stderr string) func(string, ...string) *exec.Cmd {
	return func(name string, args ...string) *exec.Cmd {
		// Build the command that re-runs the current test binary.
		cs := []string{"-test.run=TestHelperProcess", "--"}
		cs = append(cs, args...)
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

// ---------------------------------------------------------------------------
// Detect tests
// ---------------------------------------------------------------------------

func TestDetect_Installed(t *testing.T) {
	d := &WingetDriver{ExecCommand: fakeCommand(0, "", "")}
	got, err := d.Detect("Microsoft.VisualStudioCode")
	if err != nil {
		t.Fatalf("Detect returned unexpected error: %v", err)
	}
	if !got {
		t.Error("expected Detect to return true for exit code 0")
	}
}

func TestDetect_NotInstalled(t *testing.T) {
	// winget returns non-zero when the package is not found.
	d := &WingetDriver{ExecCommand: fakeCommand(1, "", "")}
	got, err := d.Detect("Some.Missing.Package")
	if err != nil {
		t.Fatalf("Detect returned unexpected error: %v", err)
	}
	if got {
		t.Error("expected Detect to return false for non-zero exit code")
	}
}

// ---------------------------------------------------------------------------
// Install tests
// ---------------------------------------------------------------------------

func TestInstall_Success(t *testing.T) {
	d := &WingetDriver{ExecCommand: fakeCommand(0, "Successfully installed", "")}
	result, err := d.Install("Git.Git")
	if err != nil {
		t.Fatalf("Install returned unexpected error: %v", err)
	}
	if result.Status != driver.StatusInstalled {
		t.Errorf("expected status %q, got %q", driver.StatusInstalled, result.Status)
	}
	if result.Reason != "" {
		t.Errorf("expected empty reason, got %q", result.Reason)
	}
}

func TestInstall_AlreadyInstalled(t *testing.T) {
	// alreadyInstalledExitCode = -1978335189 (0x8A150019). On Windows,
	// os.Exit propagates the full 32-bit value and exec.ExitError.ExitCode()
	// recovers it correctly via GetExitCodeProcess. This test is therefore
	// Windows-only by design; the production target is Windows.
	d := &WingetDriver{ExecCommand: fakeCommand(alreadyInstalledExitCodeSigned, "", "")}
	result, err := d.Install("Git.Git")
	if err != nil {
		t.Fatalf("Install returned unexpected error: %v", err)
	}
	if result.Status != driver.StatusPresent {
		t.Errorf("expected status %q, got %q", driver.StatusPresent, result.Status)
	}
	if result.Reason != driver.ReasonAlreadyInstalled {
		t.Errorf("expected reason %q, got %q", driver.ReasonAlreadyInstalled, result.Reason)
	}
}

func TestInstall_Failure(t *testing.T) {
	d := &WingetDriver{ExecCommand: fakeCommand(1, "", "error: package not found")}
	result, err := d.Install("No.Such.Package")
	if err != nil {
		t.Fatalf("Install returned unexpected error: %v", err)
	}
	if result.Status != driver.StatusFailed {
		t.Errorf("expected status %q, got %q", driver.StatusFailed, result.Status)
	}
	if result.Reason != driver.ReasonInstallFailed {
		t.Errorf("expected reason %q, got %q", driver.ReasonInstallFailed, result.Reason)
	}
}

func TestInstall_UserDenied_OutputHeuristic(t *testing.T) {
	// Simulate non-zero exit with cancellation language in stderr.
	d := &WingetDriver{ExecCommand: fakeCommand(1, "", "Installation cancelled by user")}
	result, err := d.Install("Some.App")
	if err != nil {
		t.Fatalf("Install returned unexpected error: %v", err)
	}
	if result.Status != driver.StatusFailed {
		t.Errorf("expected status %q, got %q", driver.StatusFailed, result.Status)
	}
	if result.Reason != driver.ReasonUserDenied {
		t.Errorf("expected reason %q, got %q", driver.ReasonUserDenied, result.Reason)
	}
}

func TestInstall_UserDenied_DeniedKeyword(t *testing.T) {
	d := &WingetDriver{ExecCommand: fakeCommand(1, "Operation denied", "")}
	result, err := d.Install("Some.App")
	if err != nil {
		t.Fatalf("Install returned unexpected error: %v", err)
	}
	if result.Reason != driver.ReasonUserDenied {
		t.Errorf("expected reason %q, got %q", driver.ReasonUserDenied, result.Reason)
	}
}

func TestName(t *testing.T) {
	d := New()
	if d.Name() != "winget" {
		t.Errorf("expected driver name %q, got %q", "winget", d.Name())
	}
}
