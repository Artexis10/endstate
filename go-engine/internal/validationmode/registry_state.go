// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationmode

import (
	"encoding/binary"
	"fmt"
	"sort"
	"strings"
	"unicode"
	"unicode/utf16"
)

const (
	RegistryTypeString uint32 = 1
	RegistryTypeBinary uint32 = 3
	RegistryTypeDWORD  uint32 = 4
)

// RegistryValue is one exact Windows registry value in a disposable fixture.
type RegistryValue struct {
	Name string
	Type uint32
	Data []byte
}

// RegistryKey is one relative key and all of its values. The empty Path is the
// authored root and the empty Name is that key's default value.
type RegistryKey struct {
	Path   string
	Values []RegistryValue
}

// RegistryState is an immutable, canonical snapshot of a registry subtree.
type RegistryState struct {
	keys []RegistryKey
}

// NewRegistryState validates and canonically orders a registry subtree.
func NewRegistryState(keys []RegistryKey) (RegistryState, error) {
	if len(keys) == 0 {
		return RegistryState{}, fmt.Errorf("%w: registry state must include its root key", ErrUnsafeRegistry)
	}
	state := RegistryState{keys: make([]RegistryKey, len(keys))}
	seenKeys := make(map[string]struct{}, len(keys))
	for index, key := range keys {
		if err := validateRegistryRelativeKey(key.Path); err != nil {
			return RegistryState{}, err
		}
		foldedPath := registryIdentity(key.Path)
		if _, exists := seenKeys[foldedPath]; exists {
			return RegistryState{}, fmt.Errorf("%w: duplicate registry key", ErrUnsafeRegistry)
		}
		seenKeys[foldedPath] = struct{}{}
		values, err := canonicalRegistryValues(key.Values)
		if err != nil {
			return RegistryState{}, err
		}
		state.keys[index] = RegistryKey{Path: foldedPath, Values: values}
	}
	if _, exists := seenKeys[""]; !exists {
		return RegistryState{}, fmt.Errorf("%w: registry state must include its root key", ErrUnsafeRegistry)
	}
	for path := range seenKeys {
		if path == "" {
			continue
		}
		parent := ""
		if separator := strings.LastIndex(path, `\`); separator >= 0 {
			parent = path[:separator]
		}
		if _, exists := seenKeys[parent]; !exists {
			return RegistryState{}, fmt.Errorf("%w: registry state has an orphan key", ErrUnsafeRegistry)
		}
	}
	sort.Slice(state.keys, func(left, right int) bool {
		return registryIdentityLess(state.keys[left].Path, state.keys[right].Path)
	})
	return state, nil
}

// Keys returns a defensive copy of the canonical registry keys.
func (state RegistryState) Keys() []RegistryKey {
	keys := make([]RegistryKey, len(state.keys))
	for index, key := range state.keys {
		keys[index].Path = key.Path
		keys[index].Values = copyRegistryValues(key.Values)
	}
	return keys
}

// Equal compares canonical registry identity, type, and raw data exactly.
func (state RegistryState) Equal(other RegistryState) bool {
	if len(state.keys) != len(other.keys) {
		return false
	}
	for index, key := range state.keys {
		otherKey := other.keys[index]
		if !registryIdentityEqual(key.Path, otherKey.Path) || len(key.Values) != len(otherKey.Values) {
			return false
		}
		for valueIndex, value := range key.Values {
			otherValue := otherKey.Values[valueIndex]
			if !registryIdentityEqual(value.Name, otherValue.Name) || value.Type != otherValue.Type || !equalBytes(value.Data, otherValue.Data) {
				return false
			}
		}
	}
	return true
}

func canonicalRegistryValues(values []RegistryValue) ([]RegistryValue, error) {
	result := make([]RegistryValue, len(values))
	seen := make(map[string]struct{}, len(values))
	for index, value := range values {
		if err := validateRegistryValue(value); err != nil {
			return nil, err
		}
		foldedName := registryIdentity(value.Name)
		if _, exists := seen[foldedName]; exists {
			return nil, fmt.Errorf("%w: duplicate registry value", ErrUnsafeRegistry)
		}
		seen[foldedName] = struct{}{}
		result[index] = RegistryValue{Name: foldedName, Type: value.Type, Data: append([]byte(nil), value.Data...)}
	}
	sort.Slice(result, func(left, right int) bool {
		return registryIdentityLess(result[left].Name, result[right].Name)
	})
	return result, nil
}

func copyRegistryValues(values []RegistryValue) []RegistryValue {
	result := make([]RegistryValue, len(values))
	for index, value := range values {
		result[index] = RegistryValue{Name: value.Name, Type: value.Type, Data: append([]byte(nil), value.Data...)}
	}
	return result
}

func validateRegistryRelativeKey(path string) error {
	if path == "" {
		return nil
	}
	if path != strings.TrimSpace(path) || strings.Contains(path, "/") {
		return fmt.Errorf("%w: registry key is malformed", ErrUnsafeRegistry)
	}
	for _, component := range strings.Split(path, `\`) {
		if component == "" || component == "." || component == ".." || component != strings.TrimSpace(component) ||
			strings.ContainsAny(component, `:*?[]%$~`) || strings.IndexFunc(component, unicode.IsControl) >= 0 {
			return fmt.Errorf("%w: registry key is unsafe", ErrUnsafeRegistry)
		}
	}
	return nil
}

func validateRegistryValue(value RegistryValue) error {
	if value.Name != strings.TrimSpace(value.Name) || strings.ContainsAny(value.Name, `\\/:*?[]%$~`) || strings.IndexFunc(value.Name, unicode.IsControl) >= 0 {
		return fmt.Errorf("%w: registry value name is unsafe", ErrUnsafeRegistry)
	}
	switch value.Type {
	case RegistryTypeString:
		if !validRegistryString(value.Data) {
			return fmt.Errorf("%w: registry string data is malformed", ErrUnsafeRegistry)
		}
	case RegistryTypeDWORD:
		if len(value.Data) != 4 {
			return fmt.Errorf("%w: registry DWORD data is malformed", ErrUnsafeRegistry)
		}
	case RegistryTypeBinary:
	default:
		return fmt.Errorf("%w: registry value type is unsupported", ErrUnsafeRegistry)
	}
	return nil
}

func validRegistryString(data []byte) bool {
	if len(data) < 2 || len(data)%2 != 0 || data[len(data)-2] != 0 || data[len(data)-1] != 0 {
		return false
	}
	units := make([]uint16, len(data)/2)
	for index := range units {
		units[index] = binary.LittleEndian.Uint16(data[index*2:])
		if index < len(units)-1 && units[index] == 0 {
			return false
		}
	}
	for index := 0; index < len(units)-1; index++ {
		unit := units[index]
		if utf16.IsSurrogate(rune(unit)) {
			if unit >= 0xDC00 || index+1 == len(units)-1 || units[index+1] < 0xDC00 || units[index+1] > 0xDFFF {
				return false
			}
			index++
		}
	}
	return true
}

func registryIdentityLess(left, right string) bool {
	return registryIdentity(left) < registryIdentity(right)
}

func registryIdentityEqual(left, right string) bool {
	return registryIdentity(left) == registryIdentity(right)
}

func registryIdentity(value string) string {
	var result strings.Builder
	result.Grow(len(value))
	for _, current := range value {
		canonical := current
		for next := unicode.SimpleFold(current); next != current; next = unicode.SimpleFold(next) {
			if next < canonical {
				canonical = next
			}
		}
		result.WriteRune(canonical)
	}
	return result.String()
}

func equalBytes(left, right []byte) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
