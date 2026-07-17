// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package safepath

import "golang.org/x/sys/windows"

func atomicReplace(source, destination string) error {
	return windows.Rename(source, destination)
}
