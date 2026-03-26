// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package verifier

import (
	"fmt"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"golang.org/x/sys/windows/registry"
)

// CheckRegistryKeyExists opens the specified Windows registry key and
// optionally verifies that a named value exists within it. The entry's Path
// field must start with a recognised hive prefix (HKCU, HKLM, HKCR, HKU, HKCC)
// followed by a backslash and the subkey path. If ValueName is provided, the
// value must exist within the opened key for the check to pass.
func CheckRegistryKeyExists(entry manifest.VerifyEntry) VerifyResult {
	hive, subkey, err := parseRegistryPath(entry.Path)
	if err != nil {
		return VerifyResult{
			Type:    entry.Type,
			Path:    entry.Path,
			Pass:    false,
			Message: fmt.Sprintf("Registry check failed: %s", err.Error()),
		}
	}

	key, err := registry.OpenKey(hive, subkey, registry.QUERY_VALUE)
	if err != nil {
		return VerifyResult{
			Type:    entry.Type,
			Path:    entry.Path,
			Pass:    false,
			Message: fmt.Sprintf("Registry key not found: %s", entry.Path),
		}
	}
	defer key.Close()

	// If a value name is specified, check that it exists within the key.
	if entry.ValueName != "" {
		_, _, err := key.GetValue(entry.ValueName, nil)
		if err != nil {
			return VerifyResult{
				Type:      entry.Type,
				Path:      entry.Path,
				ValueName: entry.ValueName,
				Pass:      false,
				Message:   fmt.Sprintf("Registry value not found: %s\\%s", entry.Path, entry.ValueName),
			}
		}
		return VerifyResult{
			Type:      entry.Type,
			Path:      entry.Path,
			ValueName: entry.ValueName,
			Pass:      true,
			Message:   fmt.Sprintf("Registry value exists: %s\\%s", entry.Path, entry.ValueName),
		}
	}

	return VerifyResult{
		Type:    entry.Type,
		Path:    entry.Path,
		Pass:    true,
		Message: fmt.Sprintf("Registry key exists: %s", entry.Path),
	}
}

// parseRegistryPath splits a registry path string into a hive constant and
// subkey path. Recognised prefixes: HKCU, HKLM, HKCR, HKU, HKCC (or their
// long forms HKEY_CURRENT_USER, etc.).
func parseRegistryPath(path string) (registry.Key, string, error) {
	// Normalise forward-slashes to backslashes for consistent splitting.
	path = strings.ReplaceAll(path, "/", "\\")

	idx := strings.Index(path, "\\")
	if idx < 0 {
		return 0, "", fmt.Errorf("invalid registry path (no subkey): %s", path)
	}

	hiveStr := strings.ToUpper(path[:idx])
	subkey := path[idx+1:]

	var hive registry.Key
	switch hiveStr {
	case "HKCU", "HKEY_CURRENT_USER":
		hive = registry.CURRENT_USER
	case "HKLM", "HKEY_LOCAL_MACHINE":
		hive = registry.LOCAL_MACHINE
	case "HKCR", "HKEY_CLASSES_ROOT":
		hive = registry.CLASSES_ROOT
	case "HKU", "HKEY_USERS":
		hive = registry.USERS
	case "HKCC", "HKEY_CURRENT_CONFIG":
		hive = registry.CURRENT_CONFIG
	default:
		return 0, "", fmt.Errorf("unknown registry hive: %s", hiveStr)
	}

	return hive, subkey, nil
}
