// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package chocolatey

import (
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/driver"
)

func TestUninstallDoesNotRemoveDependencies(t *testing.T) {
	var calls [][]string
	d := &ChocolateyDriver{ExecCommand: scriptedCommand(map[string]scriptedResponse{
		"uninstall git --yes --no-progress --limit-output": {},
	}, &calls)}

	result, err := d.Uninstall("git")
	if err != nil {
		t.Fatalf("Uninstall returned error: %v", err)
	}
	if result.Status != driver.StatusUninstalled {
		t.Fatalf("Uninstall result = %+v", result)
	}
	if len(calls) != 1 {
		t.Fatalf("calls = %v, want one uninstall", calls)
	}
	for _, arg := range calls[0] {
		if strings.Contains(strings.ToLower(arg), "dependenc") {
			t.Fatalf("uninstall must not request dependency removal: %v", calls[0])
		}
	}
}

func TestUninstallFailureIsItemFailure(t *testing.T) {
	d := &ChocolateyDriver{ExecCommand: scriptedCommand(map[string]scriptedResponse{
		"uninstall git --yes --no-progress --limit-output": {exitCode: 1, stderr: "failure"},
	}, nil)}

	result, err := d.Uninstall("git")
	if err != nil {
		t.Fatalf("expected item failure, got infrastructure error: %v", err)
	}
	if result.Status != driver.StatusFailed {
		t.Fatalf("Uninstall result = %+v, want failed", result)
	}
}

func TestUninstallClassifiesWindowsInstallerOutcomes(t *testing.T) {
	tests := []struct {
		name     string
		exitCode int
		want     string
	}{
		{name: "software not installed", exitCode: 1605, want: driver.StatusAbsent},
		{name: "product uninstalled", exitCode: 1614, want: driver.StatusUninstalled},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := classifyUninstallResult(commandResult{exitCode: tt.exitCode})
			if result.Status != tt.want {
				t.Fatalf("exit %d result = %+v, want status %s", tt.exitCode, result, tt.want)
			}
		})
	}
}
