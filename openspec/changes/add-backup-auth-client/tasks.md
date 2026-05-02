## 1. Error codes

- [x] 1.1 Add `AUTH_REQUIRED`, `SUBSCRIPTION_REQUIRED`, `NOT_FOUND`, `RATE_LIMITED`, `BACKEND_ERROR`, `BACKEND_UNREACHABLE`, `BACKEND_INCOMPATIBLE`, `STORAGE_QUOTA_EXCEEDED` constants in `go-engine/internal/envelope/errors.go`
- [x] 1.2 Extend `TestErrorCodeSerialization` in `envelope_test.go` to cover the new codes

## 2. Crypto stub package (interface only)

- [x] 2.1 Create `go-engine/internal/backup/crypto/` with stubbed `DeriveKeys`, `GenerateDEK`, `WrapDEK`, `UnwrapDEK`, `EncryptChunk`, `DecryptChunk`, `EncryptManifest`, `DecryptManifest`, `GenerateRecoveryKey`, `ParseRecoveryPhrase`, `DeriveRecoveryKey`, `RecoveryKeyVerifier`
- [x] 2.2 Each function returns `crypto.ErrNotImplemented` with a `// TODO(prompt-3)` marker
- [x] 2.3 Lock the contract constants in code: `DEKSize=32`, `SaltSize=16`, `NonceSize=12`, `GCMTagSize=16`, `ChunkPlainSize=4 MiB`, `EnvelopeVersion=1`, `ManifestAAD=0xFFFFFFFF`, `RecoveryMnemonicWords=24`
- [x] 2.4 `KDFParams` matches the contract object; `DefaultKDFParams()` returns the v1 values; `MeetsFloor()` enforces the engine's refusal to derive keys below the floor

## 3. Keychain wrapper

- [x] 3.1 Create `internal/backup/keychain/` with `Keychain` interface (`Store`, `Load`, `Delete`, `ErrNotFound`)
- [x] 3.2 Windows implementation via `github.com/danieljoos/wincred` (`keychain_windows.go`)
- [x] 3.3 Non-Windows stub (`keychain_other.go`) that errors — Endstate is Windows-first; no plaintext fallback
- [x] 3.4 Memory implementation for tests (`memory.go`) with defensive copies on `Store`
- [x] 3.5 `AccountForUser(userID)` helper produces stable `endstate-refresh-<userID>` account names

## 4. OIDC discovery + JWKS

- [x] 4.1 Create `internal/backup/oidc/` with `Client.Discovery(ctx)` and `Client.JWKS(ctx)`
- [x] 4.2 Discovery cached in-memory for `DiscoveryTTL` (1h); JWKS for `JWKSTTL` (15m)
- [x] 4.3 `validateDocument` rejects missing `endstate_extensions`, missing `argon2id`, missing envelope v1, weak KDF floor → `ErrIncompatibleIssuer`
- [x] 4.4 Issuer mismatch in returned `iss` field → distinct error (not `ErrIncompatibleIssuer`)
- [x] 4.5 `InvalidateJWKS()` triggers a refetch on next `JWKS(ctx)` call so JWT verifier can react to key rotation
- [x] 4.6 Unit tests cover cache TTL, missing-extension rejection, weak-KDF rejection, missing-EdDSA rejection, network-error propagation

## 5. HTTP client wrapper

