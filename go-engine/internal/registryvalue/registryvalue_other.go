// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package registryvalue

// Read reports every value as missing: only Windows has a registry.
func Read(key, valueName string) (value Value, found, keyFound bool, err error) {
	return Value{}, false, false, nil
}
