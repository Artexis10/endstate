// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package restore

import (
	"golang.org/x/sys/windows/registry"

	"github.com/Artexis10/endstate/go-engine/internal/validationmode"
)

func describeRegistryTargetExists(action RestoreAction) bool {
	hive, subkey, err := splitHKCUKey(action.Key)
	if action.Type == "registry-import" {
		hive, subkey, err = splitHKCUKey(action.Target)
	}
	if err != nil {
		return false
	}
	if action.Type == "registry-set" {
		existed, _, _, _ := readRegistryValue(hive, subkey, action.ValueName)
		return existed
	}
	key, err := registry.OpenKey(hive, subkey, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	_ = key.Close()
	return true
}

func describeValidationRegistryImportTargetExists(context *validationmode.Context, action RestoreAction) bool {
	semanticKey, err := validationmode.NormalizeHKCU(action.Target)
	if err != nil {
		return false
	}
	mappedKey, err := context.MapHKCU(semanticKey)
	if err != nil {
		return false
	}
	exists, err := registryImportQueryNative(mappedKey)
	return err == nil && exists
}
