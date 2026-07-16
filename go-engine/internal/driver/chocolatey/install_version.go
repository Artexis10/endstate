// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package chocolatey

import "github.com/Artexis10/endstate/go-engine/internal/driver"

var _ driver.VersionedInstaller = (*ChocolateyDriver)(nil)

// InstallVersion installs the exact requested version of an absent package.
func (c *ChocolateyDriver) InstallVersion(ref, version string) (*driver.InstallResult, error) {
	return c.install("install", ref, version, false)
}

// ReinstallVersion converges an installed package through Chocolatey's upgrade
// command, explicitly allowing the requested version to be a downgrade.
func (c *ChocolateyDriver) ReinstallVersion(ref, version string) (*driver.InstallResult, error) {
	return c.install("upgrade", ref, version, true)
}
