// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationaudit

import (
	"io"
	"os"
)

func readBoundPatch(file *os.File) ([]byte, error) {
	before, err := file.Stat()
	if err != nil || !before.Mode().IsRegular() {
		return nil, ErrUnsafePatchPath
	}
	if before.Size() <= 0 || before.Size() > MaxPatchSize {
		return nil, ErrInvalidPatch
	}
	raw, err := io.ReadAll(io.LimitReader(file, MaxPatchSize+1))
	if err != nil || len(raw) == 0 || len(raw) > MaxPatchSize || int64(len(raw)) != before.Size() {
		return nil, ErrInvalidPatch
	}
	after, err := file.Stat()
	if err != nil || !after.Mode().IsRegular() || !os.SameFile(before, after) || after.Size() != before.Size() || !after.ModTime().Equal(before.ModTime()) {
		return nil, ErrUnsafePatchPath
	}
	return raw, nil
}
