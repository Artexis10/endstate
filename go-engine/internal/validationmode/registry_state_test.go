// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationmode

import (
	"encoding/binary"
	"errors"
	"reflect"
	"testing"
	"unicode/utf16"
)

func TestRegistryStateCanonicalizesAndDefensivelyCopies(t *testing.T) {
	input := []RegistryKey{
		{Path: `software\fixture\child`, Values: []RegistryValue{{Name: "name", Type: RegistryTypeBinary, Data: []byte{3, 2, 1}}}},
		{Path: "", Values: []RegistryValue{{Name: "", Type: RegistryTypeString, Data: utf16RegistryString("root")}, {Name: "flag", Type: RegistryTypeDWORD, Data: []byte{1, 0, 0, 0}}}},
		{Path: `software`},
		{Path: `software\fixture`, Values: []RegistryValue{{Name: "blob", Type: RegistryTypeBinary, Data: []byte{1, 2}}}},
	}
	state, err := NewRegistryState(input)
	if err != nil {
		t.Fatal(err)
	}
	input[0].Values[0].Data[0] = 9

	got := state.Keys()
	want := []RegistryKey{
		{Path: "", Values: []RegistryValue{{Name: "", Type: RegistryTypeString, Data: utf16RegistryString("root")}, {Name: "FLAG", Type: RegistryTypeDWORD, Data: []byte{1, 0, 0, 0}}}},
		{Path: `SOFTWARE`, Values: []RegistryValue{}},
		{Path: `SOFTWARE\FIXTURE`, Values: []RegistryValue{{Name: "BLOB", Type: RegistryTypeBinary, Data: []byte{1, 2}}}},
		{Path: `SOFTWARE\FIXTURE\CHILD`, Values: []RegistryValue{{Name: "NAME", Type: RegistryTypeBinary, Data: []byte{3, 2, 1}}}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Keys() = %#v, want %#v", got, want)
	}
	got[2].Values[0].Data[0] = 8
	if reflect.DeepEqual(state.Keys(), got) {
		t.Fatal("Keys() exposed mutable state")
	}
}

func utf16RegistryString(value string) []byte {
	units := append(utf16.Encode([]rune(value)), 0)
	data := make([]byte, len(units)*2)
	for index, unit := range units {
		binary.LittleEndian.PutUint16(data[index*2:], unit)
	}
	return data
}

func TestRegistryStateKeepsExactTypeAndDataInEquality(t *testing.T) {
	captured, err := NewRegistryState([]RegistryKey{{Path: "", Values: []RegistryValue{{Name: "setting", Type: RegistryTypeString, Data: utf16RegistryString("captured")}}}})
	if err != nil {
		t.Fatal(err)
	}
	mutated, err := NewRegistryState([]RegistryKey{{Path: "", Values: []RegistryValue{{Name: "setting", Type: RegistryTypeBinary, Data: []byte("captured")}}}})
	if err != nil {
		t.Fatal(err)
	}
	if captured.Equal(mutated) {
		t.Fatal("Equal accepted different registry type and raw data")
	}
	for _, keys := range [][]RegistryKey{
		{{Path: ""}},
		{{Path: "", Values: []RegistryValue{{Name: "setting", Type: RegistryTypeString, Data: utf16RegistryString("changed")}}}},
		{{Path: "", Values: []RegistryValue{{Name: "setting", Type: RegistryTypeString, Data: utf16RegistryString("captured")}, {Name: "unexpected", Type: RegistryTypeBinary}}}},
	} {
		other, err := NewRegistryState(keys)
		if err != nil {
			t.Fatal(err)
		}
		if captured.Equal(other) {
			t.Fatalf("Equal accepted missing, changed, or unexpected values: %#v", keys)
		}
	}
}

func TestRegistryStateRejectsUnsafeDuplicateAndMalformedValues(t *testing.T) {
	tests := []struct {
		name string
		keys []RegistryKey
	}{
		{"duplicate keys", []RegistryKey{{Path: "Software\\Fixture"}, {Path: "software\\fixture"}}},
		{"duplicate values", []RegistryKey{{Path: "", Values: []RegistryValue{{Name: "Name", Type: RegistryTypeBinary}, {Name: "name", Type: RegistryTypeBinary}}}}},
		{"escaped relative key", []RegistryKey{{Path: `Software\..\Fixture`}}},
		{"wildcard relative key", []RegistryKey{{Path: `Software\*`}}},
		{"control value name", []RegistryKey{{Path: "", Values: []RegistryValue{{Name: "bad\x00", Type: RegistryTypeBinary}}}}},
		{"unknown type", []RegistryKey{{Path: "", Values: []RegistryValue{{Name: "value", Type: 99}}}}},
		{"malformed string", []RegistryKey{{Path: "", Values: []RegistryValue{{Name: "value", Type: RegistryTypeString, Data: []byte{1, 0, 0}}}}}},
		{"malformed dword", []RegistryKey{{Path: "", Values: []RegistryValue{{Name: "value", Type: RegistryTypeDWORD, Data: []byte{1}}}}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewRegistryState(test.keys); !errors.Is(err, ErrUnsafeRegistry) {
				t.Fatalf("NewRegistryState() error = %v, want ErrUnsafeRegistry", err)
			}
		})
	}
}

func TestRegistryStateRequiresRootAndAncestorClosure(t *testing.T) {
	tests := []struct {
		name string
		keys []RegistryKey
	}{
		{"nil", nil},
		{"rootless", []RegistryKey{{Path: "Child"}}},
		{"orphan", []RegistryKey{{Path: ""}, {Path: `Child\Grandchild`}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewRegistryState(test.keys); !errors.Is(err, ErrUnsafeRegistry) {
				t.Fatalf("NewRegistryState() error = %v, want ErrUnsafeRegistry", err)
			}
		})
	}
}

func TestRegistryStateUsesOneCaseInsensitiveIdentityForKeysAndValues(t *testing.T) {
	for _, keys := range [][]RegistryKey{
		{{Path: ""}, {Path: `Σ`}, {Path: `ς`}},
		{{Path: "", Values: []RegistryValue{{Name: `Σ`, Type: RegistryTypeBinary}, {Name: `ς`, Type: RegistryTypeBinary}}}},
	} {
		if _, err := NewRegistryState(keys); !errors.Is(err, ErrUnsafeRegistry) {
			t.Fatalf("NewRegistryState(%#v) error = %v, want ErrUnsafeRegistry", keys, err)
		}
	}
	state, err := NewRegistryState([]RegistryKey{{Path: ""}, {Path: `σ`}})
	if err != nil {
		t.Fatal(err)
	}
	if got := state.Keys()[1].Path; got != `Σ` {
		t.Fatalf("canonical key = %q, want %q", got, `Σ`)
	}
}
