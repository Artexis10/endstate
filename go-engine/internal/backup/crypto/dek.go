// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package crypto

// GenerateDEK produces a fresh DEKSize-byte data-encryption-key from a
// CSPRNG. Contract §3 ("DEK wrapping").
//
// TODO(prompt-3): implement using crypto/rand.Read.
func GenerateDEK() ([]byte, error) {
	return nil, ErrNotImplemented
}

// WrapDEK encrypts dek with masterKey using AES-256-GCM, returning the
// wire-format wrapped blob: nonce || ciphertext || tag. Contract §3.
//
// TODO(prompt-3): implement using crypto/aes + crypto/cipher.NewGCM.
func WrapDEK(dek []byte, masterKey [MasterKeySize]byte) ([]byte, error) {
	return nil, ErrNotImplemented
}

// UnwrapDEK reverses WrapDEK. Returns ErrNotImplemented in the stub.
//
// TODO(prompt-3): implement and return an error on AEAD authentication
// failure (do NOT mask as nil — the caller must distinguish a bad key from
// a corrupt blob).
func UnwrapDEK(wrapped []byte, masterKey [MasterKeySize]byte) ([]byte, error) {
	return nil, ErrNotImplemented
}
