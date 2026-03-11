// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"os"
	"path/filepath"
	"testing"
)

// ---------------------------------------------------------------------------
// Profile Validate tests
// ---------------------------------------------------------------------------

func TestProfileValidate_ValidProfile(t *testing.T) {
	dir := t.TempDir()
	profile := filepath.Join(dir, "test.jsonc")
	content := `{
		"version": 1,
		"name": "test-profile",
		"apps": [
			{ "id": "test-app", "refs": { "windows": "Test.App" } }
		]
	}`
	if err := os.WriteFile(profile, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test profile: %v", err)
	}

	result, err := runProfileValidate(profile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	vr, ok := result.(*ProfileValidateResult)
	if !ok {
		t.Fatalf("expected *ProfileValidateResult, got %T", result)
	}
	if !vr.Valid {
		t.Error("expected valid=true")
	}
	if vr.Summary.AppCount != 1 {
		t.Errorf("expected appCount=1, got %d", vr.Summary.AppCount)
	}
	if vr.Summary.HasRestore {
		t.Error("expected hasRestore=false")
	}
	if vr.Summary.HasVerify {
		t.Error("expected hasVerify=false")
	}
}

func TestProfileValidate_ValidProfileWithRestoreAndVerify(t *testing.T) {
	dir := t.TempDir()
	profile := filepath.Join(dir, "full.jsonc")
	content := `{
		"version": 1,
		"name": "full-profile",
		"apps": [
			{ "id": "app1", "refs": { "windows": "App.One" } },
			{ "id": "app2", "refs": { "windows": "App.Two" } }
		],
		"restore": [
			{ "type": "copy", "source": "./src", "target": "./dst" }
		],
		"verify": [
			{ "type": "command-exists", "command": "git" }
		]
	}`
	if err := os.WriteFile(profile, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test profile: %v", err)
	}

	result, err := runProfileValidate(profile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	vr := result.(*ProfileValidateResult)
	if !vr.Valid {
		t.Error("expected valid=true")
	}
	if vr.Summary.AppCount != 2 {
		t.Errorf("expected appCount=2, got %d", vr.Summary.AppCount)
	}
	if !vr.Summary.HasRestore {
		t.Error("expected hasRestore=true")
	}
	if !vr.Summary.HasVerify {
		t.Error("expected hasVerify=true")
	}
}

func TestProfileValidate_InvalidProfile_MissingVersion(t *testing.T) {
	dir := t.TempDir()
	profile := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(profile, []byte(`{"apps": []}`), 0644); err != nil {
		t.Fatalf("failed to write test profile: %v", err)
	}

	result, err := runProfileValidate(profile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	vr := result.(*ProfileValidateResult)
	if vr.Valid {
		t.Error("expected valid=false for missing version")
	}
	if len(vr.Errors) == 0 {
		t.Error("expected at least one validation error")
	}

	// Check that MISSING_VERSION is among the errors
	found := false
	for _, ve := range vr.Errors {
		if ve.Code == "MISSING_VERSION" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected MISSING_VERSION error code")
	}
}

func TestProfileValidate_InvalidProfile_FileNotFound(t *testing.T) {
	result, err := runProfileValidate("/nonexistent/path/profile.json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	vr := result.(*ProfileValidateResult)
	if vr.Valid {
		t.Error("expected valid=false for missing file")
	}
	if len(vr.Errors) == 0 {
		t.Error("expected at least one validation error")
	}
}

func TestProfileValidate_ErrorsNeverNil(t *testing.T) {
	dir := t.TempDir()
	profile := filepath.Join(dir, "valid.jsonc")
	content := `{"version": 1, "apps": []}`
	if err := os.WriteFile(profile, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test profile: %v", err)
	}

	result, _ := runProfileValidate(profile)
	vr := result.(*ProfileValidateResult)

	if vr.Errors == nil {
		t.Error("errors slice should never be nil (should be empty slice)")
	}
}

// ---------------------------------------------------------------------------
// Profile List tests
// ---------------------------------------------------------------------------

func TestProfileList_EmptyDir(t *testing.T) {
	dir := t.TempDir()

	result, err := runProfileListFromDir(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	lr, ok := result.(*ProfileListResult)
	if !ok {
		t.Fatalf("expected *ProfileListResult, got %T", result)
	}
	if len(lr.Profiles) != 0 {
		t.Errorf("expected 0 profiles, got %d", len(lr.Profiles))
	}
}

func TestProfileList_NonexistentDir(t *testing.T) {
	result, err := runProfileListFromDir("/nonexistent/profiles/dir")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	lr := result.(*ProfileListResult)
	if len(lr.Profiles) != 0 {
		t.Errorf("expected 0 profiles for nonexistent dir, got %d", len(lr.Profiles))
	}
}

func TestProfileList_EmptyDirString(t *testing.T) {
	result, err := runProfileListFromDir("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	lr := result.(*ProfileListResult)
	if len(lr.Profiles) != 0 {
		t.Errorf("expected 0 profiles for empty dir, got %d", len(lr.Profiles))
	}
}

func TestProfileList_WithProfiles(t *testing.T) {
	dir := t.TempDir()

	// Create a valid profile
	validProfile := `{"version": 1, "name": "work", "apps": [{"id": "a", "refs": {"windows": "A.App"}}]}`
	if err := os.WriteFile(filepath.Join(dir, "work.jsonc"), []byte(validProfile), 0644); err != nil {
		t.Fatal(err)
	}

	// Create a meta.json with display name
	meta := `{"displayName": "My Work Laptop"}`
	if err := os.WriteFile(filepath.Join(dir, "work.meta.json"), []byte(meta), 0644); err != nil {
		t.Fatal(err)
	}

	// Create an invalid profile (should still appear but valid=false)
	if err := os.WriteFile(filepath.Join(dir, "bad.json"), []byte(`{}`), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := runProfileListFromDir(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	lr := result.(*ProfileListResult)
	if len(lr.Profiles) != 2 {
		t.Fatalf("expected 2 profiles, got %d", len(lr.Profiles))
	}

	// Profiles should be sorted by name: "bad" before "work"
	if lr.Profiles[0].Name != "bad" {
		t.Errorf("expected first profile name=bad, got %q", lr.Profiles[0].Name)
	}
	if lr.Profiles[0].Valid {
		t.Error("expected bad profile to be valid=false")
	}

	if lr.Profiles[1].Name != "work" {
		t.Errorf("expected second profile name=work, got %q", lr.Profiles[1].Name)
	}
	if !lr.Profiles[1].Valid {
		t.Error("expected work profile to be valid=true")
	}
	if lr.Profiles[1].DisplayName != "My Work Laptop" {
		t.Errorf("expected displayName='My Work Laptop', got %q", lr.Profiles[1].DisplayName)
	}
	if lr.Profiles[1].AppCount != 1 {
		t.Errorf("expected appCount=1, got %d", lr.Profiles[1].AppCount)
	}
}

func TestProfileList_SkipsMetaJson(t *testing.T) {
	dir := t.TempDir()

	// Create a meta.json file only — should NOT be listed as a profile
	if err := os.WriteFile(filepath.Join(dir, "work.meta.json"), []byte(`{"displayName": "test"}`), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := runProfileListFromDir(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	lr := result.(*ProfileListResult)
	if len(lr.Profiles) != 0 {
		t.Errorf("expected 0 profiles (meta.json should be skipped), got %d", len(lr.Profiles))
	}
}

func TestProfileList_SkipsNonProfileExtensions(t *testing.T) {
	dir := t.TempDir()

	// Create files with non-profile extensions
	if err := os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("key: val"), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := runProfileListFromDir(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	lr := result.(*ProfileListResult)
	if len(lr.Profiles) != 0 {
		t.Errorf("expected 0 profiles for non-profile extensions, got %d", len(lr.Profiles))
	}
}

func TestProfileList_AllExtensions(t *testing.T) {
	dir := t.TempDir()

	content := `{"version": 1, "name": "test", "apps": []}`
	if err := os.WriteFile(filepath.Join(dir, "a.json"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.jsonc"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "c.json5"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := runProfileListFromDir(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	lr := result.(*ProfileListResult)
	if len(lr.Profiles) != 3 {
		t.Errorf("expected 3 profiles for all valid extensions, got %d", len(lr.Profiles))
	}
}

// ---------------------------------------------------------------------------
// Profile Path tests
// ---------------------------------------------------------------------------

func TestProfilePath_FoundJsonc(t *testing.T) {
	dir := t.TempDir()
	profilePath := filepath.Join(dir, "test.jsonc")
	if err := os.WriteFile(profilePath, []byte(`{}`), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := runProfilePathFromDir(dir, "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	pr, ok := result.(*ProfilePathResult)
	if !ok {
		t.Fatalf("expected *ProfilePathResult, got %T", result)
	}
	if !pr.Exists {
		t.Error("expected exists=true")
	}
	if pr.Path != profilePath {
		t.Errorf("expected path=%q, got %q", profilePath, pr.Path)
	}
}

func TestProfilePath_FoundJson(t *testing.T) {
	dir := t.TempDir()
	profilePath := filepath.Join(dir, "test.json")
	if err := os.WriteFile(profilePath, []byte(`{}`), 0644); err != nil {
		t.Fatal(err)
	}

	result, _ := runProfilePathFromDir(dir, "test")
	pr := result.(*ProfilePathResult)

	if !pr.Exists {
		t.Error("expected exists=true")
	}
	if pr.Path != profilePath {
		t.Errorf("expected path=%q, got %q", profilePath, pr.Path)
	}
}

func TestProfilePath_FoundJson5(t *testing.T) {
	dir := t.TempDir()
	profilePath := filepath.Join(dir, "test.json5")
	if err := os.WriteFile(profilePath, []byte(`{}`), 0644); err != nil {
		t.Fatal(err)
	}

	result, _ := runProfilePathFromDir(dir, "test")
	pr := result.(*ProfilePathResult)

	if !pr.Exists {
		t.Error("expected exists=true")
	}
	if pr.Path != profilePath {
		t.Errorf("expected path=%q, got %q", profilePath, pr.Path)
	}
}

func TestProfilePath_FoundZip(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "test.zip")
	if err := os.WriteFile(zipPath, []byte("PK"), 0644); err != nil {
		t.Fatal(err)
	}

	result, _ := runProfilePathFromDir(dir, "test")
	pr := result.(*ProfilePathResult)

	if !pr.Exists {
		t.Error("expected exists=true for .zip")
	}
	if pr.Path != zipPath {
		t.Errorf("expected path=%q, got %q", zipPath, pr.Path)
	}
}

func TestProfilePath_FoundLooseFolder(t *testing.T) {
	dir := t.TempDir()
	folderDir := filepath.Join(dir, "test")
	if err := os.MkdirAll(folderDir, 0755); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(folderDir, "manifest.jsonc")
	if err := os.WriteFile(manifestPath, []byte(`{}`), 0644); err != nil {
		t.Fatal(err)
	}

	result, _ := runProfilePathFromDir(dir, "test")
	pr := result.(*ProfilePathResult)

	if !pr.Exists {
		t.Error("expected exists=true for loose folder")
	}
	if pr.Path != manifestPath {
		t.Errorf("expected path=%q, got %q", manifestPath, pr.Path)
	}
}

func TestProfilePath_ResolutionOrder(t *testing.T) {
	// When multiple formats exist, .zip should win (first in order)
	dir := t.TempDir()

	zipPath := filepath.Join(dir, "test.zip")
	if err := os.WriteFile(zipPath, []byte("PK"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "test.jsonc"), []byte(`{}`), 0644); err != nil {
		t.Fatal(err)
	}

	result, _ := runProfilePathFromDir(dir, "test")
	pr := result.(*ProfilePathResult)

	if pr.Path != zipPath {
		t.Errorf("expected .zip to win resolution order, got %q", pr.Path)
	}
}

func TestProfilePath_NotFound(t *testing.T) {
	dir := t.TempDir()

	result, err := runProfilePathFromDir(dir, "nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	pr := result.(*ProfilePathResult)
	if pr.Exists {
		t.Error("expected exists=false")
	}
	// Should return expected .jsonc path
	expected := filepath.Join(dir, "nonexistent.jsonc")
	if pr.Path != expected {
		t.Errorf("expected path=%q, got %q", expected, pr.Path)
	}
}

// ---------------------------------------------------------------------------
// Display Label Priority tests
// ---------------------------------------------------------------------------

func TestDisplayLabel_MetaJsonTakesPriority(t *testing.T) {
	dir := t.TempDir()

	// Profile with name field
	profile := `{"version": 1, "name": "manifest-name", "apps": []}`
	if err := os.WriteFile(filepath.Join(dir, "file-stem.jsonc"), []byte(profile), 0644); err != nil {
		t.Fatal(err)
	}

	// Meta.json with displayName (highest priority)
	meta := `{"displayName": "Meta Display Name"}`
	if err := os.WriteFile(filepath.Join(dir, "file-stem.meta.json"), []byte(meta), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := runProfileListFromDir(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	lr := result.(*ProfileListResult)
	if len(lr.Profiles) != 1 {
		t.Fatalf("expected 1 profile, got %d", len(lr.Profiles))
	}
	if lr.Profiles[0].DisplayName != "Meta Display Name" {
		t.Errorf("expected displayName='Meta Display Name', got %q", lr.Profiles[0].DisplayName)
	}
}

func TestDisplayLabel_ManifestNameFallback(t *testing.T) {
	dir := t.TempDir()

	// Profile with name field but no meta.json
	profile := `{"version": 1, "name": "manifest-name", "apps": []}`
	if err := os.WriteFile(filepath.Join(dir, "file-stem.jsonc"), []byte(profile), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := runProfileListFromDir(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	lr := result.(*ProfileListResult)
	if len(lr.Profiles) != 1 {
		t.Fatalf("expected 1 profile, got %d", len(lr.Profiles))
	}
	if lr.Profiles[0].DisplayName != "manifest-name" {
		t.Errorf("expected displayName='manifest-name', got %q", lr.Profiles[0].DisplayName)
	}
}

func TestDisplayLabel_FilenameStemFallback(t *testing.T) {
	dir := t.TempDir()

	// Profile without name field and no meta.json
	profile := `{"version": 1, "apps": []}`
	if err := os.WriteFile(filepath.Join(dir, "my-profile.jsonc"), []byte(profile), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := runProfileListFromDir(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	lr := result.(*ProfileListResult)
	if len(lr.Profiles) != 1 {
		t.Fatalf("expected 1 profile, got %d", len(lr.Profiles))
	}
	if lr.Profiles[0].DisplayName != "my-profile" {
		t.Errorf("expected displayName='my-profile', got %q", lr.Profiles[0].DisplayName)
	}
}

// ---------------------------------------------------------------------------
// RunProfile router tests
// ---------------------------------------------------------------------------

func TestRunProfile_UnknownSubcommand(t *testing.T) {
	_, err := RunProfile(ProfileFlags{Subcommand: "invalid"})
	if err == nil {
		t.Fatal("expected error for unknown subcommand")
	}
	if string(err.Code) != "INTERNAL_ERROR" {
		t.Errorf("expected INTERNAL_ERROR, got %q", err.Code)
	}
}

func TestRunProfile_PathMissingArg(t *testing.T) {
	_, err := RunProfile(ProfileFlags{Subcommand: "path", Args: []string{}})
	if err == nil {
		t.Fatal("expected error for missing path argument")
	}
}

func TestRunProfile_ValidateMissingArg(t *testing.T) {
	_, err := RunProfile(ProfileFlags{Subcommand: "validate", Args: []string{}})
	if err == nil {
		t.Fatal("expected error for missing validate argument")
	}
}
