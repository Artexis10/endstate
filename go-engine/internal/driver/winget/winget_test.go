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
	got, _, err := d.Detect("Microsoft.VisualStudioCode")
	if err != nil {
		t.Fatalf("Detect returned unexpected error: %v", err)
	}
	if !got {
		t.Error("expected Detect to return true for exit code 0")
	}
}

func TestDetect_InstalledWithDisplayName(t *testing.T) {
	// Simulate winget list output with Name column.
	output := "Name                          Id                                Version\r\n" +
		"------------------------------------------------------------\r\n" +
		"Visual Studio Code            Microsoft.VisualStudioCode        1.85.0\r\n"
	d := &WingetDriver{ExecCommand: fakeCommand(0, output, "")}
	got, name, err := d.Detect("Microsoft.VisualStudioCode")
	if err != nil {
		t.Fatalf("Detect returned unexpected error: %v", err)
	}
	if !got {
		t.Error("expected Detect to return true for exit code 0")
	}
	if name != "Visual Studio Code" {
		t.Errorf("expected display name %q, got %q", "Visual Studio Code", name)
	}
}

func TestDetect_NotInstalled(t *testing.T) {
	// winget returns non-zero when the package is not found.
	d := &WingetDriver{ExecCommand: fakeCommand(1, "", "")}
	got, name, err := d.Detect("Some.Missing.Package")
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

// ---------------------------------------------------------------------------
// parseDisplayName tests
// ---------------------------------------------------------------------------

func TestParseDisplayName_ValidOutput(t *testing.T) {
	output := "Name                          Id                                Version\r\n" +
		"--------------------------------------------------------------\r\n" +
		"Visual Studio Code            Microsoft.VisualStudioCode        1.85.0\r\n"
	name := parseDisplayName(output)
	if name != "Visual Studio Code" {
		t.Errorf("expected %q, got %q", "Visual Studio Code", name)
	}
}

func TestParseDisplayName_NoHeader(t *testing.T) {
	output := "No packages found.\n"
	name := parseDisplayName(output)
	if name != "" {
		t.Errorf("expected empty string for no-header output, got %q", name)
	}
}

func TestParseDisplayName_EmptyOutput(t *testing.T) {
	name := parseDisplayName("")
	if name != "" {
		t.Errorf("expected empty string for empty output, got %q", name)
	}
}

func TestParseDisplayName_MultiWordName(t *testing.T) {
	output := "Name                                          Id                          Version\n" +
		"---------------------------------------------------------------------------------\n" +
		"Microsoft Visual Studio Build Tools 2022       Microsoft.VisualStudio.2022.BuildTools 17.8.0\n"
	name := parseDisplayName(output)
	if name != "Microsoft Visual Studio Build Tools 2022" {
		t.Errorf("expected %q, got %q", "Microsoft Visual Studio Build Tools 2022", name)
	}
}

func TestParseDisplayName_WithSpinnerOutput(t *testing.T) {
	// Real winget output contains \r-based progress spinner before the table.
	output := "\r   - \r   \\ \r" +
		"                                                                                                                        \r" +
		"Name              Id        Version Available Source\r\n" +
		"----------------------------------------------------\r\n" +
		"7-Zip 25.01 (x64) 7zip.7zip 25.01   26.00     winget\r\n"
	name := parseDisplayName(output)
	if name != "7-Zip 25.01 (x64)" {
		t.Errorf("expected %q, got %q", "7-Zip 25.01 (x64)", name)
	}
}

func TestResolveCarriageReturns(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"no carriage return", "no carriage return"},
		{"\r   - \r   \\ \rActual content", "Actual content"},
		{"\r   - \r                    \r", "   - "},
		{"", ""},
	}
	for _, tc := range tests {
		got := resolveCarriageReturns(tc.input)
		if got != tc.want {
			t.Errorf("resolveCarriageReturns(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestName(t *testing.T) {
	d := New()
	if d.Name() != "winget" {
		t.Errorf("expected driver name %q, got %q", "winget", d.Name())
	}
}
