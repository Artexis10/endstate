// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package restore

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/registryfile"
	"github.com/Artexis10/endstate/go-engine/internal/registryvalue"
	"github.com/Artexis10/endstate/go-engine/internal/validationmode"
	"golang.org/x/sys/windows/registry"
)

var (
	registrySetReadNative = func(key, valueName string) (bool, string, string, bool, error) {
		value, found, keyFound, err := registryvalue.Read(key, valueName)
		if err != nil {
			return false, "", "", keyFound, err
		}
		if !manifest.IsRegistrySetValueType(value.Type) {
			// A type registry-set cannot write never matches a desired value,
			// and its backup records the prior type as unknown.
			value.Type = ""
		}
		return found, value.Type, value.Data, keyFound, nil
	}
	registrySetWriteNative = func(key, valueName, valueType, data string) error {
		hive, subkey, err := splitHKCUKey(key)
		if err != nil {
			return err
		}
		return writeRegistryValue(hive, subkey, valueName, valueType, data)
	}
	registrySetDeleteNative = func(key, valueName string) error {
		hive, subkey, err := splitHKCUKey(key)
		if err != nil {
			return err
		}
		return deleteRegistryValue(hive, subkey, valueName)
	}
)

// RestoreRegistrySet implements the value-level registry-set restore strategy.
// It sets a single named value (entry.ValueName of entry.ValueType to
// entry.Data) under the HKCU key entry.Key.
//
// Contract:
//   - HKCU-only: a non-HKCU key fails (reuses ValidateRegistryTarget).
//   - Backup-before-overwrite: the prior value (type/data, or "absent") is
//     recorded to state/backups/<runID>/ BEFORE any write.
//   - Idempotent: when the named value already equals the desired type+data the
//     write is skipped (status skipped_up_to_date) and no backup is written.
//   - Dry-run: opts.DryRun reports the intended write without touching the
//     registry and without writing a backup.
//   - Creates the key if missing.
func RestoreRegistrySet(entry RestoreAction, opts RestoreOptions) (*RestoreResult, error) {
	result := &RestoreResult{
		Target:      registrySetTarget(entry),
		RestoreType: "registry-set",
	}
	defer projectValidationRestoreResult(result, entry, opts)

	// Validate target + value (cross-platform — runs before any registry call).
	if err := validateRegistrySet(entry); err != nil {
		result.Status = "failed"
		result.Error = err.Error()
		return result, nil
	}
	nativeKey := entry.Key
	if opts.ValidationContext != nil {
		semanticKey, err := validationmode.NormalizeHKCU(entry.Key)
		if err != nil {
			result.Status = "failed"
			result.Error = err.Error()
			return result, nil
		}
		mappedKey, err := opts.ValidationContext.MapHKCU(semanticKey)
		if err != nil {
			result.Status = "failed"
			result.Error = err.Error()
			return result, nil
		}
		nativeKey = mappedKey
	}

	// Probe the current state of the named value (existence + type + data).
	// A value that cannot be read is never treated as absent: its backup would
	// record "absent", and revert would then delete a value whose data was
	// never saved. So nothing is backed up or written, in a preview as well.
	existed, curType, curData, keyExisted, readErr := registrySetReadNative(nativeKey, entry.ValueName)
	if readErr != nil {
		result.Status = "failed"
		result.Error = registrySetUnreadablePriorMessage
		return result, nil
	}
	result.TargetExistedBefore = existed
	// The read reports an existing value of a type that registry-set cannot
	// write with an empty type. Its backup could not restore it exactly, so it
	// is left alone as well.
	if existed && curType == "" {
		result.Status = "failed"
		result.Error = registrySetUnsupportedPriorMessage
		return result, nil
	}

	desiredType := strings.ToUpper(strings.TrimSpace(entry.ValueType))

	// Idempotent skip: the value already equals the desired type+data.
	if existed && curType == desiredType && curData == normalizeData(desiredType, entry.Data) {
		result.Status = "skipped_up_to_date"
		return result, nil
	}

	// Record the prior value BEFORE writing (backup-before-overwrite). This is
	// unconditional for registry-set: the value-level default is
	// backup-and-overwrite (see design.md), distinct from file-restore's skip.
	backup := registrySetBackup{
		Key:       entry.Key,
		ValueName: entry.ValueName,
		Existed:   existed,
		PriorType: curType,
		PriorData: curData,
	}

	// Dry-run: report the intended write; touch neither the registry nor disk.
	if opts.DryRun {
		result.Status = "restored"
		return result, nil
	}
	backupDir := restoreBackupDirectory(opts)
	boundary := legacyValidationBoundary{context: opts.ValidationContext, backupDir: backupDir}
	if err := boundary.authorizeBackupDir(backupDir); err != nil {
		result.Status = "failed"
		result.Error = fmt.Sprintf("backup: %v", err)
		return result, nil
	}

	backupPath, err := writeRegistrySetBackup(backup, opts)
	if err != nil {
		result.Status = "failed"
		result.Error = fmt.Sprintf("backup: %v", err)
		return result, nil
	}
	result.BackupPath = backupPath
	result.BackupCreated = true

	// Create the key if missing, then write the value.
	if err := registrySetWriteNative(nativeKey, entry.ValueName, desiredType, entry.Data); err != nil {
		result.Status = "failed"
		result.Error = err.Error()
		return result, nil
	}
	_ = keyExisted

	result.Status = "restored"
	return result, nil
}

