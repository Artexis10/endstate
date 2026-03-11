// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package winget

import (
	"errors"
	"os/exec"
)

// ErrWingetNotAvailable is returned when the winget binary cannot be found on
// PATH. Callers should surface this as WINGET_NOT_AVAILABLE in the JSON
// envelope rather than treating it as a per-package failure.
var ErrWingetNotAvailable = errors.New("winget is not installed or not available on PATH")

// Detect checks whether the package identified by ref is currently installed.
//
// It runs:
//
//	winget list --id <ref> -e --accept-source-agreements
//
// and interprets the exit code:
//   - 0  → installed (true, nil)
//   - non-zero → not installed (false, nil)
//   - binary not found → (false, ErrWingetNotAvailable)
//
// The implementation mirrors Test-AppInstalled in drivers/winget.ps1 but uses
// exit-code semantics rather than output parsing, which is more reliable across
// winget versions.
func (w *WingetDriver) Detect(ref string) (bool, error) {
	cmd := w.ExecCommand(
		"winget",
		"list",
		"--id", ref,
		"-e",
		"--accept-source-agreements",
	)

	err := cmd.Run()
	if err == nil {
		return true, nil
	}

	// Distinguish "winget not found" from "package not found".
	var execErr *exec.Error
	if errors.As(err, &execErr) && execErr.Err == exec.ErrNotFound {
		return false, ErrWingetNotAvailable
	}

	// Any non-zero exit code from winget means the package is not listed.
	return false, nil
}
