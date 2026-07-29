// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build !windows && !linux && !darwin && !freebsd && !netbsd && !openbsd && !dragonfly && !solaris && !aix

package validationaudit

import "os"

func unsafePathInfo(_ string, info os.FileInfo) bool {
	return info.Mode()&os.ModeSymlink != 0
}

func unsafePathTransition(_ os.FileInfo, _ os.FileInfo) bool {
	return false
}
