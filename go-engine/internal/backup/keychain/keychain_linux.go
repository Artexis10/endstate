// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

//go:build linux

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
// groups them under one collection in the Secret Service (e.g. the GNOME
// Keyring / KWallet "login" collection).
const serviceName = "Endstate"

// NewSystem returns the platform-native Keychain — the freedesktop Secret
// Service (GNOME Keyring, KWallet, …) over D-Bus via
// github.com/zalando/go-keyring (no cgo).
//
// The contract requires no fallback to plaintext file storage: if no
// Secret Service daemon is reachable (headless box, WSL, locked keyring)
// the caller surfaces the error rather than silently degrading
// (contract §1, §5). That fail-closed behaviour is intentional — see
// mapKeyringErr for the actionable hint attached to those failures.
func NewSystem() Keychain {
	return &keyringKeychain{}
}

// keyringKeychain adapts go-keyring's package-level Set/Get/Delete to the
// narrow Keychain interface. go-keyring is stateless (it dials D-Bus per
// call and holds no handles), so a zero-value struct is safe for
// concurrent use by multiple goroutines.
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
//
// On Linux the most common non-not-found failure is "no Secret Service
// daemon reachable" (headless server, WSL, or a locked session keyring).
// That is correct fail-closed behaviour — the engine never falls back to
// plaintext — so the wrapped error carries an actionable hint naming what
// the user must provide. The hint is appended in the keychain layer
// because that is the only place that knows the failure is Secret-Service
// shaped; the command layer's envelope remediation (see
// internal/commands/backup_recover.go) then surfaces it verbatim.
func mapKeyringErr(op string, err error) error {
	if errors.Is(err, keyring.ErrNotFound) {
		return ErrNotFound
	}
	return errors.New("keychain: " + op + ": " + err.Error() +
		" (requires an unlocked OS keyring exposing the Secret Service API, " +
		"e.g. GNOME Keyring or KWallet; headless or WSL sessions may have none running)")
}
