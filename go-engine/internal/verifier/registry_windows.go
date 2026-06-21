// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package verifier

import (
	"fmt"
	"strconv"
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

// CheckRegistryValueEquals opens the specified registry key and compares the
// named value's DATA against the expected entry.Data (and, when given,
// entry.ValueType). This is the value-DATA assertion that pairs with the
// value-level registry-set restore strategy — unlike CheckRegistryKeyExists,
// which only checks existence.
//
// entry.Path is the key path, entry.ValueName the value to read, entry.Data the
// expected data in string form, and entry.ValueType the expected REG_* type
// (optional; when set, a type mismatch fails). DWORD comparisons are numeric so
// "0x1" and "1" both match a stored DWORD of 1.
func CheckRegistryValueEquals(entry manifest.VerifyEntry) VerifyResult {
	base := VerifyResult{
		Type:      entry.Type,
		Path:      entry.Path,
		ValueName: entry.ValueName,
	}

	if entry.ValueName == "" {
		base.Pass = false
		base.Message = "registry-value-equals requires a valueName"
		return base
	}

	hive, subkey, err := parseRegistryPath(entry.Path)
	if err != nil {
		base.Pass = false
		base.Message = fmt.Sprintf("Registry check failed: %s", err.Error())
		return base
	}

	key, err := registry.OpenKey(hive, subkey, registry.QUERY_VALUE)
	if err != nil {
		base.Pass = false
		base.Message = fmt.Sprintf("Registry key not found: %s", entry.Path)
		return base
	}
	defer key.Close()

	actualType, actualData, ok := readRegistryValueData(key, entry.ValueName)
	if !ok {
		base.Pass = false
		base.Message = fmt.Sprintf("Registry value not found: %s\\%s", entry.Path, entry.ValueName)
		return base
	}

	// Optional type assertion.
	wantType := strings.ToUpper(strings.TrimSpace(entry.ValueType))
	if wantType != "" && wantType != actualType {
		base.Pass = false
		base.Message = fmt.Sprintf("Registry value type mismatch at %s\\%s: expected %s, got %s",
			entry.Path, entry.ValueName, wantType, actualType)
		return base
	}

	// Data comparison — numeric for DWORD, exact for strings.
	if !registryDataEqual(actualType, actualData, entry.Data) {
		base.Pass = false
		base.Message = fmt.Sprintf("Registry value mismatch at %s\\%s: expected %q, got %q",
			entry.Path, entry.ValueName, entry.Data, actualData)
		return base
	}

	base.Pass = true
	base.Message = fmt.Sprintf("Registry value matches: %s\\%s = %q", entry.Path, entry.ValueName, actualData)
	return base
}

// readRegistryValueData reads the named value's REG_* type string and string-
// form data. Returns ok=false when the value is missing or an unsupported type.
func readRegistryValueData(key registry.Key, valueName string) (regType, data string, ok bool) {
	_, valType, err := key.GetValue(valueName, nil)
	if err != nil {
		return "", "", false
	}
	switch valType {
	case registry.DWORD:
		v, _, gerr := key.GetIntegerValue(valueName)
		if gerr != nil {
			return "", "", false
		}
		return "REG_DWORD", strconv.FormatUint(v, 10), true
	case registry.SZ:
		v, _, gerr := key.GetStringValue(valueName)
		if gerr != nil {
			return "", "", false
		}
		return "REG_SZ", v, true
	case registry.EXPAND_SZ:
		v, _, gerr := key.GetStringValue(valueName)
		if gerr != nil {
			return "", "", false
		}
		return "REG_EXPAND_SZ", v, true
	default:
		return "", "", false
	}
}

// registryDataEqual compares the stored value against an expected string. DWORD
// data is compared numerically so "0x1" equals "1"; other types compare exactly.
func registryDataEqual(regType, actual, expected string) bool {
	if regType == "REG_DWORD" {
		a, aerr := parseDwordString(actual)
		e, eerr := parseDwordString(expected)
		if aerr == nil && eerr == nil {
			return a == e
		}
	}
	return actual == expected
}

// parseDwordString parses a DWORD from decimal or 0x-prefixed hex string form.
func parseDwordString(s string) (uint64, error) {
	t := strings.TrimSpace(s)
	base := 10
	if strings.HasPrefix(strings.ToLower(t), "0x") {
		t = t[2:]
		base = 16
	}
	return strconv.ParseUint(t, base, 64)
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
