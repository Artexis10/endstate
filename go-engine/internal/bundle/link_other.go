// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package bundle

import "os"

func isLinkOrReparse(info os.FileInfo) bool {
	return info.Mode()&os.ModeSymlink != 0
}
