// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build linux

package validationaudit

import (
	"os"
	"syscall"
	"unsafe"
)

const (
	linuxOpenat2Sysno = 437
	resolveNoXDev     = 0x01
	resolveNoMagic    = 0x02
	resolveNoSymlinks = 0x04
	resolveBeneath    = 0x08
)

type linuxOpenHow struct {
	flags   uint64
	mode    uint64
	resolve uint64
}

func readSafePatch(root, declared, _ string) ([]byte, error) {
	rootFD, err := syscall.Open(root, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, ErrUnsafePatchPath
	}
	rootFile := os.NewFile(uintptr(rootFD), root)
	defer rootFile.Close()
	patchFD, err := linuxOpenat2(rootFD, declared, syscall.O_RDONLY|syscall.O_CLOEXEC, resolveBeneath|resolveNoSymlinks|resolveNoMagic|resolveNoXDev)
	if err != nil {
		return nil, ErrUnsafePatchPath
	}
	patchFile := os.NewFile(uintptr(patchFD), declared)
	defer patchFile.Close()
	return readBoundPatch(patchFile)
}

func linuxOpenat2(directoryFD int, path string, flags, resolve uint64) (int, error) {
	pathBytes := append([]byte(path), 0)
	how := linuxOpenHow{flags: flags, resolve: resolve}
	result, _, errno := syscall.Syscall6(linuxOpenat2Sysno, uintptr(directoryFD), uintptr(unsafe.Pointer(&pathBytes[0])), uintptr(unsafe.Pointer(&how)), unsafe.Sizeof(how), 0, 0)
	if errno != 0 {
		return 0, errno
	}
	return int(result), nil
}
