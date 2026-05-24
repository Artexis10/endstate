## Why

Substrate's `wire-anonymous-buyer-account-linking` change (substrate PR
#12, archived 2026-05-24) shipped `POST /api/auth/claim` so buyers who
purchase Hosted Backup anonymously — via the `/endstate` storefront,
without a pre-existing session — can attach credentials to the
*pre-account* substrate created for them at Paddle webhook time.

The buyer receives an email containing a single-use 43-char URL-safe
base64 claim token (`endstate://claim?token=…`). Until the engine can
spawn a CLI surface for this flow, the GUI side (already shipped in
`endstate-gui#add-hosted-backup-claim-input`) has nothing to call:
`backupClaim()` spawns `endstate backup claim --token <t>
--save-recovery-to <p>` and that subcommand does not exist yet.

This change adds that subcommand. The whole flow is structurally
identical to `backup signup`: same passphrase-on-stdin, same Argon2id
KDF, same 24-word BIP39 mnemonic generation, same wrappedDEK and
recovery-key derivation, same write-recovery-file-before-network
ordering. The only deltas are: bearer-token authentication, an
empty-email request body (substrate has the email from Paddle), and
four substrate-specific error codes that must reach the GUI verbatim
so its `friendlyAuthError` map can render the right copy.

## What Changes

- New `endstate backup claim --token <43-char-base64url>
  --save-recovery-to <path>` subcommand. Passphrase via stdin (line 1),
  optional 24-word BIP39 recovery mnemonic on line 2 — exactly mirrors
  `backup signup`.
- New `auth.ClaimBody` request struct (= `SignupBody` minus `Email`)
  and `Authenticator.Claim(ctx, claimToken, body)` HTTP method.
  `Authorization: Bearer <claimToken>` carries auth; `SkipAuthRefresh:
  true` prevents the access-token refresh hook from interfering.
  Existing precedent: `RecoverFinalize` uses the identical pattern for
  the recovery bearer.
- Optional `EndstateExtensions.AuthClaimEndpoint` discovery field
  (`json:"auth_claim_endpoint,omitempty"`). Substrate does **not** yet
  advertise it, so this field is non-required and the engine falls
  back to `<issuer>/api/auth/claim` when absent — same pattern as
  `Me()` and `Subscribe()` use for endpoints not in
  `endstate_extensions`. A future substrate change can advertise the
  field without engine changes.
- Optional `Email` field on `auth.CompleteLoginResponse` (`omitempty`).
  Populated only by claim — other endpoints (signup, login, recover,
  refresh) carry on as before.
- `client.parseAPIError` body-code passthrough widens to surface
  substrate's domain codes (`CLAIM_TOKEN_INVALID`,
  `CLAIM_TOKEN_EXPIRED`, `CLAIM_TOKEN_CONSUMED`, `KDF_TOO_WEAK`) on
  `envelope.error.code` verbatim. The GUI's friendly-error map
  switches on the wire-string. `envelope.ErrorCode` is `type
  ErrorCode string`, so passing arbitrary substrate codes through
  without defining new constants is type-safe.

Explicitly out of scope:

- `endstate://claim?token=…` deep-link handler (deferred — paste in
  the GUI works fine for v1).
- `--token-stdin` mode (defers a `ps`-leak mitigation; the token is
  single-use and expires in 30 days, so v1 accepts the modest leak).
- `/api/auth/claim/resend` engine wrapper. Substrate's resend cron
  sends a nudge email rather than re-issuing the token (plaintext is
  not stored server-side), so there is no engine-side action.
- Cross-repo livewire e2e. Add once both engine and GUI ship if
  regression risk warrants it.

## Capabilities

### Modified Capabilities

- `hosted-backup-auth-client`: gains the claim-token credential-attach
  flow, including the new request body, the new auth method, the
  discovery-field fallback, and the substrate-domain error-code
  passthrough.

## Impact

- **New file**:
  - `internal/commands/backup_claim.go` (~170 LOC; ~80% structural
    duplicate of `backup_signup.go`).
  - `internal/commands/backup_claim_test.go` (~250 LOC; mirrors
    `backup_signup_test.go` with a `newClaimBackend` fixture and the
    four error-code scenarios).
- **Modified files**:
  - `internal/backup/oidc/oidc.go` (+1 line: optional discovery
    field).
  - `internal/backup/auth/authenticator.go` (~+50 lines: `ClaimBody`,
    `Claim()`, `Email` field on `CompleteLoginResponse`).
  - `internal/backup/client/client.go` (+5 lines: extended body-code
    switch in `parseAPIError`).
  - `internal/commands/backup.go` (+3 lines: dispatch case + `Token`
    field on `BackupFlags`).
  - `cmd/endstate/main.go` (~+10 lines: `--token` parser, dispatch
    passthrough, usage blurb append).
- **No substrate dependency.** Substrate's `/api/auth/claim` is live.
  We do **not** require a substrate discovery-doc PR — the engine
  falls back to issuer-relative URL when the optional field is
  absent.
- **GUI unblocks on merge.** `endstate-gui`'s
  `add-hosted-backup-claim-input` change waits on this engine PR; the
  predev rebuild script will pick up the new subcommand once a binary
  with `backup claim` is built into the GUI repo's bundled engine.
