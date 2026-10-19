// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

// Package registryvalue reads one named registry value through the Windows
// registry API, in the form that capture records and registry-set restore
// compares and writes. Capture and restore both read through it, so a
// captured value and a restored value always have the same representation.
package registryvalue

// Value is one named registry value. Type is the REG_* name. Data holds a
// REG_DWORD as a decimal string and a REG_SZ or REG_EXPAND_SZ verbatim,
// unexpanded and with every space and tab kept. Data is empty for any other
// type, because registry-set cannot write it.
type Value struct {
	Type string
	Data string
}
