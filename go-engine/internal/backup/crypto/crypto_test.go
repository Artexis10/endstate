// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package crypto_test

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/backup/crypto"
)

// kdfTestParams returns parameters that meet the v1 floor — required by
// DeriveKeys / DeriveRecoveryKey / RecoveryKeyVerifier — but use the
// floor exactly to keep test wall-clock cost bounded. The Python
// vector cross-check is gated under !testing.Short() so it stays out of
// fast-iteration loops.
func kdfTestParams() crypto.KDFParams {
	return crypto.DefaultKDFParams()
}

// TestDefaultKDFParamsLockedV1 confirms the locked v1 parameter set
// matches docs/contracts/hosted-backup-contract.md §2 exactly.
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

// TestManifestAAD_IsSentinel locks the contract value (§3). Changing this
// value would break interoperability with all existing encrypted manifests.
func TestManifestAAD_IsSentinel(t *testing.T) {
	if crypto.ManifestAAD != 0xFFFFFFFF {
		t.Errorf("ManifestAAD = %#x, want 0xFFFFFFFF", crypto.ManifestAAD)
	}
}

// --- KDF ---

func TestDeriveKeys_RoundtripDeterministic(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping v1-floor KDF run under -short")
	}
	salt := bytes.Repeat([]byte{0xAB}, crypto.SaltSize)
	d1, err := crypto.DeriveKeys("hunter2", salt, kdfTestParams())
	if err != nil {
		t.Fatalf("first DeriveKeys: %v", err)
	}
	d2, err := crypto.DeriveKeys("hunter2", salt, kdfTestParams())
	if err != nil {
		t.Fatalf("second DeriveKeys: %v", err)
	}
	if d1.ServerPassword != d2.ServerPassword {
		t.Error("ServerPassword differs across deterministic runs")
	}
	if d1.MasterKey != d2.MasterKey {
		t.Error("MasterKey differs across deterministic runs")
	}
	if d1.ServerPassword == d1.MasterKey {
		t.Error("ServerPassword and MasterKey unexpectedly equal — split likely wrong")
	}
}

func TestDeriveKeys_RejectsSubFloorParams(t *testing.T) {
	salt := bytes.Repeat([]byte{0x00}, crypto.SaltSize)
	weak := crypto.KDFParams{Algorithm: "argon2id", Memory: 1024, Iterations: 1, Parallelism: 1}
	_, err := crypto.DeriveKeys("pw", salt, weak)
	if !errors.Is(err, crypto.ErrKDFParamsBelowFloor) {
		t.Errorf("err = %v, want ErrKDFParamsBelowFloor", err)
	}
}

func TestDeriveKeys_RejectsShortSalt(t *testing.T) {
	short := bytes.Repeat([]byte{0x00}, crypto.SaltSize-1)
	_, err := crypto.DeriveKeys("pw", short, crypto.DefaultKDFParams())
	if !errors.Is(err, crypto.ErrSaltTooShort) {
		t.Errorf("err = %v, want ErrSaltTooShort", err)
	}
}

// --- DEK ---

func TestGenerateDEK_LengthAndUniqueness(t *testing.T) {
	a, err := crypto.GenerateDEK()
	if err != nil {
		t.Fatalf("first GenerateDEK: %v", err)
	}
	b, err := crypto.GenerateDEK()
	if err != nil {
		t.Fatalf("second GenerateDEK: %v", err)
	}
	if len(a) != crypto.DEKSize {
		t.Errorf("len(a) = %d, want %d", len(a), crypto.DEKSize)
	}
	if bytes.Equal(a, b) {
		t.Error("two GenerateDEK calls returned identical bytes — CSPRNG suspect")
	}
}

func TestWrapUnwrapDEK_Roundtrip(t *testing.T) {
	dek := bytes.Repeat([]byte{0x42}, crypto.DEKSize)
	var mk [crypto.MasterKeySize]byte
	copy(mk[:], bytes.Repeat([]byte{0x99}, crypto.MasterKeySize))

	wrapped, err := crypto.WrapDEK(dek, mk)
	if err != nil {
		t.Fatalf("WrapDEK: %v", err)
	}
	if want := crypto.NonceSize + crypto.DEKSize + crypto.GCMTagSize; len(wrapped) != want {
		t.Errorf("len(wrapped) = %d, want %d", len(wrapped), want)
	}

	got, err := crypto.UnwrapDEK(wrapped, mk)
	if err != nil {
		t.Fatalf("UnwrapDEK: %v", err)
	}
	if !bytes.Equal(got, dek) {
		t.Error("unwrapped DEK does not byte-equal original")
	}
}

