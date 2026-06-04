// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package brew

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"regexp"

	"github.com/Artexis10/endstate/go-engine/internal/driver"
)

// compile-time assertion: the brew driver implements driver.Uninstaller, so the
// rollback command can discover best-effort rollback by type-assertion.
var _ driver.Uninstaller = (*BrewDriver)(nil)

// noPackageFoundPattern matches brew output indicating nothing matched the
// uninstall request. ASSUMPTION (see brew.go header #4): `brew uninstall` of an
// absent package exits non-zero with one of these substrings; this output match
// is the primary "absent" signal because the exit code alone is ambiguous (any
// failure is non-zero). The phrasings below are assumed and MUST be confirmed on
// real macOS.
var noPackageFoundPattern = regexp.MustCompile(`(?i)(no such keg|not installed|no available formula|no installed keg|no cask(s)? found|is not installed)`)

// Uninstall removes the package identified by ref via brew, satisfying
// driver.Uninstaller. It is NON-DESTRUCTIVE: it never passes `--zap` (which
// would also remove app data/config), honoring Endstate's non-destructive
// defaults.
//
// The command spawned is:
//
//	brew uninstall <name>            (formula)
//	brew uninstall --cask <name>     (cask)
//
// Exit/output semantics:
//   - 0                                    → StatusUninstalled
//   - already-absent output substring      → StatusAbsent (no-op)
//   - other non-zero                       → StatusFailed
//
// If the brew binary is not found, Uninstall returns (nil, ErrBrewNotAvailable).
func (b *BrewDriver) Uninstall(ref string) (*driver.UninstallResult, error) {
	name, isCask := parseRef(ref)

	args := []string{"uninstall"}
	if isCask {
		args = append(args, "--cask")
	}
	args = append(args, name)

	cmd := b.ExecCommand("brew", args...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := cmd.Run()

	// Detect missing binary before inspecting exit code.
	if runErr != nil {
		var execErr *exec.Error
		if errors.As(runErr, &execErr) && execErr.Err == exec.ErrNotFound {
			return nil, ErrBrewNotAvailable
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
				Message: fmt.Sprintf("brew did not exit normally: %v", runErr),
			}, nil
		}
	}

	switch {
	case exitCode == 0:
		return &driver.UninstallResult{
			Status:  driver.StatusUninstalled,
			Message: "Uninstalled successfully",
		}, nil

	case noPackageFoundPattern.MatchString(combined):
		// Already not installed — a successful no-op for rollback idempotency.
		return &driver.UninstallResult{
			Status:  driver.StatusAbsent,
			Message: "Package was not installed",
		}, nil

	default:
		return &driver.UninstallResult{
			Status:  driver.StatusFailed,
			Message: fmt.Sprintf("brew exited with code %d", exitCode),
		}, nil
	}
}
