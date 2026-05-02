// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package crypto

import "errors"

// ErrKDFParamsBelowFloor is returned when a caller asks the engine to
// derive keys with parameters weaker than the locked v1 floor (contract §2).
// Returned from DeriveKeys, DeriveRecoveryKey, and RecoveryKeyVerifier.
var ErrKDFParamsBelowFloor = errors.New("crypto: KDF parameters below v1 floor")

// ErrSaltTooShort is returned when the supplied salt is shorter than the
// per-user 16-byte minimum (contract §2).
var ErrSaltTooShort = errors.New("crypto: salt shorter than required 16 bytes")

// ErrInvalidDEKLength is returned when a caller passes a slice that is not
// exactly DEKSize (32) bytes to a DEK-shaped operation.
var ErrInvalidDEKLength = errors.New("crypto: DEK must be 32 bytes")

// ErrInvalidMasterKeyLength is returned by the wrapping helpers when a
// caller passes a master key that is not MasterKeySize bytes. The exposed
// API uses a [MasterKeySize]byte array so this is reachable only via the
// internal helpers.
var ErrInvalidMasterKeyLength = errors.New("crypto: master key must be 32 bytes")

// ErrCiphertextTooShort is returned when a wire-format AEAD blob is too
// short to contain a 12-byte nonce and a 16-byte tag.
var ErrCiphertextTooShort = errors.New("crypto: ciphertext shorter than nonce+tag")

// ErrAEADAuthFailed is returned when AES-256-GCM Open fails authentication.
// Callers MUST treat this as both "bad key" and "bad ciphertext" — the two
// are indistinguishable by design.
var ErrAEADAuthFailed = errors.New("crypto: AEAD authentication failed")

// ErrInvalidWrappedDEKLength is returned when a wrapped-DEK blob is not
// exactly 60 bytes (12 nonce + 32 ciphertext + 16 tag).
var ErrInvalidWrappedDEKLength = errors.New("crypto: wrapped DEK must be 60 bytes")

// ErrNotImplemented was returned by every stub before the cryptographic
// implementation landed. Consumers in internal/commands check for this
// sentinel via errors.Is to surface a friendly "not yet implemented"
// message; with real bodies in place it is no longer returned, but the
// variable is retained so those consumer call sites continue to compile
// without an unrelated edit. Pruning the dead consumer branches is a
// follow-up change.
var ErrNotImplemented = errors.New("crypto: not implemented")