func TestWrapDEK_RejectsBadDEKLength(t *testing.T) {
	short := make([]byte, crypto.DEKSize-1)
	var mk [crypto.MasterKeySize]byte
	if _, err := crypto.WrapDEK(short, mk); !errors.Is(err, crypto.ErrInvalidDEKLength) {
		t.Errorf("err = %v, want ErrInvalidDEKLength", err)
	}
}

func TestUnwrapDEK_WrongMasterKeyFails(t *testing.T) {
	dek := bytes.Repeat([]byte{0x42}, crypto.DEKSize)
	var mk [crypto.MasterKeySize]byte
	copy(mk[:], bytes.Repeat([]byte{0x99}, crypto.MasterKeySize))
	wrapped, err := crypto.WrapDEK(dek, mk)
	if err != nil {
		t.Fatalf("WrapDEK: %v", err)
	}

	var wrongMK [crypto.MasterKeySize]byte
	copy(wrongMK[:], bytes.Repeat([]byte{0xAA}, crypto.MasterKeySize))
	if _, err := crypto.UnwrapDEK(wrapped, wrongMK); !errors.Is(err, crypto.ErrAEADAuthFailed) {
		t.Errorf("err = %v, want ErrAEADAuthFailed", err)
	}
}

func TestUnwrapDEK_RejectsBadBlobLength(t *testing.T) {
	short := make([]byte, crypto.NonceSize+crypto.GCMTagSize) // missing the 32-byte ciphertext middle
	var mk [crypto.MasterKeySize]byte
	if _, err := crypto.UnwrapDEK(short, mk); !errors.Is(err, crypto.ErrInvalidWrappedDEKLength) {
		t.Errorf("err = %v, want ErrInvalidWrappedDEKLength", err)
	}
}

// --- chunk envelope ---

func TestEncryptChunk_NonceUniqueness(t *testing.T) {
	dek := bytes.Repeat([]byte{0x01}, crypto.DEKSize)
	plaintext := []byte("hello world")
	a, err := crypto.EncryptChunk(plaintext, 0, dek)
	if err != nil {
		t.Fatalf("first EncryptChunk: %v", err)
	}
	b, err := crypto.EncryptChunk(plaintext, 0, dek)
	if err != nil {
		t.Fatalf("second EncryptChunk: %v", err)
	}
	if bytes.Equal(a, b) {
		t.Error("two EncryptChunk calls with same plaintext + key + index returned identical ciphertext — nonce reuse")
	}
	if bytes.Equal(a[:crypto.NonceSize], b[:crypto.NonceSize]) {
		t.Error("nonces match across two encrypts — CSPRNG suspect")
	}
}

