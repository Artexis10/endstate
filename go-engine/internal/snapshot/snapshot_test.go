// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package snapshot

import (
	"errors"
	"os/exec"
	"testing"
)

// sampleWingetOutput is a realistic winget list output used across tests.
const sampleWingetOutput = `Name                             Id                                Version        Source
---------------------------------------------------------------------------------------------------------
Visual Studio Code               Microsoft.VisualStudioCode        1.85.0         winget
Git                              Git.Git                           2.43.0         winget
Google Chrome                    Google.Chrome                     120.0.6099.130 winget
`

// withFakeExec temporarily replaces ExecCommand with a function that returns
// the given output and error. The original is restored when the returned
// cleanup function is called.
func withFakeExec(output []byte, err error) func() {
	orig := ExecCommand
	ExecCommand = func(name string, args ...string) ([]byte, error) {
		return output, err
	}
	return func() { ExecCommand = orig }
}

func TestTakeSnapshot_ParsesCorrectly(t *testing.T) {
	cleanup := withFakeExec([]byte(sampleWingetOutput), nil)
	defer cleanup()

	apps, err := TakeSnapshot()
	if err != nil {
		t.Fatalf("TakeSnapshot returned unexpected error: %v", err)
	}

	if len(apps) != 3 {
		t.Fatalf("expected 3 apps, got %d", len(apps))
	}

	// Verify first app.
	if apps[0].Name != "Visual Studio Code" {
		t.Errorf("expected Name=%q, got %q", "Visual Studio Code", apps[0].Name)
	}
	if apps[0].ID != "Microsoft.VisualStudioCode" {
		t.Errorf("expected ID=%q, got %q", "Microsoft.VisualStudioCode", apps[0].ID)
	}
	if apps[0].Version != "1.85.0" {
		t.Errorf("expected Version=%q, got %q", "1.85.0", apps[0].Version)
	}
	if apps[0].Source != "winget" {
		t.Errorf("expected Source=%q, got %q", "winget", apps[0].Source)
	}

	// Verify second app.
	if apps[1].Name != "Git" {
		t.Errorf("expected Name=%q, got %q", "Git", apps[1].Name)
	}
	if apps[1].ID != "Git.Git" {
		t.Errorf("expected ID=%q, got %q", "Git.Git", apps[1].ID)
	}
	if apps[1].Version != "2.43.0" {
		t.Errorf("expected Version=%q, got %q", "2.43.0", apps[1].Version)
	}

	// Verify third app.
	if apps[2].Name != "Google Chrome" {
		t.Errorf("expected Name=%q, got %q", "Google Chrome", apps[2].Name)
	}
	if apps[2].ID != "Google.Chrome" {
		t.Errorf("expected ID=%q, got %q", "Google.Chrome", apps[2].ID)
	}
	if apps[2].Version != "120.0.6099.130" {
		t.Errorf("expected Version=%q, got %q", "120.0.6099.130", apps[2].Version)
	}
}

func TestTakeSnapshot_EmptyOutput_ReturnsEmptySlice(t *testing.T) {
	cleanup := withFakeExec([]byte(""), nil)
	defer cleanup()

	apps, err := TakeSnapshot()
	if err != nil {
		t.Fatalf("TakeSnapshot returned unexpected error: %v", err)
	}

	if apps != nil && len(apps) != 0 {
		t.Errorf("expected empty/nil slice, got %d apps", len(apps))
	}
}

func TestTakeSnapshot_HeaderOnlyNoData_ReturnsEmptySlice(t *testing.T) {
	headerOnly := `Name                             Id                                Version        Source
---------------------------------------------------------------------------------------------------------
`
	cleanup := withFakeExec([]byte(headerOnly), nil)
	defer cleanup()

	apps, err := TakeSnapshot()
	if err != nil {
		t.Fatalf("TakeSnapshot returned unexpected error: %v", err)
	}

	if len(apps) != 0 {
		t.Errorf("expected 0 apps, got %d", len(apps))
	}
}

