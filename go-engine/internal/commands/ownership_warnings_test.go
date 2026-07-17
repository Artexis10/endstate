// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"reflect"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/manifest"
)

func ownershipRoute(index int, driverName, ref, displayName string) *routedDriverApp {
	return &routedDriverApp{
		index:      index,
		driverName: driverName,
		ref:        ref,
		app:        manifest.App{DisplayName: displayName},
	}
}

func TestPossibleDuplicatePackageWarnings_ExactTrimmedCaseInsensitiveMatch(t *testing.T) {
	routes := []*routedDriverApp{
		ownershipRoute(0, "winget", "jqlang.jq", " jq "),
		ownershipRoute(1, "chocolatey", "jq", "JQ"),
	}

	warnings := possibleDuplicatePackageWarnings(routes)
	if len(warnings) != 1 {
		t.Fatalf("warnings = %+v, want one", warnings)
	}
	warning := warnings[0]
	if warning.Code != "possible_duplicate" || warning.Driver != "chocolatey" || warning.Ref != "jq" {
		t.Fatalf("warning = %+v, want later Chocolatey entry provenance", warning)
	}
}

func TestPossibleDuplicatePackageWarnings_ConservativeExclusions(t *testing.T) {
	tests := []struct {
		name   string
		routes []*routedDriverApp
	}{
		{
			name: "same driver",
			routes: []*routedDriverApp{
				ownershipRoute(0, "winget", "Vendor.One", "Example"),
				ownershipRoute(1, "winget", "Vendor.Two", " example "),
			},
		},
		{
			name: "empty explicit name does not fall back to ref",
			routes: []*routedDriverApp{
				ownershipRoute(0, "winget", "same-ref", ""),
				ownershipRoute(1, "chocolatey", "same-ref", "  "),
			},
		},
		{
			name: "similar substring",
			routes: []*routedDriverApp{
				ownershipRoute(0, "winget", "Vendor.Git", "Git"),
				ownershipRoute(1, "chocolatey", "git.install", "Git for Windows"),
			},
		},
		{
			name: "punctuation is not normalized",
			routes: []*routedDriverApp{
				ownershipRoute(0, "winget", "7zip.7zip", "7-Zip"),
				ownershipRoute(1, "chocolatey", "7zip", "7 Zip"),
			},
		},
		{
			name: "manual entry",
			routes: []*routedDriverApp{
				ownershipRoute(0, "winget", "Vendor.Tool", "Tool"),
				func() *routedDriverApp {
					route := ownershipRoute(1, "manual", "", "tool")
					route.isManual = true
					return route
				}(),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if warnings := possibleDuplicatePackageWarnings(tt.routes); len(warnings) != 0 {
				t.Fatalf("warnings = %+v, want none", warnings)
			}
		})
	}
}

func TestPossibleDuplicatePackageWarnings_ThreeDriversWarnOncePerLaterEntry(t *testing.T) {
	routes := []*routedDriverApp{
		ownershipRoute(0, "winget", "Vendor.Tool", "Tool"),
		ownershipRoute(1, "chocolatey", "tool", "tool"),
		ownershipRoute(2, "brew", "tool", " TOOL "),
	}

	warnings := possibleDuplicatePackageWarnings(routes)
	want := []CommandWarning{
		{Code: "possible_duplicate", Driver: "chocolatey", Ref: "tool"},
		{Code: "possible_duplicate", Driver: "brew", Ref: "tool"},
	}
	if len(warnings) != len(want) {
		t.Fatalf("warnings = %+v, want two", warnings)
	}
	for i := range want {
		if warnings[i].Code != want[i].Code || warnings[i].Driver != want[i].Driver || warnings[i].Ref != want[i].Ref {
			t.Fatalf("warning provenance = %+v, want %+v", warnings, want)
		}
	}
	if got := []string{warnings[0].Driver, warnings[1].Driver}; !reflect.DeepEqual(got, []string{"chocolatey", "brew"}) {
		t.Fatalf("warning order = %v", got)
	}
}

func TestPossibleDuplicatePackageWarnings_DriverReturnsAfterCrossDriverCollision(t *testing.T) {
	routes := []*routedDriverApp{
		ownershipRoute(0, "winget", "Vendor.Tool.One", "Tool"),
		ownershipRoute(1, "chocolatey", "tool", "tool"),
		ownershipRoute(2, "winget", "Vendor.Tool.Two", " TOOL "),
	}

	warnings := possibleDuplicatePackageWarnings(routes)
	wantDrivers := []string{"chocolatey", "winget"}
	if len(warnings) != len(wantDrivers) {
		t.Fatalf("warnings = %+v, want one for each later cross-driver-conflicting entry", warnings)
	}
	for i, driverName := range wantDrivers {
		if warnings[i].Code != "possible_duplicate" || warnings[i].Driver != driverName {
			t.Fatalf("warning %d = %+v, want driver %q", i, warnings[i], driverName)
		}
	}
}
