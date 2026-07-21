// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package winget

import (
	"runtime"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/driver"
)

// wantCancelMessage is the exact engine-authored, friendly message surfaced for
// a user-cancelled install. It is deliberately jargon-free (no exit codes, no
// "run winget ...") so a GUI can present it verbatim.
const wantCancelMessage = "Installation was cancelled before it finished — Windows asked for permission and the request was declined or dismissed."

// When the user declines/dismisses the UAC elevation prompt the bundled MSI
// aborts with 1602 (ERROR_INSTALL_USEREXIT) and winget prints
// "Installer failed with exit code: 1602" while its own process exit stays
// generic. The installer exit code parsed from stdout must classify this as
// cancelled_by_user, not a generic install failure. Cross-platform: does not
// rely on an HRESULT surviving the process exit code.
func TestInstall_UserCancelled_InstallerExitCode1602(t *testing.T) {
	d := &WingetDriver{ExecCommand: fakeCommand(1, "Installer failed with exit code: 1602\n", "")}
	res, err := d.Install("voidtools.Everything")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Status != driver.StatusFailed {
		t.Errorf("Status = %q, want %q", res.Status, driver.StatusFailed)
	}
	if res.Reason != driver.ReasonCancelledByUser {
		t.Errorf("Reason = %q, want %q", res.Reason, driver.ReasonCancelledByUser)
	}
	if res.Message != wantCancelMessage {
		t.Errorf("Message = %q, want %q", res.Message, wantCancelMessage)
	}
}

// 1223 (ERROR_CANCELLED — "The operation was canceled by the user") can also
// surface as the installer exit code when elevation is declined. Cross-platform.
func TestInstall_UserCancelled_InstallerExitCode1223(t *testing.T) {
	d := &WingetDriver{ExecCommand: fakeCommand(1, "Installer failed with exit code: 1223\n", "")}
	res, err := d.Install("voidtools.Everything")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Status != driver.StatusFailed || res.Reason != driver.ReasonCancelledByUser {
		t.Fatalf("want failed/cancelled_by_user, got %q/%q", res.Status, res.Reason)
	}
	if res.Message != wantCancelMessage {
		t.Errorf("Message = %q, want %q", res.Message, wantCancelMessage)
	}
}

// A non-cancellation installer exit code adjacent to the allowlist (1603,
// ERROR_INSTALL_FAILURE) must stay a generic install failure — proves the
// installer-code classification is a strict allowlist, not "any installer code".
// Cross-platform.
func TestInstall_InstallerExitCode1603_StaysGeneric(t *testing.T) {
	d := &WingetDriver{ExecCommand: fakeCommand(1, "Installer failed with exit code: 1603\n", "")}
	res, err := d.Install("Some.App")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Status != driver.StatusFailed || res.Reason != driver.ReasonInstallFailed {
		t.Fatalf("want failed/install_failed, got %q/%q", res.Status, res.Reason)
	}
}

// winget's own process exit code is the HRESULT
// APPINSTALLER_CLI_ERROR_INSTALL_CANCELLED_BY_USER (0x8A15010C). Windows-only:
// an HRESULT exit code cannot survive POSIX 8-bit exit-code truncation, exactly
// like TestInstall_AlreadyInstalled.
func TestInstall_UserCancelled_WingetHResult_InstallCancelled(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("HRESULT exit code 0x8A15010C cannot survive POSIX 8-bit exit-code truncation; winget is Windows-only in production")
	}
	// 0x8A15010C signed int32 = -1978334964
	d := &WingetDriver{ExecCommand: fakeCommand(-1978334964, "", "")}
	res, err := d.Install("Some.App")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Status != driver.StatusFailed || res.Reason != driver.ReasonCancelledByUser {
		t.Fatalf("want failed/cancelled_by_user, got %q/%q", res.Status, res.Reason)
	}
	if res.Message != wantCancelMessage {
		t.Errorf("Message = %q, want %q", res.Message, wantCancelMessage)
	}
}

// winget's own process exit code is the HRESULT
// APPINSTALLER_CLI_ERROR_AUTHENTICATION_CANCELLED_BY_USER (0x8A150077).
// Windows-only for the same HRESULT-truncation reason.
func TestInstall_UserCancelled_WingetHResult_AuthCancelled(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("HRESULT exit code 0x8A150077 cannot survive POSIX 8-bit exit-code truncation; winget is Windows-only in production")
	}
	// 0x8A150077 signed int32 = -1978335113
	d := &WingetDriver{ExecCommand: fakeCommand(-1978335113, "", "")}
	res, err := d.Install("Some.App")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Status != driver.StatusFailed || res.Reason != driver.ReasonCancelledByUser {
		t.Fatalf("want failed/cancelled_by_user, got %q/%q", res.Status, res.Reason)
	}
}

// UAC declined can also surface as winget's own process exit code being
// HRESULT_FROM_WIN32(ERROR_CANCELLED) = 0x800704C7. Windows-only.
func TestInstall_UserCancelled_UacDeclined_ErrorCancelledHResult(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("HRESULT exit code 0x800704C7 cannot survive POSIX 8-bit exit-code truncation; winget is Windows-only in production")
	}
	// 0x800704C7 signed int32 = -2147023673
	d := &WingetDriver{ExecCommand: fakeCommand(-2147023673, "", "")}
	res, err := d.Install("Some.App")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Status != driver.StatusFailed || res.Reason != driver.ReasonCancelledByUser {
		t.Fatalf("want failed/cancelled_by_user, got %q/%q", res.Status, res.Reason)
	}
	if res.Message != wantCancelMessage {
		t.Errorf("Message = %q, want %q", res.Message, wantCancelMessage)
	}
}

// An unrelated non-zero process exit code with no cancellation signals keeps
// exactly today's behavior: StatusFailed / ReasonInstallFailed. Windows-only
// because the code under test is an HRESULT (0x80070005, access denied).
func TestInstall_UnrelatedHResult_StaysGeneric(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("HRESULT exit code 0x80070005 cannot survive POSIX 8-bit exit-code truncation")
	}
	// 0x80070005 (E_ACCESSDENIED) signed int32 = -2147024891 — not a cancellation.
	d := &WingetDriver{ExecCommand: fakeCommand(-2147024891, "", "")}
	res, err := d.Install("Some.App")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Status != driver.StatusFailed || res.Reason != driver.ReasonInstallFailed {
		t.Fatalf("want failed/install_failed, got %q/%q", res.Status, res.Reason)
	}
}
