// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package crypto

// GenerateRecoveryKey produces a fresh 32-byte CSPRNG value encoded as a
// 24-word BIP39 mnemonic. Contract §6.
//
// TODO(prompt-3): implement using a vetted BIP39 library; surface the
// chosen library in the PROMPT 3 plan.
func GenerateRecoveryKey() (RecoveryKey, error) {
	return RecoveryKey{}, ErrNotImplemented
}

// ParseRecoveryPhrase decodes a BIP39 mnemonic of RecoveryMnemonicWords
// words back to its 32-byte raw key. The phrase is normalised (whitespace
// collapsed, case-folded per BIP39) and the checksum is validated.
//
// TODO(prompt-3): implement.
func ParseRecoveryPhrase(phrase string) ([32]byte, error) {
	return [32]byte{}, ErrNotImplemented
}

// DeriveRecoveryKey runs Argon2id over the recovery key bytes using the
// supplied salt and parameters, producing the 32-byte recoveryKey used to
// wrap the DEK on the second unlock path (contract §6).
//
// TODO(prompt-3): implement using golang.org/x/crypto/argon2.IDKey.
func DeriveRecoveryKey(rk [32]byte, salt []byte, params KDFParams) ([32]byte, error) {
	return [32]byte{}, ErrNotImplemented
}

// RecoveryKeyVerifier produces Argon2id(recoveryKey, salt) — the value the
// server stores so it can confirm a recovery attempt without ever seeing
// the recovery key itself (contract §6).
//
// TODO(prompt-3): implement.
func RecoveryKeyVerifier(recoveryKey [32]byte, salt []byte, params KDFParams) ([]byte, error) {
	return nil, ErrNotImplemented
}