func TestChunkRoundtrip(t *testing.T) {
	dek := bytes.Repeat([]byte{0x02}, crypto.DEKSize)
	plaintext := bytes.Repeat([]byte("payload-"), 1024)
	const idx uint32 = 7

	blob, err := crypto.EncryptChunk(plaintext, idx, dek)
	if err != nil {
		t.Fatalf("EncryptChunk: %v", err)
	}
	got, err := crypto.DecryptChunk(blob, idx, dek)
	if err != nil {
		t.Fatalf("DecryptChunk: %v", err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Error("decrypted plaintext does not byte-equal original")
	}
}

func TestEncryptChunk_AADBindsIndex(t *testing.T) {
	dek := bytes.Repeat([]byte{0x03}, crypto.DEKSize)
	plaintext := []byte("position-bound payload")
	blob, err := crypto.EncryptChunk(plaintext, 5, dek)
	if err != nil {
		t.Fatalf("EncryptChunk: %v", err)
	}
	if _, err := crypto.DecryptChunk(blob, 6, dek); !errors.Is(err, crypto.ErrAEADAuthFailed) {
		t.Errorf("err = %v, want ErrAEADAuthFailed (AAD must bind chunk index)", err)
	}
}

func TestDecryptChunk_TamperedCiphertext(t *testing.T) {
	dek := bytes.Repeat([]byte{0x04}, crypto.DEKSize)
	plaintext := []byte("untouched")
	blob, err := crypto.EncryptChunk(plaintext, 0, dek)
	if err != nil {
		t.Fatalf("EncryptChunk: %v", err)
	}

	// Flip a bit inside the ciphertext (between nonce and tag).
	flipIdx := crypto.NonceSize + 1
	tampered := bytes.Clone(blob)
	tampered[flipIdx] ^= 0x01
	if _, err := crypto.DecryptChunk(tampered, 0, dek); !errors.Is(err, crypto.ErrAEADAuthFailed) {
		t.Errorf("err = %v, want ErrAEADAuthFailed (tampered ciphertext)", err)
	}
}

func TestDecryptChunk_TamperedTag(t *testing.T) {
	dek := bytes.Repeat([]byte{0x05}, crypto.DEKSize)
	plaintext := []byte("untouched")
	blob, err := crypto.EncryptChunk(plaintext, 0, dek)
	if err != nil {
		t.Fatalf("EncryptChunk: %v", err)
	}

	// Flip a bit in the trailing tag (last 16 bytes).
	tampered := bytes.Clone(blob)
	tampered[len(tampered)-1] ^= 0x80
	if _, err := crypto.DecryptChunk(tampered, 0, dek); !errors.Is(err, crypto.ErrAEADAuthFailed) {
		t.Errorf("err = %v, want ErrAEADAuthFailed (tampered tag)", err)
	}
}

func TestEncryptChunk_RejectsBadDEKLength(t *testing.T) {
	short := make([]byte, crypto.DEKSize-1)
	if _, err := crypto.EncryptChunk([]byte("x"), 0, short); !errors.Is(err, crypto.ErrInvalidDEKLength) {
		t.Errorf("err = %v, want ErrInvalidDEKLength", err)
	}
}

// --- manifest envelope ---

func TestManifestRoundtrip(t *testing.T) {
	dek := bytes.Repeat([]byte{0x06}, crypto.DEKSize)
	mj := []byte(`{"envelopeVersion":1,"chunkCount":3}`)

	blob, err := crypto.EncryptManifest(mj, dek)
	if err != nil {
		t.Fatalf("EncryptManifest: %v", err)
	}
	got, err := crypto.DecryptManifest(blob, dek)
	if err != nil {
		t.Fatalf("DecryptManifest: %v", err)
	}
	if !bytes.Equal(got, mj) {
		t.Error("decrypted manifest does not byte-equal original")
	}
}

func TestManifest_NotDecryptableAsChunk0(t *testing.T) {
	dek := bytes.Repeat([]byte{0x07}, crypto.DEKSize)
	blob, err := crypto.EncryptManifest([]byte("manifest"), dek)
	if err != nil {
		t.Fatalf("EncryptManifest: %v", err)
	}
	if _, err := crypto.DecryptChunk(blob, 0, dek); !errors.Is(err, crypto.ErrAEADAuthFailed) {
		t.Errorf("err = %v, want ErrAEADAuthFailed (manifest must NOT decrypt as chunk 0 — AAD distinguishes them)", err)
	}
}

func TestChunk0_NotDecryptableAsManifest(t *testing.T) {
	dek := bytes.Repeat([]byte{0x08}, crypto.DEKSize)
	blob, err := crypto.EncryptChunk([]byte("chunk0"), 0, dek)
	if err != nil {
		t.Fatalf("EncryptChunk: %v", err)
	}
	if _, err := crypto.DecryptManifest(blob, dek); !errors.Is(err, crypto.ErrAEADAuthFailed) {
		t.Errorf("err = %v, want ErrAEADAuthFailed (chunk 0 must NOT decrypt as manifest)", err)
	}
}

// --- recovery key ---

func TestGenerateRecoveryKey_24WordsAndRoundtrip(t *testing.T) {
	rk, err := crypto.GenerateRecoveryKey()
	if err != nil {
		t.Fatalf("GenerateRecoveryKey: %v", err)
	}
	words := strings.Fields(rk.Phrase)
	if len(words) != crypto.RecoveryMnemonicWords {
		t.Errorf("len(words) = %d, want %d", len(words), crypto.RecoveryMnemonicWords)
	}
	parsed, err := crypto.ParseRecoveryPhrase(rk.Phrase)
	if err != nil {
		t.Fatalf("ParseRecoveryPhrase on freshly-generated phrase: %v", err)
	}
	if parsed != rk.Bytes {
		t.Error("ParseRecoveryPhrase did not roundtrip GenerateRecoveryKey")
	}
}

func TestParseRecoveryPhrase_RejectsBadChecksum(t *testing.T) {
	// Generate a known-valid phrase, then swap the last word for a
	// different valid BIP39 word — that ALMOST always invalidates the
	// checksum (the checksum is 8 bits; ~1/256 swap chance to land on a
	// valid checksum, but using a deliberately-far swap is robust).
	rk, err := crypto.GenerateRecoveryKey()
	if err != nil {
		t.Fatalf("GenerateRecoveryKey: %v", err)
	}
	words := strings.Fields(rk.Phrase)
	// "abandon" is the first BIP39 word; "zoo" is the last. Swap last
	// to whichever it currently isn't — guarantees a different word.
	if words[len(words)-1] == "abandon" {
		words[len(words)-1] = "zoo"
	} else {
		words[len(words)-1] = "abandon"
	}
	bad := strings.Join(words, " ")
	if _, err := crypto.ParseRecoveryPhrase(bad); err == nil {
		t.Error("ParseRecoveryPhrase accepted a bad-checksum phrase")
	}
}

func TestDeriveRecoveryKey_RoundtripDeterministic(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping v1-floor recovery KDF run under -short")
	}
	var rk [32]byte
	copy(rk[:], bytes.Repeat([]byte{0xCD}, 32))
	salt := bytes.Repeat([]byte{0xEF}, crypto.SaltSize)

	a, err := crypto.DeriveRecoveryKey(rk, salt, kdfTestParams())
	if err != nil {
		t.Fatalf("first DeriveRecoveryKey: %v", err)
	}
	b, err := crypto.DeriveRecoveryKey(rk, salt, kdfTestParams())
	if err != nil {
		t.Fatalf("second DeriveRecoveryKey: %v", err)
	}
	if a != b {
		t.Error("DeriveRecoveryKey not deterministic across runs with identical inputs")
	}
}

