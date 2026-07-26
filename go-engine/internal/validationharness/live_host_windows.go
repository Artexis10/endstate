// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package validationharness

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"golang.org/x/sys/windows/registry"
)

const (
	liveUninstallKey       = `SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall`
	liveMachineEnvironment = `SYSTEM\CurrentControlSet\Control\Session Manager\Environment`
	liveUserEnvironment    = `Environment`
	maxLiveObserverRecords = 512
)

// LiveVersionSource isolates native file-version extraction. Callers may use a
// reviewed Windows implementation without making observer tests host-dependent.
type LiveVersionSource interface {
	FileVersion(string) (string, error)
}

// NewWindowsLiveObserver provides the read-only Windows host adapters. A
// version source is injected because version-resource policy is separate from
// registry, process, and file observation.
func NewWindowsLiveObserver(versions LiveVersionSource) (LiveObserver, error) {
	if versions == nil {
		return LiveObserver{}, fmt.Errorf("live observer requires a file version source")
	}
	return LiveObserver{
		Process:  windowsLiveProcess{},
		Registry: windowsLiveRegistry{},
		Path:     windowsLivePath{},
		Files:    windowsLiveFiles{versions: versions},
	}, nil
}

type windowsLiveProcess struct{}

func (windowsLiveProcess) Run(ctx context.Context, name string, args ...string) (LiveProcessResult, error) {
	command := exec.CommandContext(ctx, name, args...)
	output, err := command.Output()
	result := LiveProcessResult{ExitCode: 0, Stdout: output, Classification: LiveProcessCompleted}
	if err == nil {
		return result, nil
	}
	if exitError, ok := err.(*exec.ExitError); ok {
		result.ExitCode = exitError.ExitCode()
		result.Classification = ""
		return result, nil
	}
	return LiveProcessResult{}, err
}

type windowsLiveRegistry struct{}

func (windowsLiveRegistry) UninstallRecords(context.Context) ([]LiveUninstallRecord, error) {
	views := []struct {
		view   LiveRegistryView
		hive   registry.Key
		access uint32
	}{
		{LiveRegistryHKLM64, registry.LOCAL_MACHINE, registry.READ | registry.WOW64_64KEY},
		{LiveRegistryHKLM32, registry.LOCAL_MACHINE, registry.READ | registry.WOW64_32KEY},
		{LiveRegistryHKCU, registry.CURRENT_USER, registry.READ},
	}
	var records []LiveUninstallRecord
	for _, view := range views {
		entries, err := readWindowsLiveUninstallView(view.hive, view.access, view.view)
		if err != nil {
			return nil, err
		}
		records = append(records, entries...)
		if len(records) > maxLiveObserverRecords {
			return nil, fmt.Errorf("live uninstall records exceed bound")
		}
	}
	return records, nil
}

func readWindowsLiveUninstallView(hive registry.Key, access uint32, view LiveRegistryView) ([]LiveUninstallRecord, error) {
	root, err := registry.OpenKey(hive, liveUninstallKey, access)
	if err == registry.ErrNotExist {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer root.Close()
	info, err := root.Stat()
	if err != nil {
		return nil, err
	}
	if info.SubKeyCount > maxLiveObserverRecords {
		return nil, fmt.Errorf("live uninstall records exceed bound")
	}
	names, err := root.ReadSubKeyNames(int(info.SubKeyCount))
	if err != nil && err != io.EOF {
		return nil, err
	}
	records := make([]LiveUninstallRecord, 0, len(names))
	for _, name := range names {
		if !validLiveObserverValue(name) {
			return nil, fmt.Errorf("live uninstall key name is unsafe")
		}
		key, openErr := registry.OpenKey(root, name, access)
		if openErr != nil {
			return nil, openErr
		}
		record, readErr := readWindowsLiveUninstallRecord(key, view)
		key.Close()
		if readErr != nil {
			return nil, readErr
		}
		records = append(records, record)
	}
	return records, nil
}

func readWindowsLiveUninstallRecord(key registry.Key, view LiveRegistryView) (LiveUninstallRecord, error) {
	read := func(name string) (string, error) {
		value, _, err := key.GetStringValue(name)
		if err == registry.ErrNotExist {
			return "", nil
		}
		if err != nil {
			return "", err
		}
		if value != "" && !validLiveObserverValue(value) {
			return "", fmt.Errorf("live uninstall value is unsafe")
		}
		return value, nil
	}
	name, err := read("DisplayName")
	if err != nil {
		return LiveUninstallRecord{}, err
	}
	version, err := read("DisplayVersion")
	if err != nil {
		return LiveUninstallRecord{}, err
	}
	location, err := read("InstallLocation")
	if err != nil {
		return LiveUninstallRecord{}, err
	}
	icon, err := read("DisplayIcon")
	if err != nil {
		return LiveUninstallRecord{}, err
	}
	return LiveUninstallRecord{View: view, DisplayName: name, DisplayVersion: version, InstallLocation: location, DisplayIcon: icon}, nil
}

type windowsLivePath struct{}

func (windowsLivePath) MachineAndUserPath(context.Context) ([]string, error) {
	machine, err := readWindowsLivePath(registry.LOCAL_MACHINE, liveMachineEnvironment, registry.READ|registry.WOW64_64KEY)
	if err != nil {
		return nil, err
	}
	user, err := readWindowsLivePath(registry.CURRENT_USER, liveUserEnvironment, registry.READ)
	if err != nil {
		return nil, err
	}
	values := append(machine, user...)
	if len(values) > maxLiveObserverRecords {
		return nil, fmt.Errorf("live PATH entries exceed bound")
	}
	return values, nil
}

func readWindowsLivePath(hive registry.Key, location string, access uint32) ([]string, error) {
	key, err := registry.OpenKey(hive, location, access)
	if err == registry.ErrNotExist {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer key.Close()
	value, valueType, err := key.GetStringValue("Path")
	if err == registry.ErrNotExist || value == "" {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if valueType == registry.EXPAND_SZ {
		value, err = registry.ExpandString(value)
		if err != nil {
			return nil, err
		}
	}
	if len(value) > maxLiveObserverOutputBytes || !validLiveObserverText(value) {
		return nil, fmt.Errorf("live PATH is unsafe")
	}
	entries := strings.Split(value, ";")
	if len(entries) > maxLiveObserverRecords {
		return nil, fmt.Errorf("live PATH entries exceed bound")
	}
	return entries, nil
}

type windowsLiveFiles struct{ versions LiveVersionSource }

func (files windowsLiveFiles) Stat(path string) (LiveFileInfo, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return LiveFileInfo{}, err
	}
	reparse := info.Mode()&os.ModeSymlink != 0
	if data, ok := info.Sys().(*syscall.Win32FileAttributeData); ok {
		reparse = reparse || data.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0
	}
	return LiveFileInfo{Regular: info.Mode().IsRegular(), ReparsePoint: reparse}, nil
}

func (files windowsLiveFiles) FileVersion(path string) (string, error) {
	return files.versions.FileVersion(path)
}
