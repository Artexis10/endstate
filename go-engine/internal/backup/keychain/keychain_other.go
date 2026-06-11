// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

//go:build !windows && !darwin && !linux

package keychain

import "errors"

// NewSystem on platforms without a supported native secret store returns a
// Keychain that always errors. Windows (Credential Manager), macOS
// (Keychain), and Linux (Secret Service) each have a real backend; every
// other platform fails closed. The engine refuses to store the refresh
// token or DEK in plaintext fallback storage (contract §1, contract §5).
func NewSystem() Keychain {
	return &unsupportedKeychain{}
}

type unsupportedKeychain struct{}

func (*unsupportedKeychain) Store(account string, secret []byte) error {
	return errUnsupported()
}

func (*unsupportedKeychain) Load(account string) ([]byte, error) {
	return nil, errUnsupported()
}

func (*unsupportedKeychain) Delete(account string) error {
	return errUnsupported()
}

func errUnsupported() error {
	return errors.New("keychain: platform not supported " +
		"(supported: Windows Credential Manager, macOS Keychain, Linux Secret Service)")
}
