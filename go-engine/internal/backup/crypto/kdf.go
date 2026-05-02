// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package crypto

// DeriveKeys runs Argon2id over the user's passphrase using the given salt
// and parameters, producing 64 bytes split into ServerPassword (first 32) +
// MasterKey (last 32). Contract §1, §2.
//
// The caller MUST verify params.MeetsFloor() returns true before calling.
//
// TODO(prompt-3): implement using golang.org/x/crypto/argon2.IDKey.
func DeriveKeys(passphrase string, salt []byte, params KDFParams) (DerivedKeys, error) {
	return DerivedKeys{}, ErrNotImplemented
}
