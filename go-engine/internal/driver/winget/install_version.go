// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package winget

import "github.com/Artexis10/endstate/go-engine/internal/driver"

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
	return w.install(ref, version)
}
