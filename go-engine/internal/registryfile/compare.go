// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package registryfile

// SameKey reports whether two registry keys name the same key: their
// CanonicalKey spellings compare as the registry compares names.
func SameKey(a, b string) bool {
	return sameName(CanonicalKey(a), CanonicalKey(b))
}

// SameValueName reports whether two value names name the same value of one
// key, compared as the registry compares names.
func SameValueName(a, b string) bool {
	return sameName(a, b)
}