- [x] 5.1 Create `internal/backup/client/` with `Client.Do(ctx, req, out)` returning `*envelope.Error`
- [x] 5.2 Bearer-token injection via the `TokenProvider` interface (`Anonymous{}` for unauth flows)
- [x] 5.3 JSON marshal/unmarshal with `Content-Type: application/json` and `Accept: application/json`
- [x] 5.4 `X-Endstate-API-Version` parsing on every response; major mismatch → `SCHEMA_INCOMPATIBLE` always; minor mismatch → warn-and-proceed for `ReadOnly` requests, `SCHEMA_INCOMPATIBLE` for writes
- [x] 5.5 Status → ErrorCode mapping: 401 → `AUTH_REQUIRED`, 402 → `SUBSCRIPTION_REQUIRED`, 404 → `NOT_FOUND`, 429 → `RATE_LIMITED`, 5xx → `BACKEND_ERROR`, transport → `BACKEND_UNREACHABLE`
- [x] 5.6 Retry policy: max 3 retries on 5xx + transport errors only; 4xx never retried; 429 honours `Retry-After` (delta-seconds form); exponential backoff with ±25% jitter capped at 8 s
- [x] 5.7 401 response triggers a single refresh-then-retry hop via `TokenProvider.RefreshAccessToken`; subsequent 401 returns `AUTH_REQUIRED`
- [x] 5.8 Substrate body code `STORAGE_QUOTA_EXCEEDED` (any 4xx/5xx) overrides the status-derived code
- [x] 5.9 Unit tests cover happy path, bearer injection, 401-refresh-retry, 401×2 → AUTH_REQUIRED, 402 → SUB_REQUIRED, 404 → NOT_FOUND, 429 retry-then-give-up, 5xx-then-200, 5xx-exhausted, 4xx-no-retry, version major/minor mismatch, transport-unreachable, body remediation passthrough, STORAGE_QUOTA_EXCEEDED from body

## 6. Auth orchestration

- [x] 6.1 Create `internal/backup/auth/` with `SessionStore` (in-memory + keychain persistence)
- [x] 6.2 `Authenticator` exposes `PreHandshake`, `CompleteLogin`, `Logout`, `Me`; refresh wired via `Session().WithRefreshFn`
- [x] 6.3 `PreHandshake` rejects KDF params below the v1 floor → `BACKEND_INCOMPATIBLE`
- [x] 6.4 `CompleteLogin` persists the refresh token to the keychain on success; persistence failure surfaces as `INTERNAL_ERROR` rather than silent loss
- [x] 6.5 `Logout` is best-effort against the backend; local state is wiped regardless of backend reachability
- [x] 6.6 `Me` updates the cached session from the authoritative `/api/account/me` response
- [x] 6.7 `Verify` (JWT verification) checks signature against JWKS, `iss`, `aud`, `exp`, `nbf` with 60s leeway; signature failures invalidate the JWKS cache so the next call refreshes keys
- [x] 6.8 Unit tests cover JWT happy path + expired + wrong-aud + wrong-iss + bad-signature + rotated-kid; httptest-driven Authenticator tests cover login persist, refresh rotation, logout-while-backend-down, Me, 404 on PreHandshake → `NOT_FOUND`

## 7. Top-level wiring

- [x] 7.1 Create `internal/backup/backup.go` with `IssuerURL()`, `Audience()`, `Concurrency()`, and `NewAuthenticator()` constructors honouring `ENDSTATE_OIDC_ISSUER_URL`, `ENDSTATE_OIDC_AUDIENCE`, `ENDSTATE_BACKUP_CONCURRENCY`
- [x] 7.2 Add `commands.BackupFlags` + `commands.RunBackup` dispatcher
- [x] 7.3 Add `commands.AccountFlags` + `commands.RunAccount` dispatcher (delete handler stubbed for the storage-client change)
- [x] 7.4 Implement `runBackupLogin`, `runBackupLogout`, `runBackupStatus`
- [x] 7.5 Stdin-only passphrase reader; `WithPassphraseReader` test seam
- [x] 7.6 Wire `backup` and `account` cases into `cmd/endstate/main.go` `dispatch()`
- [x] 7.7 Add new flags (`--email`, `--backup-id`, `--version-id`, `--to`, `--confirm`) to `parseArgs()`
- [x] 7.8 Add `backup` and `account` blurbs to `commandUsage()` and `usageText`
- [x] 7.9 Update `commands/capabilities.go` to advertise `hostedBackup` and register `backup` / `account` commands

## 8. Tests

- [x] 8.1 `backup_test.go` covers signed-out status, signed-in status, login no-email, login empty-passphrase, login pre-handshake-OK-then-crypto-stub-blocks, login backend-unreachable, logout idempotent, logout clears keychain, runbackup-no-subcommand, runaccount-delete-stubbed, capabilities advertises hostedBackup with env-overridden values
- [x] 8.2 `go test ./...` green; `go vet ./...` clean

## 9. Documentation

- [ ] 9.1 Add "Hosted Backup" section to `README.md` describing env vars + command tree + the PR-2/PR-3 caveats
