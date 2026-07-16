// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package chocolatey

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/driver"
)

var _ driver.InstalledEnumerator = (*ChocolateyDriver)(nil)

// EnumerateInstalled returns every entry in Chocolatey's local package ledger,
// including dependency and meta-packages, with versions as reported.
func (c *ChocolateyDriver) EnumerateInstalled() ([]driver.InstalledPackage, error) {
	return c.enumerate("")
}

func (c *ChocolateyDriver) enumerate(filter string) ([]driver.InstalledPackage, error) {
	major, err := c.majorVersion()
	if err != nil {
		return nil, err
	}

	args := []string{"list"}
	if filter != "" {
		args = append(args, filter)
	}
	if major < 2 {
		args = append(args, "--local-only")
	}
	if filter != "" {
		args = append(args, "--exact")
	}
	args = append(args, "--limit-output")

	result, err := c.run(args...)
	if err != nil {
		return nil, err
	}
	// Enhanced exit code 2 means the local query returned no results.
	if result.exitCode == 2 {
		return []driver.InstalledPackage{}, nil
	}
	if result.exitCode != 0 {
		return nil, fmt.Errorf("chocolatey local package listing failed with exit code %d", result.exitCode)
	}
	return parseLimitOutput(result.stdout), nil
}

func (c *ChocolateyDriver) majorVersion() (int, error) {
	result, err := c.run("--version")
	if err != nil {
		return 0, err
	}
	if result.exitCode != 0 {
		return 0, fmt.Errorf("chocolatey version check failed with exit code %d", result.exitCode)
	}
	version := strings.TrimSpace(result.stdout)
	version = strings.TrimPrefix(strings.ToLower(version), "v")
	majorText := version
	if dot := strings.IndexByte(majorText, '.'); dot >= 0 {
		majorText = majorText[:dot]
	}
	major, err := strconv.Atoi(majorText)
	if err != nil || major < 1 {
		return 0, fmt.Errorf("chocolatey returned an unusable version %q", strings.TrimSpace(result.stdout))
	}
	return major, nil
}

func parseLimitOutput(output string) []driver.InstalledPackage {
	packages := make([]driver.InstalledPackage, 0)
	for _, raw := range strings.Split(output, "\n") {
		line := strings.TrimSpace(raw)
		ref, version, found := strings.Cut(line, "|")
		ref = strings.TrimSpace(ref)
		version = strings.TrimSpace(version)
		if !found || ref == "" || strings.Contains(version, "|") {
			continue
		}
		packages = append(packages, driver.InstalledPackage{
			Ref:         ref,
			DisplayName: ref,
			Version:     version,
		})
	}

	sort.SliceStable(packages, func(i, j int) bool {
		left, right := strings.ToLower(packages[i].Ref), strings.ToLower(packages[j].Ref)
		if left != right {
			return left < right
		}
		if packages[i].Version != packages[j].Version {
			return packages[i].Version < packages[j].Version
		}
		return packages[i].Ref < packages[j].Ref
	})
	return packages
}
