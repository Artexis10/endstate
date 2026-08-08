// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands_test

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
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

// commitLog records the `POST .../versions/:versionId/commit` calls the
// substrate mock received (contract §7). Shared by the push orchestration
// tests: the durability invariant they assert is "no commit ⇒ the
// generation was never presented as protected", so every one of them needs
// to count commits per version.
type commitLog struct {
	mu    sync.Mutex
	calls []string // versionIds, in arrival order
}

func (c *commitLog) record(versionID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls = append(c.calls, versionID)
}

// count returns how many commits arrived for versionID.
func (c *commitLog) count(versionID string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, v := range c.calls {
		if v == versionID {
			n++
		}
	}
	return n
}

// total returns the number of commit calls across all versions.
func (c *commitLog) total() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.calls)
}

// sha256Hex is the hex SHA-256 of b — the shape substrate returns as
// `manifestSha256` on `GET /api/backups/:id/versions`.
func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
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
