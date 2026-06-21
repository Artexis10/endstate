// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package restore

import (
	"os/exec"
	"runtime"
	"strings"
	"testing"
)

// scratchKey is a disposable HKCU key used by the Windows registry-set tests.
// It is removed before and after each test via cleanupScratchKey.
const scratchKey = `HKCU\Software\Endstate\Test`

// ---------------------------------------------------------------------------
// Cross-platform validation (runs before the GOOS guard, like registry-import)
// ---------------------------------------------------------------------------

func TestRestoreRegistrySet_HKLMRejected(t *testing.T) {
	entry := RestoreAction{
		Type:      "registry-set",
		Key:       `HKLM\Software\Endstate\Test`,
		ValueName: "Foo",
		ValueType: "REG_DWORD",
		Data:      "1",
	}
	result, err := RestoreRegistrySet(entry, RestoreOptions{})
	if err != nil {
		t.Fatalf("unexpected error return: %v", err)
	}
	if result.Status != "failed" {
		t.Errorf("expected status=failed, got %q", result.Status)
	}
	if !strings.Contains(result.Error, "only supports HKCU") {
		t.Errorf("expected error to mention HKCU, got: %q", result.Error)
	}
}

func TestRestoreRegistrySet_HKCRRejected(t *testing.T) {
	entry := RestoreAction{
		Type:      "registry-set",
		Key:       `HKCR\Software\Endstate\Test`,
		ValueName: "Foo",
		ValueType: "REG_DWORD",
		Data:      "1",
	}
	result, _ := RestoreRegistrySet(entry, RestoreOptions{})
	if result.Status != "failed" || !strings.Contains(result.Error, "only supports HKCU") {
		t.Errorf("expected HKCU rejection, got status=%q error=%q", result.Status, result.Error)
	}
}

func TestRestoreRegistrySet_EmptyValueNameRejected(t *testing.T) {
	entry := RestoreAction{
		Type:      "registry-set",
		Key:       scratchKey,
		ValueName: "",
		ValueType: "REG_DWORD",
		Data:      "1",
	}
	result, _ := RestoreRegistrySet(entry, RestoreOptions{})
	if result.Status != "failed" || !strings.Contains(result.Error, "valueName") {
		t.Errorf("expected empty-valueName rejection, got status=%q error=%q", result.Status, result.Error)
	}
}

func TestRestoreRegistrySet_UnsupportedTypeRejected(t *testing.T) {
	entry := RestoreAction{
		Type:      "registry-set",
		Key:       scratchKey,
		ValueName: "Foo",
		ValueType: "REG_BINARY",
		Data:      "00",
	}
	result, _ := RestoreRegistrySet(entry, RestoreOptions{})
	if result.Status != "failed" || !strings.Contains(result.Error, "unsupported valueType") {
		t.Errorf("expected unsupported-type rejection, got status=%q error=%q", result.Status, result.Error)
	}
}

// ---------------------------------------------------------------------------
// Windows-only behavioural tests against a real scratch key.
// ---------------------------------------------------------------------------

// cleanupScratchKey removes the scratch key tree. Safe to call when absent.
func cleanupScratchKey(t *testing.T) {
	t.Helper()
	_ = exec.Command("reg", "delete", scratchKey, "/f").Run()
}

// regQueryValue reads the type+data of a named value via reg.exe. ok=false when
// the value or key is missing. Returns raw reg.exe data (e.g. "0x1" for DWORD).
func regQueryValue(t *testing.T, key, valueName string) (regType, data string, ok bool) {
	t.Helper()
	out, err := exec.Command("reg", "query", key, "/v", valueName).Output()
	if err != nil {
		return "", "", false
	}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		for i, f := range fields {
			if strings.HasPrefix(f, "REG_") && i > 0 {
				if !strings.EqualFold(strings.Join(fields[:i], " "), valueName) {
					break
				}
				d := ""
				if i+1 < len(fields) {
					d = strings.Join(fields[i+1:], " ")
				}
				return f, d, true
			}
		}
	}
	return "", "", false
}

