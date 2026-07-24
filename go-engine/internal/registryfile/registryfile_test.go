// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package registryfile

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"
	"unicode/utf16"
)

func TestRewriteSubtreeRewritesNestedSectionsAndPreservesValues(t *testing.T) {
	physical := `HKCU\Software\Endstate\Validation\nonce-one\Software\Vendor`
	semantic := `HKCU\Software\Vendor`
	input := testEncodeUTF16LE("Windows Registry Editor Version 5.00\r\n\r\n" +
		`[HKEY_CURRENT_USER\Software\Endstate\Validation\nonce-one\Software\Vendor]` + "\r\n" +
		`"Root"="nonce-one must stay in value data"` + "\r\n\r\n" +
		`[HKEY_CURRENT_USER\Software\Endstate\Validation\nonce-one\Software\Vendor\Child]` + "\r\n" +
		`"Number"=dword:0000002a` + "\r\n")

	got, err := RewriteSubtree(input, physical, semantic)
	if err != nil {
		t.Fatalf("RewriteSubtree: %v", err)
	}
	if !bytes.HasPrefix(got, []byte{0xff, 0xfe}) {
		t.Fatal("RewriteSubtree did not emit a UTF-16LE BOM")
	}
	text := decodeUTF16LE(t, got)
	want := "Windows Registry Editor Version 5.00\r\n\r\n" +
		`[HKCU\Software\Vendor]` + "\r\n" +
		`"Root"="nonce-one must stay in value data"` + "\r\n\r\n" +
		`[HKCU\Software\Vendor\Child]` + "\r\n" +
		`"Number"=dword:0000002a` + "\r\n"
	if text != want {
		t.Fatalf("rewritten document mismatch\n got: %q\nwant: %q", text, want)
	}
}

func TestRewriteSubtreeAcceptsCanonicalUTF8AndIsDeterministic(t *testing.T) {
	input := []byte("\xef\xbb\xbfWindows Registry Editor Version 5.00\n\n[HKCU\\Software\\Vendor]\n@=hex(2):41,00,00,00\n")
	first, err := RewriteSubtree(input, `HKCU\Software\Vendor`, `HKCU\Software\Mapped`)
	if err != nil {
		t.Fatal(err)
	}
	second, err := RewriteSubtree(first, `HKCU\Software\Mapped`, `HKCU\Software\Mapped`)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("RewriteSubtree output is not deterministic")
	}
}

func TestRewriteSubtreeRejectsAmbiguousOrOutOfScopeDocuments(t *testing.T) {
	validHeader := "Windows Registry Editor Version 5.00\r\n\r\n"
	tests := []struct {
		name string
		data []byte
	}{
		{name: "unrelated section", data: []byte(validHeader + "[HKCU\\Software\\Other]\r\n")},
		{name: "other hive", data: []byte(validHeader + "[HKEY_LOCAL_MACHINE\\Software\\Vendor]\r\n")},
		{name: "bare hive", data: []byte(validHeader + "[HKCU]\r\n")},
		{name: "traversal component", data: []byte(validHeader + "[HKCU\\Software\\Vendor\\..\\Other]\r\n")},
		{name: "malformed section", data: []byte(validHeader + "[HKCU\\Software\\Vendor\r\n")},
		{name: "deletion section", data: []byte(validHeader + "[-HKCU\\Software\\Vendor]\r\n")},
		{name: "deletion value", data: []byte(validHeader + "[HKCU\\Software\\Vendor]\r\n\"Token\"=-\r\n")},
		{name: "no section", data: []byte(validHeader + `"Value"="orphan"` + "\r\n")},
		{name: "multiple documents", data: []byte(validHeader + "[HKCU\\Software\\Vendor]\r\n" + validHeader)},
		{name: "embedded nul", data: []byte(validHeader + "[HKCU\\Software\\Vendor]\r\n\x00")},
		{name: "control byte", data: []byte(validHeader + "[HKCU\\Software\\Vendor]\r\n\x01")},
		{name: "invalid utf8", data: append([]byte(validHeader+"[HKCU\\Software\\Vendor]\r\n"), 0xff)},
		{name: "utf16 without bom", data: encodeUTF16LENoBOM(validHeader + "[HKCU\\Software\\Vendor]\r\n")},
		{name: "utf16 odd length", data: []byte{0xff, 0xfe, 0x41}},
		{name: "utf16 unpaired surrogate", data: []byte{0xff, 0xfe, 0x00, 0xd8}},
		{name: "utf16be", data: []byte{0xfe, 0xff, 0x00, 0x41}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := RewriteSubtree(test.data, `HKCU\Software\Vendor`, `HKCU\Software\Mapped`); err == nil {
				t.Fatal("RewriteSubtree accepted unsafe registry document")
			}
		})
	}
}

func TestRewriteSubtreeRejectsUnsafeExpectedAndReplacementRoots(t *testing.T) {
	data := []byte("Windows Registry Editor Version 5.00\r\n\r\n[HKCU\\Software\\Vendor]\r\n")
	for _, test := range []struct{ from, to string }{
		{`HKLM\Software\Vendor`, `HKCU\Software\Mapped`},
		{`HKCU\Software\Vendor`, `HKCU`},
		{`HKCU\Software\Vendor`, `HKCU\Software\..\Mapped`},
	} {
		if _, err := RewriteSubtree(data, test.from, test.to); err == nil {
			t.Fatalf("RewriteSubtree accepted roots from=%q to=%q", test.from, test.to)
		}
	}
}

func testEncodeUTF16LE(value string) []byte {
	words := utf16.Encode([]rune(value))
	result := make([]byte, 2+len(words)*2)
	result[0], result[1] = 0xff, 0xfe
	for index, word := range words {
		binary.LittleEndian.PutUint16(result[2+index*2:], word)
	}
	return result
}

func encodeUTF16LENoBOM(value string) []byte {
	return testEncodeUTF16LE(value)[2:]
}

func decodeUTF16LE(t *testing.T, value []byte) string {
	t.Helper()
	if len(value) < 2 || value[0] != 0xff || value[1] != 0xfe || (len(value)-2)%2 != 0 {
		t.Fatal("invalid UTF-16LE test value")
	}
	words := make([]uint16, (len(value)-2)/2)
	for index := range words {
		words[index] = binary.LittleEndian.Uint16(value[2+index*2:])
	}
	return strings.TrimPrefix(string(utf16.Decode(words)), "\ufeff")
}
