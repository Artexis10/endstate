// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package configrestore

import (
	"os"
	"path/filepath"
)

func replaceTransactionFile(temporary, destination string) error {
	if err := os.Rename(temporary, destination); err != nil {
		return err
	}
	return syncDurableDirectory(filepath.Dir(destination))
}
