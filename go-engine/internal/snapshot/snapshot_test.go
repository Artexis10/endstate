// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package snapshot

import (
	"errors"
	"os"
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

// --- WingetExport tests ---

// sampleWingetExportJSON is a realistic winget export output used across tests.
const sampleWingetExportJSON = `{
  "$schema": "https://aka.ms/winget-packages.schema.2.0.json",
  "CreationDate": "2026-03-11T00:00:00.000Z",
  "Sources": [
    {
      "SourceDetails": {
        "Name": "winget",
        "Identifier": "Microsoft.Winget.Source_8wekyb3d8bbwe",
        "Argument": "https://cdn.winget.microsoft.com/cache",
        "Type": "Microsoft.PreIndexed.Package"
      },
      "Packages": [
        { "PackageIdentifier": "Microsoft.VisualStudioCode" },
        { "PackageIdentifier": "Git.Git" },
        { "PackageIdentifier": "Google.Chrome" }
      ]
    }
  ]
}`

// withFakeExecWithFile temporarily replaces ExecCommandWithFile with a function
// that writes the given file content to the outFile path and returns the given
// error.  The original is restored when the returned cleanup function is called.
func withFakeExecWithFile(fileContent []byte, cmdErr error) func() {
	orig := ExecCommandWithFile
	ExecCommandWithFile = func(outFile string, name string, args ...string) error {
		if fileContent != nil {
			if writeErr := os.WriteFile(outFile, fileContent, 0600); writeErr != nil {
				return writeErr
			}
		}
		return cmdErr
	}
	return func() { ExecCommandWithFile = orig }
}

func TestParseWingetExport_ParsesCorrectly(t *testing.T) {
	apps, err := parseWingetExport([]byte(sampleWingetExportJSON))
	if err != nil {
		t.Fatalf("parseWingetExport returned unexpected error: %v", err)
	}

	if len(apps) != 3 {
		t.Fatalf("expected 3 apps, got %d", len(apps))
	}

	expected := []struct{ id, source string }{
		{"Microsoft.VisualStudioCode", "winget"},
		{"Git.Git", "winget"},
		{"Google.Chrome", "winget"},
	}

	for i, want := range expected {
		if apps[i].ID != want.id {
			t.Errorf("apps[%d].ID = %q, want %q", i, apps[i].ID, want.id)
		}
		if apps[i].Source != want.source {
			t.Errorf("apps[%d].Source = %q, want %q", i, apps[i].Source, want.source)
		}
		// Name is not populated by export — it comes from the display-name map.
		if apps[i].Name != "" {
			t.Errorf("apps[%d].Name = %q, want empty (export does not provide names)", i, apps[i].Name)
		}
	}
}

func TestParseWingetExport_EmptyInput_ReturnsNil(t *testing.T) {
	apps, err := parseWingetExport([]byte(""))
	if err != nil {
		t.Fatalf("expected no error on empty input, got: %v", err)
	}
	if apps != nil {
		t.Errorf("expected nil slice for empty input, got %v", apps)
	}
}

func TestParseWingetExport_NoSources_ReturnsEmpty(t *testing.T) {
	input := `{"Sources": []}`
	apps, err := parseWingetExport([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(apps) != 0 {
		t.Errorf("expected 0 apps, got %d", len(apps))
	}
}

func TestParseWingetExport_SkipsEmptyPackageIdentifier(t *testing.T) {
	input := `{
  "Sources": [{
    "SourceDetails": {"Name": "winget"},
    "Packages": [
      {"PackageIdentifier": "Git.Git"},
      {"PackageIdentifier": ""},
      {"PackageIdentifier": "Google.Chrome"}
    ]
  }]
}`
	apps, err := parseWingetExport([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(apps) != 2 {
		t.Fatalf("expected 2 apps (empty identifier skipped), got %d", len(apps))
	}
	if apps[0].ID != "Git.Git" {
		t.Errorf("expected apps[0].ID=%q, got %q", "Git.Git", apps[0].ID)
	}
	if apps[1].ID != "Google.Chrome" {
		t.Errorf("expected apps[1].ID=%q, got %q", "Google.Chrome", apps[1].ID)
	}
}

func TestParseWingetExport_MultipleSourcesNotIncluded(t *testing.T) {
	// In practice winget export --source winget returns only the winget source.
	// Verify the parser handles multiple sources correctly if they appear.
	input := `{
  "Sources": [
    {
      "SourceDetails": {"Name": "winget"},
      "Packages": [{"PackageIdentifier": "Git.Git"}]
    },
    {
      "SourceDetails": {"Name": "msstore"},
      "Packages": [{"PackageIdentifier": "9NKSQGP7F2NH"}]
    }
  ]
}`
	apps, err := parseWingetExport([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(apps) != 2 {
		t.Fatalf("expected 2 apps across sources, got %d", len(apps))
	}
	if apps[0].Source != "winget" {
		t.Errorf("apps[0].Source = %q, want %q", apps[0].Source, "winget")
	}
	if apps[1].Source != "msstore" {
		t.Errorf("apps[1].Source = %q, want %q", apps[1].Source, "msstore")
	}
}

func TestParseWingetExport_InvalidJSON_ReturnsError(t *testing.T) {
	_, err := parseWingetExport([]byte("{not valid json"))
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestWingetExport_ParsesCorrectly(t *testing.T) {
	cleanup := withFakeExecWithFile([]byte(sampleWingetExportJSON), nil)
	defer cleanup()

	apps, err := WingetExport()
	if err != nil {
		t.Fatalf("WingetExport returned unexpected error: %v", err)
	}

	if len(apps) != 3 {
		t.Fatalf("expected 3 apps, got %d", len(apps))
	}

	if apps[0].ID != "Microsoft.VisualStudioCode" {
		t.Errorf("expected ID=%q, got %q", "Microsoft.VisualStudioCode", apps[0].ID)
	}
	if apps[1].ID != "Git.Git" {
		t.Errorf("expected ID=%q, got %q", "Git.Git", apps[1].ID)
	}
	if apps[2].ID != "Google.Chrome" {
		t.Errorf("expected ID=%q, got %q", "Google.Chrome", apps[2].ID)
	}
}

func TestWingetExport_WingetNotFound_ReturnsError(t *testing.T) {
	cleanup := withFakeExecWithFile(nil, &exec.Error{Name: "winget", Err: exec.ErrNotFound})
	defer cleanup()

	_, err := WingetExport()
	if err == nil {
		t.Fatal("expected error when winget not found, got nil")
	}

	var execErr *exec.Error
	if !errors.As(err, &execErr) {
		t.Errorf("expected exec.Error, got %T: %v", err, err)
	}
}

func TestWingetExport_NonZeroExitButFileWritten_StillParses(t *testing.T) {
	// winget export may return non-zero but still produce a valid file.
	cleanup := withFakeExecWithFile([]byte(sampleWingetExportJSON), errors.New("exit status 1"))
	defer cleanup()

	apps, err := WingetExport()
	if err != nil {
		t.Fatalf("WingetExport should parse output even with non-zero exit, got error: %v", err)
	}

	if len(apps) != 3 {
		t.Errorf("expected 3 apps, got %d", len(apps))
	}
}

// ---------------------------------------------------------------------------
// IsRuntimePackage edge cases (mirrors Pester Capture.Filters.RuntimePackages)
// ---------------------------------------------------------------------------

func TestIsRuntimePackage_EmptyString(t *testing.T) {
	if IsRuntimePackage("") {
		t.Error("expected false for empty string")
	}
}

func TestIsRuntimePackage_RegularMicrosoftApps(t *testing.T) {
	// These are Microsoft apps but NOT runtimes.
	nonRuntime := []string{
		"Microsoft.VisualStudioCode",
		"Microsoft.PowerShell",
		"Microsoft.WindowsTerminal",
	}
	for _, id := range nonRuntime {
		if IsRuntimePackage(id) {
			t.Errorf("IsRuntimePackage(%q) = true, want false", id)
		}
	}
}

func TestIsRuntimePackage_AllRuntimeFamilies(t *testing.T) {
	runtimes := []struct {
		id   string
		desc string
	}{
		{"Microsoft.VCRedist.2015+.x64", "VCRedist"},
		{"Microsoft.VCRedist.2019.x86", "VCRedist variant"},
		{"Microsoft.VCLibs.140.00", "VCLibs"},
		{"Microsoft.VCLibs.Desktop", "VCLibs Desktop"},
		{"Microsoft.UI.Xaml.2.7", "UI Xaml 2.7"},
		{"Microsoft.UI.Xaml.2.8", "UI Xaml 2.8"},
		{"Microsoft.DotNet.DesktopRuntime.6", "DotNet DesktopRuntime"},
		{"Microsoft.DotNet.SDK.8", "DotNet SDK"},
		{"Microsoft.WindowsAppRuntime.1.4", "WindowsAppRuntime"},
		{"Microsoft.DirectX.Runtime", "DirectX Runtime"},
	}
	for _, tt := range runtimes {
		if !IsRuntimePackage(tt.id) {
			t.Errorf("IsRuntimePackage(%q) [%s] = false, want true", tt.id, tt.desc)
		}
	}
}

func TestIsRuntimePackage_NonMicrosoftApps(t *testing.T) {
	nonRuntime := []string{"Git.Git", "Mozilla.Firefox"}
	for _, id := range nonRuntime {
		if IsRuntimePackage(id) {
			t.Errorf("IsRuntimePackage(%q) = true, want false", id)
		}
	}
}

// ---------------------------------------------------------------------------
// IsStoreID edge cases (mirrors Pester Capture.Filters.StoreApps)
// ---------------------------------------------------------------------------

func TestIsStoreID_9NPattern(t *testing.T) {
	if !IsStoreID("9NBLGGH4NNS1") {
		t.Error("expected true for 9N* store ID pattern")
	}
}

func TestIsStoreID_XPPattern(t *testing.T) {
	if !IsStoreID("XPDC2RH70K22MN") {
		t.Error("expected true for XP* store ID pattern")
	}
}

func TestIsStoreID_RegularWingetID(t *testing.T) {
	regular := []string{"Git.Git", "Microsoft.VisualStudioCode", "Mozilla.Firefox"}
	for _, id := range regular {
		if IsStoreID(id) {
			t.Errorf("IsStoreID(%q) = true, want false", id)
		}
	}
}

func TestIsStoreID_EmptyString(t *testing.T) {
	if IsStoreID("") {
		t.Error("expected false for empty string")
	}
}

// ---------------------------------------------------------------------------
// Carriage return spinner cleanup (mirrors winget output edge cases)
// ---------------------------------------------------------------------------

func TestParseWingetList_CRSpinnerCleanup(t *testing.T) {
	// Winget writes progress spinners using \r. The parser must take text
	// after the last \r on each line.
	outputWithCR := "Some spinner\rName                             Id                                Version        Source\n" +
		"---------------------------------------------------------------------------------------------------------\n" +
		"Spinner\rGit                              Git.Git                           2.43.0         winget\n"

	cleanup := withFakeExec([]byte(outputWithCR), nil)
	defer cleanup()

	apps, err := TakeSnapshot()
	if err != nil {
		t.Fatalf("TakeSnapshot returned unexpected error: %v", err)
	}

	if len(apps) != 1 {
		t.Fatalf("expected 1 app after CR cleanup, got %d", len(apps))
	}
	if apps[0].ID != "Git.Git" {
		t.Errorf("expected ID=%q, got %q", "Git.Git", apps[0].ID)
	}
}

// ---------------------------------------------------------------------------
// Display name map edge case: empty output
// ---------------------------------------------------------------------------

func TestGetDisplayNameMap_EmptyOutput_ReturnsEmptyMap(t *testing.T) {
	cleanup := withFakeExec([]byte(""), nil)
	defer cleanup()

	nameMap, err := GetDisplayNameMap()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if nameMap == nil {
		t.Error("expected non-nil map for empty output")
	}
	if len(nameMap) != 0 {
		t.Errorf("expected empty map, got %d entries", len(nameMap))
	}
}
