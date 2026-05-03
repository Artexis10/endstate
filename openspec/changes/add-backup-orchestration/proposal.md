## Why

PRs #19 (`add-backup-auth-client`), #22 (`add-backup-storage-client`), and #23 (`add-backup-crypto-module`) shipped the auth scaffold, the storage HTTP client, and the real cryptographic primitives. The substrate backend is fully operational in production: JWKS at `https://substratesystems.io/api/.well-known/jwks.json` serves `endstate-prod-2026-05`, OIDC discovery is spec-compliant, and the R2 bucket and credentials are live.

What is still stubbed is the **orchestration glue**: the engine command surface knows how to validate inputs and call into auth/storage/crypto, but the post-pre-handshake login flow, the chunked upload pipeline, the chunked download pipeline, and the recovery flow all return `"orchestration not yet implemented"`. There is also no `endstate backup signup` entry point; the GUI was the only sign-up surface in earlier prompts, but a CLI entry is needed for end-to-end smoke tests against production substrate.

This change fills those gaps. After it lands the engine completes the full lifecycle — sign up → push → pull → recover — against production. A byte-equal roundtrip smoke test against `https://substratesystems.io` is the merge gate.

The four ambiguities that remained after the contract was locked are resolved here:

1. **Recovery key client/server division (§6).** `recoveryKeyVerifier = Argon2id(recoveryKey, salt, params)` is computed by the client both at signup (sent in the signup body) and at recovery (sent as `recoveryKeyProof`). The server stores at signup and constant-time-compares at recovery. The crypto module's shipped API (`crypto.RecoveryKeyVerifier`) implements exactly this.
2. **Profile container format.** Uncompressed POSIX tar via stdlib `archive/tar`. Stream-friendly, byte-stable across roundtrips (no gzip header timestamps), zero new dependencies.
3. **Session storage of unwrapped DEK.** Encrypted in OS keychain alongside the refresh token (Windows Credential Manager). Same trust boundary as the refresh token (gated on the OS user account); cleared on logout and on account delete.
4. **Single-version GET endpoint.** Substrate exposes `GET /api/backups/:backupId/versions` (list) but no single-version GET. For this PR the engine filters the list response client-side; a follow-up substrate change can add a single-version GET if real usage demands one.

## What Changes

- **New `endstate backup signup --email <addr> --save-recovery-to <path>`** command. Reads the passphrase and (optionally) the BIP39 recovery mnemonic from stdin; generates fresh DEK, salt, and recovery materials; writes the recovery mnemonic to `--save-recovery-to` (mode 0600) **before** the network call; POSTs `/api/auth/signup`; persists tokens and DEK to the keychain on success.
- **Complete `backup login`** post-pre-handshake orchestration: derive Argon2id keys → `CompleteLogin` → `UnwrapDEK` → cache DEK in keychain alongside the refresh token.
- **Complete `backup push`** orchestration in a new `internal/backup/upload/` package: tar the profile, chunk into 4 MiB blocks, encrypt each with `crypto.EncryptChunk(chunkIndex)`, encrypt the manifest with `crypto.EncryptManifest`, `CreateVersion` to mint presigned URLs, PUT the chunks and manifest to R2 with bounded concurrency. Per-chunk progress streams as item events; phase + summary events bracket the run.
- **Complete `backup pull`** orchestration in a new `internal/backup/download/` package: request the manifest URL (`chunkIndex == -1`), decrypt and parse the manifest, request per-chunk URLs, GET each, verify SHA-256 against the manifest **before** decrypting, decrypt with `crypto.DecryptChunk(chunkIndex)`, untar to the target path. Refuses to overwrite without `--overwrite`.
- **Complete `backup recover`** orchestration: parse the mnemonic, derive the recovery key, prove possession via `POST /api/auth/recover`, unwrap the DEK with the recovery key, derive new keys from the new passphrase, re-wrap the DEK, finalize via `POST /api/auth/recover/finalize`. Tokens and DEK are persisted on success.
- **`backup logout`** clears the DEK from the keychain in addition to the refresh token (handled centrally by extending `Session.Forget()`).
- **`account delete`** likewise clears the DEK as part of `Session.Forget()`.
- **`internal/backup/keychain/`** adds `AccountForDEK(userID)`; the existing `Keychain` interface is unchanged.
- **`internal/backup/auth/`** gains `Signup`, `Recover`, and `RecoverFinalize` methods on `Authenticator`, and `StoreDEK` / `LoadDEK` / `ClearDEK` on `SessionStore`.
- **Test suite** covers signup happy path, login full two-step, login wrong-passphrase, push multi-chunk + 5xx-retry, pull happy roundtrip + tampered-chunk rejection + overwrite refusal, recover happy path, logout clears both refresh + DEK.
- **Cleanup**: the four stale `errors.Is(err, crypto.ErrNotImplemented)` short-circuits in `backup_login.go`, `backup_push.go`, and `backup_recover.go` are removed (the crypto module returns real values now). `crypto.ErrNotImplemented` itself is retained as an exported sentinel for symmetry.
- **README** Hosted Backup section is expanded with the full command surface and an end-to-end workflow.

## Capabilities

### New Capabilities

- `hosted-backup-orchestration`: Engine-side end-to-end orchestration for Endstate Hosted Backup — `backup signup` command, full login post-pre-handshake flow, chunked upload pipeline (`internal/backup/upload/`), chunked download pipeline (`internal/backup/download/`), recovery flow, and DEK session storage in the OS keychain.

### Modified Capabilities

- `hosted-backup-auth-client`: extends `Authenticator` with `Signup`, `Recover`, and `RecoverFinalize`; extends `SessionStore` with `StoreDEK`, `LoadDEK`, `ClearDEK`; `Forget()` now clears both the refresh token and the DEK.
- `hosted-backup-storage-client`: no API change; `backup push` and `backup pull` now exercise it end-to-end via the new upload/download packages.
- `command-dispatcher`: registers `backup signup` and adds `--save-recovery-to` and `--overwrite` flag plumbing.

## Impact

- **`go-engine/internal/commands/`** — new `backup_signup.go`; `backup_login.go`, `backup_push.go`, `backup_pull.go`, `backup_recover.go` finished; stale `ErrNotImplemented` branches removed.
- **`go-engine/internal/backup/upload/`** — new package: profile-to-tar, chunker, presigned-URL PUT loop, retry policy.
- **`go-engine/internal/backup/download/`** — new package: presigned-URL GET loop, SHA-256 verifier, untar, byte-equal write.
- **`go-engine/internal/backup/auth/`** — new methods: `Signup`, `Recover`, `RecoverFinalize`; `SessionStore` gains DEK helpers; `Forget()` clears DEK.
- **`go-engine/internal/backup/keychain/`** — `AccountForDEK(userID)` added.
- **`go-engine/cmd/endstate/main.go`** — register `backup signup`; add `--save-recovery-to` and `--overwrite` flag plumbing; expand backup help text.
- **`go-engine/internal/commands/backup.go`** — dispatcher routes `signup`; `BackupFlags` carries `SaveRecoveryTo` and `Overwrite`.
- **`README.md`** — Hosted Backup section expanded with the full command surface.
- **No new external dependencies.** Tar uses stdlib `archive/tar`; chunking and presigned PUT/GET use stdlib `net/http`. Existing crypto/storage packages cover the rest.
- **Smoke test before merge.** Build the engine, sign up a `smoketest+<timestamp>@example.com` account against production, push a small profile, pull it back to a different path, byte-equal-diff the result, then `account delete --confirm`. Merge blocker if the diff is not byte-equal.
