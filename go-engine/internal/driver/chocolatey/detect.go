// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package chocolatey

import (
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/driver"
)

// Detect checks Chocolatey's local ledger for one exact package identifier.
func (c *ChocolateyDriver) Detect(ref string) (bool, string, error) {
	packages, err := c.enumerate(ref)
	if err != nil {
		return false, "", err
	}
	for _, pkg := range packages {
		if strings.EqualFold(pkg.Ref, ref) {
			return true, pkg.DisplayName, nil
		}
	}
	return false, "", nil
}

// DetectBatch checks all requested refs against one local-ledger enumeration.
func (c *ChocolateyDriver) DetectBatch(refs []string) (map[string]driver.DetectResult, error) {
	packages, err := c.EnumerateInstalled()
	if err != nil {
		return nil, err
	}
	installed := make(map[string]driver.InstalledPackage, len(packages))
	for _, pkg := range packages {
		installed[strings.ToLower(pkg.Ref)] = pkg
	}

	results := make(map[string]driver.DetectResult, len(refs))
	for _, ref := range refs {
		pkg, found := installed[strings.ToLower(ref)]
		results[ref] = driver.DetectResult{
			Installed:   found,
			DisplayName: pkg.DisplayName,
			Version:     pkg.Version,
		}
	}
	return results, nil
}
