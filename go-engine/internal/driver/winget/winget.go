// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

// Package winget implements the driver.Driver interface for Windows Package
// Manager (winget). It mirrors the install/detect logic from
// drivers/winget.ps1 in the PowerShell engine.
package winget

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"regexp"

	"github.com/Artexis10/endstate/go-engine/internal/driver"
)

// alreadyInstalledExitCode is the winget exit code that means the package is
// already installed. The HRESULT is 0x8A150019. On Windows, ExitProcess() takes
// a UINT (uint32), so Go's ExitCode() may return either the signed int32
// interpretation (-1978335189) or the unsigned uint32 interpretation (2316632107)
// depending on the Go version and platform. We check both.
const alreadyInstalledExitCodeSigned = -1978335189
const alreadyInstalledExitCodeUnsigned = 2316632107

// userDeniedPattern matches output text that indicates the user cancelled or
// denied the installation. This is heuristic and unreliable — winget provides
// no standardised exit code for user cancellation (see event-contract.md).
var userDeniedPattern = regexp.MustCompile(`(?i)(cancel(l?ed)?|denied|canceled|user.*abort|user.*decline|installation.*cancel)`)

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
// Exit code semantics (matching drivers/winget.ps1):
//   - 0                 → StatusInstalled
//   - -1978335189 (0x8A150019) → StatusPresent / ReasonAlreadyInstalled
//   - other non-zero    → StatusFailed / ReasonInstallFailed
//
// If combined stdout+stderr contains cancellation keywords the reason is
// overridden to ReasonUserDenied (heuristic, unreliable per event-contract.md).
//
// If the winget binary is not found, Install returns (nil, ErrWingetNotAvailable).
func (w *WingetDriver) Install(ref string) (*driver.InstallResult, error) {
	cmd := w.ExecCommand(
		"winget",
		"install",
		"--id", ref,
		"--accept-source-agreements",
		"--accept-package-agreements",
		"-e",
		"--silent",
	)

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
			Message: "Installed successfully",
		}, nil

	case exitCode == alreadyInstalledExitCodeSigned || exitCode == alreadyInstalledExitCodeUnsigned:
		return &driver.InstallResult{
			Status:  driver.StatusPresent,
			Reason:  driver.ReasonAlreadyInstalled,
			Message: "Already installed",
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
			Message: fmt.Sprintf("winget exited with code %d", exitCode),
		}, nil
	}
}
