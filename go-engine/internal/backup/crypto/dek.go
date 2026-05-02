// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package crypto

import "crypto/rand"

// wrappedDEKSize is the locked wire size of a wrapped DEK:
// NonceSize (12) + DEKSize (32) + GCMTagSize (16) = 60 bytes.
const wrappedDEKSize = NonceSize + DEKSize + GCMTagSize

// GenerateDEK produces a fresh DEKSize-byte data-encryption-key from a
// CSPRNG (crypto/rand). Contract §3 ("DEK wrapping").
func GenerateDEK() ([]byte, error) {
	dek := make([]byte, DEKSize)
	if _, err := rand.Read(dek); err != nil {
		return nil, err
	}
	return dek, nil
}

// WrapDEK encrypts dek with masterKey using AES-256-GCM, returning the
// wire-format wrapped blob: nonce || ciphertext || tag (60 bytes total).
// No AAD — the wrapped DEK is bound by being inside the manifest, which
// has its own AAD sentinel (contract §3).
func WrapDEK(dek []byte, masterKey [MasterKeySize]byte) ([]byte, error) {
	if len(dek) != DEKSize {
		return nil, ErrInvalidDEKLength
	}
	return aeadSeal(masterKey[:], dek, nil)
}

// UnwrapDEK reverses WrapDEK. Returns ErrAEADAuthFailed on AEAD
// authentication failure (caller cannot distinguish "wrong masterKey"
// from "tampered ciphertext"; both are errors).
func UnwrapDEK(wrapped []byte, masterKey [MasterKeySize]byte) ([]byte, error) {
	if len(wrapped) != wrappedDEKSize {
		return nil, ErrInvalidWrappedDEKLength
	}
	return aeadOpen(masterKey[:], wrapped, nil)
}
