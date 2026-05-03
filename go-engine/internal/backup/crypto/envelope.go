// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package crypto

// EncryptChunk encrypts plaintext with AES-256-GCM using dek as the key,
// a fresh random nonce, and chunkIndex (4-byte big-endian uint32) as AAD.
// Returns the wire-format blob: nonce || ciphertext || tag (contract §3).
//
// Plaintext chunk size is the caller's responsibility — the orchestrator
// in internal/backup/upload splits the input into ChunkPlainSize-byte
// chunks (with a possibly-shorter trailing chunk).
func EncryptChunk(plaintext []byte, chunkIndex uint32, dek []byte) ([]byte, error) {
	if len(dek) != DEKSize {
		return nil, ErrInvalidDEKLength
	}
	return aeadSeal(dek, plaintext, indexAAD(chunkIndex))
}

// DecryptChunk reverses EncryptChunk. Returns ErrAEADAuthFailed on AEAD
// authentication failure or when blob is malformed (the two error modes
// are intentionally not distinguishable).
func DecryptChunk(blob []byte, chunkIndex uint32, dek []byte) ([]byte, error) {
	if len(dek) != DEKSize {
		return nil, ErrInvalidDEKLength
	}
	return aeadOpen(dek, blob, indexAAD(chunkIndex))
}

// EncryptManifest encrypts the marshalled manifest JSON. Identical wire
// format to EncryptChunk but binds the manifest sentinel AAD
// (ManifestAAD = 0xFFFFFFFF) so a manifest blob cannot be substituted for
// a chunk-index-0 ciphertext or vice versa (contract §3).
//
// The 0xFFFFFFFF sentinel is a cryptographic binding INSIDE the encrypted
// blob and is independent of the chunkIndex == -1 transport-layer flag
// used in API presigned-URL responses (contract §7). The two layers MUST
// NOT be conflated.
func EncryptManifest(manifestJSON []byte, dek []byte) ([]byte, error) {
	if len(dek) != DEKSize {
		return nil, ErrInvalidDEKLength
	}
	return aeadSeal(dek, manifestJSON, indexAAD(ManifestAAD))
}

// DecryptManifest reverses EncryptManifest. Returns ErrAEADAuthFailed on
// AEAD authentication failure.
func DecryptManifest(blob []byte, dek []byte) ([]byte, error) {
	if len(dek) != DEKSize {
		return nil, ErrInvalidDEKLength
	}
	return aeadOpen(dek, blob, indexAAD(ManifestAAD))
}
