// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package winget

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"regexp"

	"github.com/Artexis10/endstate/go-engine/internal/driver"
	"github.com/Artexis10/endstate/go-engine/internal/packagesource"
)

// compile-time assertion: the winget driver implements driver.Uninstaller, so
// the rollback command can discover best-effort rollback by type-assertion.
var _ driver.Uninstaller = (*WingetDriver)(nil)

// noPackageFoundExitCode is winget's APPINSTALLER_CLI_ERROR_NO_APPLICATIONS_FOUND
// (HRESULT 0x8A150014), returned by `winget uninstall` when no installed package
// matches. Confirmed empirically against winget v1.28.240: `winget uninstall` of
// a non-existent id exits -1978335212 (the signed 32-bit form). As with the
// install codes, Windows may surface the signed or unsigned interpretation, and
// an HRESULT cannot survive POSIX 8-bit exit-code truncation — so on non-Windows
// this path is untested and the output-substring check below is the primary
// "absent" signal.
const noPackageFoundExitCodeSigned = -1978335212
const noPackageFoundExitCodeUnsigned = 2316632084

// noPackageFoundPattern matches winget output indicating nothing matched the
// uninstall request. This is the cross-platform-reliable "absent" signal (the
// exit code cannot survive POSIX truncation, so hermetic tests drive this).
var noPackageFoundPattern = regexp.MustCompile(`(?i)(no installed package found|no applicable (package|upgrade|installer)|not installed|no package(s)? found)`)

// Uninstall removes the package identified by ref via winget, satisfying
// driver.Uninstaller. It is the engine's only uninstall path and is used by the
// best-effort rollback (Phase 4).
//
// The command spawned is:
//
//	winget uninstall --id <ref> -e --silent --accept-source-agreements
//
// Exit/output semantics:
//   - 0                                  → StatusUninstalled
//   - "no installed package found" (output) or the not-found HRESULT → StatusAbsent (no-op)
//   - other non-zero                     → StatusFailed
//
// If the winget binary is not found, Uninstall returns (nil, ErrWingetNotAvailable).
func (w *WingetDriver) Uninstall(ref string) (*driver.UninstallResult, error) {
	return w.UninstallSource(ref, packagesource.ResolveWinget(ref, ""))
}

func (w *WingetDriver) UninstallSource(ref, source string) (*driver.UninstallResult, error) {
	source = packagesource.ResolveWinget(ref, source)
	cmd := w.ExecCommand(
		"winget",
		"uninstall",
		"--id", ref,
		"--source", source,
		"-e",
		"--silent",
		"--accept-source-agreements",
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

	exitCode := 0
	if runErr != nil {
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			// Process did not exit normally (e.g. killed); treat as failure.
			return &driver.UninstallResult{
				Status:  driver.StatusFailed,
				Message: fmt.Sprintf("winget did not exit normally: %v", runErr),
			}, nil
		}
	}

	switch {
	case exitCode == 0:
		return &driver.UninstallResult{
			Status:  driver.StatusUninstalled,
			Message: "Uninstalled successfully",
		}, nil

	case noPackageFoundPattern.MatchString(combined),
		exitCode == noPackageFoundExitCodeSigned,
		exitCode == noPackageFoundExitCodeUnsigned:
		// Already not installed — a successful no-op for rollback idempotency.
		return &driver.UninstallResult{
			Status:  driver.StatusAbsent,
			Message: "Package was not installed",
		}, nil

	default:
		return &driver.UninstallResult{
			Status:  driver.StatusFailed,
			Message: fmt.Sprintf("winget exited with code %d", exitCode),
		}, nil
	}
}
