// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package chocolatey

import (
	"fmt"

	"github.com/Artexis10/endstate/go-engine/internal/driver"
)

var _ driver.Uninstaller = (*ChocolateyDriver)(nil)

const (
	exitSoftwareNotInstalled = 1605
	exitProductUninstalled   = 1614
)

// Uninstall removes only the selected package. Chocolatey's dependency-removal
// switch is intentionally omitted so rollback cannot recursively remove shared
// dependencies.
func (c *ChocolateyDriver) Uninstall(ref string) (*driver.UninstallResult, error) {
	result, err := c.run("uninstall", ref, "--yes", "--no-progress", "--limit-output")
	if err != nil {
		return nil, err
	}
	if result.exitCode == exitSoftwareNotInstalled {
		return &driver.UninstallResult{Status: driver.StatusAbsent, Message: "Package was not installed"}, nil
	}
	if successfulExit(result.exitCode) || result.exitCode == exitProductUninstalled {
		return &driver.UninstallResult{Status: driver.StatusUninstalled, Message: "Uninstalled successfully"}, nil
	}
	if absentPattern.MatchString(result.stdout + result.stderr) {
		return &driver.UninstallResult{Status: driver.StatusAbsent, Message: "Package was not installed"}, nil
	}
	return &driver.UninstallResult{
		Status:  driver.StatusFailed,
		Message: fmt.Sprintf("chocolatey exited with code %d", result.exitCode),
	}, nil
}
