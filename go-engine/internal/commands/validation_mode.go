// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/Artexis10/endstate/go-engine/internal/bootstrap"
	"github.com/Artexis10/endstate/go-engine/internal/driver"
	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/events"
	"github.com/Artexis10/endstate/go-engine/internal/realizer"
	"github.com/Artexis10/endstate/go-engine/internal/validationmode"
)

var validationModeActivationMu sync.Mutex
var currentValidationMode *validationmode.Context

// ValidationModeSession owns the command-layer package seams for one CLI run.
// The underlying package driver is constructed once and shared by every lane.
type ValidationModeSession struct {
	driver *validationPackageDriver

	restoreOnce sync.Once
	restoreErr  error
	restoreFn   func()
}

// ActivateValidationMode replaces package-manager construction, capture
// enumeration, realizer selection, and backend bootstrap with descriptor-bound
// disposable implementations. Restore is idempotent and reinstates every seam.
func ActivateValidationMode(context *validationmode.Context) (*ValidationModeSession, error) {
	if context == nil {
		return nil, errors.New("validation context is nil")
	}
	validationModeActivationMu.Lock()

	packageDriver, err := context.NewPackageDriver()
	if err != nil {
		validationModeActivationMu.Unlock()
		return nil, err
	}
	guarded := &validationPackageDriver{driver: packageDriver}
	descriptor := context.Descriptor()

	originalDefault := newDriverFn
	originalNamed := newNamedDriverFn
	originalRealizer := newRealizerFn
	originalBrew := newBrewDriverFn
	originalBootstrap := bootstrapBackendsFn
	originalCapture := enumerateCapturePackagesFn
	originalCaptureGOOS := captureGOOSFn
	originalRollback := rollbackDriverFn
	originalContext := currentValidationMode

	matchDriver := func(name string) (driver.Driver, error) {
		if !strings.EqualFold(strings.TrimSpace(name), descriptor.Inventory.Driver) {
			err := fmt.Errorf("%w: driver mismatch", validationmode.ErrPackageIdentity)
			guarded.record(err)
			return nil, err
		}
		return guarded, nil
	}

	newDriverFn = func() (driver.Driver, error) { return guarded, nil }
	newNamedDriverFn = matchDriver
	newRealizerFn = func() (realizer.Realizer, error) { return nil, ErrNoRealizer }
	newBrewDriverFn = func() (driver.Driver, error) {
		if strings.EqualFold(descriptor.Inventory.Driver, "brew") {
			return guarded, nil
		}
		return nil, ErrNoBrewDriver
	}
	bootstrapBackendsFn = func(needed []bootstrap.Backend, _ bool, _ Consent, _ *events.Emitter) (map[bootstrap.Backend]bool, *envelope.Error) {
		available := make(map[bootstrap.Backend]bool, len(needed))
		for _, backend := range needed {
			available[backend] = true
		}
		return available, nil
	}
	enumerateCapturePackagesFn = func(flags CaptureFlags) ([]enumeratedCapturePackage, []CommandWarning, *envelope.Error) {
		for _, requested := range flags.Drivers {
			if _, err := matchDriver(requested); err != nil {
				return nil, nil, envelope.NewError(envelope.ErrCaptureFailed, err.Error())
			}
		}
		packages, err := guarded.EnumerateInstalled()
		if err != nil {
			return nil, nil, envelope.NewError(envelope.ErrCaptureFailed, err.Error())
		}
		result := make([]enumeratedCapturePackage, 0, len(packages))
		for _, pkg := range packages {
			result = append(result, enumeratedCapturePackage{Driver: descriptor.Inventory.Driver, Package: pkg})
		}
		return result, nil, nil
	}
	captureGOOSFn = func() string { return "windows" }
	rollbackDriverFn = matchDriver
	currentValidationMode = context

	session := &ValidationModeSession{driver: guarded}
	session.restoreFn = func() {
		newDriverFn = originalDefault
		newNamedDriverFn = originalNamed
		newRealizerFn = originalRealizer
		newBrewDriverFn = originalBrew
		bootstrapBackendsFn = originalBootstrap
		enumerateCapturePackagesFn = originalCapture
		captureGOOSFn = originalCaptureGOOS
		rollbackDriverFn = originalRollback
		currentValidationMode = originalContext
		validationModeActivationMu.Unlock()
	}
	return session, nil
}

