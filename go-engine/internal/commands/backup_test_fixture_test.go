// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands_test

import (
	"encoding/base64"
	"sync"

	"github.com/Artexis10/endstate/go-engine/internal/backup/crypto"
)

// testFixture holds a precomputed Argon2id result so the suite doesn't
// pay the ~1s KDF cost per test. The locked v1 floor (64 MiB, 3 iter, 4
// par) is deliberately heavy in production; for tests we compute it once
// across the whole package via sync.Once.
type testFixture struct {
	Email          string
	Passphrase     string
	Salt           []byte // 16 bytes, deterministic
	SaltB64        string
	Derived        crypto.DerivedKeys
	DEK            []byte // 32 bytes, deterministic
	WrappedDEK     []byte // 60 bytes
	WrappedDEKB64  string
	ServerPassB64  string
}

var (
	fixtureOnce sync.Once
	fixture     *testFixture
)

// loadFixture returns the package-level test fixture, computing it on
// first call. Safe for concurrent test workers.
func loadFixture() *testFixture {
	fixtureOnce.Do(func() {
		f := &testFixture{
			Email:      "user@example.com",
			Passphrase: "secret-pass",
			Salt:       bytes16(0x55), // deterministic 16-byte salt for tests
			DEK:        bytes32(0x42), // deterministic DEK; not real entropy, fine for tests
		}
		f.SaltB64 = base64.StdEncoding.EncodeToString(f.Salt)
		derived, err := crypto.DeriveKeys(f.Passphrase, f.Salt, crypto.DefaultKDFParams())
		if err != nil {
			panic("test fixture: DeriveKeys: " + err.Error())
		}
		f.Derived = derived
		wrapped, werr := crypto.WrapDEK(f.DEK, derived.MasterKey)
		if werr != nil {
			panic("test fixture: WrapDEK: " + werr.Error())
		}
		f.WrappedDEK = wrapped
		f.WrappedDEKB64 = base64.StdEncoding.EncodeToString(wrapped)
		f.ServerPassB64 = base64.StdEncoding.EncodeToString(derived.ServerPassword[:])
		fixture = f
	})
	return fixture
}

func bytes16(b byte) []byte {
	out := make([]byte, 16)
	for i := range out {
		out[i] = b
	}
	return out
}

func bytes32(b byte) []byte {
	out := make([]byte, 32)
	for i := range out {
		out[i] = b
	}
	return out
}
