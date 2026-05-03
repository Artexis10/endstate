## 1. Keychain + session DEK storage

- [ ] 1.1 `internal/backup/keychain/keychain.go`: add `AccountForDEK(userID string) string` returning `"endstate-dek-" + userID`
- [ ] 1.2 `internal/backup/auth/session.go`: add `(*SessionStore).StoreDEK(dek []byte) error`, `LoadDEK() ([]byte, error)`, `ClearDEK() error`
- [ ] 1.3 `(*SessionStore).Forget()` extended to clear both refresh token and DEK; idempotent

## 2. Authenticator: signup + recover

- [ ] 2.1 `internal/backup/auth/authenticator.go`: add `Signup(ctx, body SignupBody) → (*CompleteLoginResponse, *envelope.Error)` — POSTs `/api/auth/signup`; persists tokens via `SessionStore.SetTokens` + `Persist`
- [ ] 2.2 `Recover(ctx, body RecoverBody) → (*RecoverResponse, *envelope.Error)` — POSTs `/api/auth/recover`; returns `recoveryKeyWrappedDEK` (and salt if the server returns one)
- [ ] 2.3 `RecoverFinalize(ctx, body RecoverFinalizeBody) → (*CompleteLoginResponse, *envelope.Error)` — POSTs `/api/auth/recover/finalize`; persists new tokens

## 3. `backup signup` command

- [ ] 3.1 `internal/commands/backup.go`: extend `BackupFlags` with `SaveRecoveryTo string` and `Overwrite bool`; route `signup` in dispatcher
- [ ] 3.2 `cmd/endstate/main.go`: parse `--save-recovery-to` and `--overwrite`; thread into `BackupFlags`; expand `backup` help
- [ ] 3.3 New `internal/commands/backup_signup.go`: stdin passphrase + optional mnemonic, `crypto.GenerateRecoveryKey()` if absent
- [ ] 3.4 Refuse to proceed if generating but `--save-recovery-to` is empty
- [ ] 3.5 Generate 16-byte salt via `crypto/rand`; `crypto.GenerateDEK()`; `crypto.DeriveKeys(passphrase, salt, params)`; `crypto.WrapDEK(dek, masterKey)` → wrappedDEK
- [ ] 3.6 `crypto.DeriveRecoveryKey(rkBytes, salt, params)` → recoveryKey; `crypto.WrapDEK(dek, recoveryKey)` → recoveryKeyWrappedDEK; `crypto.RecoveryKeyVerifier(recoveryKey, salt, params)` → recoveryKeyVerifier
- [ ] 3.7 Write mnemonic to `--save-recovery-to` (mode 0600) BEFORE the network call
- [ ] 3.8 `Authenticator.Signup` with body `{email, serverPassword, salt, kdfParams, wrappedDEK, recoveryKeyVerifier, recoveryKeyWrappedDEK}` (all base64 where bytes)
- [ ] 3.9 On success, `Session.StoreDEK(dek)`; return `SignupResult{userId, email, recoveryKeySavedTo}` envelope; exit 0
- [ ] 3.10 Zeroise: passphrase bytes, masterKey, recoveryKey, mnemonic-byte buffer, dek copy

## 4. Complete `backup login`

- [ ] 4.1 `backup_login.go`: after `crypto.DeriveKeys`, call `Authenticator.CompleteLogin`
- [ ] 4.2 `crypto.UnwrapDEK(base64-decode(resp.WrappedDEK), derived.MasterKey)` → DEK
- [ ] 4.3 `Session.StoreDEK(dek)`
- [ ] 4.4 Return `LoginResult{UserID, Email, SubscriptionStatus}`; exit 0
- [ ] 4.5 Remove the `errors.Is(err, crypto.ErrNotImplemented)` short-circuit

## 5. Upload package (`backup push`)

- [ ] 5.1 New `internal/backup/upload/upload.go`: `PushVersion(ctx, deps, backupID, profilePath, name) (*PushResult, error)`
- [ ] 5.2 Tar the profile contents (POSIX tar, uncompressed) via stdlib `archive/tar`
- [ ] 5.3 Chunk into 4 MiB blocks; encrypt each with `crypto.EncryptChunk(plaintext, idx, dek)`; SHA-256 the encrypted blob
- [ ] 5.4 Build manifest (`crypto.EnvelopeVersion`, fresh `versionId` UUID, `originalSize`, `chunkSize=4MiB`, `chunkCount`, chunk metas, `kdf`, `wrappedDEK` from session)
- [ ] 5.5 `manifest.Marshal` → JSON; `crypto.EncryptManifest(json, dek)` → encrypted manifest
- [ ] 5.6 `storage.CreateVersion(ctx, backupID, encryptedManifest, chunkMeta)` → `{versionId, uploadUrls}`
- [ ] 5.7 PUT each encrypted chunk to its presigned URL via bare `http.Client`; bounded concurrency `backup.Concurrency()`; retry once on 5xx
- [ ] 5.8 PUT encryptedManifest to manifest URL (`chunkIndex == -1`)
- [ ] 5.9 Emit `phase: "backup-push"`, then per-chunk `item` events, then `summary`
- [ ] 5.10 Auto-create backup if `--backup-id` empty and the user has zero backups (via `storage.CreateBackup(name)`)
- [ ] 5.11 Wire `internal/commands/backup_push.go` to call `PushVersion`; remove `ErrNotImplemented` branch

