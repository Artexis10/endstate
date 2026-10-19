// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package registryvalue

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/sys/windows/registry"

	"github.com/Artexis10/endstate/go-engine/internal/registryfile"
)

// typeNames maps the value types that the Windows registry API defines to
// their REG_* names. The set is fixed by the API, not by data.
var typeNames = map[uint32]string{
	registry.NONE:                       "REG_NONE",
	registry.SZ:                         "REG_SZ",
	registry.EXPAND_SZ:                  "REG_EXPAND_SZ",
	registry.BINARY:                     "REG_BINARY",
	registry.DWORD:                      "REG_DWORD",
	registry.DWORD_BIG_ENDIAN:           "REG_DWORD_BIG_ENDIAN",
	registry.LINK:                       "REG_LINK",
	registry.MULTI_SZ:                   "REG_MULTI_SZ",
	registry.RESOURCE_LIST:              "REG_RESOURCE_LIST",
	registry.FULL_RESOURCE_DESCRIPTOR:   "REG_FULL_RESOURCE_DESCRIPTOR",
	registry.RESOURCE_REQUIREMENTS_LIST: "REG_RESOURCE_REQUIREMENTS_LIST",
	registry.QWORD:                      "REG_QWORD",
}

// splitKey returns the hive token (hkcu or hklm) and the subkey of a key such
// as HKCU\Software\Vendor or HKEY_CURRENT_USER\Software\Vendor. ok is false for
// any other hive or for a key without a subkey. The subkey keeps its case: Go's
// lower-casing would merge sibling keys that the registry keeps apart, such as
// İ and i, and open the wrong one.
func splitKey(key string) (hive, subkey string, ok bool) {
	hive, subkey, found := registryfile.SplitKey(key)
	hive = strings.ToLower(hive)
	if !found || subkey == "" || (hive != "hkcu" && hive != "hklm") {
		return "", "", false
	}
	return hive, subkey, true
}

// notFound reports whether err means that a key or a value does not exist.
func notFound(err error) bool {
	return errors.Is(err, registry.ErrNotExist) || errors.Is(err, syscall.ERROR_PATH_NOT_FOUND)
}

// Read returns the named value under key. found is false when the key or the
// value is missing; keyFound reports whether the key opened. err reports a key
// or value that may exist but cannot be read, for example when access is
// denied, so a caller never mistakes a failed read for a missing value.
func Read(key, valueName string) (value Value, found, keyFound bool, err error) {
	hiveName, subkey, ok := splitKey(key)
	if !ok {
		return Value{}, false, false, fmt.Errorf("unsupported registry key %q", key)
	}
	hive := registry.CURRENT_USER
	if hiveName == "hklm" {
		hive = registry.LOCAL_MACHINE
	}
	opened, err := registry.OpenKey(hive, subkey, registry.QUERY_VALUE)
	if notFound(err) {
		return Value{}, false, false, nil
	}
	if err != nil {
		return Value{}, false, false, err
	}
	defer opened.Close()

	_, valueType, err := opened.GetValue(valueName, nil)
	if notFound(err) {
		return Value{}, false, true, nil
	}
	if err != nil {
		return Value{}, false, true, err
	}
	switch valueType {
	case registry.DWORD:
		number, _, err := opened.GetIntegerValue(valueName)
		if err != nil {
			return Value{}, false, true, err
		}
		return Value{Type: "REG_DWORD", Data: strconv.FormatUint(number, 10)}, true, true, nil
	case registry.SZ, registry.EXPAND_SZ:
		text, _, err := opened.GetStringValue(valueName)
		if err != nil {
			return Value{}, false, true, err
		}
		return Value{Type: typeNames[valueType], Data: text}, true, true, nil
	default:
		name, known := typeNames[valueType]
		if !known {
			name = fmt.Sprintf("REG_TYPE_%d", valueType)
		}
		return Value{Type: name}, true, true, nil
	}
}
