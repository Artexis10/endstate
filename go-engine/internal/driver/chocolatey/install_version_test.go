// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package chocolatey

import (
	"strconv"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/driver"
)

func TestInstallVersionRequestsExactVersion(t *testing.T) {
	var calls [][]string
	d := &ChocolateyDriver{ExecCommand: scriptedCommand(map[string]scriptedResponse{
		"install git --yes --no-progress --limit-output --version 2.46.0": {},
	}, &calls)}

	result, err := d.InstallVersion("git", "2.46.0")
	if err != nil {
		t.Fatalf("InstallVersion returned error: %v", err)
	}
	if result.Status != driver.StatusInstalled {
		t.Fatalf("InstallVersion result = %+v", result)
	}
	if len(calls) != 1 {
		t.Fatalf("calls = %v, want exactly one exact-version install", calls)
	}
	assertConfiguredSourcesUntouched(t, calls)
}

func TestReinstallVersionUsesDowngradeCapableUpgrade(t *testing.T) {
	var calls [][]string
	d := &ChocolateyDriver{ExecCommand: scriptedCommand(map[string]scriptedResponse{
		"upgrade git --yes --no-progress --limit-output --version 2.45.0 --allow-downgrade": {},
	}, &calls)}

	result, err := d.ReinstallVersion("git", "2.45.0")
	if err != nil {
		t.Fatalf("ReinstallVersion returned error: %v", err)
	}
	if result.Status != driver.StatusInstalled {
		t.Fatalf("ReinstallVersion result = %+v", result)
	}
	if len(calls) != 1 {
		t.Fatalf("calls = %v, want exactly one downgrade-capable upgrade", calls)
	}
	assertConfiguredSourcesUntouched(t, calls)
}

func TestInstallRebootExitIsSuccessfulFact(t *testing.T) {
	for _, exitCode := range []int{1641, 3010} {
		t.Run(strconv.Itoa(exitCode), func(t *testing.T) {
			d := &ChocolateyDriver{ExecCommand: scriptedCommand(map[string]scriptedResponse{
				"install git --yes --no-progress --limit-output --version 2.46.0": {exitCode: exitCode},
			}, nil)}

			result, err := d.InstallVersion("git", "2.46.0")
			if err != nil {
				t.Fatalf("InstallVersion returned error: %v", err)
			}
			if result.Status != driver.StatusInstalled || !result.RebootRequired {
				t.Fatalf("exit %d result = %+v, want successful reboot fact", exitCode, result)
			}
		})
	}
}
