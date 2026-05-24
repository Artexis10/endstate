## ADDED Requirements

### Requirement: Anonymous-buyer credential claim subcommand

The engine SHALL provide an `endstate backup claim --token <token>
--save-recovery-to <path>` subcommand that attaches passphrase-derived
credentials to a Hosted Backup pre-account identified by a single-use
bearer claim token. The passphrase MUST be read from stdin (line 1).
An optional caller-supplied 24-word BIP39 recovery mnemonic MAY be
provided on line 2 of stdin, in which case the engine SHALL use it
instead of generating one; this mirrors `backup signup`'s stdin
protocol exactly.

The HTTP call SHALL be `POST <claim-endpoint>` with
`Authorization: Bearer <token>` and a request body equal to the
`backup signup` request body MINUS the `email` field — substrate
identifies the user from the bearer's claim-token row.

The credentials block (`serverPassword`, `salt`, `kdfParams`,
`wrappedDEK`, `recoveryKeyVerifier`, `recoveryKeyWrappedDEK`) SHALL
be derived using the same primitives `backup signup` uses
(`crypto.DeriveKeys`, `crypto.WrapDEK`, `crypto.DeriveRecoveryKey`,
`crypto.RecoveryKeyVerifier`) with the same `crypto.DefaultKDFParams`.

The recovery file at `--save-recovery-to` SHALL be written before any
network call (contract §1 — load-bearing for the user-has-the-key
guarantee). If the write fails, the engine SHALL return
`INTERNAL_ERROR` and SHALL NOT issue the HTTP request.

On a successful 200 response, the engine SHALL:

1. Update the session via `session.SetTokens(userID, lower(email),
   accessToken, refreshToken, subscriptionStatus, accessExpiry)`,
   where `email` is the server-supplied value from the response body
   (NOT a value the engine constructed locally).
2. Persist the refresh token to the OS keychain via
   `session.Persist()`.
3. Cache the unwrapped DEK and the masterKey-wrapped DEK in the
   keychain via `session.StoreDEK(dek)` and
   `session.StoreWrappedDEK(wrappedDEK)`.
4. Best-effort zero the local DEK copy.
5. Emit a `SignupResult` envelope with the server-supplied email
   lowercased.

#### Scenario: Successful claim attaches credentials and returns a session

- **GIVEN** substrate has a `claim_tokens` row for the supplied
  43-char URL-safe base64 token, and a passphrase is piped to stdin
- **WHEN** the user runs `endstate backup claim --token <t>
  --save-recovery-to <p>`
- **THEN** the engine SHALL derive serverPassword, masterKey,
  recoveryKey, and wrappedDEK from the passphrase exactly as
  `backup signup` does
- **AND** SHALL write the 24-word recovery mnemonic to `<p>` BEFORE
  any network call
- **AND** SHALL `POST <claim-endpoint>` with `Authorization: Bearer
  <t>` and a body that omits the `email` field
- **AND** on HTTP 200 SHALL persist the refresh token + DEK +
  wrappedDEK to the OS keychain
- **AND** SHALL return `{ userId, email, subscriptionStatus,
  recoveryKeySavedTo }` where `email` is the value substrate
  returned, lowercased

#### Scenario: Recovery file write failure aborts before the network call

- **GIVEN** the path supplied to `--save-recovery-to` cannot be
  written (parent directory missing on a read-only filesystem, or
  permissions deny)
- **WHEN** the user runs `endstate backup claim`
- **THEN** the engine SHALL return `INTERNAL_ERROR` with a
  remediation that suggests choosing a writable path
- **AND** SHALL NOT call `POST <claim-endpoint>`

#### Scenario: Malformed token rejected before the network call

- **WHEN** `--token` is empty, has a length other than 43
  characters, or contains any character outside `[A-Za-z0-9_-]`
- **THEN** the engine SHALL return `INTERNAL_ERROR` with a
  remediation pointing the user back to the claim link in their
  email
- **AND** SHALL NOT call `POST <claim-endpoint>`

### Requirement: Substrate claim error codes surface verbatim

The engine SHALL surface substrate's claim-flow error codes —
`CLAIM_TOKEN_INVALID`, `CLAIM_TOKEN_EXPIRED`, `CLAIM_TOKEN_CONSUMED`,
and `KDF_TOO_WEAK` — in `envelope.error.code` verbatim (uppercased).
The engine SHALL NOT translate or replace these codes with a generic
engine-native code such as `BACKEND_ERROR` or `AUTH_REQUIRED`. The
GUI's `friendlyAuthError` map switches on the wire-string code, so
verbatim passthrough is the contract.

