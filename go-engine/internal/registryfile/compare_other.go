// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package registryfile

import "strings"

// sameName stands in for the Windows uppercase table where there is no
// registry: strings.EqualFold agrees with it for ASCII names but merges a few
// names that Windows keeps apart, such as the Kelvin sign and k.
func sameName(a, b string) bool {
	return strings.EqualFold(a, b)
}
