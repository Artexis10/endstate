## 1. Dependencies

- [ ] 1.1 Add `golang.org/x/crypto` direct dep to `go-engine/go.mod` (used: `argon2.IDKey`)
- [ ] 1.2 Add `github.com/tyler-smith/go-bip39` direct dep to `go-engine/go.mod` (used: 24-word mnemonic gen + checksum validation)
- [ ] 1.3 `go mod tidy` keeping the existing `go 1.22` directive (run with `GOTOOLCHAIN=go1.22.0` or `-compat=1.22`); CI uses Go 1.22

## 2. KDF

- [ ] 2.1 Implement `DeriveKeys(passphrase string, salt []byte, params KDFParams) (DerivedKeys, error)` calling `argon2.IDKey` with 64-byte output
- [ ] 2.2 Reject params that fail `params.MeetsFloor()` with a clear sentinel error
- [ ] 2.3 Reject salt shorter than `SaltSize` (16 bytes) with a clear sentinel error
- [ ] 2.4 Split output: copy `out[0:32]` into `ServerPassword`, `out[32:64]` into `MasterKey`; zero the intermediate buffer

## 3. DEK and chunk/manifest envelope

- [ ] 3.1 Implement `GenerateDEK` using `crypto/rand.Read` over a `DEKSize` slice
- [ ] 3.2 Implement `WrapDEK` / `UnwrapDEK` as AES-256-GCM with fresh nonce, no AAD, wire format `nonce || ciphertext || tag` (60 bytes for 32-byte plaintext)
- [ ] 3.3 Implement `EncryptChunk` / `DecryptChunk` with fresh 12-byte nonce, AAD = 4-byte big-endian `chunkIndex`
- [ ] 3.4 Implement `EncryptManifest` / `DecryptManifest` with the same primitive but AAD = `ManifestAAD` (`0xFFFFFFFF`)
- [ ] 3.5 Centralise the AES-256-GCM seal/open path in a private helper so chunk and manifest paths cannot drift; both call into it with a different AAD `uint32`
- [ ] 3.6 AEAD authentication failure on every `Decrypt*` / `Unwrap*` path returns a non-nil, non-masked error

## 4. Recovery key

- [ ] 4.1 Implement `GenerateRecoveryKey` — 32 bytes CSPRNG, encoded via `bip39.NewMnemonic` (24 words / 256 bits)
- [ ] 4.2 Implement `ParseRecoveryPhrase` — `bip39.MnemonicToByteArray(_, true)` (validates checksum); copy into `[32]byte`
- [ ] 4.3 Implement `DeriveRecoveryKey` — Argon2id over the 32-byte recovery key with the same params, 32-byte output
- [ ] 4.4 Implement `RecoveryKeyVerifier` — Argon2id over the recovery key with the same params, 32-byte output (the server stores this)

## 5. Memory hygiene

- [ ] 5.1 New `zero.go` with `zeroBytes(b []byte)` helper (and an honest doc comment about Go's GC limitations on zeroization guarantees)
- [ ] 5.2 Apply `zeroBytes` to derived intermediate buffers (Argon2id output, decrypted DEK plaintext from `UnwrapDEK` is the caller's responsibility)

## 6. Test vectors

- [ ] 6.1 Add `scripts/generate-crypto-vectors.py` producing `go-engine/internal/backup/crypto/testdata/vectors.json` with: 5 Argon2id, 5 AES-256-GCM chunk decrypt, 3 DEK unwrap, 3 manifest decrypt, 3 recovery (mnemonic + KDF)
- [ ] 6.2 Add `scripts/requirements.txt` pinning `cryptography` and `argon2-cffi`
- [ ] 6.3 Vectors test the *decrypt* path against fixed ciphertexts (encrypt path is exercised by Go-only roundtrip tests because nonces are random)

## 7. Tests

- [ ] 7.1 Keep `TestDefaultKDFParamsLockedV1`, `TestKDFParams_MeetsFloor`, `TestManifestAAD_IsSentinel`
- [ ] 7.2 Drop `TestStubsReturnNotImplemented` — superseded by the real tests
- [ ] 7.3 Add `TestDeriveKeys_Roundtrip` and rejection tests (weak params, short salt)
- [ ] 7.4 Add `TestEncryptChunk_NonceUniqueness`, `TestEncryptChunk_AADBindsIndex`, `TestDecryptChunk_TamperedCiphertext`, `TestDecryptChunk_TamperedTag`
- [ ] 7.5 Add `TestWrapUnwrapDEK_Roundtrip`, `TestUnwrapDEK_WrongMasterKey`
- [ ] 7.6 Add `TestManifestRoundtrip`, `TestManifest_NotDecryptableAsChunk0`
- [ ] 7.7 Add `TestGenerateRecoveryKey_24Words_ValidChecksum`, `TestParseRecoveryPhrase_RejectsBadChecksum`, `TestDeriveRecoveryKey_Roundtrip`
- [ ] 7.8 Add `TestVectors_PythonReference` reading `testdata/vectors.json`, gated on `!testing.Short()`
- [ ] 7.9 Tier KDF tests: most use reduced params for speed; v1-floor tests gated on `!testing.Short()`

## 8. Documentation

- [ ] 8.1 Rewrite `internal/backup/crypto/doc.go` from "stub" narrative to "implementation" narrative; keep contract-section anchors
- [ ] 8.2 Add `internal/backup/crypto/README.md` summarising the package and its mapping to contract sections + standards
