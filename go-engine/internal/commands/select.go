// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"errors"

	"github.com/Artexis10/endstate/go-engine/internal/driver"
	"github.com/Artexis10/endstate/go-engine/internal/driver/winget"
	"github.com/Artexis10/endstate/go-engine/internal/realizer"
	"github.com/Artexis10/endstate/go-engine/internal/realizer/nix"
)

// ErrNoBackend indicates that no package backend is implemented for the host
// operating system. Non-Windows hosts return this until a platform backend
// (e.g. a Nix realizer) is added.
var ErrNoBackend = errors.New("no package backend available for this platform")

// ErrNoRealizer indicates that no whole-set package realizer is implemented for
// the host operating system. Windows returns this (it uses the per-package
// winget driver via selectBackend instead).
var ErrNoRealizer = errors.New("no package realizer available for this platform")

// selectBackend returns the package-manager driver for the given OS. Windows
// uses the winget driver; other platforms have no backend yet and return
// ErrNoBackend so callers fail explicitly rather than attempting installs.
func selectBackend(goos string) (driver.Driver, error) {
	switch goos {
	case "windows":
		return winget.New(), nil
	default:
		return nil, ErrNoBackend
	}
}

// selectRealizer returns the whole-set package realizer for the given OS. Linux
// and macOS use the Nix realizer; other platforms (Windows) have no realizer and
// return ErrNoRealizer, so callers fall back to the per-package driver path.
//
// selectBackend and selectRealizer are siblings: on any host at most one
// succeeds. The realizer is deliberately NOT a driver.Driver (the whole-set,
// atomic-generation model is kept beside the per-package Driver, not shoehorned
// into Driver.Install).
func selectRealizer(goos string) (realizer.Realizer, error) {
	switch goos {
	case "linux", "darwin":
		return nix.New(), nil
	default:
		return nil, ErrNoRealizer
	}
}
