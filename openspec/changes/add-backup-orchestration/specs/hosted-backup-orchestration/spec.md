## ADDED Requirements

### Requirement: `backup signup` Writes Recovery Mnemonic Before Network Call

The `endstate backup signup` command SHALL write the BIP39 recovery mnemonic to the file path supplied via `--save-recovery-to` BEFORE issuing the `POST /api/auth/signup` network call. The file SHALL be written with mode `0600`. The command SHALL refuse to run if the recovery mnemonic is generated client-side and `--save-recovery-to` is empty.

The order matters: a successful signup followed by a failed disk write would leave the user with an active account they cannot recover. The reverse order — file written, signup fails — leaves a harmless orphan file the user can delete.

#### Scenario: Missing --save-recovery-to with generated mnemonic refuses

- **WHEN** `endstate backup signup --email <addr>` is invoked without `--save-recovery-to` and without a mnemonic on stdin
- **THEN** the engine SHALL refuse and return a clear error envelope naming the missing flag
- **AND** SHALL NOT make the signup network call

#### Scenario: Recovery file is written before signup POST

- **WHEN** `endstate backup signup --email <addr> --save-recovery-to <path>` runs
- **THEN** the engine SHALL write the mnemonic to `<path>` (mode 0600) before issuing `POST /api/auth/signup`
- **AND** SHALL return success only if both the file write and the signup POST succeed

### Requirement: DEK Cached in OS Keychain Between CLI Invocations

After a successful `backup signup`, `backup login`, or `backup recover`, the engine SHALL cache the unwrapped DEK in the OS keychain alongside the refresh token. The keychain account name SHALL be `"endstate-dek-" + userID` (distinct from the refresh-token account `"endstate-refresh-" + userID`).

`backup logout` and `account delete` SHALL clear both entries.

#### Scenario: DEK persists across CLI invocations

- **GIVEN** a successful `backup login` for userId `U`
- **WHEN** the engine returns
- **THEN** the OS keychain SHALL contain an entry under `"endstate-dek-" + U` carrying the unwrapped DEK
- **AND** subsequent `backup push` / `backup pull` calls SHALL load that DEK without re-prompting for the passphrase

#### Scenario: Logout clears both refresh token and DEK

- **GIVEN** a signed-in session with refresh token and DEK in the keychain
- **WHEN** `endstate backup logout` runs
- **THEN** both `"endstate-refresh-" + userID` and `"endstate-dek-" + userID` SHALL be absent from the keychain after the command returns

### Requirement: Profile Container Is Uncompressed POSIX Tar

The engine SHALL package profile contents as an uncompressed POSIX tar archive (stdlib `archive/tar`) before encryption on push, and SHALL un-tar after decryption on pull. Compression is not applied.

A push followed by a pull SHALL produce a byte-equal copy of the original profile contents.

#### Scenario: Pull restores byte-equal profile contents

- **GIVEN** a profile pushed to a backup
- **WHEN** that version is pulled to a fresh path
- **THEN** every file and directory in the restored profile SHALL be byte-equal to the original
- **AND** file modes and timestamps reproducible by `archive/tar` SHALL be preserved

### Requirement: `backup pull` Refuses to Overwrite Without Flag

`endstate backup pull --to <path>` SHALL refuse to write into `<path>` if `<path>` already exists, unless `--overwrite` is also supplied. The refusal is non-destructive: no chunks are downloaded, no plaintext is materialised on disk.

#### Scenario: Existing target rejected without --overwrite

- **GIVEN** `<path>` exists on disk
- **WHEN** `endstate backup pull --backup-id <id> --to <path>` runs without `--overwrite`
- **THEN** the engine SHALL return a clear error envelope with remediation pointing at `--overwrite`
- **AND** SHALL NOT issue the download-URL request

### Requirement: Recovery Key Verifier Is Client-Computed at Signup and Recovery

The `recoveryKeyVerifier` field sent in the `POST /api/auth/signup` body SHALL be computed client-side as `Argon2id(recoveryKey, salt, params)`. The same value SHALL be recomputed client-side at recovery and sent as `recoveryKeyProof` to `POST /api/auth/recover`. The server stores the verifier at signup and constant-time-compares at recovery; the server never derives the verifier itself.

#### Scenario: Signup payload carries client-computed verifier

- **WHEN** `backup signup` constructs the request body
- **THEN** the `recoveryKeyVerifier` field SHALL equal `crypto.RecoveryKeyVerifier(recoveryKey, salt, params)` for the salt and params used in this signup

#### Scenario: Recover payload reuses identical computation

- **WHEN** `backup recover` constructs the proof for `POST /api/auth/recover`
- **THEN** `recoveryKeyProof` SHALL be computed by the same `crypto.RecoveryKeyVerifier(recoveryKey, salt, params)` over the recoveryKey derived from the user's mnemonic and the salt returned by the server in `PreHandshake`
