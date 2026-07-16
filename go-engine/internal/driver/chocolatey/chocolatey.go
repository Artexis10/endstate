// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

// Package chocolatey implements Endstate's per-package driver for Chocolatey.
package chocolatey

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"

	"github.com/Artexis10/endstate/go-engine/internal/driver"
)

const executable = "choco"

const (
	exitSuccess         = 0
	exitRebootInitiated = 1641
	exitRebootRequired  = 3010
)

// ErrChocolateyNotAvailable identifies a missing choco.exe executable. Callers
// treat it as an unavailable backend, not as a per-package failure.
var ErrChocolateyNotAvailable = errors.New("chocolatey is not installed or choco.exe is not available on PATH or under ProgramData")

var absentPattern = regexp.MustCompile(`(?i)(not installed|cannot uninstall.*not installed|package.*was not found|no package.*found)`)

// ChocolateyDriver wraps choco.exe. ExecCommand is injected in hermetic tests.
type ChocolateyDriver struct {
	ExecCommand func(name string, args ...string) *exec.Cmd
	Executable  string
	resolveErr  error
}

// New returns a Chocolatey driver backed by the host command runner.
func New() *ChocolateyDriver {
	return newWithExecutableResolver(exec.Command, resolveChocolateyExecutable)
}

func newWithExecutableResolver(execCommand func(name string, args ...string) *exec.Cmd, resolver func() (string, error)) *ChocolateyDriver {
	executablePath, err := resolver()
	return &ChocolateyDriver{
		ExecCommand: execCommand,
		Executable:  executablePath,
		resolveErr:  err,
	}
}

func resolveChocolateyExecutable() (string, error) {
	return findChocolateyExecutable(exec.LookPath, os.Getenv, func(path string) bool {
		info, err := os.Stat(path)
		return err == nil && !info.IsDir()
	})
}

func findChocolateyExecutable(
	lookPath func(string) (string, error),
	getenv func(string) string,
	fileExists func(string) bool,
) (string, error) {
	if path, err := lookPath(executable); err == nil {
		return path, nil
	}

	if programData := getenv("ProgramData"); programData != "" {
		knownPath := filepath.Join(programData, "chocolatey", "bin", "choco.exe")
		if fileExists(knownPath) {
			return knownPath, nil
		}
	}

	return "", ErrChocolateyNotAvailable
}

// Name returns the stable manifest/backend identifier.
func (c *ChocolateyDriver) Name() string { return "chocolatey" }

type commandResult struct {
	stdout   string
	stderr   string
	exitCode int
}

// run is the sole choco.exe execution path. It preserves the machine's source
// configuration by accepting only operation arguments supplied by the driver;
// no source selection or source mutation is added here.
func (c *ChocolateyDriver) run(args ...string) (commandResult, error) {
	if c.resolveErr != nil {
		return commandResult{}, c.resolveErr
	}

	executablePath := c.Executable
	if executablePath == "" {
		// Directly constructed drivers retain the injected-command test seam.
		executablePath = executable
	}
	cmd := c.ExecCommand(executablePath, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	result := commandResult{stdout: stdout.String(), stderr: stderr.String()}
	if err == nil {
		return result, nil
	}

	var execErr *exec.Error
	if errors.As(err, &execErr) && execErr.Err == exec.ErrNotFound {
		return commandResult{}, ErrChocolateyNotAvailable
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		result.exitCode = exitErr.ExitCode()
		return result, nil
	}

	return commandResult{}, fmt.Errorf("chocolatey could not execute: %w", err)
}

func successfulExit(exitCode int) bool {
	return exitCode == exitSuccess || exitCode == exitRebootInitiated || exitCode == exitRebootRequired
}

func rebootExit(exitCode int) bool {
	return exitCode == exitRebootInitiated || exitCode == exitRebootRequired
}

// Install installs the latest configured-source version. Presence is checked
// first so an installed package is a deterministic no-op.
func (c *ChocolateyDriver) Install(ref string) (*driver.InstallResult, error) {
	present, _, err := c.Detect(ref)
	if err != nil {
		return nil, err
	}
	if present {
		return &driver.InstallResult{
			Status:  driver.StatusPresent,
			Reason:  driver.ReasonAlreadyInstalled,
			Message: "Already installed",
		}, nil
	}
	return c.install("install", ref, "", false)
}

func (c *ChocolateyDriver) install(action, ref, version string, allowDowngrade bool) (*driver.InstallResult, error) {
	args := []string{action, ref, "--yes", "--no-progress", "--limit-output"}
	if version != "" {
		args = append(args, "--version", version)
	}
	if allowDowngrade {
		args = append(args, "--allow-downgrade")
	}

	result, err := c.run(args...)
	if err != nil {
		return nil, err
	}
	if successfulExit(result.exitCode) {
		message := "Installed successfully"
		if version != "" {
			message = "Installed version " + version
		}
		return &driver.InstallResult{
			Status:         driver.StatusInstalled,
			Message:        message,
			RebootRequired: rebootExit(result.exitCode),
		}, nil
	}

	message := fmt.Sprintf("chocolatey exited with code %d", result.exitCode)
	if version != "" {
		message += " (requested version " + version + ")"
	}
	return &driver.InstallResult{
		Status:  driver.StatusFailed,
		Reason:  driver.ReasonInstallFailed,
		Message: message,
	}, nil
}