func TestDeriveRecoveryKey_RejectsSubFloor(t *testing.T) {
	var rk [32]byte
	salt := bytes.Repeat([]byte{0x00}, crypto.SaltSize)
	weak := crypto.KDFParams{Algorithm: "argon2id", Memory: 1024, Iterations: 1, Parallelism: 1}
	if _, err := crypto.DeriveRecoveryKey(rk, salt, weak); !errors.Is(err, crypto.ErrKDFParamsBelowFloor) {
		t.Errorf("err = %v, want ErrKDFParamsBelowFloor", err)
	}
	if _, err := crypto.RecoveryKeyVerifier(rk, salt, weak); !errors.Is(err, crypto.ErrKDFParamsBelowFloor) {
		t.Errorf("verifier err = %v, want ErrKDFParamsBelowFloor", err)
	}
}

// --- vectors cross-check (Python reference) ---

type vectorsFile struct {
	Argon2id        []argon2idVec        `json:"argon2id"`
	ChunkDecrypt    []chunkDecryptVec    `json:"chunk_decrypt"`
	DEKUnwrap       []dekUnwrapVec       `json:"dek_unwrap"`
	ManifestDecrypt []manifestDecryptVec `json:"manifest_decrypt"`
	Recovery        []recoveryVec        `json:"recovery"`
}

type argon2idVec struct {
	Passphrase string            `json:"passphrase"`
	SaltHex    string            `json:"salt_hex"`
	Params     crypto.KDFParams  `json:"params"`
	OutputHex  string            `json:"output_hex"`
}

type chunkDecryptVec struct {
	KeyHex       string `json:"key_hex"`
	ChunkIndex   uint32 `json:"chunk_index"`
	PlaintextHex string `json:"plaintext_hex"`
	BlobHex      string `json:"blob_hex"`
}

type dekUnwrapVec struct {
	MasterKeyHex string `json:"master_key_hex"`
	WrappedHex   string `json:"wrapped_hex"`
	DEKHex       string `json:"dek_hex"`
}

type manifestDecryptVec struct {
	KeyHex          string `json:"key_hex"`
	BlobHex         string `json:"blob_hex"`
	ManifestJSONHex string `json:"manifest_json_hex"`
}

type recoveryVec struct {
	EntropyHex            string           `json:"entropy_hex"`
	Phrase                string           `json:"phrase"`
	SaltHex               string           `json:"salt_hex"`
	Params                crypto.KDFParams `json:"params"`
	DerivedRecoveryKeyHex string           `json:"derived_recovery_key_hex"`
	VerifierHex           string           `json:"verifier_hex"`
}

func loadVectors(t *testing.T) vectorsFile {
	t.Helper()
	path := filepath.Join("testdata", "vectors.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v (regenerate with scripts/generate-crypto-vectors.py)", path, err)
	}
	var v vectorsFile
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("parse vectors.json: %v", err)
	}
	return v
}

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatalf("hex decode %q: %v", s, err)
	}
	return b
}

