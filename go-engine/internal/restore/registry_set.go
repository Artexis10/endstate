// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package restore

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// registrySetValueTypes is the set of REG_* value types the registry-set
// restore strategy supports. These cover the Windows OS-settings preference
// values the seed modules need (DWORDs and string forms); deliberately narrow
// so the surface stays reversible single-value writes only.
var registrySetValueTypes = map[string]bool{
	"REG_DWORD":     true,
	"REG_SZ":        true,
	"REG_EXPAND_SZ": true,
}

// registrySetBackup is the prior-value record persisted to the backup/journal
// directory BEFORE a registry-set write, so revert can restore the exact prior
// state (or delete the value when it was absent before).
//
// It is written as a JSON sidecar under state/backups/<runID>/ and referenced
// from the restore journal via RestoreResult.BackupPath, reusing the existing
// backup-then-revert flow that registry-import established.
type registrySetBackup struct {
	Key       string `json:"key"`
	ValueName string `json:"valueName"`
	// Existed reports whether the named value was present before the write.
	Existed bool `json:"existed"`
	// PriorType / PriorData hold the value's prior REG_* type and string-form
	// data. Only meaningful when Existed is true.
	PriorType string `json:"priorType,omitempty"`
	PriorData string `json:"priorData,omitempty"`
}

// registrySetTarget renders a human-readable target string (Key\ValueName) for
// a registry-set action, used in results and events. It is cross-platform so
// the non-Windows failure path can report it too.
func registrySetTarget(entry RestoreAction) string {
	return fmt.Sprintf("%s\\%s", entry.Key, entry.ValueName)
}

// validateRegistrySet performs the cross-platform validation shared by the
// Windows and non-Windows entry points: HKCU-only target, a non-empty value
// name, and a supported REG_* value type. Returning an error here keeps the
// HKCU rejection testable on any platform (like ValidateRegistryTarget).
func validateRegistrySet(entry RestoreAction) error {
	if err := ValidateRegistryTarget(entry.Key); err != nil {
		// Reuse the HKCU gate; reword for the registry-set surface.
		return fmt.Errorf("registry-set only supports HKCU keys: %s", entry.Key)
	}
	if strings.TrimSpace(entry.ValueName) == "" {
		return fmt.Errorf("registry-set requires a non-empty valueName")
	}
	vt := strings.ToUpper(strings.TrimSpace(entry.ValueType))
	if !registrySetValueTypes[vt] {
		return fmt.Errorf("registry-set unsupported valueType %q (supported: REG_DWORD, REG_SZ, REG_EXPAND_SZ)", entry.ValueType)
	}
	return nil
}

// writeRegistrySetBackup persists the prior-value record as a JSON sidecar and
// returns its path. The filename is derived from the sanitized key + value name
// so concurrent entries within a run do not collide.
func writeRegistrySetBackup(backup registrySetBackup, opts RestoreOptions) (string, error) {
	backupDir := opts.BackupDir
	if backupDir == "" {
		backupDir = defaultBackupDir(opts.RunID)
	}
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		return "", fmt.Errorf("cannot create backup directory: %w", err)
	}
	safe := strings.NewReplacer(`\`, "_", ` `, "_", "/", "_", ":", "_").Replace(
		backup.Key + "_" + backup.ValueName)
	backupPath := filepath.Join(backupDir, "regset_"+safe+".json")

	data, err := json.MarshalIndent(backup, "", "  ")
	if err != nil {
		return "", fmt.Errorf("cannot marshal registry-set backup: %w", err)
	}
	if err := os.WriteFile(backupPath, data, 0644); err != nil {
		return "", fmt.Errorf("cannot write registry-set backup: %w", err)
	}
	return backupPath, nil
}

// readRegistrySetBackup reads a prior-value sidecar written by
// writeRegistrySetBackup. Used by revert.
func readRegistrySetBackup(path string) (*registrySetBackup, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read registry-set backup: %w", err)
	}
	var b registrySetBackup
	if err := json.Unmarshal(data, &b); err != nil {
		return nil, fmt.Errorf("cannot parse registry-set backup: %w", err)
	}
	return &b, nil
}
