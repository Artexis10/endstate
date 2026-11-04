// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package restore

import (
	"errors"
	"os"
	"testing"
)

// TestRestoreRegistrySetLeavesAPriorValueItCannotBackUpUnchanged catches
// restore writing over a prior value that its backup cannot restore. A value
// that cannot be read would be backed up as "absent", so revert would delete
// it; a value of an unsupported type would be backed up without its data, so
// revert could not put it back. Either way the old data would be lost.
func TestRestoreRegistrySetLeavesAPriorValueItCannotBackUpUnchanged(t *testing.T) {
	originalRead, originalWrite := registrySetReadNative, registrySetWriteNative
	t.Cleanup(func() { registrySetReadNative, registrySetWriteNative = originalRead, originalWrite })
	registrySetWriteNative = func(string, string, string, string) error {
		t.Error("restore wrote over a value that its backup cannot restore")
		return nil
	}

	for _, test := range []struct {
		name        string
		read        func(string, string) (bool, string, string, bool, error)
		wantMessage string
	}{
		{
			name: "unreadable",
			read: func(string, string) (bool, string, string, bool, error) {
				return false, "", "", true, errors.New("Access is denied.")
			},
			wantMessage: registrySetUnreadablePriorMessage,
		},
		{
			// The read reports an existing REG_BINARY value with an empty type.
			name: "unsupported type",
			read: func(string, string) (bool, string, string, bool, error) {
				return true, "", "", true, nil
			},
			wantMessage: registrySetUnsupportedPriorMessage,
		},
	} {
		registrySetReadNative = test.read
		for _, preview := range []bool{true, false} {
			backupDir := t.TempDir()
			result, err := RestoreRegistrySet(RestoreAction{
				Type: "registry-set", Key: `HKCU\Software\Endstate\Example`, ValueName: "Setting", ValueType: "REG_SZ", Data: "desired",
			}, RestoreOptions{DryRun: preview, BackupDir: backupDir})
			if err != nil || result.Status != "failed" || result.Error != test.wantMessage ||
				result.BackupCreated || result.BackupPath != "" {
				t.Fatalf("%s, preview=%v: result = %+v, err = %v", test.name, preview, result, err)
			}
			if entries, err := os.ReadDir(backupDir); err != nil || len(entries) != 0 {
				t.Fatalf("%s, preview=%v: backup directory holds %v (err %v), want nothing", test.name, preview, entries, err)
			}
		}
	}
}
