// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationmode

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/Artexis10/endstate/go-engine/internal/driver"
	"github.com/Artexis10/endstate/go-engine/internal/safepath"
)

const packageStateFilename = "validation-package-state.json"

type packageState struct {
	SchemaVersion int    `json:"schemaVersion"`
	Driver        string `json:"driver"`
	Ref           string `json:"ref"`
	Source        string `json:"source,omitempty"`
	Present       bool   `json:"present"`
	Version       string `json:"version,omitempty"`
}

// PackageDriver is a data-only implementation of all production package
// capability interfaces. It has no process or network runner.
type PackageDriver struct {
	mu        sync.Mutex
	inventory Inventory
	statePath string
	state     packageState
}

// NewPackageDriver opens or initializes the descriptor-bound disposable state.
func (context *Context) NewPackageDriver() (*PackageDriver, error) {
	statePath, err := safepath.Resolve(context.root, ".endstate/"+packageStateFilename)
	if err != nil {
		return nil, fmt.Errorf("%w: state path: %v", ErrInvalidState, err)
	}
	value := &PackageDriver{inventory: context.descriptor.Inventory, statePath: statePath}
	if _, err := os.Lstat(statePath); os.IsNotExist(err) {
		value.state = packageState{
			SchemaVersion: 1,
			Driver:        value.inventory.Driver,
			Ref:           value.inventory.Ref,
			Source:        value.inventory.Source,
			Present:       value.inventory.InitialState == "present",
			Version:       value.inventory.Version,
		}
		if !value.state.Present {
			value.state.Version = ""
		}
		if err := value.persist(); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, fmt.Errorf("%w: stat state: %v", ErrInvalidState, err)
	} else if err := value.load(); err != nil {
		return nil, err
	}
	return value, nil
}

func (value *PackageDriver) Name() string { return value.inventory.Driver }

func (value *PackageDriver) Detect(ref string) (bool, string, error) {
	if err := value.checkCore(ref); err != nil {
		return false, "", err
	}
	return value.detect()
}

func (value *PackageDriver) DetectSource(ref, source string) (bool, string, error) {
	if err := value.checkSource(ref, source); err != nil {
		return false, "", err
	}
	return value.detect()
}

func (value *PackageDriver) detect() (bool, string, error) {
	value.mu.Lock()
	defer value.mu.Unlock()
	if !value.state.Present {
		return false, "", nil
	}
	return true, value.inventory.DisplayName, nil
}

func (value *PackageDriver) Install(ref string) (*driver.InstallResult, error) {
	if err := value.checkCore(ref); err != nil {
		return nil, err
	}
	return value.install(value.inventory.Version, false)
}

func (value *PackageDriver) InstallSource(ref, source string) (*driver.InstallResult, error) {
	if err := value.checkSource(ref, source); err != nil {
		return nil, err
	}
	return value.install(value.inventory.Version, false)
}

func (value *PackageDriver) InstallVersion(ref, version string) (*driver.InstallResult, error) {
	if err := value.checkCore(ref); err != nil {
		return nil, err
	}
	return value.installVersion(version, false)
}

func (value *PackageDriver) ReinstallVersion(ref, version string) (*driver.InstallResult, error) {
	if err := value.checkCore(ref); err != nil {
		return nil, err
	}
	return value.installVersion(version, true)
}

func (value *PackageDriver) InstallVersionSource(ref, version, source string) (*driver.InstallResult, error) {
	if err := value.checkSource(ref, source); err != nil {
		return nil, err
	}
	return value.installVersion(version, false)
}

func (value *PackageDriver) ReinstallVersionSource(ref, version, source string) (*driver.InstallResult, error) {
	if err := value.checkSource(ref, source); err != nil {
		return nil, err
	}
	return value.installVersion(version, true)
}

func (value *PackageDriver) installVersion(version string, force bool) (*driver.InstallResult, error) {
	if !safeToken(version) {
		return nil, fmt.Errorf("%w: invalid version", ErrPackageIdentity)
	}
	return value.install(version, force)
}

func (value *PackageDriver) install(version string, force bool) (*driver.InstallResult, error) {
	value.mu.Lock()
	defer value.mu.Unlock()
	wasPresent := value.state.Present
	value.state.Present = true
	if version != "" {
		value.state.Version = version
	}
	if err := value.persist(); err != nil {
		return nil, err
	}
	if wasPresent && !force {
		return &driver.InstallResult{Status: driver.StatusPresent, Reason: driver.ReasonAlreadyInstalled, Message: "package already present in disposable inventory"}, nil
	}
	return &driver.InstallResult{Status: driver.StatusInstalled, Message: "package installed in disposable inventory"}, nil
}

func (value *PackageDriver) Uninstall(ref string) (*driver.UninstallResult, error) {
	if err := value.checkCore(ref); err != nil {
		return nil, err
	}
	return value.uninstall()
}

func (value *PackageDriver) UninstallSource(ref, source string) (*driver.UninstallResult, error) {
	if err := value.checkSource(ref, source); err != nil {
		return nil, err
	}
	return value.uninstall()
}

