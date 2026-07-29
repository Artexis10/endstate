// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build darwin

package validationaudit

import (
	"os"
	"strings"
	"syscall"
	"unsafe"
)

const darwinOpenatSysno = 463

func readSafePatch(root, declared, _ string) ([]byte, error) {
	rootFD, err := syscall.Open(root, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, ErrUnsafePatchPath
	}
	files := []*os.File{os.NewFile(uintptr(rootFD), root)}
	defer func() {
		for index := len(files) - 1; index >= 0; index-- {
			files[index].Close()
		}
	}()
	rootInfo, err := files[0].Stat()
	if err != nil || !rootInfo.IsDir() {
		return nil, ErrUnsafePatchPath
	}
	var rootMount syscall.Statfs_t
	if err := syscall.Fstatfs(rootFD, &rootMount); err != nil {
		return nil, ErrUnsafePatchPath
	}

	components := splitRepositoryPath(declared)
	for index, component := range components {
		leaf := index == len(components)-1
		flags := syscall.O_RDONLY | syscall.O_CLOEXEC | syscall.O_NOFOLLOW
		if !leaf {
			flags |= syscall.O_DIRECTORY
		}
		fd, openErr := darwinOpenat(int(files[len(files)-1].Fd()), component, flags, 0)
		if openErr != nil {
			return nil, ErrUnsafePatchPath
		}
		file := os.NewFile(uintptr(fd), component)
		files = append(files, file)
		info, statErr := file.Stat()
		if statErr != nil || (!leaf && !info.IsDir()) || (leaf && !info.Mode().IsRegular()) {
			return nil, ErrUnsafePatchPath
		}
		var currentMount syscall.Statfs_t
		if err := syscall.Fstatfs(fd, &currentMount); err != nil || !sameDarwinMount(rootMount, currentMount) {
			return nil, ErrUnsafePatchPath
		}
		if leaf {
			return readBoundPatch(file)
		}
	}
	return nil, ErrUnsafePatchPath
}

func darwinOpenat(directoryFD int, path string, flags int, mode uint32) (int, error) {
	pathBytes := append([]byte(path), 0)
	result, _, errno := syscall.Syscall6(darwinOpenatSysno, uintptr(directoryFD), uintptr(unsafe.Pointer(&pathBytes[0])), uintptr(flags), uintptr(mode), 0, 0)
	if errno != 0 {
		return 0, errno
	}
	return int(result), nil
}

func splitRepositoryPath(path string) []string {
	return strings.Split(path, "/")
}

func sameDarwinMount(left, right syscall.Statfs_t) bool {
	return left.Fsid == right.Fsid && left.Mntonname == right.Mntonname && left.Mntfromname == right.Mntfromname
}