func TestRestoreRegistrySet_SetsValueAndBacksUp(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("registry operations require Windows")
	}
	cleanupScratchKey(t)
	defer cleanupScratchKey(t)

	backupDir := t.TempDir()
	entry := RestoreAction{
		Type:      "registry-set",
		Key:       scratchKey,
		ValueName: "DarkMode",
		ValueType: "REG_DWORD",
		Data:      "0",
	}

	result, err := RestoreRegistrySet(entry, RestoreOptions{BackupDir: backupDir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "restored" {
		t.Fatalf("expected status=restored, got %q (err=%q)", result.Status, result.Error)
	}
	if result.TargetExistedBefore {
		t.Errorf("expected TargetExistedBefore=false for a brand-new value")
	}
	if !result.BackupCreated || result.BackupPath == "" {
		t.Errorf("expected a backup to be recorded; got BackupCreated=%v BackupPath=%q", result.BackupCreated, result.BackupPath)
	}

	// The value must now exist with the desired data.
	rt, data, ok := regQueryValue(t, scratchKey, "DarkMode")
	if !ok {
		t.Fatalf("value DarkMode was not written")
	}
	if rt != "REG_DWORD" || (data != "0x0" && data != "0") {
		t.Errorf("expected REG_DWORD 0, got type=%q data=%q", rt, data)
	}

	// The backup sidecar must record that the value was absent before.
	b, rerr := readRegistrySetBackup(result.BackupPath)
	if rerr != nil {
		t.Fatalf("cannot read backup sidecar: %v", rerr)
	}
	if b.Existed {
		t.Errorf("expected backup.Existed=false for a value that did not exist before")
	}
}

func TestRestoreRegistrySet_DryRunMakesNoChange(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("registry operations require Windows")
	}
	cleanupScratchKey(t)
	defer cleanupScratchKey(t)

	backupDir := t.TempDir()
	entry := RestoreAction{
		Type:      "registry-set",
		Key:       scratchKey,
		ValueName: "DryRunValue",
		ValueType: "REG_DWORD",
		Data:      "1",
	}

	result, err := RestoreRegistrySet(entry, RestoreOptions{BackupDir: backupDir, DryRun: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "restored" {
		t.Errorf("expected status=restored for dry-run, got %q", result.Status)
	}
	if result.BackupCreated {
		t.Errorf("dry-run must not write a backup sidecar")
	}
	if _, _, ok := regQueryValue(t, scratchKey, "DryRunValue"); ok {
		t.Errorf("dry-run must not write the value to the registry")
	}
}

func TestRestoreRegistrySet_IdempotentSkip(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("registry operations require Windows")
	}
	cleanupScratchKey(t)
	defer cleanupScratchKey(t)

	backupDir := t.TempDir()
	entry := RestoreAction{
		Type:      "registry-set",
		Key:       scratchKey,
		ValueName: "Hidden",
		ValueType: "REG_DWORD",
		Data:      "1",
	}

	// First write.
	if r, _ := RestoreRegistrySet(entry, RestoreOptions{BackupDir: backupDir}); r.Status != "restored" {
		t.Fatalf("first apply: expected restored, got %q", r.Status)
	}

	// Second apply with identical desired state must skip.
	result, err := RestoreRegistrySet(entry, RestoreOptions{BackupDir: backupDir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "skipped_up_to_date" {
		t.Errorf("expected status=skipped_up_to_date on re-apply, got %q", result.Status)
	}
	if result.BackupCreated {
		t.Errorf("idempotent skip must not write a backup")
	}
}

// 0x-hex data must compare equal to a stored decimal DWORD (idempotent skip).
func TestRestoreRegistrySet_HexDataIdempotent(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("registry operations require Windows")
	}
	cleanupScratchKey(t)
	defer cleanupScratchKey(t)

	backupDir := t.TempDir()
	base := RestoreAction{
		Type:      "registry-set",
		Key:       scratchKey,
		ValueName: "HexCheck",
		ValueType: "REG_DWORD",
		Data:      "1",
	}
	if r, _ := RestoreRegistrySet(base, RestoreOptions{BackupDir: backupDir}); r.Status != "restored" {
		t.Fatalf("first apply: expected restored, got %q", r.Status)
	}

	hexEntry := base
	hexEntry.Data = "0x1"
	result, _ := RestoreRegistrySet(hexEntry, RestoreOptions{BackupDir: backupDir})
	if result.Status != "skipped_up_to_date" {
		t.Errorf("expected 0x1 to be idempotent with stored 1, got %q", result.Status)
	}
}

// REG_SZ string values (used by the mouse/keyboard windows-settings modules)
// must round-trip: write, read back exact string, then idempotently skip.
func TestRestoreRegistrySet_StringValueRoundTrip(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("registry operations require Windows")
	}
	cleanupScratchKey(t)
	defer cleanupScratchKey(t)

	backupDir := t.TempDir()
	entry := RestoreAction{
		Type:      "registry-set",
		Key:       scratchKey,
		ValueName: "StringPref",
		ValueType: "REG_SZ",
		Data:      "0",
	}

	result, err := RestoreRegistrySet(entry, RestoreOptions{BackupDir: backupDir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "restored" {
		t.Fatalf("expected status=restored, got %q (err=%q)", result.Status, result.Error)
	}

	rt, data, ok := regQueryValue(t, scratchKey, "StringPref")
	if !ok || rt != "REG_SZ" || data != "0" {
		t.Errorf("expected REG_SZ \"0\", got type=%q data=%q ok=%v", rt, data, ok)
	}

	// Re-apply identical desired state → idempotent skip (string compare).
	if r, _ := RestoreRegistrySet(entry, RestoreOptions{BackupDir: backupDir}); r.Status != "skipped_up_to_date" {
		t.Errorf("expected idempotent skip on re-apply, got %q", r.Status)
	}
}
