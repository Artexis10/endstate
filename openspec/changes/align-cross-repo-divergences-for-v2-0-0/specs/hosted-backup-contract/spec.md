## ADDED Requirements

### Requirement: Recovery Flow Wire Format (v2.0)

The recovery flow's two-step exchange SHALL use a bearer-borne `recoveryToken` rather than carrying credentials in the request body.

`POST /api/auth/recover` SHALL return `{ recoveryToken, recoveryKeyWrappedDEK, ttlSeconds }`. The `recoveryToken` is single-use, audience-bound, and is the bearer credential for the subsequent `/finalize` call.

`POST /api/auth/recover/finalize` SHALL require `Authorization: Bearer <recoveryToken>` and a body of `{ newServerPassword, newSalt, newKdfParams, newWrappedDEK }`. On success, the server SHALL atomically update the password hash and `wrappedDEK`, invalidate the recoveryToken, and return `{ userId, accessToken, refreshToken, subscriptionStatus }`.

The recoveryToken's `ttlSeconds` field is advisory; the server is authoritative for expiry.

#### Scenario: missing Authorization header → 401

- **WHEN** a client posts to `/api/auth/recover/finalize` without an `Authorization` header
- **THEN** the server SHALL respond `401 UNAUTHENTICATED`
- **AND** SHALL NOT alter user credentials or revoke refresh chains

#### Scenario: non-Bearer scheme → 401

- **WHEN** a client posts with `Authorization: Basic ...` (or any non-`Bearer` scheme)
- **THEN** the server SHALL respond `401 UNAUTHENTICATED`

#### Scenario: replay of a consumed recoveryToken → RECOVERY_TOKEN_EXPIRED

- **WHEN** a recoveryToken has already been used in a successful finalize call
- **THEN** any subsequent finalize call presenting the same token SHALL respond `401 RECOVERY_TOKEN_EXPIRED`
- **AND** SHALL NOT alter user credentials

#### Scenario: happy path returns rich response

- **WHEN** a client posts a valid bearer token + body
- **THEN** the server SHALL return `{ userId, accessToken, refreshToken, subscriptionStatus }`
- **AND** the client SHALL be able to populate its session without a follow-up `/me` call

### Requirement: Engine Honors `backup_api_base` From OIDC Discovery

The engine's storage client SHALL resolve the base URL for `/api/backups/*` calls from the `endstate_extensions.backup_api_base` field of the OIDC discovery document. When discovery is unavailable or the field is empty, the engine SHALL fall back to `${issuer}/api/backups`.

#### Scenario: custom backup_api_base is consumed

- **WHEN** the discovery doc advertises `backup_api_base: "https://files.example.com/v1/backups"`
- **THEN** subsequent `ListBackups`, `CreateBackup`, etc. calls SHALL hit `https://files.example.com/v1/backups/*`

#### Scenario: missing backup_api_base falls back to issuer

- **WHEN** the discovery doc omits or empties the field
- **THEN** the engine SHALL hit `${issuer}/api/backups/*` instead of failing

### Requirement: Issuer Claim Mismatch Surfaces BACKEND_INCOMPATIBLE

The engine SHALL emit `BACKEND_INCOMPATIBLE` (not `BACKEND_UNREACHABLE`) when the OIDC discovery document's `issuer` claim does not match the configured `ENDSTATE_OIDC_ISSUER_URL`. The remediation message SHALL point at the env-var disagreement between engine and substrate sides.

#### Scenario: issuer mismatch produces specific code + remediation

- **WHEN** the engine fetches discovery and `doc.issuer != ENDSTATE_OIDC_ISSUER_URL`
- **THEN** Discovery SHALL return an error wrapping `oidc.ErrIssuerMismatch`
- **AND** the auth package SHALL map it to `BACKEND_INCOMPATIBLE` with remediation about env-var agreement

### Requirement: DELETE Endpoints Are Not Subscription-Gated

`DELETE /api/backups/:backupId` and `DELETE /api/backups/:backupId/versions/:versionId` SHALL accept calls from any authenticated user in any non-`none` subscription state. The write-block rule that applies to other write endpoints (`grace`, `cancelled` blocked) does NOT apply to DELETE.

#### Scenario: cancelled user can DELETE their own backup

- **WHEN** a user in `subscriptionStatus = cancelled` calls `DELETE /api/backups/<their-backup>`
- **THEN** the server SHALL allow the call
- **AND** SHALL NOT respond `SUBSCRIPTION_REQUIRED`

#### Scenario: grace user can delete a version

- **WHEN** a user in `subscriptionStatus = grace` calls `DELETE /api/backups/<id>/versions/<vid>`
- **THEN** the server SHALL allow the call

### Requirement: Engine Binary Version Aligned With Contract Floor

The engine binary SHALL ship with `VERSION` `>= 2.0.0` for any release that includes the `backup` subcommand tree. The `EngineSchemaMajor` constant SHALL be `2`, matching `apiSchemaVersion = 2.0` advertised by substrate.

#### Scenario: binary version meets §11 floor

- **WHEN** a GUI capability check reads `cliVersion`
- **THEN** for any engine that exposes `backup *` subcommands, `cliVersion` SHALL be `>= 2.0.0`

### Requirement: Recovery Token Single-Use Replay Protection

The substrate SHALL invalidate a `recoveryToken` on successful `/finalize` and SHALL reject replays.

#### Scenario: append-only ledger catches replay

- **WHEN** a `/finalize` call succeeds for a given jti
- **THEN** the substrate SHALL record the jti in `recovery_tokens_used`
- **AND** any subsequent `/finalize` call presenting the same jti SHALL fail with `RECOVERY_TOKEN_EXPIRED` due to the primary-key collision

#### Scenario: concurrent finalize calls are race-safe

- **WHEN** two `/finalize` requests with the same recoveryToken arrive concurrently
- **THEN** at most one SHALL succeed
- **AND** the other SHALL fail with `RECOVERY_TOKEN_EXPIRED` rather than producing inconsistent credential state

### Requirement: Recovery Token TTL of 600 Seconds

The substrate SHALL mint recoveryTokens with a 600-second (10-minute) TTL. The TTL SHALL be exposed to clients in the `/api/auth/recover` response as `ttlSeconds` for UI display purposes; the server is authoritative for expiry enforcement.

#### Scenario: TTL surfaced in response

- **WHEN** a client posts a valid `/api/auth/recover` request
- **THEN** the response SHALL include `ttlSeconds: 600`
- **AND** the recoveryToken's JWT `exp` claim SHALL be `iat + 600`
