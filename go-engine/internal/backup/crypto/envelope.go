// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package crypto

// EncryptChunk encrypts plaintext with AES-256-GCM using dek as the key,
// a fresh random nonce, and chunkIndex (4-byte big-endian uint32) as AAD.
// Returns the wire-format blob: nonce || ciphertext || tag. Contract §3.
//
// TODO(prompt-3): implement.
func EncryptChunk(plaintext []byte, chunkIndex uint32, dek []byte) ([]byte, error) {
	return nil, ErrNotImplemented
}

// DecryptChunk reverses EncryptChunk. Returns an error on AEAD
// authentication failure or when blob is malformed.
//
// TODO(prompt-3): implement.
func DecryptChunk(blob []byte, chunkIndex uint32, dek []byte) ([]byte, error) {
	return nil, ErrNotImplemented
}

// EncryptManifest encrypts the marshalled manifest JSON. Identical wire
// format to EncryptChunk but binds the manifest sentinel AAD (ManifestAAD,
// 0xFFFFFFFF) so a manifest blob cannot be substituted for a chunk-index-0
// ciphertext or vice versa (contract §3).
//
// Note: ManifestAAD is the cryptographic binding inside the encrypted blob
// and is independent of the chunkIndex=-1 transport-layer flag used in API
// presigned-URL responses (contract §7). Implementations of upload/download
// must NOT conflate them.
//
// TODO(prompt-3): implement.
func EncryptManifest(manifestJSON []byte, dek []byte) ([]byte, error) {
	return nil, ErrNotImplemented
}

// DecryptManifest reverses EncryptManifest.
//
// TODO(prompt-3): implement.
func DecryptManifest(blob []byte, dek []byte) ([]byte, error) {
	return nil, ErrNotImplemented
}
