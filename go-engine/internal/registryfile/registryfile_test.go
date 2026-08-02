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
		`"Root"="value data stays unchanged"` + "\r\n\r\n" +
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
		`[HKEY_CURRENT_USER\Software\Vendor]` + "\r\n" +
		`"Root"="value data stays unchanged"` + "\r\n\r\n" +
		`[HKEY_CURRENT_USER\Software\Vendor\Child]` + "\r\n" +
		`"Number"=dword:0000002a` + "\r\n"
	if text != want {
		t.Fatalf("rewritten document mismatch\n got: %q\nwant: %q", text, want)
	}
	if strings.Contains(strings.ToLower(text), "nonce-one") {
		t.Fatalf("rewritten document retained physical nonce namespace: %q", text)
	}
	roundTrip, err := RewriteSubtree(got, semantic, semantic)
	if err != nil {
		t.Fatalf("RewriteSubtree round trip: %v", err)
	}
	if !bytes.Equal(roundTrip, got) {
		t.Fatal("canonical semantic registry document did not round-trip deterministically")
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

func TestRewriteSubtreeAcceptsStrictExportValueGrammar(t *testing.T) {
	input := []byte("Windows Registry Editor Version 5.00\n" +
		"; preamble comment\n" +
		"# alternate comment\n\n" +
		"[HKCU\\Software\\Vendor]\n" +
		`@="default"` + "\n" +
		`"Path"="C:\\Users\\\"quoted\""` + "\n" +
		`"Empty"=""` + "\n" +
		`"Dword"=dword:0000002a` + "\n" +
		`"EmptyBinary"=hex:` + "\n" +
		`"Binary"=hex:00,01,af,FF` + "\n" +
		`"Expand"=hex(2):25,00,41,00,50,00,50,00,44,00,41,00,54,00,41,00,25,00,\` + "\n" +
		`  00,00` + "\n" +
		`"Multi"=hex(7):6f,00,6e,00,65,00,00,00,74,00,77,00,6f,00,00,00,00,00` + "\n" +
		`"Qword"=hex(b):2a,00,00,00,00,00,00,00` + "\n" +
		"; section comment\n\n" +
		"[HKEY_CURRENT_USER\\Software\\Vendor\\Child]\n" +
		`"Child"="preserved"` + "\n")

	got, err := RewriteSubtree(input, `HKCU\Software\Vendor`, `HKCU\Software\Mapped`)
	if err != nil {
		t.Fatalf("RewriteSubtree: %v", err)
	}
	text := decodeUTF16LE(t, got)
	for _, required := range []string{
		`[HKEY_CURRENT_USER\Software\Mapped]`,
		`[HKEY_CURRENT_USER\Software\Mapped\Child]`,
		`"Path"="C:\\Users\\\"quoted\""`,
		`"Expand"=hex(2):25,00,41,00,50,00,50,00,44,00,41,00,54,00,41,00,25,00,\` + "\r\n  00,00",
		`"Qword"=hex(b):2a,00,00,00,00,00,00,00`,
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("strict output omitted or changed %q: %q", required, text)
		}
	}
}

func TestRewriteSubtreeRejectsNonSectionSyntaxAndContextAmbiguity(t *testing.T) {
	header := "Windows Registry Editor Version 5.00\r\n\r\n"
	section := "[HKCU\\Software\\Vendor]\r\n"
	tests := []struct {
		name string
		data []byte
	}{
		{name: "arbitrary junk", data: []byte(header + section + "this is not registry syntax\r\n")},
		{name: "orphan value before section", data: []byte(header + `"Orphan"="value"` + "\r\n" + section)},
		{name: "regedit4 header", data: []byte(header + section + "REGEDIT4\r\n")},
		{name: "extra header suffix", data: []byte(header + section + registryHeader + " extra\r\n")},
		{name: "indented extra header", data: []byte(header + section + " " + registryHeader + "\r\n")},
		{name: "deletion value whitespace", data: []byte(header + section + `"Token"= -` + "\r\n")},
		{name: "default deletion whitespace", data: []byte(header + section + `@= -` + "\r\n")},
		{name: "deletion with spaced equals", data: []byte(header + section + `"Token" =-` + "\r\n")},
		{name: "unsupported qword spelling", data: []byte(header + section + `"Token"=qword:000000000000002a` + "\r\n")},
		{name: "short dword", data: []byte(header + section + `"Token"=dword:2a` + "\r\n")},
		{name: "invalid hex type", data: []byte(header + section + `"Token"=hex(z):01` + "\r\n")},
		{name: "short hex byte", data: []byte(header + section + `"Token"=hex:0` + "\r\n")},
		{name: "empty hex element", data: []byte(header + section + `"Token"=hex:01,,02` + "\r\n")},
		{name: "unterminated string", data: []byte(header + section + `"Token"="value` + "\r\n")},
		{name: "unsupported string escape", data: []byte(header + section + `"Token"="bad\qescape"` + "\r\n")},
		{name: "continuation without value", data: []byte(header + section + "  01,02\r\n")},
		{name: "continuation missing indent", data: []byte(header + section + `"Token"=hex:01,\` + "\r\n02,03\r\n")},
		{name: "continuation interrupted by value", data: []byte(header + section + `"Token"=hex:01,\` + "\r\n" + `"Other"="value"` + "\r\n")},
		{name: "continuation malformed bytes", data: []byte(header + section + `"Token"=hex:01,\` + "\r\n  0g\r\n")},
		{name: "unterminated continuation", data: []byte(header + section + `"Token"=hex:01,\` + "\r\n")},
		{name: "lone carriage return", data: []byte(header + section + `"Token"="value"` + "\r")},
		{name: "second utf8 bom document", data: append([]byte(header+section), []byte("\xef\xbb\xbf"+registryHeader+"\r\n"+section)...)},
		{name: "second utf16 bom document", data: testEncodeUTF16LE(header + section + "\ufeff" + registryHeader + "\r\n" + section)},
		{name: "repeated utf8 bom", data: append([]byte("\xef\xbb\xbf"), []byte("\xef\xbb\xbf"+registryHeader+"\r\n"+section)...)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := RewriteSubtree(test.data, `HKCU\Software\Vendor`, `HKCU\Software\Mapped`); err == nil {
				t.Fatal("RewriteSubtree accepted unsupported registry syntax")
			}
		})
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
