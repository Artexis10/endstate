// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package registryfile

import (
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

// compareStringOrdinal is kernel32's CompareStringOrdinal, which
// golang.org/x/sys/windows does not wrap.
var compareStringOrdinal = windows.NewLazySystemDLL("kernel32.dll").NewProc("CompareStringOrdinal")

// sameName compares two key or value names with the operating system's
// uppercase table, which the registry uses for names. Go's case functions do
// not match it: strings.ToLower merges İ and i, and strings.EqualFold merges
// the Kelvin sign and k, but the registry keeps both pairs apart.
func sameName(a, b string) bool {
	if a == b {
		return true
	}
	first, second := utf16.Encode([]rune(a)), utf16.Encode([]rune(b))
	// The comparison maps each UTF-16 unit to one unit, so names of different
	// lengths always differ. This also keeps both buffers non-empty below.
	if len(first) != len(second) {
		return false
	}
	const ignoreCase, equal = 1, 2 // TRUE and CSTR_EQUAL in the Windows API
	result, _, _ := compareStringOrdinal.Call(
		uintptr(unsafe.Pointer(&first[0])), uintptr(len(first)),
		uintptr(unsafe.Pointer(&second[0])), uintptr(len(second)),
		ignoreCase)
	return result == equal
}
