// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package validationaudit

import (
	"os"
	"syscall"
)

func unsafePathInfo(path string, info os.FileInfo) bool {
	if info.Mode()&os.ModeSymlink != 0 {
		return true
	}
	attributes, err := syscall.GetFileAttributes(syscall.StringToUTF16Ptr(path))
	return err != nil || attributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0
}

func unsafePathTransition(_ os.FileInfo, _ os.FileInfo) bool {
	return false
}
