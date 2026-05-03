// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
)

// aeadSeal performs AES-256-GCM sealing with a freshly generated 12-byte
// nonce from crypto/rand and the supplied AAD. Returns the wire format
// nonce || ciphertext || tag (RFC 5116 layout, §3).
//
// This single helper is the only place in the package that calls
// cipher.NewGCM().Seal so that the chunk and manifest paths cannot drift.
func aeadSeal(key, plaintext, aad []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, NonceSize)
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}

	// gcm.Seal appends ciphertext+tag to its first arg. We pre-allocate so
	// the resulting slice is exactly nonce || ciphertext || tag.
	out := make([]byte, NonceSize, NonceSize+len(plaintext)+GCMTagSize)
	copy(out, nonce)
	return gcm.Seal(out, nonce, plaintext, aad), nil
}

// aeadOpen reverses aeadSeal. blob is nonce || ciphertext || tag. On
// authentication failure returns ErrAEADAuthFailed (errors from cipher.GCM
// are intentionally indistinguishable; we surface a single sentinel).
func aeadOpen(key, blob, aad []byte) ([]byte, error) {
	if len(blob) < NonceSize+GCMTagSize {
		return nil, ErrCiphertextTooShort
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonce := blob[:NonceSize]
	ct := blob[NonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ct, aad)
	if err != nil {
		return nil, ErrAEADAuthFailed
	}
	return plaintext, nil
}

// indexAAD encodes a uint32 chunk index (or the 0xFFFFFFFF manifest
// sentinel) as 4 big-endian bytes for use as AEAD AAD (contract §3).
func indexAAD(idx uint32) []byte {
	var buf [4]byte
	binary.BigEndian.PutUint32(buf[:], idx)
	return buf[:]
}
