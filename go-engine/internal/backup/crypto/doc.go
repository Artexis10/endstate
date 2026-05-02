// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

// Package crypto implements the cryptographic primitives required by
// Endstate Hosted Backup as locked in
// docs/contracts/hosted-backup-contract.md (sections 1, 2, 3 and 6).
//
// Primitives:
//
//   - Argon2id (RFC 9106) key derivation with the locked v1 parameters
//     (memory 64 MiB, iterations 3, parallelism 4, 64-byte output, 16-byte
//     salt). The 64-byte output is split into the value sent to the
//     server as serverPassword (first 32 bytes) and the masterKey
//     (last 32 bytes) which never leaves the device.
//
//   - AES-256-GCM (NIST SP 800-38D, RFC 5116) for chunk encryption,
//     manifest encryption, and DEK wrapping. Every encryption uses a
//     freshly generated 12-byte nonce from crypto/rand. Chunks bind the
//     4-byte big-endian chunkIndex as AAD; the manifest binds the
//     0xFFFFFFFF sentinel as AAD. DEK wrapping uses no AAD.
//
//   - 32-byte data-encryption-key (DEK) generation from crypto/rand,
//     wrapped under masterKey for storage in the manifest.
//
//   - 24-word BIP39 recovery key generation and parsing
//     (github.com/tyler-smith/go-bip39), with the 32-byte raw key
//     KDF-derived to produce a recovery-flow recoveryKey and a
//     server-side verifier value.
//
// Library boundaries are deliberately narrow: stdlib (crypto/aes,
// crypto/cipher, crypto/rand, encoding/binary), golang.org/x/crypto
// (argon2.IDKey), and github.com/tyler-smith/go-bip39. Nothing else.
//
// AAD sentinels: the 0xFFFFFFFF AAD used for manifest encryption
// (contract §3) is independent of the chunkIndex == -1 wire flag used in
// presigned-URL responses (contract §7). They share a notion of "this
// targets the manifest" but live at different layers and MUST NOT be
// conflated in code or comments.
package crypto
