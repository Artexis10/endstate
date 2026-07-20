// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package importer

import (
	"errors"
	"strings"
	"testing"
)

// A valid export_version 3 bundle parses every package with all mapped fields.
func TestParseUniGetUI_ValidV3(t *testing.T) {
	src := `{
      "export_version": 3,
      "packages": [
        {"Id": "Microsoft.VisualStudioCode", "Name": "Microsoft Visual Studio Code", "Version": "1.85.1", "Source": "winget", "ManagerName": "WinGet",
         "InstallationOptions": {"Version": "1.85.0"}},
        {"Id": "Git.Git", "Name": "Git", "Version": "2.43.0", "Source": "winget", "ManagerName": "WinGet"}
      ],
      "incompatible_packages": [
        {"Id": "Contoso.Local", "Name": "Local App", "Version": "1.0.0", "Source": "Local PC"}
      ]
    }`

	b, warnings, err := ParseUniGetUI(strings.NewReader(src))
	if err != nil {
		t.Fatalf("ParseUniGetUI returned error: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("expected no warnings for v3, got %v", warnings)
	}
	if b.ExportVersion != 3 {
		t.Errorf("export_version = %v, want 3", b.ExportVersion)
	}
	if len(b.Packages) != 2 {
		t.Fatalf("packages = %d, want 2", len(b.Packages))
	}
	p0 := b.Packages[0]
	if p0.ID != "Microsoft.VisualStudioCode" || p0.Name != "Microsoft Visual Studio Code" ||
		p0.Version != "1.85.1" || p0.Source != "winget" || p0.ManagerName != "WinGet" {
		t.Errorf("package[0] fields not parsed as expected: %+v", p0)
	}
	if p0.InstallationOptions.Version != "1.85.0" {
		t.Errorf("InstallationOptions.Version = %q, want 1.85.0", p0.InstallationOptions.Version)
	}
	if len(b.IncompatiblePackages) != 1 || b.IncompatiblePackages[0].ID != "Contoso.Local" {
		t.Errorf("incompatible_packages not parsed: %+v", b.IncompatiblePackages)
	}
}

// A future export_version parses but returns a warning (forward compatibility).
func TestParseUniGetUI_FutureVersionWarns(t *testing.T) {
	src := `{"export_version": 4, "packages": [{"Id": "Git.Git", "Name": "Git", "Source": "winget"}]}`

	b, warnings, err := ParseUniGetUI(strings.NewReader(src))
	if err != nil {
		t.Fatalf("expected forward-compatible parse, got error: %v", err)
	}
	if len(warnings) == 0 {
		t.Fatal("expected a version warning for export_version 4, got none")
	}
	if !strings.Contains(warnings[0], "4") {
		t.Errorf("warning should mention the version, got %q", warnings[0])
	}
	if len(b.Packages) != 1 {
		t.Errorf("packages should still parse under a future version, got %d", len(b.Packages))
	}
}

// export_version encoded as a decimal (C# double) still compares equal to 3.
func TestParseUniGetUI_DecimalVersionNoWarn(t *testing.T) {
	src := `{"export_version": 3.0, "packages": []}`
	_, warnings, err := ParseUniGetUI(strings.NewReader(src))
	if err != nil {
		t.Fatalf("ParseUniGetUI returned error: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("3.0 should equal 3 with no warning, got %v", warnings)
	}
}

// A missing export_version (decodes to 0) is treated as a version mismatch and warns.
func TestParseUniGetUI_MissingVersionWarns(t *testing.T) {
	src := `{"packages": []}`
	_, warnings, err := ParseUniGetUI(strings.NewReader(src))
	if err != nil {
		t.Fatalf("ParseUniGetUI returned error: %v", err)
	}
	if len(warnings) == 0 {
		t.Error("expected a warning when export_version is absent")
	}
}

// Malformed JSON is a hard error.
func TestParseUniGetUI_MalformedJSON(t *testing.T) {
	_, _, err := ParseUniGetUI(strings.NewReader(`{not valid json`))
	if err == nil {
		t.Fatal("expected an error for malformed JSON, got nil")
	}
}

// A quoted export_version ("3") coerces to the number 3 — no version warning.
func TestParseUniGetUI_StringVersionCoerces(t *testing.T) {
	src := `{"export_version": "3", "packages": [{"Id": "Git.Git", "Name": "Git", "Source": "winget"}]}`
	b, warnings, err := ParseUniGetUI(strings.NewReader(src))
	if err != nil {
		t.Fatalf("a quoted numeric export_version must parse, got error: %v", err)
	}
	if b.ExportVersion != 3 {
		t.Errorf("export_version = %v, want 3 (coerced from \"3\")", b.ExportVersion)
	}
	if len(warnings) != 0 {
		t.Errorf("\"3\" equals 3 with no warning, got %v", warnings)
	}
	if len(b.Packages) != 1 {
		t.Errorf("packages should still parse, got %d", len(b.Packages))
	}
}

// A non-numeric export_version ("abc") degrades to a warning, not a parse error.
func TestParseUniGetUI_GarbageVersionWarns(t *testing.T) {
	src := `{"export_version": "abc", "packages": [{"Id": "Git.Git", "Name": "Git", "Source": "winget"}]}`
	b, warnings, err := ParseUniGetUI(strings.NewReader(src))
	if err != nil {
		t.Fatalf("an unrecognized export_version must degrade to a warning, got error: %v", err)
	}
	if len(warnings) == 0 {
		t.Fatal("expected a warning for an unrecognized export_version, got none")
	}
	if !strings.Contains(warnings[0], "unrecognized export_version") {
		t.Errorf("warning should name the unrecognized version, got %q", warnings[0])
	}
	if len(b.Packages) != 1 {
		t.Errorf("packages should still parse under an unknown version, got %d", len(b.Packages))
	}
}

// A well-formed JSON file that is not a bundle (no export_version and no
// packages) is rejected with ErrNotUniGetUIBundle.
func TestParseUniGetUI_WrongFileShapeRejected(t *testing.T) {
	// A package.json-shaped object: valid JSON, but not a UniGetUI bundle.
	src := `{"name": "my-app", "version": "1.0.0", "dependencies": {"left-pad": "^1.3.0"}}`
	_, _, err := ParseUniGetUI(strings.NewReader(src))
	if err == nil {
		t.Fatal("expected a non-bundle JSON file to be rejected, got nil")
	}
	if !errors.Is(err, ErrNotUniGetUIBundle) {
		t.Errorf("expected ErrNotUniGetUIBundle, got %v", err)
	}
}

// A legitimately empty but versioned bundle imports as empty (no error).
func TestParseUniGetUI_EmptyVersionedBundleOK(t *testing.T) {
	src := `{"export_version": 3, "packages": []}`
	b, warnings, err := ParseUniGetUI(strings.NewReader(src))
	if err != nil {
		t.Fatalf("an empty versioned bundle must parse, got error: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("expected no warnings for an empty v3 bundle, got %v", warnings)
	}
	if len(b.Packages) != 0 {
		t.Errorf("expected zero packages, got %d", len(b.Packages))
	}
}

// A bundle carrying only packages (no export_version key) is still a bundle — the
// shape guard requires just one of the two signature keys.
func TestParseUniGetUI_PackagesOnlyIsBundle(t *testing.T) {
	src := `{"packages": [{"Id": "Git.Git", "Name": "Git", "Source": "winget"}]}`
	_, _, err := ParseUniGetUI(strings.NewReader(src))
	if err != nil {
		t.Fatalf("a packages-only payload is a bundle, got error: %v", err)
	}
}

// Unknown fields (e.g. incompatible_packages_info, per-package Updates) are
// tolerated and ignored — the known fields still parse.
func TestParseUniGetUI_ToleratesUnknownFields(t *testing.T) {
	src := `{
      "export_version": 3,
      "incompatible_packages_info": "some message",
      "future_top_level_field": {"nested": true},
      "packages": [
        {"Id": "Git.Git", "Name": "Git", "Source": "winget", "ManagerName": "WinGet",
         "Updates": {"UpdatesIgnored": true}, "SomeFutureField": 123}
      ]
    }`
	b, _, err := ParseUniGetUI(strings.NewReader(src))
	if err != nil {
		t.Fatalf("unknown fields should be ignored, got error: %v", err)
	}
	if len(b.Packages) != 1 || b.Packages[0].ID != "Git.Git" {
		t.Errorf("known fields should still parse alongside unknown ones: %+v", b.Packages)
	}
}

// A missing InstallationOptions object leaves the zero value (empty Version).
func TestParseUniGetUI_MissingInstallOptions(t *testing.T) {
	src := `{"export_version": 3, "packages": [{"Id": "Git.Git", "Name": "Git", "Source": "winget"}]}`
	b, _, err := ParseUniGetUI(strings.NewReader(src))
	if err != nil {
		t.Fatalf("ParseUniGetUI returned error: %v", err)
	}
	if b.Packages[0].InstallationOptions.Version != "" {
		t.Errorf("absent InstallationOptions should yield empty Version, got %q", b.Packages[0].InstallationOptions.Version)
	}
}

// A leading UTF-8 BOM is tolerated.
func TestParseUniGetUI_ToleratesBOM(t *testing.T) {
	src := "\xEF\xBB\xBF" + `{"export_version": 3, "packages": []}`
	_, _, err := ParseUniGetUI(strings.NewReader(src))
	if err != nil {
		t.Fatalf("leading BOM should be tolerated, got error: %v", err)
	}
}
