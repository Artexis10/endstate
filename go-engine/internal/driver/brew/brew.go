// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

// Package brew implements the driver.Driver interface for the macOS Homebrew
// package manager (`brew`). It mirrors the winget driver's structure: an
// injectable command runner (ExecCommand) so tests can substitute a fake
// process, and an exit-code/output classification that maps brew outcomes onto
// the shared driver.Status/Reason constants.
//
// ---------------------------------------------------------------------------
// REAL-OUTPUT ANCHORS ARE ASSUMPTIONS (the winget lesson).
//
// This package was written on a Linux box with NO macOS / Homebrew available,
// so every brew exit code and output format below is an ASSUMPTION encoded into
// the hermetic tests via the fake command runner. A real-macOS smoke test (run
// in the later pipeline-wiring increment) MUST confirm each of these before the
// driver is trusted in production:
//
//  1. EXIT CODES — brew is assumed to follow the conventional shell contract:
//     `brew list <name>` (and `brew list --cask <name>`) exits 0 when the
//     formula/cask is installed and NON-ZERO (assumed 1) when it is not.
//     `brew install` exits 0 on success (both fresh install AND a no-op when the
//     package is already present — see #3) and non-zero on failure.
//     `brew uninstall` exits 0 when it removed something, and non-zero when
//     nothing matched (assumed paired with a "No such keg"/"not installed"
//     style message — see #4). brew does NOT use Windows HRESULTs, so its codes
//     survive POSIX 8-bit truncation and the hermetic tests can drive them
//     directly (unlike winget's already-installed/no-package HRESULTs).
//
//  2. `brew list --versions` FORMAT — assumed one package per line as
//     "<name> <version> [<version> ...]", whitespace-separated, e.g.
//     "node 20.11.0" or "python@3.11 3.11.7". A line with only a name and no
//     version is tolerated (Installed=true, Version=""). Casks are queried with
//     `brew list --cask --versions` and assumed to share the same shape.
//
//  3. ALREADY-INSTALLED DETECTION — `brew install` of an already-present
//     package is assumed to exit 0 (a successful no-op), so it is NOT
//     distinguishable from a fresh install by exit code alone. We therefore
//     detect-before-install (like winget): if Detect reports the package
//     present we short-circuit to StatusPresent/ReasonAlreadyInstalled without
//     spawning `brew install`. The assumed already-installed stdout substrings
//     (e.g. "already installed") are also matched defensively as a fallback.
//
//  4. ALREADY-ABSENT DETECTION (uninstall) — `brew uninstall` of a package that
//     is not installed is assumed to exit non-zero with a message containing one
//     of the assumed substrings in noPackageFoundPattern (e.g. "no such keg",
//     "not installed", "no available formula"). Because the exit code alone is
//     ambiguous (any failure is non-zero), the output substring is the primary
//     "absent" signal — matching winget's cross-platform-reliable approach.
//
//  5. CASK vs FORMULA — selected by the engine via the `cask:` ref scheme (see
//     parseRef), NOT auto-detected from brew output. A leading "cask:" marks a
//     Cask (we pass `--cask`); a bare ref is a formula. brew's own
//     formula-vs-cask disambiguation is assumed not needed because the manifest
//     declares the kind.
//
//  6. VERSION PINNING is WEAK / advisory — see install_version.go.
// ---------------------------------------------------------------------------
package brew

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/driver"
)

// caskPrefix marks a ref as a Homebrew Cask (a GUI app / binary artifact)
// rather than a formula. parseRef strips it and signals isCask=true.
const caskPrefix = "cask:"

// alreadyInstalledPattern matches brew stdout/stderr indicating the package was
// already present. ASSUMPTION (see brew.go header #3): `brew install` of an
// already-installed package exits 0, so this is a defensive fallback; the
// primary already-installed signal is the detect-before-install short-circuit.
var alreadyInstalledPattern = regexp.MustCompile(`(?i)(already installed|is already installed|already up-to-date)`)

// BrewDriver implements driver.Driver using the Homebrew CLI.
// ExecCommand is an injection point so tests can substitute a fake command
// runner without spawning a real brew process. It mirrors winget's design.
type BrewDriver struct {
	// ExecCommand creates an *exec.Cmd for the named binary and args.
	// Defaults to exec.Command; tests replace it with a helper-process shim.
	ExecCommand func(name string, args ...string) *exec.Cmd
}

// New returns a BrewDriver backed by the real exec.Command.
func New() *BrewDriver {
	return &BrewDriver{
		ExecCommand: exec.Command,
	}
}

// Name satisfies driver.Driver and returns the stable driver identifier.
func (b *BrewDriver) Name() string { return "brew" }

