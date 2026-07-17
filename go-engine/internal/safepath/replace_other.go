// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package safepath

import "os"

func atomicReplace(source, destination string) error {
	return os.Rename(source, destination)
}