## 6. Download package (`backup pull`)

- [ ] 6.1 New `internal/backup/download/download.go`: `PullVersion(ctx, deps, backupID, versionID, to, overwrite) (*PullResult, error)`
- [ ] 6.2 Refuse if `to` exists and `!overwrite` — `INTERNAL_ERROR` with remediation pointing at `--overwrite`
- [ ] 6.3 Resolve `versionID` if empty: `storage.ListVersions` → `manifest.SelectLatest`
- [ ] 6.4 `storage.DownloadURLs(ctx, backupID, versionID, []int{-1})` → manifest URL
- [ ] 6.5 GET manifest URL via bare `http.Client`; `crypto.DecryptManifest(blob, dek)` → JSON; `manifest.Unmarshal`
- [ ] 6.6 `storage.DownloadURLs(ctx, backupID, versionID, [0..N-1])` → per-chunk URLs
- [ ] 6.7 For each chunk: GET; SHA-256 verify against manifest; refuse to decrypt on mismatch
- [ ] 6.8 `crypto.DecryptChunk(blob, idx, dek)` → plaintext
- [ ] 6.9 Untar plaintext stream into `to` (preserve byte-for-byte file contents)
- [ ] 6.10 Emit phase + per-chunk + summary events
- [ ] 6.11 Wire `internal/commands/backup_pull.go` to call `PullVersion`; thread `--overwrite`

## 7. `backup recover`

- [ ] 7.1 `backup_recover.go`: `crypto.ParseRecoveryPhrase(phrase)` → rkBytes
- [ ] 7.2 `Authenticator.PreHandshake(email)` → salt + kdfParams
- [ ] 7.3 `crypto.DeriveRecoveryKey(rkBytes, salt, params)` → recoveryKey
- [ ] 7.4 `crypto.RecoveryKeyVerifier(recoveryKey, salt, params)` → proof
- [ ] 7.5 `Authenticator.Recover(ctx, {email, recoveryKeyProof: proof})` → `recoveryKeyWrappedDEK`
- [ ] 7.6 `crypto.UnwrapDEK(recoveryKeyWrappedDEK, recoveryKey)` → DEK
- [ ] 7.7 `crypto.DeriveKeys(newPassphrase, salt, params)` → new {serverPassword, masterKey}
- [ ] 7.8 `crypto.WrapDEK(dek, newMasterKey)` → newWrappedDEK
- [ ] 7.9 `Authenticator.RecoverFinalize(ctx, {email, serverPassword, wrappedDEK})` → tokens
- [ ] 7.10 `Session.SetTokens` + `Persist` + `StoreDEK(dek)`
- [ ] 7.11 Remove `ErrNotImplemented` branch; return `RecoverResult{UserID, Email}`

## 8. Cleanup

- [ ] 8.1 Drop the `errors.Is(err, crypto.ErrNotImplemented)` blocks in `backup_login.go`, `backup_push.go`, `backup_recover.go`
- [ ] 8.2 Drop the unreachable comment-only "post-crypto orchestration" narratives now that the flow is real
- [ ] 8.3 Keep `crypto.ErrNotImplemented` exported (low cost, future use)

## 9. Tests

- [ ] 9.1 `backup_signup_test.go`: happy path → recovery file written, refresh token + DEK in keychain, success envelope; missing `--save-recovery-to` rejection
- [ ] 9.2 `backup_test.go`: extend login test for full two-step happy path → DEK unwrapped + cached in keychain; wrong-passphrase → 401 → clean envelope, no DEK stored
- [ ] 9.3 `upload_test.go`: multi-chunk happy path against httptest R2; verifies all chunks + manifest received, manifest URL handled at `chunkIndex=-1`
- [ ] 9.4 `upload_test.go`: chunk PUT 5xx → retry succeeds → push completes
- [ ] 9.5 `download_test.go`: byte-equal roundtrip on a small in-memory profile fixture
- [ ] 9.6 `download_test.go`: tampered chunk → SHA-256 mismatch → no plaintext written, error envelope
- [ ] 9.7 `download_test.go`: `to` exists without `--overwrite` → refuses
- [ ] 9.8 `backup_recover_test.go`: full flow → finalize transitions tokens, new DEK in keychain
- [ ] 9.9 `backup_test.go`: logout clears both refresh + DEK from keychain
- [ ] 9.10 `backup_storage_test.go`: extend with signup/recover/finalize routes; presigned R2 routes on a separate httptest server

## 10. Documentation

- [ ] 10.1 README "Hosted Backup" section: full command surface, end-to-end workflow example, env vars, recovery key warning
- [ ] 10.2 Engine doc.go for new `upload` and `download` packages
