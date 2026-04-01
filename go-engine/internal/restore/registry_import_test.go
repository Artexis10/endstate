// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package restore

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// ValidateRegistryTarget / isHKCUKey tests (platform-independent)
// ---------------------------------------------------------------------------

func TestIsHKCUKey(t *testing.T) {
	cases := []struct {
		target string
		want   bool
	}{
		{`HKCU\Software\Test`, true},
		{`hkcu\Software\Test`, true},
		{`HKEY_CURRENT_USER\Software\Test`, true},
		{`hkey_current_user\Software\Test`, true},
		{`HKLM\Software\Test`, false},
		{`HKEY_LOCAL_MACHINE\Software\Test`, false},
		{`HKCR\Software\Test`, false},
		{`HKEY_CLASSES_ROOT\Software\Test`, false},
		{`HKU\Software\Test`, false},
		{``, false},
	}

	for _, tc := range cases {
		got := isHKCUKey(tc.target)
		if got != tc.want {
			t.Errorf("isHKCUKey(%q) = %v, want %v", tc.target, got, tc.want)
		}
	}
}

func TestValidateRegistryTarget_HKCUAccepted(t *testing.T) {
	if err := ValidateRegistryTarget(`HKCU\Software\Test`); err != nil {
		t.Errorf("expected nil error for HKCU key, got: %v", err)
	}
}

func TestValidateRegistryTarget_HKEYCurrentUserAccepted(t *testing.T) {
	if err := ValidateRegistryTarget(`HKEY_CURRENT_USER\Software\Test`); err != nil {
		t.Errorf("expected nil error for HKEY_CURRENT_USER key, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// HKCU validation rejection tests (platform-independent because validation
// runs before the GOOS guard)
// ---------------------------------------------------------------------------

func TestRestoreRegistryImport_HKLMRejected(t *testing.T) {
	entry := RestoreAction{
		Type:   "registry-import",
		Source: "nonexistent.reg",
		Target: `HKLM\Software\Test`,
	}
	result, err := RestoreRegistryImport(entry, "nonexistent.reg", RestoreOptions{})
	if err != nil {
		t.Fatalf("unexpected error return: %v", err)
	}
	if result.Status != "failed" {
		t.Errorf("expected status=failed, got %q", result.Status)
	}
	if !strings.Contains(result.Error, "only supports HKCU") {
		t.Errorf("expected error to contain 'only supports HKCU', got: %q", result.Error)
	}
}

func TestRestoreRegistryImport_HKCRRejected(t *testing.T) {
	entry := RestoreAction{
		Type:   "registry-import",
		Source: "nonexistent.reg",
		Target: `HKCR\Software\Test`,
	}
	result, err := RestoreRegistryImport(entry, "nonexistent.reg", RestoreOptions{})
	if err != nil {
		t.Fatalf("unexpected error return: %v", err)
	}
	if result.Status != "failed" {
		t.Errorf("expected status=failed, got %q", result.Status)
	}
	if !strings.Contains(result.Error, "only supports HKCU") {
		t.Errorf("expected error to contain 'only supports HKCU', got: %q", result.Error)
	}
}

func TestRestoreRegistryImport_HKEYLocalMachineRejected(t *testing.T) {
	entry := RestoreAction{
		Type:   "registry-import",
		Source: "nonexistent.reg",
		Target: `HKEY_LOCAL_MACHINE\Software\Test`,
	}
	result, err := RestoreRegistryImport(entry, "nonexistent.reg", RestoreOptions{})
	if err != nil {
		t.Fatalf("unexpected error return: %v", err)
	}
	if result.Status != "failed" {
		t.Errorf("expected status=failed, got %q", result.Status)
	}
	if !strings.Contains(result.Error, "only supports HKCU") {
		t.Errorf("expected error to contain 'only supports HKCU', got: %q", result.Error)
	}
}

// ---------------------------------------------------------------------------
// Tests that require passing the GOOS check — Windows-only beyond this point,
// or non-Windows tests that exercise pre-GOOS-check logic only.
// ---------------------------------------------------------------------------

func TestRestoreRegistryImport_OptionalMissingSource(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("registry operations require Windows")
	}
	entry := RestoreAction{
		Type:     "registry-import",
		Source:   "definitely_does_not_exist_12345.reg",
		Target:   `HKCU\Software\EndstateTest\Missing`,
		Optional: true,
	}
	result, err := RestoreRegistryImport(entry, "definitely_does_not_exist_12345.reg", RestoreOptions{})
	if err != nil {
		t.Fatalf("unexpected error return: %v", err)
	}
	if result.Status != "skipped_missing_source" {
		t.Errorf("expected status=skipped_missing_source, got %q", result.Status)
	}
}

func TestRestoreRegistryImport_NonExistentSourceFails(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("registry operations require Windows")
	}
	entry := RestoreAction{
		Type:     "registry-import",
		Source:   "definitely_does_not_exist_12345.reg",
		Target:   `HKCU\Software\EndstateTest\Missing`,
		Optional: false,
	}
	result, err := RestoreRegistryImport(entry, "definitely_does_not_exist_12345.reg", RestoreOptions{})
	if err != nil {
		t.Fatalf("unexpected error return: %v", err)
	}
	if result.Status != "failed" {
		t.Errorf("expected status=failed, got %q", result.Status)
	}
	if !strings.Contains(result.Error, "source not found") {
		t.Errorf("expected error to contain 'source not found', got: %q", result.Error)
	}
}

func TestRestoreRegistryImport_DryRun(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("registry operations require Windows")
	}

	tmp := t.TempDir()
	regFile := filepath.Join(tmp, "test.reg")

	// Write a minimal valid .reg file.
	regContent := "Windows Registry Editor Version 5.00\r\n\r\n[HKEY_CURRENT_USER\\Software\\EndstateTest\\DryRun]\r\n"
	if err := os.WriteFile(regFile, []byte(regContent), 0644); err != nil {
		t.Fatalf("failed to create temp reg file: %v", err)
	}

	entry := RestoreAction{
		Type:   "registry-import",
		Source: regFile,
		Target: `HKCU\Software\EndstateTest\DryRun`,
	}

	result, err := RestoreRegistryImport(entry, regFile, RestoreOptions{DryRun: true})
	if err != nil {
		t.Fatalf("unexpected error return: %v", err)
	}
	if result.Status != "restored" {
		t.Errorf("expected status=restored for dry-run, got %q", result.Status)
	}
}
