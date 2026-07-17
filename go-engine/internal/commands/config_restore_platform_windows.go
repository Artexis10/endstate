// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package commands

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"syscall"
	"unsafe"

	"github.com/Artexis10/endstate/go-engine/internal/configrestore"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

type windowsConfigRestoreRegistry struct{}
type windowsConfigRestoreProcessObserver struct{}

var configRestoreRegSetValueEx = windows.NewLazySystemDLL("advapi32.dll").NewProc("RegSetValueExW")

func newConfigRestorePlatformAdapters() (configrestore.RegistryMutator, configrestore.ProcessObserver) {
	return windowsConfigRestoreRegistry{}, windowsConfigRestoreProcessObserver{}
}

func (windowsConfigRestoreRegistry) ReadValue(
	ctx context.Context,
	key string,
	valueName string,
) (configrestore.RegistryReadResult, error) {
	if err := ctx.Err(); err != nil {
		return configrestore.RegistryReadResult{}, err
	}
	subkey, err := configRestoreHKCUSubkey(key)
	if err != nil {
		return configrestore.RegistryReadResult{}, err
	}
	handle, err := registry.OpenKey(registry.CURRENT_USER, subkey, registry.QUERY_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return configrestore.RegistryReadResult{}, nil
	}
	if err != nil {
		return configrestore.RegistryReadResult{}, err
	}
	defer handle.Close()
	size, valueType, err := handle.GetValue(valueName, nil)
	if errors.Is(err, registry.ErrNotExist) {
		return configrestore.RegistryReadResult{}, nil
	}
	if err != nil {
		return configrestore.RegistryReadResult{}, err
	}
	data := make([]byte, size)
	if size > 0 {
		read, actualType, readErr := handle.GetValue(valueName, data)
		if readErr != nil {
			return configrestore.RegistryReadResult{}, readErr
		}
		data, valueType = data[:read], actualType
	}
	return configrestore.RegistryReadResult{Exists: true, ValueType: valueType, Data: data}, nil
}

func (windowsConfigRestoreRegistry) SetValue(
	ctx context.Context,
	key string,
	valueName string,
	valueType uint32,
	data []byte,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	subkey, err := configRestoreHKCUSubkey(key)
	if err != nil {
		return err
	}
	handle, _, err := registry.CreateKey(registry.CURRENT_USER, subkey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer handle.Close()
	name, err := syscall.UTF16PtrFromString(valueName)
	if err != nil {
		return err
	}
	var dataPointer *byte
	if len(data) > 0 {
		dataPointer = &data[0]
	}
	result, _, callErr := configRestoreRegSetValueEx.Call(
		uintptr(handle), uintptr(unsafe.Pointer(name)), 0, uintptr(valueType),
		uintptr(unsafe.Pointer(dataPointer)), uintptr(len(data)),
	)
	if result != 0 {
		if callErr != nil && callErr != syscall.Errno(0) {
			return callErr
		}
		return syscall.Errno(result)
	}
	return nil
}

func (windowsConfigRestoreRegistry) DeleteValue(ctx context.Context, key string, valueName string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	subkey, err := configRestoreHKCUSubkey(key)
	if err != nil {
		return err
	}
	handle, err := registry.OpenKey(registry.CURRENT_USER, subkey, registry.SET_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer handle.Close()
	err = handle.DeleteValue(valueName)
	if errors.Is(err, registry.ErrNotExist) {
		return nil
	}
	return err
}

func configRestoreHKCUSubkey(key string) (string, error) {
	normalized := strings.ReplaceAll(key, "/", `\`)
	if !strings.HasPrefix(strings.ToUpper(normalized), `HKCU\`) {
		return "", fmt.Errorf("registry configuration restore only supports HKCU subkeys")
	}
	subkey := normalized[len(`HKCU\`):]
	if subkey == "" || strings.ContainsRune(subkey, '\x00') {
		return "", fmt.Errorf("registry configuration restore requires a clean HKCU subkey")
	}
	return subkey, nil
}

func (windowsConfigRestoreProcessObserver) RunningProcessBasenames(ctx context.Context) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, err
	}
	defer windows.CloseHandle(snapshot)
	entry := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	if err := windows.Process32First(snapshot, &entry); err != nil {
		return nil, err
	}
	names := []string{}
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if name := windows.UTF16ToString(entry.ExeFile[:]); name != "" {
			names = append(names, name)
		}
		err = windows.Process32Next(snapshot, &entry)
		if errors.Is(err, syscall.ERROR_NO_MORE_FILES) {
			break
		}
		if err != nil {
			return nil, err
		}
	}
	sort.Strings(names)
	return names, nil
}
