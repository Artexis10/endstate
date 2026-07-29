// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package validationaudit

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

func readSafePatch(root, declared, patchPath string) ([]byte, error) {
	current := root
	handles := make([]*os.File, 0, len(strings.Split(declared, "/"))+1)
	defer func() {
		for index := len(handles) - 1; index >= 0; index-- {
			handles[index].Close()
		}
	}()

	rootHandle, err := openWindowsSafePath(root, true)
	if err != nil {
		return nil, ErrUnsafePatchPath
	}
	handles = append(handles, rootHandle)
	components := strings.Split(declared, "/")
	for index, component := range components {
		current = filepath.Join(current, component)
		leaf := index == len(components)-1
		file, openErr := openWindowsSafePath(current, !leaf)
		if openErr != nil {
			return nil, ErrUnsafePatchPath
		}
		handles = append(handles, file)
		if leaf {
			if current != patchPath {
				return nil, ErrUnsafePatchPath
			}
			return readBoundPatch(file)
		}
	}
	return nil, ErrUnsafePatchPath
}

func openWindowsSafePath(path string, directory bool) (*os.File, error) {
	shareMode := uint32(syscall.FILE_SHARE_READ)
	if directory {
		shareMode |= syscall.FILE_SHARE_WRITE
	}
	handle, err := syscall.CreateFile(
		syscall.StringToUTF16Ptr(path),
		syscall.GENERIC_READ,
		shareMode,
		nil,
		syscall.OPEN_EXISTING,
		syscall.FILE_FLAG_BACKUP_SEMANTICS|syscall.FILE_FLAG_OPEN_REPARSE_POINT,
		0,
	)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(handle), path)
	var handleInfo syscall.ByHandleFileInformation
	if err := syscall.GetFileInformationByHandle(handle, &handleInfo); err != nil || handleInfo.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		file.Close()
		return nil, ErrUnsafePatchPath
	}
	info, err := file.Stat()
	if err != nil || info.IsDir() != directory || (!directory && !info.Mode().IsRegular()) {
		file.Close()
		return nil, ErrUnsafePatchPath
	}
	return file, nil
}
