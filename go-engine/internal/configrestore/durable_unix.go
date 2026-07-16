// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package configrestore

import (
	"fmt"

	"golang.org/x/sys/unix"
)

func syncDurableFile(path string) error {
	return syncUnixPath(path, false)
}

func syncDurableDirectory(path string) error {
	return syncUnixPath(path, true)
}

func syncUnixPath(path string, wantDirectory bool) error {
	flags := unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW
	if wantDirectory {
		flags |= unix.O_DIRECTORY
	}
	descriptor, err := unix.Open(path, flags, 0)
	if err != nil {
		return err
	}
	defer unix.Close(descriptor)
	var stat unix.Stat_t
	if err := unix.Fstat(descriptor, &stat); err != nil {
		return err
	}
	kind := stat.Mode & unix.S_IFMT
	if (wantDirectory && kind != unix.S_IFDIR) || (!wantDirectory && kind != unix.S_IFREG) {
		return fmt.Errorf("durability path %q has unexpected file type", path)
	}
	return unix.Fsync(descriptor)
}
