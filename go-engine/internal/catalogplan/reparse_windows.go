//go:build windows

// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package catalogplan

import "syscall"

func isReparsePoint(path string) bool {
	pointer, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return false
	}
	attributes, err := syscall.GetFileAttributes(pointer)
	return err == nil && attributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0
}