// Restore reinstates all command seams. It is safe to call repeatedly.
func (session *ValidationModeSession) Restore() error {
	if session == nil {
		return nil
	}
	session.restoreOnce.Do(session.restoreFn)
	return session.restoreErr
}

// IsolationError reports whether any package boundary rejected identity during
// the run. The CLI uses it to replace generic per-item errors with the stable
// fail-closed validation-mode envelope code.
func (session *ValidationModeSession) IsolationError() error {
	if session == nil || session.driver == nil {
		return nil
	}
	return session.driver.isolationError()
}

type validationPackageDriver struct {
	driver *validationmode.PackageDriver
	mu     sync.Mutex
	err    error
}

func (value *validationPackageDriver) record(err error) error {
	if err == nil {
		return nil
	}
	value.mu.Lock()
	value.err = errors.Join(value.err, err)
	value.mu.Unlock()
	return err
}

func (value *validationPackageDriver) isolationError() error {
	value.mu.Lock()
	defer value.mu.Unlock()
	return value.err
}

func (value *validationPackageDriver) Name() string { return value.driver.Name() }

func (value *validationPackageDriver) Detect(ref string) (bool, string, error) {
	present, name, err := value.driver.Detect(ref)
	return present, name, value.record(err)
}

func (value *validationPackageDriver) DetectSource(ref, source string) (bool, string, error) {
	present, name, err := value.driver.DetectSource(ref, source)
	return present, name, value.record(err)
}

func (value *validationPackageDriver) Install(ref string) (*driver.InstallResult, error) {
	result, err := value.driver.Install(ref)
	return result, value.record(err)
}

func (value *validationPackageDriver) InstallSource(ref, source string) (*driver.InstallResult, error) {
	result, err := value.driver.InstallSource(ref, source)
	return result, value.record(err)
}

func (value *validationPackageDriver) DetectBatch(refs []string) (map[string]driver.DetectResult, error) {
	result, err := value.driver.DetectBatch(refs)
	return result, value.record(err)
}

func (value *validationPackageDriver) DetectBatchSource(refs []string, source string) (map[string]driver.DetectResult, error) {
	result, err := value.driver.DetectBatchSource(refs, source)
	return result, value.record(err)
}

func (value *validationPackageDriver) EnumerateInstalled() ([]driver.InstalledPackage, error) {
	result, err := value.driver.EnumerateInstalled()
	return result, value.record(err)
}

func (value *validationPackageDriver) Uninstall(ref string) (*driver.UninstallResult, error) {
	result, err := value.driver.Uninstall(ref)
	return result, value.record(err)
}

func (value *validationPackageDriver) UninstallSource(ref, source string) (*driver.UninstallResult, error) {
	result, err := value.driver.UninstallSource(ref, source)
	return result, value.record(err)
}

func (value *validationPackageDriver) InstallVersion(ref, version string) (*driver.InstallResult, error) {
	result, err := value.driver.InstallVersion(ref, version)
	return result, value.record(err)
}

func (value *validationPackageDriver) ReinstallVersion(ref, version string) (*driver.InstallResult, error) {
	result, err := value.driver.ReinstallVersion(ref, version)
	return result, value.record(err)
}

func (value *validationPackageDriver) InstallVersionSource(ref, version, source string) (*driver.InstallResult, error) {
	result, err := value.driver.InstallVersionSource(ref, version, source)
	return result, value.record(err)
}

func (value *validationPackageDriver) ReinstallVersionSource(ref, version, source string) (*driver.InstallResult, error) {
	result, err := value.driver.ReinstallVersionSource(ref, version, source)
	return result, value.record(err)
}

var _ driver.Driver = (*validationPackageDriver)(nil)
var _ driver.SourceDriver = (*validationPackageDriver)(nil)
var _ driver.BatchDetector = (*validationPackageDriver)(nil)
var _ driver.SourceBatchDetector = (*validationPackageDriver)(nil)
var _ driver.InstalledEnumerator = (*validationPackageDriver)(nil)
var _ driver.Uninstaller = (*validationPackageDriver)(nil)
var _ driver.SourceUninstaller = (*validationPackageDriver)(nil)
var _ driver.VersionedInstaller = (*validationPackageDriver)(nil)
var _ driver.SourceVersionedInstaller = (*validationPackageDriver)(nil)
