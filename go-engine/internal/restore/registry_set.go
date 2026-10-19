// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package restore

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/manifest"
)

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

// registrySetUnreadablePriorMessage is the failure message for a registry-set
// whose current value cannot be read, so that no backup of it can be made.
const registrySetUnreadablePriorMessage = "The current value of this setting cannot be read, so restore leaves it unchanged."

// registrySetUnsupportedPriorMessage is the failure message for a registry-set
// whose current value has a type that a backup cannot restore exactly.
const registrySetUnsupportedPriorMessage = "The current value of this setting has a type that restore cannot back up, so restore leaves it unchanged."

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
	if !manifest.IsRegistrySetValueType(entry.ValueType) {
		return fmt.Errorf("registry-set unsupported valueType %q (supported: %s)", entry.ValueType, strings.Join(manifest.RegistrySetValueTypes(), ", "))
	}
	return nil
}

// writeRegistrySetBackup persists the prior-value record as a JSON sidecar and
// returns its path. The filename is derived from the sanitized key + value name
// so entries within a run do not collide. It never overwrites an existing
// record, because two runs in the same second share one backup directory.
func writeRegistrySetBackup(backup registrySetBackup, opts RestoreOptions) (string, error) {
	backupDir := opts.BackupDir
	if backupDir == "" {
		backupDir = defaultBackupDir(opts.RunID)
	}
	boundary := legacyValidationBoundary{context: opts.ValidationContext, backupDir: backupDir}
	if err := boundary.authorizeBackupDir(backupDir); err != nil {
		return "", err
	}
	if err := boundary.mkdirAll("registry-set-backup-directory-mkdir", backupDir, 0755); err != nil {
		return "", fmt.Errorf("cannot create backup directory: %w", err)
	}
	safe := strings.NewReplacer(`\`, "_", ` `, "_", "/", "_", ":", "_").Replace(
		backup.Key + "_" + backup.ValueName)
	name := "regset_" + safe + ".json"
	backupPath := filepath.Join(backupDir, name)
	// Run IDs have one-second resolution, so a second run in the same second
	// shares backupDir. Overwriting the first run's record would lose its prior
	// value; a repeated record goes to a fresh action directory, as CreateBackup
	// does for files. The live-restore mutation lock serializes the writers.
	if _, err := boundary.lstat("registry-set-backup-destination-lstat", backupPath); err == nil {
		for ordinal := 1; ; ordinal++ {
			actionDir := filepath.Join(backupDir, fmt.Sprintf("action-%06d", ordinal))
			if err := boundary.mkdir("registry-set-backup-action-mkdir", actionDir, 0o755); os.IsExist(err) {
				continue
			} else if err != nil {
				return "", fmt.Errorf("allocate unique registry-set backup directory: %w", err)
			}
			backupPath = filepath.Join(actionDir, name)
			break
		}
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("inspect registry-set backup destination: %w", err)
	}

	data, err := json.MarshalIndent(backup, "", "  ")
	if err != nil {
		return "", fmt.Errorf("cannot marshal registry-set backup: %w", err)
	}
	if err := boundary.writeFile("registry-set-backup-write", backupPath, data, 0644); err != nil {
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
