// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package crypto

import (
	"crypto/rand"

	"github.com/tyler-smith/go-bip39"
	"golang.org/x/crypto/argon2"
)

// recoveryKeyEntropySize is the locked entropy size for a recovery key
// (contract §6). 32 bytes → 256 bits → 24-word BIP39 mnemonic.
const recoveryKeyEntropySize = 32

// recoveryDerivedSize is the locked output size of the recovery KDF —
// 32 bytes, single-purpose (unlike the passphrase KDF which produces 64
// bytes split 32/32). Contract §6.
const recoveryDerivedSize = 32

// GenerateRecoveryKey produces a fresh 32-byte CSPRNG value encoded as a
// 24-word BIP39 mnemonic. Returns both the bytes (for downstream KDF
// derivation) and the phrase (for the user to record). Contract §6.
func GenerateRecoveryKey() (RecoveryKey, error) {
	entropy := make([]byte, recoveryKeyEntropySize)
	if _, err := rand.Read(entropy); err != nil {
		return RecoveryKey{}, err
	}

	phrase, err := bip39.NewMnemonic(entropy)
	if err != nil {
		return RecoveryKey{}, err
	}

	var rk RecoveryKey
	copy(rk.Bytes[:], entropy)
	rk.Phrase = phrase
	return rk, nil
}

// ParseRecoveryPhrase decodes a BIP39 mnemonic of RecoveryMnemonicWords
// words back to its 32-byte raw key. The phrase is normalised by the
// BIP39 library (whitespace and case handling per the spec) and the
// checksum is validated; phrases with a bad checksum return an error.
func ParseRecoveryPhrase(phrase string) ([32]byte, error) {
	// MnemonicToByteArray with checksum validation; the second arg true
	// trims the appended checksum byte from the returned slice for 24-
	// word mnemonics (256 bits of entropy + 8-bit checksum → 33 bytes
	// otherwise).
	entropy, err := bip39.MnemonicToByteArray(phrase, true)
	if err != nil {
		return [32]byte{}, err
	}
	if len(entropy) != recoveryKeyEntropySize {
		return [32]byte{}, bip39.ErrInvalidMnemonic
	}
	var out [32]byte
	copy(out[:], entropy)
	return out, nil
}

// DeriveRecoveryKey runs Argon2id over the recovery key bytes using the
// supplied salt and parameters, producing the 32-byte recoveryKey used
// to wrap the DEK on the second unlock path (contract §6).
func DeriveRecoveryKey(rk [32]byte, salt []byte, params KDFParams) ([32]byte, error) {
	if !params.MeetsFloor() {
		return [32]byte{}, ErrKDFParamsBelowFloor
	}
	if len(salt) < SaltSize {
		return [32]byte{}, ErrSaltTooShort
	}

	out := argon2.IDKey(rk[:], salt, params.Iterations, params.Memory, params.Parallelism, recoveryDerivedSize)
	defer zeroBytes(out)

	var derived [32]byte
	copy(derived[:], out)
	return derived, nil
}

// RecoveryKeyVerifier produces Argon2id(recoveryKey, salt) — the value
// the server stores so it can confirm a recovery attempt without ever
// seeing the recovery key itself (contract §6).
//
// The verifier is sent to the server during signup and during recovery;
// it is never compared on the client. Server-side comparison should use
// a constant-time equality check.
func RecoveryKeyVerifier(recoveryKey [32]byte, salt []byte, params KDFParams) ([]byte, error) {
	if !params.MeetsFloor() {
		return nil, ErrKDFParamsBelowFloor
	}
	if len(salt) < SaltSize {
		return nil, ErrSaltTooShort
	}

	return argon2.IDKey(recoveryKey[:], salt, params.Iterations, params.Memory, params.Parallelism, recoveryDerivedSize), nil
}
