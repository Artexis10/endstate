## 1. Substrate — recovery wire format

- [x] 1.1 Migration `0011_recovery_tokens_used.sql` (jti PK, FK on user_id, used_at default now())
- [x] 1.2 `jwt.ts` — RECOVERY_TOKEN_TTL_S 300 → 600; `verifyRecoveryToken` returns `{ userId, jti }`
- [x] 1.3 `db.ts` — `markRecoveryTokenUsed({ jti, userId })` returns boolean (false on PK collision)
- [x] 1.4 `types.ts` — RecoverResponse adds `ttlSeconds`; RecoverFinalizeRequest drops `recoveryToken`; RecoverFinalizeResponse adds `userId` + `subscriptionStatus`
- [x] 1.5 `recover/route.ts` — include `ttlSeconds` in response
- [x] 1.6 `recover/finalize/route.ts` — extract Authorization: Bearer; verify; mark token used (atomic); return rich response
- [x] 1.7 `types.ts` — SchemaVersion `1.0` → `2.0`

## 2. Substrate — tests

- [x] 2.1 `recover-finalize.test.ts` (new) — missing/wrong/empty header → 401; happy path → 200 + full body; replay → 401 RECOVERY_TOKEN_EXPIRED; body validation
- [x] 2.2 `recover-route.test.ts` (new) — response includes ttlSeconds=600
- [x] 2.3 Existing tests updated for X-Endstate-API-Version `1.0` → `2.0`

## 3. Engine — recovery wire format

- [x] 3.1 `authenticator.go` — RecoverResponse adds RecoveryToken + TTLSeconds, drops Salt
- [x] 3.2 `authenticator.go` — RecoverFinalizeBody renamed to NewServerPassword/NewSalt/NewKDFParams/NewWrappedDEK
- [x] 3.3 `authenticator.go` — RecoverFinalize signature accepts (ctx, recoveryToken, email, body); threads bearer via http.Header; SkipAuthRefresh=true
- [x] 3.4 `backup_recover.go` — capture RecoveryToken from step 3; generate fresh salt; build new finalize body; pass token + email
- [x] 3.5 `backup_recover.go` — drop salt-rotation handling (no longer in response)

## 4. Engine — `backup_api_base`

- [x] 4.1 `storage.go` — Client struct adds `oc *oidc.Client` field
- [x] 4.2 `storage.go` — `New(issuer, oc, hc)` signature
- [x] 4.3 `storage.go` — `backupBaseURL(ctx)` helper; consults discovery, falls back to `${issuer}/api/backups`
- [x] 4.4 `storage.go` — all `c.url(suffix)` call sites threaded with ctx
- [x] 4.5 `backup.go` — pass oc into storage.New

## 5. Engine — issuer mismatch

- [x] 5.1 `oidc.go` — `ErrIssuerMismatch` sentinel
- [x] 5.2 `oidc.go` — `validateDocument` wraps issuer-equality error via `fmt.Errorf("%w: ...", ErrIssuerMismatch, ...)`
- [x] 5.3 `authenticator.go` — `mapDiscoveryError` recognizes `errors.Is(err, oidc.ErrIssuerMismatch)` and returns BACKEND_INCOMPATIBLE with remediation

## 6. Engine — versioning

- [x] 6.1 `VERSION` — 1.7.5 → 2.0.0
- [x] 6.2 `client/version.go` — `EngineSchemaMajor` 1 → 2
- [x] 6.3 All test mocks: `X-Endstate-API-Version: "1.0"` → `"2.0"` (23 sites)
- [x] 6.4 `client_test.go` version-mismatch tests use `3.0` (major) and `2.5` (minor)

## 7. Engine — tests

- [x] 7.1 `backup_orchestration_test.go` — recover mock returns `recoveryToken`/`ttlSeconds`; finalize mock asserts Authorization: Bearer + body shape
- [x] 7.2 `oidc_test.go` — `TestDiscovery_IssuerMismatch_ReturnsErrIssuerMismatch`; `TestDiscovery_BackupAPIBase_PassesThroughCustomURL`
- [x] 7.3 `storage_test.go` (new) — `TestListBackups_HonorsBackupAPIBase`; `TestListBackups_FallsBackToIssuerWhenBackupAPIBaseMissing`

## 8. Contract

- [x] 8.1 §6 rewritten for bearer-header recovery flow + recovery token semantics paragraph
- [x] 8.2 §9 — `backup_api_base` and issuer-claim equality clarifications
- [x] 8.3 §10 — DELETE-not-gated subsection added
- [x] 8.4 §11 — apiSchemaVersion `1.0` → `2.0`; X-Endstate-API-Version response stamp updated
- [x] 8.5 Header — Schema Version `1.0` → `2.0`; Last Updated `2026-05-10`
- [x] 8.6 Changelog — v2.0 entry summarizing breaking changes

## 9. Verification

- [ ] 9.1 `go test ./...` from `go-engine/` — runs in CI; locally requires Go toolchain (not available on author's machine)
- [x] 9.2 `npm test` from substrate — 87 tests pass (was 87, two existing tests updated for new SchemaVersion)
- [ ] 9.3 OpenSpec validate (`npm run openspec:validate` from engine root)
- [ ] 9.4 Lefthook pre-push runs both
- [ ] 9.5 Smoke test: signup → push → pull (byte-equal diff) → recover → login with new passphrase → push (post-merge, blocks Prompt 4)

## 10. Release coordination

- [ ] 10.1 Open substrate PR first
- [ ] 10.2 Open engine PR with `BREAKING CHANGE:` in commit so release-please mints v2.0.0
- [ ] 10.3 Land substrate, then engine, then run smoke test
