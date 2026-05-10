## Design notes

This change resolves the cross-repo divergences from the 2026-05-09 audit (`/tmp/cross-repo-audit-2026-05-09.md` on the author's machine; the report was a one-shot artifact, not committed). It is the prerequisite for Prompt 4 (the GUI hosted-backup wiring).

### Why bearer-header instead of body for recoveryToken

The recover-finalize flow had three viable shapes:

1. **Bearer header (chosen).** Matches the access-token convention everywhere else in the API; lets the body carry only the new passphrase-derived material; the substrate's existing `verifyAccessToken(token, { audience })` machinery is reusable verbatim by aliasing as `verifyRecoveryToken`. Single-use enforcement is layered on top via the new `recovery_tokens_used` table.
2. **Body field.** Substrate's pre-v2 shape. Compatible with the engine's audit-time wire format. But every other authenticated route uses a bearer header; carrying a one-shot token in the body for a single endpoint is a special case for no real benefit.
3. **Hybrid (header + body fallback).** Adds permanent dual-path code in substrate. Worth it only if engine and substrate ship out of lockstep, which they don't here.

The user picked option 1 explicitly during plan-mode clarification; the contract previously hadn't taken a side because the flow had never been smoke-tested.

### Why the recovery-token TTL is 10 minutes

5 minutes (substrate's pre-v2 default) is tight for a workflow that includes deriving Argon2id (~1s on slow hardware) + typing a new passphrase + re-deriving Argon2id from the new passphrase. 15 minutes is overgenerous for a single-shot operation. 10 minutes balances those concerns and matches the principle that the token's window is "as short as is comfortable" rather than "as long as a session."

The TTL is exposed in the response as `ttlSeconds` (advisory, not load-bearing — server is authoritative). This lets the GUI surface a "you have N minutes" hint without parsing the JWT internals.

### Why a separate `recovery_tokens_used` table for replay protection

Two designs were considered:

1. **`recovery_tokens_used` table** (chosen) — append-only, jti as PRIMARY KEY. INSERT collides on a replay → catch unique-violation → reject. Concurrent finalize calls also collide. Cleanup of expired rows is a future GC concern but not load-bearing for correctness.
2. **`recovery_token_jti_used` column on auth_credentials** — single column update. Simpler schema. But replays of OLD tokens (jti1) after a new recovery (jti2) would WRITE jti1 over jti2 if the WHERE clause was permissive, or fail to detect replay if too restrictive. The race is subtle.

The append-only table is foolproof: the PK constraint is the atomic guard, no clever WHERE clauses needed. One extra table is cheap insurance.

### Why we honor `backup_api_base` from discovery rather than just hardcoding

The engine's pre-v2 hardcoded `${issuer}/api/backups` worked for Endstate Cloud because that's exactly what substrate's discovery doc advertised. But the contract §9 told self-hosters they could relocate the backup API. Having a documented field that is silently ignored is a hollow promise. The fix is mechanical: thread the OIDC client into the storage client, look up `backup_api_base` per request (cached via the existing 1-hour OIDC cache), fall back to the hardcoded path on missing field for backward compat with legacy substrate builds.

### Why the issuer-mismatch error is now `BACKEND_INCOMPATIBLE` with specific remediation

The pre-v2 code path produced a generic transport-error envelope (`BACKEND_UNREACHABLE`) when the discovery doc's issuer claim didn't match the configured `ENDSTATE_OIDC_ISSUER_URL`. The actual cause is almost always env-var disagreement between the engine side and the substrate side; surfacing it as a transport error sends the user diagnosing network issues that don't exist. The `ErrIssuerMismatch` sentinel + dedicated remediation closes that diagnostic loop.

### Why DELETE is exempt from the write-block

The contract's pre-v2 §10 said writes are blocked in `cancelled` and `grace`. Substrate disagreed in code by using `requireReadAccess` for both DELETE endpoints. Two reasons to align with substrate's behavior rather than its documentation:

1. GDPR's user-controlled-deletion principle outweighs the storage-billing rationale that motivates blocking writes during lapse.
2. A user in `cancelled` is on a 30-day countdown to data purge anyway. Forcing them to re-subscribe to delete is hostile.

The contract gets a new subsection in §10 documenting this exception. Substrate code stays as it is.

### Why engine v2.0.0 and apiSchemaVersion 2.0 are bumped together

The v2.0 wire format change is a breaking auth-flow shape change (contract §13 enumerated this category). The contract had previously carried language saying backup commands required `engineVersion >= 2.0.0`, but the engine shipped them at 1.7.x. This change aligns the binary version with the contract floor and makes the API schema version match. After this lands, engines and substrates are strictly major-paired — no v1 engine can talk to a v2 substrate or vice versa.

### Open question for the GUI work (Prompt 4)

The GUI's `endstate-gui/ENGINE_VERSION` is `1.8.0` and `cli-bridge.ts` uses `MIN/MAX/REQUIRED_SCHEMA_VERSION = '1.0'` for the CLI envelope schema (separate axis from hosted-backup `apiSchemaVersion`). The CLI envelope schema does NOT change in this PR — only the hosted-backup HTTP API schema does. The GUI's bundled engine pin needs bumping to 2.0.0 in a separate change before the GUI hosted-backup commands can talk to a v2 substrate.

This is out of scope for this PR but should be the first issue filed in the GUI repo immediately after the engine PR merges.
