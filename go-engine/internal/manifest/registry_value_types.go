// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package manifest

import "strings"

// registrySetValueTypes is the closed set of REG_* types that registry-set
// restore writes and that capture binds into registry-set and
// registry-value-equals entries. It is a fixed engine capability, not data.
var registrySetValueTypes = []string{"REG_DWORD", "REG_SZ", "REG_EXPAND_SZ"}

// RegistrySetValueTypes returns the REG_* types registry-set supports.
func RegistrySetValueTypes() []string {
	return append([]string(nil), registrySetValueTypes...)
}

// IsRegistrySetValueType reports whether registry-set can write valueType.
func IsRegistrySetValueType(valueType string) bool {
	for _, supported := range registrySetValueTypes {
		if strings.EqualFold(strings.TrimSpace(valueType), supported) {
			return true
		}
	}
	return false
}