// TestVectors_PythonReference is the cross-implementation byte-equality
// check: vectors are produced by scripts/generate-crypto-vectors.py
// (Python pyca/cryptography + argon2-cffi + python-mnemonic) and the Go
// engine must produce or accept the exact same bytes.
//
// Gated under !testing.Short() because the Argon2id v1 floor is
// intentionally slow (~50–100 ms / derivation).
func TestVectors_PythonReference(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping Python-vector cross-check under -short")
	}
	v := loadVectors(t)

	t.Run("Argon2id", func(t *testing.T) {
		for i, vec := range v.Argon2id {
			d, err := crypto.DeriveKeys(vec.Passphrase, mustHex(t, vec.SaltHex), vec.Params)
			if err != nil {
				t.Fatalf("vec[%d] DeriveKeys: %v", i, err)
			}
			want := mustHex(t, vec.OutputHex)
			got := append(append([]byte{}, d.ServerPassword[:]...), d.MasterKey[:]...)
			if !bytes.Equal(got, want) {
				t.Errorf("vec[%d]: Go output != Python reference", i)
			}
		}
	})

	t.Run("ChunkDecrypt", func(t *testing.T) {
		for i, vec := range v.ChunkDecrypt {
			pt, err := crypto.DecryptChunk(mustHex(t, vec.BlobHex), vec.ChunkIndex, mustHex(t, vec.KeyHex))
			if err != nil {
				t.Fatalf("vec[%d] DecryptChunk: %v", i, err)
			}
			if !bytes.Equal(pt, mustHex(t, vec.PlaintextHex)) {
				t.Errorf("vec[%d]: plaintext mismatch", i)
			}
		}
	})

	t.Run("DEKUnwrap", func(t *testing.T) {
		for i, vec := range v.DEKUnwrap {
			var mk [crypto.MasterKeySize]byte
			copy(mk[:], mustHex(t, vec.MasterKeyHex))
			got, err := crypto.UnwrapDEK(mustHex(t, vec.WrappedHex), mk)
			if err != nil {
				t.Fatalf("vec[%d] UnwrapDEK: %v", i, err)
			}
			if !bytes.Equal(got, mustHex(t, vec.DEKHex)) {
				t.Errorf("vec[%d]: DEK mismatch", i)
			}
		}
	})

	t.Run("ManifestDecrypt", func(t *testing.T) {
		for i, vec := range v.ManifestDecrypt {
			got, err := crypto.DecryptManifest(mustHex(t, vec.BlobHex), mustHex(t, vec.KeyHex))
			if err != nil {
				t.Fatalf("vec[%d] DecryptManifest: %v", i, err)
			}
			if !bytes.Equal(got, mustHex(t, vec.ManifestJSONHex)) {
				t.Errorf("vec[%d]: manifest JSON mismatch", i)
			}
		}
	})

	t.Run("Recovery", func(t *testing.T) {
		for i, vec := range v.Recovery {
			// Phrase decode must yield the same entropy bytes.
			parsed, err := crypto.ParseRecoveryPhrase(vec.Phrase)
			if err != nil {
				t.Fatalf("vec[%d] ParseRecoveryPhrase: %v", i, err)
			}
			wantEntropy := mustHex(t, vec.EntropyHex)
			if !bytes.Equal(parsed[:], wantEntropy) {
				t.Errorf("vec[%d]: entropy mismatch", i)
			}

			// Recovery KDF and verifier must match Python's Argon2id output.
			var rk [32]byte
			copy(rk[:], wantEntropy)
			derived, err := crypto.DeriveRecoveryKey(rk, mustHex(t, vec.SaltHex), vec.Params)
			if err != nil {
				t.Fatalf("vec[%d] DeriveRecoveryKey: %v", i, err)
			}
			if !bytes.Equal(derived[:], mustHex(t, vec.DerivedRecoveryKeyHex)) {
				t.Errorf("vec[%d]: recovery KDF mismatch", i)
			}
			verifier, err := crypto.RecoveryKeyVerifier(rk, mustHex(t, vec.SaltHex), vec.Params)
			if err != nil {
				t.Fatalf("vec[%d] RecoveryKeyVerifier: %v", i, err)
			}
			if !bytes.Equal(verifier, mustHex(t, vec.VerifierHex)) {
				t.Errorf("vec[%d]: verifier mismatch", i)
			}
		}
	})
}
