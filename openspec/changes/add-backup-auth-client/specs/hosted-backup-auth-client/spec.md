## ADDED Requirements

### Requirement: Engine Honours Two-Var OIDC Configuration

The engine SHALL read its hosted-backup backend configuration from exactly two environment variables: `ENDSTATE_OIDC_ISSUER_URL` and `ENDSTATE_OIDC_AUDIENCE`. Both have defaults pointing at Endstate Cloud.

#### Scenario: Defaults applied when env vars unset

- **WHEN** the engine reads its backup configuration with no env vars set
- **THEN** `ENDSTATE_OIDC_ISSUER_URL` SHALL default to `https://substratesystems.io`
- **AND** `ENDSTATE_OIDC_AUDIENCE` SHALL default to `endstate-backup`

#### Scenario: Self-host configuration overrides defaults

- **WHEN** `ENDSTATE_OIDC_ISSUER_URL` is set to a self-host URL
- **THEN** the engine SHALL fetch discovery from `${ISSUER}/.well-known/openid-configuration`
- **AND** SHALL refuse the backend if `endstate_extensions` is missing or its `min_kdf_params` is below the v1 floor

### Requirement: OIDC Discovery Caching

The engine SHALL cache the OIDC discovery document for one hour and the JWKS for fifteen minutes. Cache invalidation MUST be available so a JWT signature failure can trigger a JWKS refresh without waiting out the TTL.

#### Scenario: Discovery served from cache within TTL

- **WHEN** the engine fetches discovery a second time within one hour of the first fetch
- **THEN** the engine SHALL serve the cached document
- **AND** SHALL NOT issue a network request

#### Scenario: JWKS refreshed on signature failure

- **WHEN** an access-token signature verification fails
- **THEN** the engine SHALL invalidate its JWKS cache
- **AND** the next access-token verification SHALL fetch the JWKS afresh

### Requirement: EdDSA-Only JWT Verification

Access tokens SHALL be verified with EdDSA (Ed25519) per RFC 8032/8037. Tokens signed with any other algorithm SHALL be rejected.

#### Scenario: HMAC token rejected

- **WHEN** an access token signed with HMAC-SHA256 is presented
- **THEN** the engine SHALL refuse to verify the token
- **AND** SHALL return an authentication error

#### Scenario: Audience and issuer enforced

- **WHEN** an access token has the wrong `aud` or `iss` claim
- **THEN** verification SHALL fail
- **AND** the engine SHALL return an authentication error without using the token's claims

### Requirement: Refresh Token Persisted in Windows Credential Manager Only

The refresh token SHALL be stored in Windows Credential Manager via a single named entry of the form `endstate-refresh-<userID>`. The engine SHALL NOT write the refresh token to a plaintext file as fallback.

#### Scenario: Refresh token survives engine restart

- **WHEN** the engine logs in successfully and exits
- **AND** is invoked again
- **THEN** the engine SHALL retrieve the refresh token from Credential Manager
- **AND** the user SHALL NOT be prompted to log in again

#### Scenario: Refresh token rotated atomically

- **WHEN** `/api/auth/refresh` returns a new access + refresh token pair
- **THEN** the engine SHALL update both the in-memory session and the Credential Manager entry
- **AND** the previous refresh token SHALL no longer be retrievable

#### Scenario: Logout clears Credential Manager regardless of backend reachability

- **WHEN** `endstate backup logout` is invoked while the backend returns 5xx
- **THEN** the engine SHALL still delete the Credential Manager entry
- **AND** SHALL still report `signedOut: true`

### Requirement: Stdin-Only Secret Input

The engine SHALL read passphrases and recovery keys from stdin only. Accepting them as CLI flags is forbidden because that would expose secrets in shell history and process listings.

#### Scenario: Passphrase flag rejected

- **WHEN** a CLI invocation includes `--passphrase` or any equivalent flag
- **THEN** the engine SHALL NOT accept the value
- **AND** SHALL prompt the user (via stdin) for the passphrase

### Requirement: HTTP Status Mapped to Stable Engine Error Codes

Every backend response SHALL be translated to an engine error code suitable for inclusion in the standard JSON envelope. The mapping is fixed.

#### Scenario: 401 maps to AUTH_REQUIRED after one refresh attempt

- **WHEN** a backend response status is 401 Unauthorized
- **THEN** the engine SHALL attempt to refresh the access token once
- **AND** SHALL retry the original request
- **AND** if the retry also returns 401, the engine SHALL return `AUTH_REQUIRED`

#### Scenario: Status code mapping table

- **WHEN** the backend returns a non-2xx response
- **THEN** the engine SHALL map the status to one of: `AUTH_REQUIRED` (401), `SUBSCRIPTION_REQUIRED` (402), `NOT_FOUND` (404), `RATE_LIMITED` (429), `BACKEND_ERROR` (5xx and unmapped 4xx)
- **AND** transport / DNS / timeout errors SHALL map to `BACKEND_UNREACHABLE`

#### Scenario: Substrate error envelope passes remediation through

- **WHEN** the backend returns a non-2xx response with a body matching the standard error envelope
- **THEN** the engine SHALL surface the body's `remediation` and `docsKey` verbatim
- **AND** SHALL fall back to engine defaults only when the backend omits them

### Requirement: Bounded Retry on Transient Errors

The engine SHALL retry transient backend failures up to three times with exponential backoff and jitter. 4xx responses (except 429) SHALL NOT be retried.

#### Scenario: 5xx retried up to three times

- **WHEN** a backend request returns 5xx
- **THEN** the engine SHALL retry up to three additional times
- **AND** SHALL apply exponential backoff with ±25% jitter capped at 8 seconds

#### Scenario: 429 honours Retry-After

- **WHEN** a backend response has status 429 with a `Retry-After: <seconds>` header
- **THEN** the engine SHALL wait at least the indicated duration before retrying
- **AND** SHALL still respect the maximum-retries limit

#### Scenario: 4xx never retried

- **WHEN** a backend response status is 400, 403, 404, or any 4xx other than 429
- **THEN** the engine SHALL surface the mapped error code immediately without retrying

### Requirement: Capabilities Feature Flag

The engine's capabilities response SHALL advertise a `hostedBackup` feature block populated from the resolved OIDC configuration so the GUI can gate hosted-backup UI surfaces accordingly.

#### Scenario: Capabilities advertises issuer and audience

- **WHEN** the GUI queries `endstate capabilities --json`
- **THEN** `data.features.hostedBackup` SHALL be present with `supported: true`
- **AND** `issuerUrl` and `audience` SHALL reflect the active env-var configuration
- **AND** `minSchemaVersion` SHALL be `"1.0"`
