//go:build windows

// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package arp

import (
	"runtime"

	"golang.org/x/sys/windows/registry"
)

const uninstallPath = `SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall`

// Read returns the visible Add/Remove Programs inventory across the machine
// 64-bit, machine 32-bit, and current-user uninstall hives.
func Read() []Entry {
	return readHives([]hive{
		{root: registryKey{registry.LOCAL_MACHINE}, path: uninstallPath, scope: "Machine\\" + nativeArchitecture()},
		{root: registryKey{registry.LOCAL_MACHINE}, path: `SOFTWARE\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall`, scope: "Machine\\X86"},
		{root: registryKey{registry.CURRENT_USER}, path: uninstallPath, scope: "User\\" + nativeArchitecture()},
	})
}

type hive struct {
	root  registryValueKey
	path  string
	scope string
}

func nativeArchitecture() string {
	return architectureForGOARCH(runtime.GOARCH)
}

func architectureForGOARCH(goarch string) string {
	switch goarch {
	case "386":
		return "X86"
	case "arm64":
		return "ARM64"
	default:
		return "X64"
	}
}

type registryValueKey interface {
	ReadSubKeyNames(int) ([]string, error)
	OpenKey(string) (registryValueKey, error)
	GetStringValue(string) (string, uint32, error)
	GetIntegerValue(string) (uint64, uint32, error)
	Close() error
}

type registryKey struct{ registry.Key }

func (key registryKey) OpenKey(path string) (registryValueKey, error) {
	child, err := registry.OpenKey(key.Key, path, registry.READ)
	if err != nil {
		return nil, err
	}
	return registryKey{child}, nil
}

func readHives(hives []hive) []Entry {
	var entries []Entry
	for _, hive := range hives {
		root, err := hive.root.OpenKey(hive.path)
		if err != nil {
			continue
		}
		entries = append(entries, readHive(root, hive.scope)...)
		root.Close()
	}
	return entries
}

func readHive(root registryValueKey, scope string) []Entry {
	names, err := root.ReadSubKeyNames(-1)
	if err != nil {
		return nil
	}
	entries := make([]Entry, 0, len(names))
	for _, name := range names {
		key, err := root.OpenKey(name)
		if err != nil {
			continue
		}
		displayName, _, _ := key.GetStringValue("DisplayName")
		systemComponent, _, _ := key.GetIntegerValue("SystemComponent")
		if displayName != "" && systemComponent != 1 {
			publisher, _, _ := key.GetStringValue("Publisher")
			version, _, _ := key.GetStringValue("DisplayVersion")
			location, _, _ := key.GetStringValue("InstallLocation")
			entries = append(entries, Entry{
				LocalIdentifier: "ARP\\" + scope + "\\" + name,
				Key:             name,
				DisplayName:     displayName,
				DisplayVersion:  version,
				Publisher:       publisher,
				InstallLocation: location,
			})
		}
		key.Close()
	}
	return entries
}
