// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build !windows && !linux

package validationaudit

func readSafePatch(_, _, _ string) ([]byte, error) {
	return nil, ErrUnsafePatchPath
}
