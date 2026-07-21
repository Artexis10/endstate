// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

// Package winget implements the driver.Driver interface for Windows Package
// Manager (winget). It mirrors the install/detect logic from
// It wraps the winget CLI for package detection and installation.
package winget

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"

	"github.com/Artexis10/endstate/go-engine/internal/driver"
	"github.com/Artexis10/endstate/go-engine/internal/packagesource"
)

// alreadyInstalledExitCode is the winget exit code that means the package is
// already installed. The HRESULT is 0x8A15002B; confirmed empirically against
// winget v1.28.240 (re-installing a pinned, already-present version exits
// -1978335189). On Windows, ExitProcess() takes a UINT (uint32), so Go's
// ExitCode() may return either the signed int32 interpretation (-1978335189) or
// the unsigned uint32 interpretation (2316632107) depending on the Go version
// and platform. We check both.
const alreadyInstalledExitCodeSigned = -1978335189
const alreadyInstalledExitCodeUnsigned = 2316632107

// userDeniedPattern matches output text that indicates the user cancelled or
// denied the installation. This is heuristic and unreliable — winget provides
// no standardised exit code for user cancellation (see event-contract.md).
var userDeniedPattern = regexp.MustCompile(`(?i)(cancel(l?ed)?|denied|canceled|user.*abort|user.*decline|installation.*cancel)`)

// cancelledExitHResults are winget/Windows process exit codes (HRESULTs) that
// deterministically mean the user declined or dismissed an elevation/permission
// prompt. This is a documented allowlist — unlike userDeniedPattern it does not
// guess from output text. On Windows an HRESULT exit code may be reported by
// Go's exec ExitCode() as either its signed int32 or unsigned uint32
// interpretation (same as alreadyInstalledExitCode*), so both are listed.
//
// Sources:
//   - 0x8A15010C APPINSTALLER_CLI_ERROR_INSTALL_CANCELLED_BY_USER
//     ("You cancelled the installation.")
//   - 0x8A150077 APPINSTALLER_CLI_ERROR_AUTHENTICATION_CANCELLED_BY_USER
//     ("Authentication failed. User cancelled.")
//     Both: microsoft/winget-cli doc/windows/package-manager/winget/returnCodes.md
//   - 0x800704C7 HRESULT_FROM_WIN32(ERROR_CANCELLED) — the Windows "operation
//     cancelled by the user" code (1223) surfaced as an HRESULT (UAC declined).
var cancelledExitHResults = map[int]struct{}{
	-1978334964: {}, 2316632332: {}, // 0x8A15010C INSTALL_CANCELLED_BY_USER
	-1978335113: {}, 2316632183: {}, // 0x8A150077 AUTHENTICATION_CANCELLED_BY_USER
	-2147023673: {}, 2147943623: {}, // 0x800704C7 HRESULT_FROM_WIN32(ERROR_CANCELLED)
}

// cancelledInstallerCodes are Windows/MSI installer exit codes — as reported by
// winget's "Installer failed with exit code: <n>" output line, not winget's own
// process exit code — that mean the user cancelled. When the user declines the
// UAC prompt the bundled installer aborts with one of these while winget's
// process exit stays a generic installer-failed HRESULT.
//
// Sources (Windows/MSI system error codes):
//   - 1602 ERROR_INSTALL_USEREXIT   ("User cancel installation.")
//   - 1223 ERROR_CANCELLED          ("The operation was canceled by the user.")
var cancelledInstallerCodes = map[int]struct{}{
	1602: {},
	1223: {},
}

// installerExitCodeRe extracts <n> from winget's
// "Installer failed with exit code: <n>" line.
var installerExitCodeRe = regexp.MustCompile(`(?i)installer failed with exit code:\s*(-?\d+)`)

// cancelledMessage is the engine-authored, jargon-free message surfaced for a
// user-cancelled install. It carries no exit codes or CLI remediation so a GUI
// can present it verbatim.
const cancelledMessage = "Installation was cancelled before it finished — Windows asked for permission and the request was declined or dismissed."

// isUserCancelled reports whether a non-zero winget outcome was the user
// declining/dismissing an elevation or permission prompt, using the documented
// exit-code allowlists (winget/Windows HRESULT process exit code, or the
// installer exit code winget printed). It is deterministic — the heuristic
// userDeniedPattern is a separate, weaker fallback for unclassified codes.
func isUserCancelled(exitCode int, combined string) bool {
	if _, ok := cancelledExitHResults[exitCode]; ok {
		return true
	}
	if code, ok := parseInstallerExitCode(combined); ok {
		if _, ok := cancelledInstallerCodes[code]; ok {
			return true
		}
	}
	return false
}

// parseInstallerExitCode returns the <n> from a winget "Installer failed with
// exit code: <n>" line, or (0, false) when the line is absent/unparseable.
func parseInstallerExitCode(output string) (int, bool) {
	m := installerExitCodeRe.FindStringSubmatch(output)
	if m == nil {
		return 0, false
	}
	code, err := strconv.Atoi(m[1])
	if err != nil {
		return 0, false
	}
	return code, true
}

