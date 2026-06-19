// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package restore

import (
	"os/exec"
	"runtime"
	"testing"
)

// buildRegistrySetJournal runs a single registry-set action and returns a
// Journal built from its result, mirroring how WriteJournal records a run.
func buildRegistrySetJournal(t *testing.T, entry RestoreAction, opts RestoreOptions) *Journal {
	t.Helper()
	result, err := RestoreRegistrySet(entry, opts)
	if err != nil {
		t.Fatalf("RestoreRegistrySet error: %v", err)
	}
	if result.Status != "restored" {
		t.Fatalf("expected restored, got %q (err=%q)", result.Status, result.Error)
	}
	return &Journal{
		Entries: []JournalEntry{
			{
				TargetPath:          result.Target,
				TargetExistedBefore: result.TargetExistedBefore,
				BackupCreated:       result.BackupCreated,
				BackupPath:          result.BackupPath,
				Action:              result.Status,
				RestoreType:         result.RestoreType,
			},
		},
	}
}

// Revert of a newly-created value deletes it (prior state was absent).
func TestRunRevert_RegistrySet_DeletesCreatedValue(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("registry operations require Windows")
	}
	cleanupScratchKey(t)
	defer cleanupScratchKey(t)

	backupDir := t.TempDir()
	entry := RestoreAction{
		Type:      "registry-set",
		Key:       scratchKey,
		ValueName: "CreatedThenReverted",
		ValueType: "REG_DWORD",
		Data:      "0",
	}

	journal := buildRegistrySetJournal(t, entry, RestoreOptions{BackupDir: backupDir})

	// Sanity: the value exists after the set.
	if _, _, ok := regQueryValue(t, scratchKey, "CreatedThenReverted"); !ok {
		t.Fatalf("value was not written before revert")
	}

	results, err := RunRevert(journal, backupDir)
	if err != nil {
		t.Fatalf("RunRevert error: %v", err)
	}
	if len(results) != 1 || results[0].Action != "deleted" {
		t.Fatalf("expected one 'deleted' revert action, got %+v", results)
	}

	// The value must be gone after revert.
	if _, _, ok := regQueryValue(t, scratchKey, "CreatedThenReverted"); ok {
		t.Errorf("revert did not delete the created value")
	}
}

// Revert of an overwritten value restores its exact prior data.
func TestRunRevert_RegistrySet_RestoresPriorData(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("registry operations require Windows")
	}
	cleanupScratchKey(t)
	defer cleanupScratchKey(t)

	// Seed a prior value of 1 directly via reg.exe.
	if err := exec.Command("reg", "add", scratchKey, "/v", "Prior", "/t", "REG_DWORD", "/d", "1", "/f").Run(); err != nil {
		t.Fatalf("failed to seed prior value: %v", err)
	}

	backupDir := t.TempDir()
	entry := RestoreAction{
		Type:      "registry-set",
		Key:       scratchKey,
		ValueName: "Prior",
		ValueType: "REG_DWORD",
		Data:      "0", // overwrite 1 -> 0
	}

	journal := buildRegistrySetJournal(t, entry, RestoreOptions{BackupDir: backupDir})
	if !journal.Entries[0].TargetExistedBefore {
		t.Errorf("expected TargetExistedBefore=true for a pre-existing value")
	}

	// Confirm the overwrite landed.
	if _, data, ok := regQueryValue(t, scratchKey, "Prior"); !ok || (data != "0x0" && data != "0") {
		t.Fatalf("overwrite to 0 did not land (ok=%v data=%q)", ok, data)
	}

	results, err := RunRevert(journal, backupDir)
	if err != nil {
		t.Fatalf("RunRevert error: %v", err)
	}
	if len(results) != 1 || results[0].Action != "reverted" {
		t.Fatalf("expected one 'reverted' action, got %+v", results)
	}

	// The prior data (1) must be restored.
	_, data, ok := regQueryValue(t, scratchKey, "Prior")
	if !ok || (data != "0x1" && data != "1") {
		t.Errorf("expected prior value 1 restored, got ok=%v data=%q", ok, data)
	}
}
