// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"errors"
	"reflect"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/driver"
	"github.com/Artexis10/endstate/go-engine/internal/realizer"
)

type registryTestDriver struct{ name string }

func (d registryTestDriver) Name() string { return d.name }
func (d registryTestDriver) Detect(string) (bool, string, error) {
	return false, "", nil
}
func (d registryTestDriver) Install(string) (*driver.InstallResult, error) {
	return &driver.InstallResult{}, nil
}

type registryTestRealizer struct{ name string }

func (r registryTestRealizer) Name() string { return r.name }
func (r registryTestRealizer) Current() (realizer.Set, error) {
	return realizer.Set{}, nil
}
func (r registryTestRealizer) Plan([]realizer.Installable) (realizer.Diff, error) {
	return realizer.Diff{}, nil
}
func (r registryTestRealizer) Realize([]realizer.Installable) (realizer.Result, error) {
	return realizer.Result{}, nil
}

func registryTestFactories() backendFactories {
	return backendFactories{
		winget: func() (driver.Driver, error) { return registryTestDriver{name: "winget"}, nil },
		chocolatey: func() (driver.Driver, error) {
			return registryTestDriver{name: "chocolatey"}, nil
		},
		brew: func() (driver.Driver, error) { return registryTestDriver{name: "brew"}, nil },
		nix:  func() (realizer.Realizer, error) { return registryTestRealizer{name: "nix"}, nil },
	}
}

func TestPlatformBackendRegistry_Contents(t *testing.T) {
	tests := []struct {
		goos          string
		drivers       []string
		defaultDriver string
		realizer      string
		supported     []string
	}{
		{goos: "windows", drivers: []string{"winget", "chocolatey"}, defaultDriver: "winget", supported: []string{"winget", "chocolatey"}},
		{goos: "linux", drivers: []string{}, realizer: "nix", supported: []string{"nix"}},
		{goos: "darwin", drivers: []string{"brew"}, realizer: "nix", supported: []string{"nix", "brew"}},
		{goos: "plan9", drivers: []string{}, supported: []string{}},
	}

	for _, tt := range tests {
		t.Run(tt.goos, func(t *testing.T) {
			registry := newPlatformBackendRegistry(tt.goos, registryTestFactories())
			if got := registry.DriverNames(); !reflect.DeepEqual(got, tt.drivers) {
				t.Errorf("DriverNames() = %v, want %v", got, tt.drivers)
			}
			if got := registry.DefaultDriverName(); got != tt.defaultDriver {
				t.Errorf("DefaultDriverName() = %q, want %q", got, tt.defaultDriver)
			}
			if got := registry.RealizerName(); got != tt.realizer {
				t.Errorf("RealizerName() = %q, want %q", got, tt.realizer)
			}
			if got := registry.SupportedNames(); !reflect.DeepEqual(got, tt.supported) {
				t.Errorf("SupportedNames() = %v, want %v", got, tt.supported)
			}
		})
	}
}

func TestPlatformBackendRegistry_ResolveDefaultAndExplicitDriver(t *testing.T) {
	registry := newPlatformBackendRegistry("windows", registryTestFactories())

	defaultDriver, err := registry.ResolveDriver("")
	if err != nil {
		t.Fatalf("ResolveDriver(default) error = %v", err)
	}
	if defaultDriver.Name() != "winget" {
		t.Errorf("ResolveDriver(default).Name() = %q, want winget", defaultDriver.Name())
	}

	explicitDriver, err := registry.ResolveDriver("ChOcOlAtEy")
	if err != nil {
		t.Fatalf("ResolveDriver(chocolatey) error = %v", err)
	}
	if explicitDriver.Name() != "chocolatey" {
		t.Errorf("ResolveDriver(chocolatey).Name() = %q, want chocolatey", explicitDriver.Name())
	}
}

func TestPlatformBackendRegistry_RejectsUnsupportedDriver(t *testing.T) {
	registry := newPlatformBackendRegistry("windows", registryTestFactories())
	got, err := registry.ResolveDriver("scoop")
	if got != nil {
		t.Errorf("ResolveDriver(scoop) = %v, want nil", got)
	}
	if !errors.Is(err, ErrUnsupportedDriver) {
		t.Fatalf("ResolveDriver(scoop) error = %v, want ErrUnsupportedDriver", err)
	}
}

func TestPlatformBackendRegistry_SupportedNamesDoNotInvokeFactories(t *testing.T) {
	called := false
	factories := registryTestFactories()
	factories.winget = func() (driver.Driver, error) {
		called = true
		return nil, errors.New("probe should not run")
	}
	factories.chocolatey = factories.winget

	registry := newPlatformBackendRegistry("windows", factories)
	if got := registry.SupportedNames(); !reflect.DeepEqual(got, []string{"winget", "chocolatey"}) {
		t.Fatalf("SupportedNames() = %v, want [winget chocolatey]", got)
	}
	if called {
		t.Fatal("SupportedNames invoked a driver factory; capabilities must not probe installed executables")
	}
}

func TestPlatformBackendRegistry_ResolveRealizer(t *testing.T) {
	registry := newPlatformBackendRegistry("darwin", registryTestFactories())
	rz, err := registry.ResolveRealizer()
	if err != nil {
		t.Fatalf("ResolveRealizer() error = %v", err)
	}
	if rz.Name() != "nix" {
		t.Errorf("ResolveRealizer().Name() = %q, want nix", rz.Name())
	}
}
