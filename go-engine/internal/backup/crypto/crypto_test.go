// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package crypto_test

import (
	"errors"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/backup/crypto"
)

// TestDefaultKDFParamsLockedV1 confirms the locked v1 parameter set matches
// docs/contracts/hosted-backup-contract.md §2 exactly.
func TestDefaultKDFParamsLockedV1(t *testing.T) {
	got := crypto.DefaultKDFParams()
	if got.Algorithm != "argon2id" {
		t.Errorf("Algorithm = %q, want %q", got.Algorithm, "argon2id")
	}
	if got.Memory != 65536 {
		t.Errorf("Memory = %d, want 65536", got.Memory)
	}
	if got.Iterations != 3 {
		t.Errorf("Iterations = %d, want 3", got.Iterations)
	}
	if got.Parallelism != 4 {
		t.Errorf("Parallelism = %d, want 4", got.Parallelism)
	}
}

// TestKDFParams_MeetsFloor exercises the floor check. The engine refuses
// any parameters weaker than the locked v1 floor regardless of what the
// server advertises (contract §2).
func TestKDFParams_MeetsFloor(t *testing.T) {
	cases := []struct {
		name string
		p    crypto.KDFParams
		want bool
	}{
		{"v1 default", crypto.DefaultKDFParams(), true},
		{"weaker memory", crypto.KDFParams{Algorithm: "argon2id", Memory: 32768, Iterations: 3, Parallelism: 4}, false},
		{"weaker iterations", crypto.KDFParams{Algorithm: "argon2id", Memory: 65536, Iterations: 2, Parallelism: 4}, false},
		{"weaker parallelism", crypto.KDFParams{Algorithm: "argon2id", Memory: 65536, Iterations: 3, Parallelism: 2}, false},
		{"wrong algorithm", crypto.KDFParams{Algorithm: "argon2i", Memory: 65536, Iterations: 3, Parallelism: 4}, false},
		{"stronger memory", crypto.KDFParams{Algorithm: "argon2id", Memory: 131072, Iterations: 3, Parallelism: 4}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.p.MeetsFloor(); got != tc.want {
				t.Errorf("MeetsFloor() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestStubsReturnNotImplemented ensures every operation surface returns
// ErrNotImplemented until PROMPT 3 lands. Locking this in a test prevents
// accidentally shipping a half-stubbed crypto module.
func TestStubsReturnNotImplemented(t *testing.T) {
	dek := make([]byte, crypto.DEKSize)
	salt := make([]byte, crypto.SaltSize)
	masterKey := [crypto.MasterKeySize]byte{}
	rkBytes := [32]byte{}

	if _, err := crypto.DeriveKeys("pw", salt, crypto.DefaultKDFParams()); !errors.Is(err, crypto.ErrNotImplemented) {
		t.Errorf("DeriveKeys: expected ErrNotImplemented, got %v", err)
	}
	if _, err := crypto.GenerateDEK(); !errors.Is(err, crypto.ErrNotImplemented) {
		t.Errorf("GenerateDEK: expected ErrNotImplemented, got %v", err)
	}
	if _, err := crypto.WrapDEK(dek, masterKey); !errors.Is(err, crypto.ErrNotImplemented) {
		t.Errorf("WrapDEK: expected ErrNotImplemented, got %v", err)
	}
	if _, err := crypto.UnwrapDEK(nil, masterKey); !errors.Is(err, crypto.ErrNotImplemented) {
		t.Errorf("UnwrapDEK: expected ErrNotImplemented, got %v", err)
	}
	if _, err := crypto.EncryptChunk(nil, 0, dek); !errors.Is(err, crypto.ErrNotImplemented) {
		t.Errorf("EncryptChunk: expected ErrNotImplemented, got %v", err)
	}
	if _, err := crypto.DecryptChunk(nil, 0, dek); !errors.Is(err, crypto.ErrNotImplemented) {
		t.Errorf("DecryptChunk: expected ErrNotImplemented, got %v", err)
	}
	if _, err := crypto.EncryptManifest(nil, dek); !errors.Is(err, crypto.ErrNotImplemented) {
		t.Errorf("EncryptManifest: expected ErrNotImplemented, got %v", err)
	}
	if _, err := crypto.DecryptManifest(nil, dek); !errors.Is(err, crypto.ErrNotImplemented) {
		t.Errorf("DecryptManifest: expected ErrNotImplemented, got %v", err)
	}
	if _, err := crypto.GenerateRecoveryKey(); !errors.Is(err, crypto.ErrNotImplemented) {
		t.Errorf("GenerateRecoveryKey: expected ErrNotImplemented, got %v", err)
	}
	if _, err := crypto.ParseRecoveryPhrase(""); !errors.Is(err, crypto.ErrNotImplemented) {
		t.Errorf("ParseRecoveryPhrase: expected ErrNotImplemented, got %v", err)
	}
	if _, err := crypto.DeriveRecoveryKey(rkBytes, salt, crypto.DefaultKDFParams()); !errors.Is(err, crypto.ErrNotImplemented) {
		t.Errorf("DeriveRecoveryKey: expected ErrNotImplemented, got %v", err)
	}
	if _, err := crypto.RecoveryKeyVerifier(rkBytes, salt, crypto.DefaultKDFParams()); !errors.Is(err, crypto.ErrNotImplemented) {
		t.Errorf("RecoveryKeyVerifier: expected ErrNotImplemented, got %v", err)
	}
}

// TestManifestAAD_IsSentinel locks the contract value (§3). Changing this
// value would break interoperability with all existing encrypted manifests.
func TestManifestAAD_IsSentinel(t *testing.T) {
	if crypto.ManifestAAD != 0xFFFFFFFF {
		t.Errorf("ManifestAAD = %#x, want 0xFFFFFFFF", crypto.ManifestAAD)
	}
}
