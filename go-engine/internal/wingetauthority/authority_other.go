// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package wingetauthority

func bindStrict(string, [32]byte) (func(), error) {
	return nil, errInvalidAuthority
}
