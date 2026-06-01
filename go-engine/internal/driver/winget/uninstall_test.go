// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package winget

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/driver"
)

// fakeUninstallCmd is fakeCommand plus argv capture, so tests can assert the
// spawned winget command line. It re-uses TestHelperProcess (winget_test.go).
func fakeUninstallCmd(exitCode int, stdout, stderr string, gotArgs *[]string) func(string, ...string) *exec.Cmd {
	return func(name string, args ...string) *exec.Cmd {
		if gotArgs != nil {
			*gotArgs = append([]string(nil), args...)
		}
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

func TestUninstall_Success(t *testing.T) {
	var args []string
	d := &WingetDriver{ExecCommand: fakeUninstallCmd(0, "Successfully uninstalled", "", &args)}
	res, err := d.Uninstall("Git.Git")
	if err != nil {
		t.Fatalf("Uninstall returned unexpected error: %v", err)
	}
	if res.Status != driver.StatusUninstalled {
		t.Errorf("Status = %q, want %q", res.Status, driver.StatusUninstalled)
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "uninstall --id Git.Git -e --silent --accept-source-agreements") {
		t.Errorf("unexpected winget argv: %q", joined)
	}
}

func TestUninstall_AbsentViaOutput(t *testing.T) {
	// winget prints this when nothing matches; the exit code is an HRESULT that
	// cannot survive POSIX truncation, so the output substring is the signal.
	d := &WingetDriver{ExecCommand: fakeUninstallCmd(1, "", "No installed package found matching input criteria.", nil)}
	res, err := d.Uninstall("Some.Removed.App")
	if err != nil {
		t.Fatalf("Uninstall returned unexpected error: %v", err)
	}
	if res.Status != driver.StatusAbsent {
		t.Errorf("Status = %q, want %q (already-absent is a no-op)", res.Status, driver.StatusAbsent)
	}
}

func TestUninstall_AbsentViaExitCode(t *testing.T) {
	// Ground truth: real `winget uninstall` for a non-existent package returns
	// HRESULT 0x8A150014 (APPINSTALLER_CLI_ERROR_NO_APPLICATIONS_FOUND, signed
	// -1978335212), confirmed empirically against winget v1.28.240. With NO
	// matching output substring, the exit code is the only "absent" signal — it
	// must classify as a no-op (StatusAbsent), not a failure.
	//
	// Windows-only: the HRESULT cannot survive POSIX 8-bit exit-code truncation;
	// winget is a Windows-only backend in production.
	if runtime.GOOS != "windows" {
		t.Skip("HRESULT exit code cannot survive POSIX 8-bit truncation; winget is Windows-only in production")
	}
	const realNotFoundExitCode = -1978335212 // 0x8A150014, confirmed on winget v1.28.240
	d := &WingetDriver{ExecCommand: fakeUninstallCmd(realNotFoundExitCode, "", "", nil)}
	res, err := d.Uninstall("Endstate.NoSuchApp.XYZ123")
	if err != nil {
		t.Fatalf("Uninstall returned unexpected error: %v", err)
	}
	if res.Status != driver.StatusAbsent {
		t.Errorf("Status = %q, want %q (not-found exit code is a no-op for rollback idempotency)", res.Status, driver.StatusAbsent)
	}
}

func TestUninstall_Failed(t *testing.T) {
	d := &WingetDriver{ExecCommand: fakeUninstallCmd(1, "", "0x80070005 : Access is denied; package is in use", nil)}
	res, err := d.Uninstall("In.Use.App")
	if err != nil {
		t.Fatalf("Uninstall returned unexpected error: %v", err)
	}
	if res.Status != driver.StatusFailed {
		t.Errorf("Status = %q, want %q", res.Status, driver.StatusFailed)
	}
}

func TestUninstall_MissingBinary(t *testing.T) {
	// ExecCommand returns a command for a binary that does not exist → Run yields
	// exec.ErrNotFound → Uninstall surfaces ErrWingetNotAvailable.
	d := &WingetDriver{ExecCommand: func(name string, args ...string) *exec.Cmd {
		return exec.Command("endstate-no-such-winget-binary-xyz")
	}}
	_, err := d.Uninstall("Any.App")
	if err != ErrWingetNotAvailable {
		t.Fatalf("expected ErrWingetNotAvailable, got %v", err)
	}
}
