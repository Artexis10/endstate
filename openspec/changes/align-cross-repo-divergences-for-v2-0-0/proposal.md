## Why

A 2026-05-09 cross-repo audit (engine ↔ substrate ↔ contract) found one P0 break and three P1 divergences that block the upcoming GUI hosted-backup work (Prompt 4):

- **Recovery flow broken end-to-end.** Engine and substrate independently implemented incompatible shapes for `POST /api/auth/recover/finalize`. The contract was silent on the exact wire format, so each side picked something. Substrate expected `{ recoveryToken, newServerPassword, newSalt, newKdfParams, newWrappedDEK }` in the body; engine sent `{ email, serverPassword, salt, kdfParams, wrappedDEK, recoveryKeyProof }`. Every recover attempt returned `BAD_REQUEST: recoveryToken is required`. The flow had never been smoke-tested end-to-end.
- **`backup_api_base` from OIDC discovery was advertised but ignored.** The engine constructed `${issuer}/api/backups` directly. Self-hosters who relocate the backup API have no way to do it through the configured channel.
- **Issuer claim mismatch silently surfaced as `BACKEND_UNREACHABLE`.** When `ENDSTATE_OIDC_ISSUER_URL` disagrees between engine and substrate (a common deployment mistake), the engine's strict-equality check produced an inscrutable transport-error envelope. There was no specific error code for the misconfiguration.
- **Engine version below contract floor.** Contract §11 says backup commands require `engineVersion >= 2.0.0`. Engine shipped them at 1.7.x.
- **DELETE-subscription gating ambiguous.** Substrate's `requireReadAccess` allowed DELETE in `cancelled`/`grace` states; contract said writes are blocked. Either the contract or substrate was wrong — needed alignment.

## What Changes

This is a coordinated cross-repo change that ships engine + substrate together. Substrate v2.0 schema must precede or land with engine v2.0.0 binary; old engines and old substrates cannot interoperate after merge (breaking change per contract §13).

### Recovery flow rewrite (contract §6 v2.0)

- Step 3 (`POST /api/auth/recover`) returns `{ recoveryToken, recoveryKeyWrappedDEK, ttlSeconds }` (was `{ recoveryToken?, recoveryKeyWrappedDEK }` divergent).
- Step 7 (`POST /api/auth/recover/finalize`) takes `Authorization: Bearer <recoveryToken>` and a body of `{ newServerPassword, newSalt, newKdfParams, newWrappedDEK }`.
- Recovery tokens are single-use; substrate burns them via the new `recovery_tokens_used` table (PK on jti is the atomic guard).
- TTL is 10 minutes (`RECOVERY_TOKEN_TTL_S = 600`).
- Response includes `userId` and `subscriptionStatus` so the engine doesn't need a follow-up `/me` call.

### `backup_api_base` honored (contract §9)

- `storage.Client` resolves the base URL from `endstate_extensions.backup_api_base` per request (cached via the OIDC client's existing 1-hour cache).
- Falls back to `${issuer}/api/backups` if the field is missing — preserves backward compatibility against legacy backends.

### Loud issuer-claim mismatch (contract §9)

- New `oidc.ErrIssuerMismatch` sentinel.
- `mapDiscoveryError` recognizes it and returns `BACKEND_INCOMPATIBLE` with remediation about `ENDSTATE_OIDC_ISSUER_URL` agreement on both sides.

### DELETE exempt from write-block (contract §10)

- Codifies substrate's existing `requireReadAccess` behavior on DELETE endpoints. Three reasons spelled out in the contract: GDPR, grace billing UX, fairness during cancelled retention window.

### Engine v2.0.0 + schema v2.0

- `VERSION` 1.7.5 → 2.0.0
- `EngineSchemaMajor` 1 → 2
- `X-Endstate-API-Version: 2.0` on every substrate response (`SchemaVersion` const bumped)

## Capabilities

### Modified Capabilities

- `hosted-backup-recovery-flow` — wire format finalized; recoveryToken bearer-borne; single-use semantics
- `hosted-backup-self-host` — backup_api_base now consumed; issuer-mismatch produces actionable remediation; DELETE-not-gated codified
- `hosted-backup-version-compatibility` — apiSchemaVersion bumps to 2.0; engine major bumps to 2

### New Capabilities

- `hosted-backup-recovery-token-replay-protection` — single-use enforcement via append-only `recovery_tokens_used` table

## Impact

### Engine

- `docs/contracts/hosted-backup-contract.md` — §6, §9, §10, §11, header, changelog
- `go-engine/internal/backup/auth/authenticator.go` — RecoverResponse, RecoverFinalizeBody, RecoverFinalize signature; mapDiscoveryError ErrIssuerMismatch branch
- `go-engine/internal/commands/backup_recover.go` — orchestration rewritten for new shapes; fresh-salt generation
- `go-engine/internal/backup/storage/storage.go` — Client takes oidc.Client; backupBaseURL resolution per-request
- `go-engine/internal/backup/backup.go` — pass oc into storage.New
- `go-engine/internal/backup/oidc/oidc.go` — ErrIssuerMismatch sentinel; validateDocument wraps it via fmt.Errorf %w
- `go-engine/internal/backup/client/version.go` — EngineSchemaMajor 1 → 2
- `VERSION` — 1.7.5 → 2.0.0
- Tests: backup_orchestration_test.go (recover flow assertions), oidc_test.go (issuer mismatch + custom backup_api_base), storage_test.go (new — backup_api_base honored + fallback), client_test.go (version-mismatch test cases use new mismatch values)
- 23 test files mass-updated their stamped `X-Endstate-API-Version: "1.0"` → `"2.0"`

### Substrate

- `migrations/0011_recovery_tokens_used.sql` — new replay-protection table
- `src/lib/hosted-backup/jwt.ts` — RECOVERY_TOKEN_TTL_S 300 → 600; verifyRecoveryToken returns jti
- `src/lib/hosted-backup/types.ts` — SchemaVersion 1.0 → 2.0; RecoverResponse +ttlSeconds; RecoverFinalizeRequest -recoveryToken; RecoverFinalizeResponse +userId/+subscriptionStatus
- `src/lib/hosted-backup/db.ts` — new markRecoveryTokenUsed
- `src/app/api/auth/recover/route.ts` — include ttlSeconds
- `src/app/api/auth/recover/finalize/route.ts` — bearer header parsing; token invalidation; richer response
- Tests: recover-route.test.ts, recover-finalize.test.ts (new); two existing tests updated for the SchemaVersion bump

### Coordination

- Substrate ships first (so the engine has a backend to talk to during the smoke test)
- Engine commit message uses `BREAKING CHANGE:` so release-please mints v2.0.0
- Single PR per repo, paired branch name: `align-cross-repo-divergences-for-v2-0-0`

### What this does NOT change

- Push/pull/list/versions/delete/account-delete/login/signup/logout flows are untouched
- Encryption envelope (manifest, chunks, AAD sentinels) — contract §3 unchanged
- KDF parameters (contract §2) unchanged
- JWT format (contract §4) unchanged
- Storage layout (contract §8) unchanged
