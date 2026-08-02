// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package modules

import (
	"strings"
	"unicode"
)

// SecretCoordinateKind separates filesystem declarations from registry
// declarations without allowing registry-shaped input to acquire filesystem
// authority.
type SecretCoordinateKind uint8

const (
	SecretCoordinateFilesystem SecretCoordinateKind = iota
	SecretCoordinateRegistry
	SecretCoordinateRegistryInvalid
)

// ClassifySecretCoordinate returns the coordinate kind and, for valid registry
// coordinates, its canonical hive and component spelling.
func ClassifySecretCoordinate(authored string) (SecretCoordinateKind, string) {
	trimmed := strings.TrimSpace(authored)
	upper := strings.ToUpper(trimmed)
	if !strings.HasPrefix(upper, "HK") && !strings.HasPrefix(upper, "HKEY") {
		return SecretCoordinateFilesystem, ""
	}
	if trimmed != authored || strings.ContainsRune(authored, '\x00') {
		return SecretCoordinateRegistryInvalid, ""
	}
	for _, hive := range []struct {
		name, canonical string
	}{
		{"HKEY_CURRENT_USER", "HKCU"},
		{"HKEY_LOCAL_MACHINE", "HKLM"},
		{"HKCU", "HKCU"},
		{"HKLM", "HKLM"},
	} {
		if !strings.HasPrefix(upper, hive.name) {
			continue
		}
		remainder := authored[len(hive.name):]
		if strings.HasPrefix(remainder, ":") {
			remainder = remainder[1:]
		}
		if len(remainder) == 0 || (remainder[0] != '\\' && remainder[0] != '/') {
			return SecretCoordinateRegistryInvalid, ""
		}
		remainder = strings.ReplaceAll(remainder[1:], "/", `\`)
		components := strings.Split(remainder, `\`)
		if len(components) == 0 {
			return SecretCoordinateRegistryInvalid, ""
		}
		for _, component := range components {
			if component == "" || component == "." || component == ".." || component != strings.TrimSpace(component) ||
				strings.ContainsAny(component, `:*?[]%$~`) || strings.IndexFunc(component, unicode.IsControl) >= 0 {
				return SecretCoordinateRegistryInvalid, ""
			}
		}
		return SecretCoordinateRegistry, hive.canonical + `\` + strings.Join(components, `\`)
	}
	return SecretCoordinateRegistryInvalid, ""
}
