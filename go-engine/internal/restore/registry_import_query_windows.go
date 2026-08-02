// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package restore

import (
	"errors"

	"golang.org/x/sys/windows/registry"
)

func queryRegistryImportTarget(key string) (bool, error) {
	hive, subkey, err := splitHKCUKey(key)
	if err != nil {
		return false, err
	}
	handle, err := registry.OpenKey(hive, subkey, registry.QUERY_VALUE)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	return true, handle.Close()
}
