// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package modules

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/Artexis10/endstate/go-engine/internal/registryfile"
	"github.com/Artexis10/endstate/go-engine/internal/validationmode"
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

// RegistrySecretScope is the resolved registry secret boundary of one module.
// Keys are compared case-insensitively after the hive spelling is
// canonicalized, so the scope also matches key text read from reg.exe exports.
type RegistrySecretScope struct {
	subtrees []string
	values   []registrySecretValueRule
	// invalid records a registry-shaped secret declaration that cannot be
	// resolved. An unresolvable boundary must not be guessed at, so it
	// intersects and conflicts with every registry capture of the module.
	invalid bool
}

type registrySecretValueRule struct {
	key      string
	name     string
	anyValue bool
	subkeys  bool
}

// RegistryCaptureConflict names one registry capture entry that its own
// module's secret declaration contradicts, so the entry cannot be captured
// without either leaking or dropping everything it declares.
type RegistryCaptureConflict struct {
	ModuleID   string `json:"moduleId"`
	Coordinate string `json:"coordinate"`
	Authored   string `json:"authored"`
	Reason     string `json:"reason"`
}

// ResolveRegistrySecretScope resolves a module's registry secret declarations
// and reports every registry capture entry they contradict:
//   - a whole-key capture equal to or inside a secret subtree;
//   - a named-value capture whose key or value is secret;
//   - every registry capture entry when a registry secret declaration is
//     malformed, because the boundary is then unknown.
//
// Filesystem secrets and file captures are outside this scope.
func ResolveRegistrySecretScope(mod *Module) (RegistrySecretScope, []RegistryCaptureConflict) {
	var scope RegistrySecretScope
	if mod == nil {
		return scope, nil
	}
	if mod.Secrets != nil {
		for _, declarations := range [][]string{mod.Secrets.Files, mod.Secrets.RegistryKeys} {
			for _, authored := range declarations {
				kind, canonical := ClassifySecretCoordinate(authored)
				switch kind {
				case SecretCoordinateFilesystem:
					continue
				case SecretCoordinateRegistryInvalid:
					scope.invalid = true
					continue
				}
				key := registryfile.MatchKey(canonical)
				scope.subtrees = append(scope.subtrees, key)
				if separator := strings.LastIndex(key, `\`); separator > 0 {
					scope.values = append(scope.values, registrySecretValueRule{key: key[:separator], name: key[separator+1:]})
				}
			}
		}
		for _, declaration := range mod.Secrets.RegistryValues {
			kind, canonical := ClassifySecretCoordinate(declaration.Key)
			if kind != SecretCoordinateRegistry {
				scope.invalid = true
				continue
			}
			scope.values = append(scope.values, registrySecretValueRule{
				key: registryfile.MatchKey(canonical), name: strings.ToLower(declaration.ValueName),
				anyValue: declaration.ValueName == "", subkeys: declaration.Subkeys,
			})
		}
	}

	var conflicts []RegistryCaptureConflict
	if mod.Capture == nil {
		return scope, nil
	}
	conflict := func(coordinate, authored, reason string) {
		conflicts = append(conflicts, RegistryCaptureConflict{ModuleID: mod.ID, Coordinate: coordinate, Authored: authored, Reason: reason})
	}
	for index, capture := range mod.Capture.RegistryKeys {
		coordinate := fmt.Sprintf("capture.registryKeys[%d].key", index)
		switch {
		case scope.invalid:
			conflict(coordinate, capture.Key, "the module declares a registry secret that cannot be resolved")
		case !registryCaptureKeyIsCanonical(capture.Key):
			conflict(coordinate, capture.Key, "the capture key cannot be checked against the module's secrets")
		case scope.ExcludesKey(capture.Key):
			conflict(coordinate, capture.Key, "the capture key is inside a declared secret key")
		}
	}
	for index, capture := range mod.Capture.RegistryValues {
		coordinate := fmt.Sprintf("capture.registryValues[%d]", index)
		switch {
		case scope.invalid:
			conflict(coordinate, capture.Key, "the module declares a registry secret that cannot be resolved")
		case !registryCaptureKeyIsCanonical(capture.Key):
			conflict(coordinate, capture.Key, "the capture key cannot be checked against the module's secrets")
		case scope.ExcludesValue(capture.Key, capture.ValueName):
			conflict(coordinate, capture.Key, "the captured value is a declared secret")
		}
	}
	return scope, conflicts
}

// ExcludesKey reports whether every value under key is secret because key is
// equal to or inside a secret subtree.
func (scope RegistrySecretScope) ExcludesKey(key string) bool {
	if scope.invalid {
		return true
	}
	key = registryfile.MatchKey(key)
	for _, subtree := range scope.subtrees {
		if registryKeyWithin(key, subtree) {
			return true
		}
	}
	return false
}

// ExcludesValue reports whether the value valueName on key is secret. The
// empty name is the key's default value.
func (scope RegistrySecretScope) ExcludesValue(key, valueName string) bool {
	if scope.ExcludesKey(key) {
		return true
	}
	key, name := registryfile.MatchKey(key), strings.ToLower(valueName)
	for _, rule := range scope.values {
		onKey := key == rule.key || rule.subkeys && registryKeyWithin(key, rule.key)
		if onKey && (rule.anyValue || rule.name == name) {
			return true
		}
	}
	return false
}

// Intersects reports whether any secret may exist inside the subtree that a
// whole-key capture of captureKey exports.
func (scope RegistrySecretScope) Intersects(captureKey string) bool {
	if scope.invalid {
		return true
	}
	captureKey = registryfile.MatchKey(captureKey)
	for _, subtree := range scope.subtrees {
		if registryKeyWithin(subtree, captureKey) || registryKeyWithin(captureKey, subtree) {
			return true
		}
	}
	for _, rule := range scope.values {
		if registryKeyWithin(rule.key, captureKey) || rule.subkeys && registryKeyWithin(captureKey, rule.key) {
			return true
		}
	}
	return false
}

func registryKeyWithin(key, root string) bool {
	return key == root || strings.HasPrefix(key, root+`\`)
}

// registryCaptureKeyIsCanonical keeps the established capture rule: only a
// canonical HKCU key is exported, because only HKCU is restorable.
func registryCaptureKeyIsCanonical(authored string) bool {
	kind, canonical := ClassifySecretCoordinate(authored)
	if kind != SecretCoordinateRegistry {
		return false
	}
	_, err := validationmode.NormalizeHKCU(canonical)
	return err == nil
}
