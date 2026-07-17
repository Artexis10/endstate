// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package chocolatey

import (
	"reflect"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/driver"
)

func TestEnumerateInstalledUsesCurrentLocalListSyntax(t *testing.T) {
	var calls [][]string
	d := &ChocolateyDriver{ExecCommand: scriptedCommand(map[string]scriptedResponse{
		"--version":           {stdout: "2.4.1\n"},
		"list --limit-output": {stdout: "git|2.46.0\n"},
	}, &calls)}

	_, err := d.EnumerateInstalled()
	if err != nil {
		t.Fatalf("EnumerateInstalled returned error: %v", err)
	}
	want := [][]string{{"choco", "--version"}, {"choco", "list", "--limit-output"}}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %v, want %v", calls, want)
	}
}

func TestEnumerateInstalledUsesLegacyLocalOnlySyntax(t *testing.T) {
	var calls [][]string
	d := &ChocolateyDriver{ExecCommand: scriptedCommand(map[string]scriptedResponse{
		"--version":                        {stdout: "1.4.0\n"},
		"list --local-only --limit-output": {stdout: "git|2.46.0\n"},
	}, &calls)}

	_, err := d.EnumerateInstalled()
	if err != nil {
		t.Fatalf("EnumerateInstalled returned error: %v", err)
	}
	want := [][]string{{"choco", "--version"}, {"choco", "list", "--local-only", "--limit-output"}}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %v, want %v", calls, want)
	}
}

func TestEnumerateInstalledParsesLedgerDeterministically(t *testing.T) {
	d := &ChocolateyDriver{ExecCommand: scriptedCommand(map[string]scriptedResponse{
		"--version":           {stdout: "2.4.1\n"},
		"list --limit-output": {stdout: "Zulu|3.0\nalpha|2.0\nChocolatey v2.4.1\n|1.0\nalpha|1.0\nempty-version|\nmalformed\n"},
	}, nil)}

	got, err := d.EnumerateInstalled()
	if err != nil {
		t.Fatalf("EnumerateInstalled returned error: %v", err)
	}
	want := []driver.InstalledPackage{
		{Ref: "alpha", DisplayName: "alpha", Version: "1.0"},
		{Ref: "alpha", DisplayName: "alpha", Version: "2.0"},
		{Ref: "empty-version", DisplayName: "empty-version", Version: ""},
		{Ref: "Zulu", DisplayName: "Zulu", Version: "3.0"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("packages = %+v, want %+v", got, want)
	}
}

func TestEnumerateInstalledExitTwoMeansNoLocalPackages(t *testing.T) {
	d := &ChocolateyDriver{ExecCommand: scriptedCommand(map[string]scriptedResponse{
		"--version":           {stdout: "2.4.1\n"},
		"list --limit-output": {exitCode: 2},
	}, nil)}

	got, err := d.EnumerateInstalled()
	if err != nil {
		t.Fatalf("EnumerateInstalled returned error: %v", err)
	}
	if got == nil || len(got) != 0 {
		t.Fatalf("packages = %#v, want non-nil empty slice", got)
	}
}

func TestEnumerateInstalledReportsListFailure(t *testing.T) {
	d := &ChocolateyDriver{ExecCommand: scriptedCommand(map[string]scriptedResponse{
		"--version":           {stdout: "2.4.1\n"},
		"list --limit-output": {exitCode: 7},
	}, nil)}

	if _, err := d.EnumerateInstalled(); err == nil {
		t.Fatal("EnumerateInstalled succeeded after failed local list")
	}
}

func TestEnumerateInstalledReportsVersionProbeFailures(t *testing.T) {
	tests := []struct {
		name     string
		response scriptedResponse
	}{
		{name: "nonzero", response: scriptedResponse{exitCode: 7}},
		{name: "malformed", response: scriptedResponse{stdout: "not-a-version\n"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := &ChocolateyDriver{ExecCommand: scriptedCommand(map[string]scriptedResponse{
				"--version": tt.response,
			}, nil)}
			if _, err := d.EnumerateInstalled(); err == nil {
				t.Fatal("EnumerateInstalled succeeded with unusable Chocolatey version response")
			}
		})
	}
}
