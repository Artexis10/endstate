// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"errors"
	"fmt"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/driver"
	"github.com/Artexis10/endstate/go-engine/internal/realizer"
)

// ErrUnsupportedDriver indicates that a named per-package driver is not
// registered for the selected platform.
var ErrUnsupportedDriver = errors.New("unsupported package driver")

// ErrDriverUnavailable indicates that a supported driver could not be
// constructed. Registration is a static capability; availability is resolved
// only when a command asks for the driver.
var ErrDriverUnavailable = errors.New("package driver unavailable")

type driverFactory func() (driver.Driver, error)
type realizerFactory func() (realizer.Realizer, error)

// backendFactories keeps platform registration hermetic and lazy. Capability
// reporting reads only the registered names and never invokes these factories.
type backendFactories struct {
	winget     driverFactory
	chocolatey driverFactory
	brew       driverFactory
	nix        realizerFactory
}

// platformBackendRegistry models the two deliberately distinct backend shapes:
// named per-package drivers and an optional whole-set realizer. driverOrder is
// the stable public capability order; the map is only for explicit resolution.
type platformBackendRegistry struct {
	driverOrder       []string
	drivers           map[string]driverFactory
	defaultDriverName string
	realizerName      string
	realizer          realizerFactory
}

func newPlatformBackendRegistry(goos string, factories backendFactories) platformBackendRegistry {
	r := platformBackendRegistry{drivers: make(map[string]driverFactory)}
	register := func(name string, factory driverFactory) {
		r.driverOrder = append(r.driverOrder, name)
		r.drivers[name] = factory
	}

	switch goos {
	case "windows":
		register("winget", factories.winget)
		register("chocolatey", factories.chocolatey)
		r.defaultDriverName = "winget"
	case "linux":
		r.realizerName = "nix"
		r.realizer = factories.nix
	case "darwin":
		r.realizerName = "nix"
		r.realizer = factories.nix
		register("brew", factories.brew)
	}
	return r
}

func (r platformBackendRegistry) DriverNames() []string {
	return append([]string{}, r.driverOrder...)
}

func (r platformBackendRegistry) DefaultDriverName() string { return r.defaultDriverName }
func (r platformBackendRegistry) RealizerName() string      { return r.realizerName }

// SupportedNames returns the public capability order. The whole-set realizer
// precedes additive per-package drivers on Unix, preserving [nix] and
// [nix, brew], while Windows returns [winget, chocolatey].
func (r platformBackendRegistry) SupportedNames() []string {
	names := make([]string, 0, len(r.driverOrder)+1)
	if r.realizerName != "" {
		names = append(names, r.realizerName)
	}
	return append(names, r.driverOrder...)
}

// ResolveDriver resolves an explicit name case-insensitively. An omitted name
// selects the platform default; platforms with only a realizer have no default
// per-package driver and return ErrNoBackend.
func (r platformBackendRegistry) ResolveDriver(name string) (driver.Driver, error) {
	canonical := strings.ToLower(strings.TrimSpace(name))
	if canonical == "" {
		canonical = r.defaultDriverName
		if canonical == "" {
			return nil, ErrNoBackend
		}
	}

	factory, ok := r.drivers[canonical]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedDriver, canonical)
	}
	if factory == nil {
		return nil, fmt.Errorf("%w: %s", ErrDriverUnavailable, canonical)
	}
	d, err := factory()
	if err != nil {
		return nil, err
	}
	if d == nil {
		return nil, fmt.Errorf("%w: %s", ErrDriverUnavailable, canonical)
	}
	return d, nil
}

func (r platformBackendRegistry) ResolveRealizer() (realizer.Realizer, error) {
	if r.realizerName == "" || r.realizer == nil {
		return nil, ErrNoRealizer
	}
	rz, err := r.realizer()
	if err != nil {
		return nil, err
	}
	if rz == nil {
		return nil, ErrNoRealizer
	}
	return rz, nil
}
