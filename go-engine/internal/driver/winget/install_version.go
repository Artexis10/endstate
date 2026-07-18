// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package winget

import (
	"github.com/Artexis10/endstate/go-engine/internal/driver"
	"github.com/Artexis10/endstate/go-engine/internal/packagesource"
)

// compile-time assertion: the winget driver implements the optional
// VersionedInstaller capability, so the apply path can discover version-pinning
// eligibility by type-assertion.
var _ driver.VersionedInstaller = (*WingetDriver)(nil)

// InstallVersion installs the exact given version of ref via
// `winget install --version <version>`. It is the version-pinning counterpart of
// Install and shares its implementation and exit-code classification: an
// unavailable version makes winget exit non-zero (and not the already-installed
// HRESULT), so it surfaces as StatusFailed/ReasonInstallFailed — the pin is
// never silently satisfied by a different version.
func (w *WingetDriver) InstallVersion(ref, version string) (*driver.InstallResult, error) {
	return w.InstallVersionSource(ref, version, packagesource.ResolveWinget(ref, ""))
}

func (w *WingetDriver) InstallVersionSource(ref, version, source string) (*driver.InstallResult, error) {
	return w.install(ref, version, false, packagesource.ResolveWinget(ref, source))
}

// ReinstallVersion force-reinstalls an exact version over an already-installed
// (drifted) one via `winget install --version <version> --force`. It is the
// convergence step of `apply --repin`: without --force, winget reports an
// installed-but-different package as already installed and won't switch versions.
// Same exit-code classification as InstallVersion.
func (w *WingetDriver) ReinstallVersion(ref, version string) (*driver.InstallResult, error) {
	return w.ReinstallVersionSource(ref, version, packagesource.ResolveWinget(ref, ""))
}

func (w *WingetDriver) ReinstallVersionSource(ref, version, source string) (*driver.InstallResult, error) {
	return w.install(ref, version, true, packagesource.ResolveWinget(ref, source))
}
