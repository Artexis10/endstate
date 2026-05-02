## ADDED Requirements

### Requirement: Client-Side Encryption — No Server Access to Plaintext

The Hosted Backup system SHALL be structured so that Endstate's infrastructure has no cryptographic path to decrypt user data. The user's passphrase and derived `masterKey` MUST never leave the client device.

#### Scenario: Server stores only hashed server-side material

- **WHEN** a user signs up for Hosted Backup
- **THEN** the server SHALL store `Argon2id(serverPassword, server_salt)` — never the raw `serverPassword`
- **AND** the server SHALL store the `wrappedDEK` but NOT the DEK or the `masterKey`
- **AND** the server SHALL have no mechanism to unwrap the DEK

#### Scenario: masterKey is derived locally and never transmitted

- **WHEN** the client derives key material from the user's passphrase using Argon2id
- **THEN** the second 32-byte output (`masterKey`) SHALL remain on the client
- **AND** only the first 32-byte output (`serverPassword`) SHALL be transmitted to the server

### Requirement: KDF Parameters Locked at v1 Values

Key derivation SHALL use Argon2id with the locked v1 parameters: memory=65536 KiB, iterations=3, parallelism=4, output=64 bytes, salt=16 bytes (per-user, server-stored).

#### Scenario: Client enforces KDF parameter floor

- **WHEN** the client derives keys
- **THEN** the client SHALL refuse to derive keys with parameters weaker than the v1 floor regardless of server response

#### Scenario: Server rejects weak KDF parameters at signup

- **WHEN** a signup request arrives with `kdfParams` weaker than the v1 floor
- **THEN** the server SHALL reject the request with an error
- **AND** SHALL NOT create the account

### Requirement: Encryption Envelope Format

Each backup version SHALL use a chunked AES-256-GCM envelope: an encrypted JSON manifest containing chunk metadata and `wrappedDEK`, plus independently encrypted 4 MiB chunks. Each chunk uses a fresh random nonce; the chunk index is bound as AAD.

#### Scenario: Chunk index bound as AAD

- **WHEN** a chunk is encrypted or decrypted
- **THEN** the 4-byte big-endian chunk index SHALL be included as Additional Authenticated Data
- **AND** decryption SHALL fail if the chunk is presented at a position other than its original index

#### Scenario: Client verifies chunk hash before decryption

- **WHEN** the client downloads a chunk during restore
- **THEN** the client SHALL verify the SHA-256 of the downloaded bytes against the manifest's recorded hash
- **AND** SHALL refuse to decrypt any chunk whose hash does not match

### Requirement: Recovery Key as Second Independent Unlock Path

A 24-word BIP39 recovery key SHALL be generated at signup and presented to the user. The recovery key provides a second path to unwrap the DEK independent of the user's passphrase. Endstate SHALL NOT store the recovery key in plaintext.

#### Scenario: Recovery key presentation is mandatory

- **WHEN** the signup flow completes
- **THEN** the client SHALL present the recovery key to the user in at least two save formats (file and printable PDF)
- **AND** SHALL require explicit user confirmation of saving before signup completes

#### Scenario: Data unrecoverable if both passphrase and recovery key are lost

- **WHEN** a user has lost both their passphrase and recovery key
- **THEN** the data SHALL be unrecoverable
- **AND** the system SHALL NOT provide any operator-assisted recovery path

### Requirement: Subscription State Controls Write Access

Backup writes SHALL be blocked for any subscription state other than `active`. Backup reads (restore) SHALL be permitted in `active`, `grace`, and `cancelled` states. The server's database row SHALL be authoritative — not the JWT claim.

#### Scenario: Write blocked in grace state

- **WHEN** a user's subscription is in `grace` state
- **THEN** backup version creation SHALL fail
- **AND** restore (download) SHALL succeed

#### Scenario: JWT claim is hint only

- **WHEN** a write-path endpoint receives a request
- **THEN** the server SHALL check the subscription state from the database
- **AND** SHALL NOT rely solely on the `subscription_status` JWT claim for authorization

### Requirement: Ownership Isolation on Backup Endpoints

All `/api/backups/*` endpoints SHALL be scoped to the authenticated user. Cross-user access SHALL return 404 (not 403) to avoid leaking the existence of other users' backups.

#### Scenario: Cross-user backup access returns 404

- **WHEN** a user requests a backup resource belonging to a different user
- **THEN** the server SHALL return HTTP 404
- **AND** SHALL NOT return HTTP 403 or any response that confirms the resource exists