func (value *PackageDriver) uninstall() (*driver.UninstallResult, error) {
	value.mu.Lock()
	defer value.mu.Unlock()
	if !value.state.Present {
		return &driver.UninstallResult{Status: driver.StatusAbsent, Message: "package already absent from disposable inventory"}, nil
	}
	value.state.Present = false
	value.state.Version = ""
	if err := value.persist(); err != nil {
		return nil, err
	}
	return &driver.UninstallResult{Status: driver.StatusUninstalled, Message: "package uninstalled from disposable inventory"}, nil
}

func (value *PackageDriver) DetectBatch(refs []string) (map[string]driver.DetectResult, error) {
	if value.inventory.Source != "" {
		return nil, fmt.Errorf("%w: source is required", ErrPackageIdentity)
	}
	return value.detectBatch(refs)
}

func (value *PackageDriver) DetectBatchSource(refs []string, source string) (map[string]driver.DetectResult, error) {
	if source != value.inventory.Source {
		return nil, fmt.Errorf("%w: source mismatch", ErrPackageIdentity)
	}
	return value.detectBatch(refs)
}

func (value *PackageDriver) detectBatch(refs []string) (map[string]driver.DetectResult, error) {
	if len(refs) == 0 {
		return map[string]driver.DetectResult{}, nil
	}
	if len(refs) != 1 || refs[0] != value.inventory.Ref {
		return nil, fmt.Errorf("%w: batch contains unknown or extra refs", ErrPackageIdentity)
	}
	value.mu.Lock()
	defer value.mu.Unlock()
	result := make(map[string]driver.DetectResult, 1)
	if value.state.Present {
		result[value.inventory.Ref] = driver.DetectResult{Installed: true, DisplayName: value.inventory.DisplayName, Version: value.state.Version}
	}
	return result, nil
}

func (value *PackageDriver) EnumerateInstalled() ([]driver.InstalledPackage, error) {
	value.mu.Lock()
	defer value.mu.Unlock()
	if !value.state.Present {
		return []driver.InstalledPackage{}, nil
	}
	return []driver.InstalledPackage{{
		Ref: value.inventory.Ref, DisplayName: value.inventory.DisplayName,
		Version: value.state.Version, Source: value.inventory.Source,
	}}, nil
}

// ExternalCallCount is a testable invariant: this driver has no external runner.
func (value *PackageDriver) ExternalCallCount() uint64 { return 0 }

func (value *PackageDriver) checkCore(ref string) error {
	if value.inventory.Source != "" {
		return fmt.Errorf("%w: source is required", ErrPackageIdentity)
	}
	return value.checkRef(ref)
}

func (value *PackageDriver) checkSource(ref, source string) error {
	if source != value.inventory.Source {
		return fmt.Errorf("%w: source mismatch", ErrPackageIdentity)
	}
	return value.checkRef(ref)
}

func (value *PackageDriver) checkRef(ref string) error {
	if ref != value.inventory.Ref {
		return fmt.Errorf("%w: ref mismatch", ErrPackageIdentity)
	}
	return nil
}

func (value *PackageDriver) load() error {
	data, _, err := safepath.ReadRegularFile(value.statePath)
	if err != nil {
		return fmt.Errorf("%w: read state: %v", ErrInvalidState, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value.state); err != nil {
		return fmt.Errorf("%w: decode state: %v", ErrInvalidState, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return fmt.Errorf("%w: trailing state data", ErrInvalidState)
	}
	if value.state.SchemaVersion != 1 || value.state.Driver != value.inventory.Driver || value.state.Ref != value.inventory.Ref || value.state.Source != value.inventory.Source || (!value.state.Present && value.state.Version != "") || (value.state.Version != "" && !safeToken(value.state.Version)) {
		return fmt.Errorf("%w: state identity or fields do not match descriptor", ErrInvalidState)
	}
	return nil
}

func (value *PackageDriver) persist() error {
	data, err := json.Marshal(value.state)
	if err != nil {
		return fmt.Errorf("%w: encode state: %v", ErrInvalidState, err)
	}
	data = append(data, '\n')
	if err := safepath.AtomicWriteFile(value.statePath, data, 0o600); err != nil {
		return fmt.Errorf("%w: persist state: %v", ErrInvalidState, err)
	}
	return nil
}

var _ driver.Driver = (*PackageDriver)(nil)
var _ driver.SourceDriver = (*PackageDriver)(nil)
var _ driver.BatchDetector = (*PackageDriver)(nil)
var _ driver.SourceBatchDetector = (*PackageDriver)(nil)
var _ driver.InstalledEnumerator = (*PackageDriver)(nil)
var _ driver.Uninstaller = (*PackageDriver)(nil)
var _ driver.SourceUninstaller = (*PackageDriver)(nil)
var _ driver.VersionedInstaller = (*PackageDriver)(nil)
var _ driver.SourceVersionedInstaller = (*PackageDriver)(nil)
