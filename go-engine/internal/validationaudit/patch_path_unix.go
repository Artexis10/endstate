// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build linux || darwin || freebsd || netbsd || openbsd || dragonfly || solaris || aix

package validationaudit

import (
	"os"
	"syscall"
)

func unsafePathInfo(_ string, info os.FileInfo) bool {
	return info.Mode()&os.ModeSymlink != 0
}

func unsafePathTransition(parent, current os.FileInfo) bool {
	parentStat, parentOK := parent.Sys().(*syscall.Stat_t)
	currentStat, currentOK := current.Sys().(*syscall.Stat_t)
	return parentOK && currentOK && current.IsDir() && parentStat.Dev != currentStat.Dev
}