func TestTakeSnapshot_ShortLinesSkipped(t *testing.T) {
	outputWithShortLine := `Name                             Id                                Version        Source
---------------------------------------------------------------------------------------------------------
Visual Studio Code               Microsoft.VisualStudioCode        1.85.0         winget
Short
Git                              Git.Git                           2.43.0         winget
`
	cleanup := withFakeExec([]byte(outputWithShortLine), nil)
	defer cleanup()

	apps, err := TakeSnapshot()
	if err != nil {
		t.Fatalf("TakeSnapshot returned unexpected error: %v", err)
	}

	// Short line should be skipped; expect 2 apps.
	if len(apps) != 2 {
		t.Fatalf("expected 2 apps (short line skipped), got %d", len(apps))
	}

	if apps[0].ID != "Microsoft.VisualStudioCode" {
		t.Errorf("expected first app ID=%q, got %q", "Microsoft.VisualStudioCode", apps[0].ID)
	}
	if apps[1].ID != "Git.Git" {
		t.Errorf("expected second app ID=%q, got %q", "Git.Git", apps[1].ID)
	}
}

func TestTakeSnapshot_WingetNotFound_ReturnsError(t *testing.T) {
	cleanup := withFakeExec(nil, &exec.Error{Name: "winget", Err: exec.ErrNotFound})
	defer cleanup()

	_, err := TakeSnapshot()
	if err == nil {
		t.Fatal("expected error when winget not found, got nil")
	}

	var execErr *exec.Error
	if !errors.As(err, &execErr) {
		t.Errorf("expected exec.Error, got %T: %v", err, err)
	}
}

func TestGetDisplayNameMap_ReturnsCorrectMapping(t *testing.T) {
	cleanup := withFakeExec([]byte(sampleWingetOutput), nil)
	defer cleanup()

	nameMap, err := GetDisplayNameMap()
	if err != nil {
		t.Fatalf("GetDisplayNameMap returned unexpected error: %v", err)
	}

	expected := map[string]string{
		"Microsoft.VisualStudioCode": "Visual Studio Code",
		"Git.Git":                    "Git",
		"Google.Chrome":              "Google Chrome",
	}

	for id, expectedName := range expected {
		got, ok := nameMap[id]
		if !ok {
			t.Errorf("expected key %q in map, not found", id)
			continue
		}
		if got != expectedName {
			t.Errorf("expected nameMap[%q]=%q, got %q", id, expectedName, got)
		}
	}

	if len(nameMap) != len(expected) {
		t.Errorf("expected %d entries, got %d", len(expected), len(nameMap))
	}
}

func TestIsRuntimePackage(t *testing.T) {
	tests := []struct {
		id   string
		want bool
	}{
		{"Microsoft.VCRedist.2015+.x64", true},
		{"Microsoft.VCLibs.Desktop.14", true},
		{"Microsoft.UI.Xaml.2.8", true},
		{"Microsoft.DotNet.Runtime.8", true},
		{"Microsoft.WindowsAppRuntime.1.4", true},
		{"Microsoft.DirectX.Direct3D", true},
		{"Git.Git", false},
		{"Google.Chrome", false},
		{"Microsoft.VisualStudioCode", false},
	}

	for _, tt := range tests {
		got := IsRuntimePackage(tt.id)
		if got != tt.want {
			t.Errorf("IsRuntimePackage(%q) = %v, want %v", tt.id, got, tt.want)
		}
	}
}

func TestIsStoreID(t *testing.T) {
	tests := []struct {
		id   string
		want bool
	}{
		{"9NKSQGP7F2NH", true},    // typical Store ID starting with 9
		{"XPDCFJDKLZJLP8", true},  // typical Store ID starting with XP
		{"Git.Git", false},
		{"Microsoft.VisualStudioCode", false},
	}

	for _, tt := range tests {
		got := IsStoreID(tt.id)
		if got != tt.want {
			t.Errorf("IsStoreID(%q) = %v, want %v", tt.id, got, tt.want)
		}
	}
}

func TestTakeSnapshot_NonZeroExitWithOutput_StillParses(t *testing.T) {
	// winget sometimes returns non-zero but still produces valid output.
	cleanup := withFakeExec([]byte(sampleWingetOutput), errors.New("exit status 1"))
	defer cleanup()

	apps, err := TakeSnapshot()
	if err != nil {
		t.Fatalf("TakeSnapshot should parse output even with non-zero exit, got error: %v", err)
	}

	if len(apps) != 3 {
		t.Errorf("expected 3 apps, got %d", len(apps))
	}
}
