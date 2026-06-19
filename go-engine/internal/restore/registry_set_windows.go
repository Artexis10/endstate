// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package restore

import (
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/sys/windows/registry"
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

	// Validate target + value (cross-platform — runs before any registry call).
	if err := validateRegistrySet(entry); err != nil {
		result.Status = "failed"
		result.Error = err.Error()
		return result, nil
	}

	hive, subkey, err := splitHKCUKey(entry.Key)
	if err != nil {
		result.Status = "failed"
		result.Error = err.Error()
		return result, nil
	}

	// Probe the current state of the named value (existence + type + data).
	existed, curType, curData, keyExisted := readRegistryValue(hive, subkey, entry.ValueName)
	result.TargetExistedBefore = existed

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

	backupPath, err := writeRegistrySetBackup(backup, opts)
	if err != nil {
		result.Status = "failed"
		result.Error = fmt.Sprintf("backup: %v", err)
		return result, nil
	}
	result.BackupPath = backupPath
	result.BackupCreated = true

	// Create the key if missing, then write the value.
	if err := writeRegistryValue(hive, subkey, entry.ValueName, desiredType, entry.Data); err != nil {
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
func splitHKCUKey(key string) (registry.Key, string, error) {
	norm := strings.ReplaceAll(key, "/", "\\")
	idx := strings.Index(norm, "\\")
	if idx < 0 {
		return 0, "", fmt.Errorf("invalid registry key (no subkey): %s", key)
	}
	return registry.CURRENT_USER, norm[idx+1:], nil
}

// readRegistryValue opens hive\subkey and reads the named value. It returns
// whether the value exists, its REG_* type string, its string-form data, and
// whether the key itself exists. Missing key or value yields existed=false.
func readRegistryValue(hive registry.Key, subkey, valueName string) (existed bool, regType string, data string, keyExisted bool) {
	k, err := registry.OpenKey(hive, subkey, registry.QUERY_VALUE)
	if err != nil {
		return false, "", "", false
	}
	defer k.Close()
	keyExisted = true

	_, valType, err := k.GetValue(valueName, nil)
	if err != nil {
		return false, "", "", true
	}

	switch valType {
	case registry.DWORD:
		v, _, gerr := k.GetIntegerValue(valueName)
		if gerr != nil {
			return false, "", "", true
		}
		return true, "REG_DWORD", strconv.FormatUint(v, 10), true
	case registry.SZ:
		v, _, gerr := k.GetStringValue(valueName)
		if gerr != nil {
			return false, "", "", true
		}
		return true, "REG_SZ", v, true
	case registry.EXPAND_SZ:
		v, _, gerr := k.GetStringValue(valueName)
		if gerr != nil {
			return false, "", "", true
		}
		return true, "REG_EXPAND_SZ", v, true
	default:
		// An unsupported existing type is reported as existing with an empty
		// normalized type so the idempotent-skip check never matches and the
		// prior value is still captured verbatim where possible.
		return true, "", "", true
	}
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
