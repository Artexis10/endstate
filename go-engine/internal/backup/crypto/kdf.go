// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package crypto

import "golang.org/x/crypto/argon2"

// kdfOutputSize is the locked Argon2id output length: 32 bytes
// serverPassword || 32 bytes masterKey (contract §1, §2).
const kdfOutputSize = ServerPasswordSize + MasterKeySize

// DeriveKeys runs Argon2id over the user's passphrase using the given salt
// and parameters, producing 64 bytes split into ServerPassword (first 32) +
// MasterKey (last 32). Contract §1, §2.
//
// Refuses parameters that fail params.MeetsFloor() (returns
// ErrKDFParamsBelowFloor); refuses salt shorter than SaltSize (returns
// ErrSaltTooShort). The intermediate 64-byte output is zeroed before
// returning.
func DeriveKeys(passphrase string, salt []byte, params KDFParams) (DerivedKeys, error) {
	if !params.MeetsFloor() {
		return DerivedKeys{}, ErrKDFParamsBelowFloor
	}
	if len(salt) < SaltSize {
		return DerivedKeys{}, ErrSaltTooShort
	}

	out := argon2.IDKey([]byte(passphrase), salt, params.Iterations, params.Memory, params.Parallelism, kdfOutputSize)
	defer zeroBytes(out)

	var derived DerivedKeys
	copy(derived.ServerPassword[:], out[:ServerPasswordSize])
	copy(derived.MasterKey[:], out[ServerPasswordSize:])
	return derived, nil
}
