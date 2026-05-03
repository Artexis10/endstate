// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

// Package keychain provides a narrow Store/Load/Delete interface around the
// platform-native secret store. Endstate Hosted Backup uses it to persist
// the refresh token between CLI invocations (contract §5).
//
// The default implementation on Windows is the Credential Manager via
// github.com/danieljoos/wincred. Callers that need to test the auth flow
// without touching the real store can use NewMemory().
package keychain

import "errors"

// ErrNotFound is returned by Load and Delete when no entry exists for the
// requested account.
var ErrNotFound = errors.New("keychain: account not found")

// Keychain is the narrow surface every command handler interacts with.
// Implementations MUST be safe for concurrent use by multiple goroutines.
type Keychain interface {
	// Store writes the secret bytes for the given account, overwriting any
	// existing value. Implementations MUST NOT log the secret bytes.
	Store(account string, secret []byte) error

	// Load returns the secret bytes for the account or ErrNotFound if no
	// entry exists.
	Load(account string) ([]byte, error)

	// Delete removes the entry for the account. Returns ErrNotFound if no
	// entry exists. Idempotent at the caller level: callers may treat
	// ErrNotFound as success when intent is "ensure absent".
	Delete(account string) error
}

// AccountForUser returns the canonical account name for the refresh token
// of a given userId. Centralised so command handlers don't drift.
func AccountForUser(userID string) string {
	return "endstate-refresh-" + userID
}

// AccountForDEK returns the canonical account name for the unwrapped data
// encryption key of a given userId. Centralised alongside AccountForUser
// so command handlers and tests share the convention.
//
// The DEK is stored in the same trust boundary as the refresh token (the
// OS user account) so a `backup push` after `backup login` does not need
// to re-prompt for the passphrase. Both entries are cleared on logout
// and on account-delete via SessionStore.Forget.
func AccountForDEK(userID string) string {
	return "endstate-dek-" + userID
}

// AccountForWrappedDEK returns the canonical account name for the
// masterKey-wrapped DEK (60 bytes raw, stored as base64 string). The
// engine populates the manifest's `wrappedDEK` field (contract §3) from
// this cached value rather than re-deriving the masterKey on every
// push. Cached at login / signup / recover-finalize time and cleared
// alongside the DEK on logout and account-delete.
//
// This value is not secret — it is the same wrappedDEK substrate stores
// on the user record, and an attacker who has it cannot unwrap the DEK
// without the masterKey. Storing it in the keychain matches the trust
// boundary of the refresh token and DEK entries.
func AccountForWrappedDEK(userID string) string {
	return "endstate-wdek-" + userID
}
