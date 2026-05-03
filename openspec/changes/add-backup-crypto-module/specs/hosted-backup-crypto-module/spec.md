## ADDED Requirements

### Requirement: Argon2id KDF With v1-Floor Enforcement

The engine SHALL derive key material from a passphrase using Argon2id (RFC 9106) with a 64-byte output. The first 32 bytes of the output ARE the value sent to the server as `serverPassword`; the second 32 bytes ARE the `masterKey` and SHALL NOT leave the device. The engine SHALL refuse to derive keys when `KDFParams.MeetsFloor()` returns false, regardless of what the server advertises (contract §1, §2).

#### Scenario: v1-floor params produce 64 bytes split 32/32

- **WHEN** the client calls `DeriveKeys` with `DefaultKDFParams()` and a 16-byte salt
- **THEN** the call SHALL succeed
- **AND** SHALL return a `DerivedKeys` whose `ServerPassword` and `MasterKey` are each 32 bytes

#### Scenario: sub-floor parameters refused

- **WHEN** the client calls `DeriveKeys` with `Memory < 65536`, or `Iterations < 3`, or `Parallelism < 4`, or `Algorithm != "argon2id"`
- **THEN** the call SHALL return a non-nil error
- **AND** SHALL NOT call `argon2.IDKey`

#### Scenario: short salt refused

- **WHEN** the client calls `DeriveKeys` with a salt shorter than `SaltSize` (16 bytes)
- **THEN** the call SHALL return a non-nil error
- **AND** SHALL NOT call `argon2.IDKey`

### Requirement: AES-256-GCM Chunk Encryption With Index AAD

The engine SHALL encrypt every plaintext chunk with AES-256-GCM (NIST SP 800-38D) using a freshly generated 12-byte nonce from `crypto/rand` and a 4-byte big-endian unsigned representation of the chunk's index as Additional Authenticated Data. The wire format SHALL be `nonce || ciphertext || tag` (RFC 5116). Reusing a nonce with the same key SHALL NOT occur (contract §3).

#### Scenario: nonce uniqueness

- **WHEN** the engine calls `EncryptChunk(plaintext, idx, dek)` twice with identical inputs
- **THEN** the two ciphertexts SHALL differ
- **AND** the differing prefix SHALL be the 12-byte nonce

#### Scenario: AAD binds chunk index

- **WHEN** ciphertext is produced by `EncryptChunk(plaintext, 5, dek)`
- **AND** the engine calls `DecryptChunk(ciphertext, 6, dek)`
- **THEN** the decrypt call SHALL return a non-nil error
- **AND** SHALL NOT return the plaintext

#### Scenario: tampered ciphertext rejected

- **WHEN** any byte of an `EncryptChunk` output is flipped
- **THEN** `DecryptChunk` with the matching key and index SHALL return a non-nil error

### Requirement: Manifest AAD Sentinel `0xFFFFFFFF`

The engine SHALL encrypt the manifest blob with AES-256-GCM using `0xFFFFFFFF` as its AAD (4-byte big-endian unsigned). This sentinel cryptographically binds the encrypted blob to the "manifest" role and prevents substitution with chunk index 0. This sentinel is unrelated to the wire-protocol flag `chunkIndex == -1` documented in contract §7 (contract §3).

#### Scenario: manifest cannot be decrypted as chunk index 0

- **WHEN** ciphertext is produced by `EncryptManifest(json, dek)`
- **AND** the engine calls `DecryptChunk(ciphertext, 0, dek)`
- **THEN** the decrypt call SHALL return a non-nil error

#### Scenario: chunk index 0 cannot be decrypted as manifest

- **WHEN** ciphertext is produced by `EncryptChunk(plaintext, 0, dek)`
- **AND** the engine calls `DecryptManifest(ciphertext, dek)`
- **THEN** the decrypt call SHALL return a non-nil error

### Requirement: DEK Generation And AES-256-GCM Wrapping

The engine SHALL generate the data-encryption-key (DEK) as `DEKSize` (32) bytes from `crypto/rand`. The engine SHALL wrap the DEK under `masterKey` using AES-256-GCM with a freshly generated 12-byte nonce and no AAD; the wire format of the wrapped DEK SHALL be `nonce || ciphertext || tag` (60 bytes total) (contract §3).

#### Scenario: wrap and unwrap are inverses

- **WHEN** the engine calls `WrapDEK(dek, masterKey)` and `UnwrapDEK(wrapped, masterKey)` in sequence
- **THEN** the unwrapped value SHALL byte-equal the original DEK

#### Scenario: unwrap with wrong master key fails

- **WHEN** the engine calls `UnwrapDEK(wrapped, otherMasterKey)` with a master key different from the one used to wrap
- **THEN** the call SHALL return a non-nil error
- **AND** SHALL NOT return the DEK bytes

### Requirement: BIP39 Recovery Key Generation And Parsing

The engine SHALL generate the recovery key as 32 bytes from `crypto/rand` and encode it as a 24-word BIP39 mnemonic (256 bits of entropy). The engine SHALL validate the BIP39 checksum when parsing a mnemonic and SHALL refuse mnemonics whose checksum does not validate (contract §6).

#### Scenario: generated mnemonic has 24 words and parses cleanly

- **WHEN** the engine calls `GenerateRecoveryKey()`
- **THEN** the returned `Phrase` SHALL be 24 whitespace-separated words
- **AND** `ParseRecoveryPhrase(phrase)` SHALL return the same `Bytes`

#### Scenario: bad-checksum phrase rejected

- **WHEN** the engine calls `ParseRecoveryPhrase` with a 24-word phrase whose checksum byte is wrong
- **THEN** the call SHALL return a non-nil error

### Requirement: Recovery KDF And Server Verifier

The engine SHALL derive the 32-byte `recoveryKey` from the recovery-key bytes via Argon2id with the locked v1 parameters, and SHALL produce the server-side `recoveryKeyVerifier` as Argon2id over the recovery key with the same parameters. The verifier value is sent to the server; the engine SHALL NOT compare verifier values client-side (contract §6).

#### Scenario: recovery KDF roundtrip

- **WHEN** the engine calls `DeriveRecoveryKey(rk, salt, params)` twice with identical inputs
- **THEN** the two outputs SHALL byte-equal each other

#### Scenario: recovery KDF rejects sub-floor params

- **WHEN** the engine calls `DeriveRecoveryKey` or `RecoveryKeyVerifier` with `params.MeetsFloor() == false`
- **THEN** the call SHALL return a non-nil error

### Requirement: AEAD Authentication Failures Are Surfaced Distinctly

Every `Decrypt*` and `Unwrap*` path that performs an AEAD `Open` SHALL return a non-nil error on authentication failure and SHALL NOT mask the failure as nil or as the empty plaintext. Callers rely on this signal to distinguish "bad key" / "bad ciphertext" from a legitimate empty plaintext.

#### Scenario: AEAD failure surfaces a non-nil error

- **WHEN** any `DecryptChunk`, `DecryptManifest`, or `UnwrapDEK` call fails AEAD verification
- **THEN** the function SHALL return `nil` (or zero value) for the plaintext output AND a non-nil error
