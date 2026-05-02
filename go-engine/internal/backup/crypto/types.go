// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package crypto

// Locked sizes and constants from docs/contracts/hosted-backup-contract.md
// sections 2 and 3. These values are part of the wire/protocol contract and
// MUST NOT be changed without a schema bump.
const (
	// DEKSize is the length of a data-encryption-key in bytes (AES-256).
	DEKSize = 32

	// MasterKeySize is the length of the second half of the Argon2id output
	// used to wrap the DEK.
	MasterKeySize = 32

	// ServerPasswordSize is the length of the first half of the Argon2id
	// output sent to the server as the auth password.
	ServerPasswordSize = 32

	// SaltSize is the per-user KDF salt length in bytes (contract §2).
	SaltSize = 16

	// NonceSize is the AES-256-GCM nonce length in bytes (RFC 5116).
	NonceSize = 12

	// GCMTagSize is the AES-256-GCM authentication tag length in bytes
	// (NIST SP 800-38D).
	GCMTagSize = 16

	// ChunkPlainSize is the plaintext chunk size in bytes — 4 MiB except
	// the trailing chunk which may be shorter (contract §3).
	ChunkPlainSize = 4 * 1024 * 1024

	// EnvelopeVersion is the manifest envelope version (contract §3).
	EnvelopeVersion = 1

	// ManifestAAD is the 4-byte big-endian unsigned sentinel value used as
	// AEAD AAD when encrypting/decrypting the manifest. Chosen because no
	// real chunk index will ever take this value, binding the encrypted
	// blob to the "manifest" role per contract §3. Distinct from — and
	// independent of — the chunkIndex=-1 transport-layer flag used in API
	// presigned-URL responses (contract §7).
	ManifestAAD uint32 = 0xFFFFFFFF

	// RecoveryMnemonicWords is the BIP39 word count required for recovery
	// keys (contract §6).
	RecoveryMnemonicWords = 24
)

// KDFParams matches the contract's Argon2id parameter object (§2).
//
// Memory is in KiB (so 65536 means 64 MiB). Iterations and Parallelism are
// the t and p parameters from RFC 9106.
type KDFParams struct {
	Algorithm   string `json:"algorithm"`
	Memory      uint32 `json:"memory"`
	Iterations  uint32 `json:"iterations"`
	Parallelism uint8  `json:"parallelism"`
}

// DefaultKDFParams returns the v1 locked parameter set.
func DefaultKDFParams() KDFParams {
	return KDFParams{
		Algorithm:   "argon2id",
		Memory:      65536,
		Iterations:  3,
		Parallelism: 4,
	}
}

// MeetsFloor reports whether the supplied parameters meet or exceed the v1
// locked floor. The engine refuses to derive keys with weaker parameters
// regardless of what the server advertises (contract §2).
func (p KDFParams) MeetsFloor() bool {
	floor := DefaultKDFParams()
	if p.Algorithm != floor.Algorithm {
		return false
	}
	if p.Memory < floor.Memory {
		return false
	}
	if p.Iterations < floor.Iterations {
		return false
	}
	if p.Parallelism < floor.Parallelism {
		return false
	}
	return true
}

// DerivedKeys is the 64-byte output of Argon2id(passphrase, salt) split into
// the two roles defined in contract §1: ServerPassword (sent to the server)
// and MasterKey (never leaves the device; wraps the DEK).
type DerivedKeys struct {
	ServerPassword [ServerPasswordSize]byte
	MasterKey      [MasterKeySize]byte
}

// RecoveryKey holds a freshly minted recovery key and its BIP39 phrase.
// The phrase is what the user is shown / saves; the bytes are what the
// engine uses for KDF derivation downstream.
type RecoveryKey struct {
	Bytes  [32]byte
	Phrase string
}
