// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"errors"

	"github.com/Artexis10/endstate/go-engine/internal/driver"
	"github.com/Artexis10/endstate/go-engine/internal/driver/brew"
	"github.com/Artexis10/endstate/go-engine/internal/driver/chocolatey"
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

// ErrNoBrewDriver indicates that the Homebrew driver is not available on the
// host operating system. Only darwin returns a brew driver; every other host
// returns this so the brew lane no-ops there (a driver:"brew" app on a non-
// darwin host is surfaced as a visible skip rather than installed).
var ErrNoBrewDriver = errors.New("no brew driver available for this platform")

// selectBackend returns the package-manager driver for the given OS. Windows
// uses the winget driver; other platforms have no backend yet and return
// ErrNoBackend so callers fail explicitly rather than attempting installs.
func selectBackend(goos string) (driver.Driver, error) {
	return platformBackendsFor(goos).ResolveDriver("")
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
	return platformBackendsFor(goos).ResolveRealizer()
}

// selectBrewDriver returns the Homebrew per-package driver for the given OS.
// Unlike selectBackend/selectRealizer (which are mutually exclusive — at most
// one succeeds per host), this is ADDITIVE: on darwin BOTH selectRealizer and
// selectBrewDriver succeed, because the realizer owns the default lane and the
// brew driver owns the explicit driver:"brew" lane in the same run. Every non-
// darwin host returns ErrNoBrewDriver.
func selectBrewDriver(goos string) (driver.Driver, error) {
	d, err := platformBackendsFor(goos).ResolveDriver("brew")
	if err != nil {
		return nil, ErrNoBrewDriver
	}
	return d, nil
}

// selectDriver resolves either the platform default (empty name) or an
// authoritative explicit per-package driver. Commands use this as multi-driver
// routing is generalized; selectBackend remains the compatibility default seam.
func selectDriver(goos, name string) (driver.Driver, error) {
	return platformBackendsFor(goos).ResolveDriver(name)
}

// platformBackendsFor is the single concrete construction point shared by the
// legacy selectors and capability reporting. Constructors are lazy: asking for
// supported names never checks whether package-manager executables are installed.
func platformBackendsFor(goos string) platformBackendRegistry {
	return newPlatformBackendRegistry(goos, backendFactories{
		winget:     func() (driver.Driver, error) { return winget.New(), nil },
		chocolatey: func() (driver.Driver, error) { return chocolatey.New(), nil },
		brew:       func() (driver.Driver, error) { return brew.New(), nil },
		nix:        func() (realizer.Realizer, error) { return nix.New(), nil },
	})
}