#### Scenario: 401 with CLAIM_TOKEN_INVALID surfaces verbatim

- **GIVEN** substrate returns HTTP 401 with body `{"error":{"code":
  "CLAIM_TOKEN_INVALID","message":"..."}}`
- **WHEN** the engine processes the response
- **THEN** the returned envelope error's `code` field SHALL equal
  the string `"CLAIM_TOKEN_INVALID"`

#### Scenario: 401 with CLAIM_TOKEN_EXPIRED surfaces verbatim

- **GIVEN** substrate returns HTTP 401 with body `{"error":{"code":
  "CLAIM_TOKEN_EXPIRED","message":"..."}}`
- **WHEN** the engine processes the response
- **THEN** the returned envelope error's `code` field SHALL equal
  the string `"CLAIM_TOKEN_EXPIRED"`

#### Scenario: 409 with CLAIM_TOKEN_CONSUMED surfaces verbatim

- **GIVEN** substrate returns HTTP 409 with body `{"error":{"code":
  "CLAIM_TOKEN_CONSUMED","message":"..."}}`
- **WHEN** the engine processes the response
- **THEN** the returned envelope error's `code` field SHALL equal
  the string `"CLAIM_TOKEN_CONSUMED"`

#### Scenario: 400 with KDF_TOO_WEAK surfaces verbatim

- **GIVEN** substrate returns HTTP 400 with body `{"error":{"code":
  "KDF_TOO_WEAK","message":"..."}}`
- **WHEN** the engine processes the response
- **THEN** the returned envelope error's `code` field SHALL equal
  the string `"KDF_TOO_WEAK"`

### Requirement: Optional discovery field for the claim endpoint

The engine SHALL prefer
`endstate_extensions.auth_claim_endpoint` from the OIDC discovery
document when the field is present and non-empty, and SHALL fall
back to `<issuer>/api/auth/claim` when the field is absent or
empty. The field's absence MUST NOT cause discovery validation to
fail — substrate v1 does not advertise this field, so making it
required would break the discovery handshake.

#### Scenario: Issuer-relative fallback when discovery omits the field

- **GIVEN** substrate's discovery document does not include
  `auth_claim_endpoint`
- **WHEN** the engine invokes `Authenticator.Claim`
- **THEN** the engine SHALL POST to `<issuer>/api/auth/claim`,
  using the configured issuer URL with any trailing slash trimmed

#### Scenario: Discovery field preferred when present

- **GIVEN** substrate's discovery document includes
  `auth_claim_endpoint = "https://substratesystems.io/api/auth/claim"`
- **WHEN** the engine invokes `Authenticator.Claim`
- **THEN** the engine SHALL POST to the advertised URL verbatim,
  not to the issuer-relative fallback

### Requirement: Bearer authentication isolates the claim request

The engine SHALL set `Authorization: Bearer <claimToken>` on the
claim request via `client.Request.Headers` and SHALL set
`SkipAuthRefresh: true` to prevent the HTTP client's 401 → refresh
→ retry hop from interfering with the bearer-borne claim. The
existing access token (if any) for a logged-in session SHALL NOT
be attached to the claim request — the bearer in `Headers` takes
precedence per the existing
`Authorization`-header-wins rule in
`internal/backup/client/client.go`.

#### Scenario: Bearer header set; no session access token attached

- **GIVEN** the engine has an active local session with a cached
  access token for `user-old@example.com`
- **WHEN** the user runs `endstate backup claim --token <t>`
- **THEN** the outgoing request SHALL carry exactly one
  `Authorization` header equal to `"Bearer " + <t>`
- **AND** the cached access token SHALL NOT appear anywhere in the
  request headers

#### Scenario: Existing session is replaced on successful claim

- **GIVEN** the engine has an active local session for
  `user-old@example.com`
- **WHEN** a claim succeeds and substrate returns
  `userId = "user-new-1"`, `email = "new-buyer@example.com"`
- **THEN** the keychain refresh-token entry SHALL be for
  `"user-new-1"`
- **AND** the session's current-user pointer SHALL be
  `"user-new-1"`
