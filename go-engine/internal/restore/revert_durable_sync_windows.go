// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package restore

import (
	"fmt"

	"golang.org/x/sys/windows"
)

func syncDurableLegacyFile(path string) error {
	return syncDurableLegacyWindowsPath(path, false)
}

func syncDurableLegacyDirectory(path string) error {
	return syncDurableLegacyWindowsPath(path, true)
}

func publishDurableLegacyRecordNoReplace(temporary, destination string) error {
	from, err := windows.UTF16PtrFromString(temporary)
	if err != nil {
		return err
	}
	to, err := windows.UTF16PtrFromString(destination)
	if err != nil {
		return err
	}
	return windows.MoveFileEx(from, to, windows.MOVEFILE_WRITE_THROUGH)
}

func syncDurableLegacyWindowsPath(path string, wantDirectory bool) error {
	pointer, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	flags := uint32(windows.FILE_FLAG_OPEN_REPARSE_POINT)
	if wantDirectory {
		flags |= windows.FILE_FLAG_BACKUP_SEMANTICS
	}
	handle, err := windows.CreateFile(
		pointer, windows.GENERIC_READ|windows.GENERIC_WRITE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, flags, 0,
	)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(handle)
	var information windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &information); err != nil {
		return err
	}
	if information.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return fmt.Errorf("durability path %q is a reparse point", path)
	}
	isDirectory := information.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0
	if isDirectory != wantDirectory {
		return fmt.Errorf("durability path %q has unexpected file type", path)
	}
	return windows.FlushFileBuffers(handle)
}