// parseRef splits an Endstate ref into the brew package name and whether it
// designates a Cask. A leading "cask:" (case-insensitive) marks a Cask and is
// stripped; a bare ref is a formula. Surrounding whitespace is trimmed.
//
// Examples:
//
//	parseRef("node")            → ("node", false)
//	parseRef("node@20")         → ("node@20", false)   // versioned formula, see install_version.go
//	parseRef("cask:firefox")    → ("firefox", true)
//	parseRef("CASK:  visual-studio-code") → ("visual-studio-code", true)
func parseRef(ref string) (name string, isCask bool) {
	trimmed := strings.TrimSpace(ref)
	if len(trimmed) >= len(caskPrefix) && strings.EqualFold(trimmed[:len(caskPrefix)], caskPrefix) {
		return strings.TrimSpace(trimmed[len(caskPrefix):]), true
	}
	return trimmed, false
}

// Install installs the package identified by ref via brew.
//
// It detects-before-install (like winget): if Detect reports the package
// already present, Install short-circuits to StatusPresent /
// ReasonAlreadyInstalled WITHOUT spawning `brew install` (ASSUMPTION: brew
// install of a present package exits 0 and is otherwise indistinguishable from
// a fresh install — see brew.go header #3).
//
// Otherwise the command spawned is:
//
//	brew install <name>            (formula)
//	brew install --cask <name>     (cask)
//
// Exit/output semantics:
//   - 0              → StatusInstalled
//   - already-installed stdout substring (defensive fallback) → StatusPresent
//   - other non-zero → StatusFailed / ReasonInstallFailed
//
// If the brew binary is not found, Install returns (nil, ErrBrewNotAvailable).
func (b *BrewDriver) Install(ref string) (*driver.InstallResult, error) {
	return b.install(ref, "", false)
}

// install is the shared brew-install implementation. The version/force
// parameters drive the (weak) VersionedInstaller path; see install_version.go.
// With version="" and force=false it installs the latest, the plain Install
// behavior.
//
// Detect-before-install: a present package short-circuits to
// StatusPresent/ReasonAlreadyInstalled. When force is true (ReinstallVersion)
// the short-circuit is skipped so the install/reinstall actually runs.
func (b *BrewDriver) install(ref, version string, force bool) (*driver.InstallResult, error) {
	name, isCask := parseRef(ref)

	// Detect-before-install (skipped on the force/reinstall path).
	if !force {
		present, _, derr := b.Detect(ref)
		if derr != nil {
			// Infrastructure failure (e.g. brew missing) — surface it.
			return nil, derr
		}
		if present {
			return &driver.InstallResult{
				Status:  driver.StatusPresent,
				Reason:  driver.ReasonAlreadyInstalled,
				Message: "Already installed",
			}, nil
		}
	}

	args := []string{"install"}
	if isCask {
		args = append(args, "--cask")
	}
	if force {
		args = append(args, "--force")
	}
	// The install target is the (possibly versioned-formula) name. brew has no
	// general `--version` flag, so a separately-declared `version` is advisory
	// only and is NOT appended here — see install_version.go for the weakness.
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
			return &driver.InstallResult{
				Status:  driver.StatusFailed,
				Reason:  driver.ReasonInstallFailed,
				Message: fmt.Sprintf("brew did not exit normally: %v", runErr),
			}, nil
		}
	}

	switch {
	case exitCode == 0:
		// Defensive fallback: brew install of an already-present package is
		// assumed to exit 0; if its output says so, report present rather than
		// installed (the detect-before-install short-circuit usually catches
		// this first).
		if !force && alreadyInstalledPattern.MatchString(combined) {
			return &driver.InstallResult{
				Status:  driver.StatusPresent,
				Reason:  driver.ReasonAlreadyInstalled,
				Message: "Already installed",
			}, nil
		}
		return &driver.InstallResult{
			Status:  driver.StatusInstalled,
			Message: installedMessage(version),
		}, nil

	default:
		return &driver.InstallResult{
			Status:  driver.StatusFailed,
			Reason:  driver.ReasonInstallFailed,
			Message: failedMessage(exitCode, version),
		}, nil
	}
}

// installedMessage / failedMessage keep messaging parallel to winget's, and
// surface the requested version on the (weak) pinned path so a failed/advisory
// pin records what was asked for.
func installedMessage(version string) string {
	if version != "" {
		return "Installed (requested version " + version + "; brew pinning is advisory)"
	}
	return "Installed successfully"
}

func failedMessage(exitCode int, version string) string {
	if version != "" {
		return fmt.Sprintf("brew exited with code %d (requested version %s)", exitCode, version)
	}
	return fmt.Sprintf("brew exited with code %d", exitCode)
}
