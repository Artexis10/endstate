// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

//go:build darwin

package keychain

import (
	"errors"

	"github.com/zalando/go-keyring"
)

// serviceName is the single label every Endstate secret is filed under in
// the platform secret store. It mirrors the Windows backend's flat target
// naming: there the account string (from AccountForUser / AccountForDEK) is
// the credential target directly; here go-keyring needs a (service, user)
// tuple, so the account string becomes the per-entry user and serviceName
// groups them under one human-readable heading in Keychain Access /
// Secret Service browsers.
const serviceName = "Endstate"

// NewSystem returns the platform-native Keychain — the macOS login
// keychain via github.com/zalando/go-keyring, which shells out to
// /usr/bin/security (no cgo).
//
// The contract requires no fallback to plaintext file storage: if the
// keychain is unavailable the caller surfaces the error rather than
// silently degrading (contract §1, §5).
func NewSystem() Keychain {
	return &keyringKeychain{}
}

// keyringKeychain adapts go-keyring's package-level Set/Get/Delete to the
// narrow Keychain interface. go-keyring is stateless (it holds no handles),
// so a zero-value struct is safe for concurrent use by multiple goroutines.
type keyringKeychain struct{}

func (*keyringKeychain) Store(account string, secret []byte) error {
	if err := keyring.Set(serviceName, account, string(secret)); err != nil {
		return mapKeyringErr("store", err)
	}
	return nil
}

func (*keyringKeychain) Load(account string) ([]byte, error) {
	v, err := keyring.Get(serviceName, account)
	if err != nil {
		return nil, mapKeyringErr("load", err)
	}
	return []byte(v), nil
}

func (*keyringKeychain) Delete(account string) error {
	if err := keyring.Delete(serviceName, account); err != nil {
		return mapKeyringErr("delete", err)
	}
	return nil
}

// mapKeyringErr translates go-keyring's not-found sentinel to the package's
// ErrNotFound and wraps every other error with a "keychain:" prefix
// consistent with the existing backends.
func mapKeyringErr(op string, err error) error {
	if errors.Is(err, keyring.ErrNotFound) {
		return ErrNotFound
	}
	return errors.New("keychain: " + op + ": " + err.Error())
}
