// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package bundle

import "os"

func replaceFileAtomically(temporary, destination string) error {
	return os.Rename(temporary, destination)
}