// WingetDriver implements driver.Driver using the winget CLI.
// ExecCommand is an injection point so tests can substitute a fake command
// runner without spawning a real winget process.
type WingetDriver struct {
	// ExecCommand creates an *exec.Cmd for the named binary and args.
	// Defaults to exec.Command; tests replace it with a helper-process shim.
	ExecCommand func(name string, args ...string) *exec.Cmd
}

// New returns a WingetDriver backed by the real exec.Command.
func New() *WingetDriver {
	return &WingetDriver{
		ExecCommand: exec.Command,
	}
}

// Name satisfies driver.Driver and returns the stable driver identifier.
func (w *WingetDriver) Name() string { return "winget" }

// Install installs the package identified by ref via winget.
//
// The command spawned is:
//
//	winget install --id <ref> --accept-source-agreements --accept-package-agreements -e --silent
//
// Exit code semantics:
//   - 0                 → StatusInstalled
//   - -1978335189 (0x8A15002B) → StatusPresent / ReasonAlreadyInstalled
//   - a documented cancellation exit code → StatusFailed / ReasonCancelledByUser
//     (user declined/dismissed an elevation prompt; see isUserCancelled)
//   - other non-zero    → StatusFailed / ReasonInstallFailed
//
// For an unclassified non-zero exit, if combined stdout+stderr contains
// cancellation keywords the reason is overridden to ReasonUserDenied
// (heuristic, unreliable per event-contract.md).
//
// If the winget binary is not found, Install returns (nil, ErrWingetNotAvailable).
func (w *WingetDriver) Install(ref string) (*driver.InstallResult, error) {
	return w.InstallSource(ref, packagesource.ResolveWinget(ref, ""))
}

func (w *WingetDriver) InstallSource(ref, source string) (*driver.InstallResult, error) {
	return w.install(ref, "", false, packagesource.ResolveWinget(ref, source))
}

// install is the shared winget-install implementation. When version is non-empty
// it pins the install via `--version <version>` (the VersionedInstaller path).
// When force is true it adds `--force` so an already-installed different version
// is reinstalled to the requested one (the `apply --repin` convergence path).
// With version="" and force=false it installs the latest, byte-identical to the
// historical Install behavior.
func (w *WingetDriver) install(ref, version string, force bool, source string) (*driver.InstallResult, error) {
	args := []string{
		"install",
		"--id", ref,
		"--source", source,
		"--accept-source-agreements",
		"--accept-package-agreements",
		"-e",
		"--silent",
	}
	if version != "" {
		args = append(args, "--version", version)
	}
	if force {
		args = append(args, "--force")
	}
	cmd := w.ExecCommand("winget", args...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := cmd.Run()

	// Detect missing binary before inspecting exit code.
	if runErr != nil {
		var execErr *exec.Error
		if errors.As(runErr, &execErr) && execErr.Err == exec.ErrNotFound {
			return nil, ErrWingetNotAvailable
		}
	}

	combined := stdout.String() + stderr.String()

	// Determine the process exit code.
	exitCode := 0
	if runErr != nil {
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			// Process did not exit normally (e.g. killed); treat as failure.
			return &driver.InstallResult{
				Status:  driver.StatusFailed,
				Reason:  driver.ReasonInstallFailed,
				Message: fmt.Sprintf("winget did not exit normally: %v", runErr),
			}, nil
		}
	}

	switch {
	case exitCode == 0:
		return &driver.InstallResult{
			Status:  driver.StatusInstalled,
			Message: installedMessage(version),
		}, nil

	case exitCode == alreadyInstalledExitCodeSigned || exitCode == alreadyInstalledExitCodeUnsigned:
		return &driver.InstallResult{
			Status:  driver.StatusPresent,
			Reason:  driver.ReasonAlreadyInstalled,
			Message: "Already installed",
		}, nil

	case isUserCancelled(exitCode, combined):
		// The user declined/dismissed an elevation prompt (documented exit-code
		// allowlist). Status stays "failed" — the install did not complete — but
		// the reason lets a consumer present a calm cancellation, not an error.
		return &driver.InstallResult{
			Status:  driver.StatusFailed,
			Reason:  driver.ReasonCancelledByUser,
			Message: cancelledMessage,
		}, nil

	default:
		reason := driver.ReasonInstallFailed
		// Heuristic user-denied detection: inspect combined output for
		// cancellation language. This is explicitly documented as unreliable
		// in event-contract.md.
		if userDeniedPattern.MatchString(combined) {
			reason = driver.ReasonUserDenied
		}
		return &driver.InstallResult{
			Status:  driver.StatusFailed,
			Reason:  reason,
			Message: failedMessage(exitCode, version),
		}, nil
	}
}

// installedMessage and failedMessage keep the unpinned messages byte-identical
// to the historical Install output, and append the requested version on the
// pinned path so a failed pin surfaces which version was unavailable.
func installedMessage(version string) string {
	if version != "" {
		return "Installed version " + version
	}
	return "Installed successfully"
}

func failedMessage(exitCode int, version string) string {
	if version != "" {
		return fmt.Sprintf("winget exited with code %d (requested version %s)", exitCode, version)
	}
	return fmt.Sprintf("winget exited with code %d", exitCode)
}