// splitHKCUKey splits an HKCU key path into the registry.CURRENT_USER hive and
// the subkey path beneath it. The caller has already validated the HKCU prefix.
// registryfile.SplitKey gives the same subkey that the registry value reader
// opens, so restore reads and writes one key.
func splitHKCUKey(key string) (registry.Key, string, error) {
	_, subkey, found := registryfile.SplitKey(key)
	if !found {
		return 0, "", fmt.Errorf("invalid registry key (no subkey): %s", key)
	}
	return registry.CURRENT_USER, subkey, nil
}

// writeRegistryValue creates hive\subkey if needed and writes valueName of the
// given REG_* type to data. regType has already been validated/upper-cased.
func writeRegistryValue(hive registry.Key, subkey, valueName, regType, data string) error {
	k, _, err := registry.CreateKey(hive, subkey, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("cannot open/create registry key: %w", err)
	}
	defer k.Close()

	switch regType {
	case "REG_DWORD":
		n, perr := parseDword(data)
		if perr != nil {
			return perr
		}
		if err := k.SetDWordValue(valueName, n); err != nil {
			return fmt.Errorf("cannot set DWORD value: %w", err)
		}
	case "REG_SZ":
		if err := k.SetStringValue(valueName, data); err != nil {
			return fmt.Errorf("cannot set string value: %w", err)
		}
	case "REG_EXPAND_SZ":
		if err := k.SetExpandStringValue(valueName, data); err != nil {
			return fmt.Errorf("cannot set expand-string value: %w", err)
		}
	default:
		return fmt.Errorf("unsupported valueType: %s", regType)
	}
	return nil
}

// deleteRegistryValue removes valueName from hive\subkey. A missing value or
// key is treated as success (already absent). Used by revert.
func deleteRegistryValue(hive registry.Key, subkey, valueName string) error {
	k, err := registry.OpenKey(hive, subkey, registry.SET_VALUE)
	if err != nil {
		// Key gone — value is already absent.
		return nil
	}
	defer k.Close()
	if err := k.DeleteValue(valueName); err != nil {
		// Value already gone is fine; surface other errors.
		if err == registry.ErrNotExist {
			return nil
		}
		return fmt.Errorf("cannot delete registry value: %w", err)
	}
	return nil
}

// revertRegistrySet undoes a registry-set write using the prior-value sidecar:
// it restores the exact prior type+data, or deletes the value when it was
// absent before the write.
func revertRegistrySet(backup *registrySetBackup) error {
	hive, subkey, err := splitHKCUKey(backup.Key)
	if err != nil {
		return err
	}
	if !backup.Existed {
		return deleteRegistryValue(hive, subkey, backup.ValueName)
	}
	if backup.PriorType == "" {
		// Prior value had an unsupported type we could not reconstruct; the
		// safest reversible action is to leave the write in place rather than
		// guess. Report as a no-op error so the caller can record a skip.
		return fmt.Errorf("registry-set revert: prior value %s\\%s had an unsupported type; left in place",
			backup.Key, backup.ValueName)
	}
	return writeRegistryValue(hive, subkey, backup.ValueName, backup.PriorType, backup.PriorData)
}

// normalizeData canonicalises desired data for the idempotent-equality check so
// "0x1" and "1" both compare equal to a stored DWORD of 1. Strings pass
// through unchanged.
func normalizeData(regType, data string) string {
	if regType == "REG_DWORD" {
		if n, err := parseDword(data); err == nil {
			return strconv.FormatUint(uint64(n), 10)
		}
	}
	return data
}

// parseDword parses a DWORD from decimal or 0x-prefixed hexadecimal string form.
func parseDword(data string) (uint32, error) {
	s := strings.TrimSpace(data)
	base := 10
	if strings.HasPrefix(strings.ToLower(s), "0x") {
		s = s[2:]
		base = 16
	}
	n, err := strconv.ParseUint(s, base, 32)
	if err != nil {
		return 0, fmt.Errorf("invalid DWORD data %q: %w", data, err)
	}
	return uint32(n), nil
}
