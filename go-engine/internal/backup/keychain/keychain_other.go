// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package keychain

import "errors"

// NewSystem on non-Windows platforms returns a Keychain that always errors.
// Endstate is Windows-first; the engine refuses to store the refresh token
// in plaintext fallback storage (contract §1, contract §5).
func NewSystem() Keychain {
	return &unsupportedKeychain{}
}

type unsupportedKeychain struct{}

func (*unsupportedKeychain) Store(account string, secret []byte) error {
	return errors.New("keychain: platform not supported (Windows only)")
}

func (*unsupportedKeychain) Load(account string) ([]byte, error) {
	return nil, errors.New("keychain: platform not supported (Windows only)")
}

func (*unsupportedKeychain) Delete(account string) error {
	return errors.New("keychain: platform not supported (Windows only)")
}
