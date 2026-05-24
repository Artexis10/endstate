## Context

`backup signup` is the canonical template for creating a Hosted Backup
identity end-to-end: read passphrase from stdin, generate the 24-word
BIP39 mnemonic, derive an Argon2id KDF (serverPassword + masterKey),
wrap a fresh DEK with masterKey, derive a recoveryKey + verifier,
wrap the DEK again with the recoveryKey, **write the recovery file to
disk before any network call**, then `POST /api/auth/signup` with the
full credential block. On success the engine persists the access +
refresh tokens to the OS keychain and caches the unwrapped DEK and
masterKey-wrapped DEK there too.

Substrate's `POST /api/auth/claim` is structurally identical from a
crypto standpoint: same KDF, same wrap, same verifier — substrate
will reject a request that doesn't satisfy the KDF floor with
`KDF_TOO_WEAK`. The two differences are at the HTTP layer:

1. Authentication is bearer-borne via a single-use claim token instead
   of the buyer-supplied email + serverPassword pair.
2. The request body omits `email` and the response body includes
   `email` — substrate has the canonical email from Paddle and
   surfaces it back so the engine displays the same identity the
   buyer purchased under.

Everything between stdin and the HTTP boundary is identical to signup
and is reused verbatim.

## Goals / Non-Goals

**Goals:**

- Ship a CLI surface that lets the GUI's already-shipped
  `backupClaim()` bridge complete the credential-attach round trip
  against a real substrate backend.
- Reuse `backup signup`'s code, helpers, and stdin protocol —
  diverging only at the request body, the URL, the bearer header,
  and the response email field.
- Surface substrate's domain error codes
  (`CLAIM_TOKEN_INVALID|EXPIRED|CONSUMED`, `KDF_TOO_WEAK`) verbatim
  in `envelope.error.code` so the GUI's friendly-error map renders
  the correct user-facing copy without further translation.
- Add no substrate-side dependencies. Discovery-doc field is
  optional with an issuer-relative fallback.

**Non-Goals:**

- A second crypto path for claim. Anything the engine would do
  differently from signup is a smell.
- A `--token-stdin` mode in v1. Token is single-use + 30-day
  expiry; the modest `ps`-leak window is acceptable. Future change
  can add stdin protocol coordinated with the GUI.
- A claim-resend wrapper. Substrate's resend cron sends a nudge
  email (the plaintext token is not re-derivable from the stored
  hash), so there is no engine-side action.
- An `endstate://claim?token=…` URL scheme handler. Deferred to the
  GUI repo; engine has no role.

## Decisions

### Mirror file (`backup_claim.go`), not extend signup

A new file `internal/commands/backup_claim.go` mirroring
`backup_signup.go` is preferred over branching inside
`runBackupSignup`. Reasons:

- One-file-per-subcommand is the established convention
  (`backup_login.go`, `backup_logout.go`, `backup_recover.go`, …).
- The error-handling messages reference the subcommand by name
  ("backup signup: …") and would have to fork inside a shared
  function. Two separate functions is cleaner than threading a
  subcommand label.
- Shared helpers (`writeRecoveryFile`, `zero32`, `b64`,
  `readSignupFromStdin`, `signupReader`, `WithSignupReader`) stay
  exactly where they are — same package, no exports needed.

The duplication is ~80% structural overlap. We accept it because the
divergence at the HTTP layer (bearer header, no-email body,
server-supplied email response) is exactly the load-bearing
difference, and forcing it through a shared abstraction obscures
where claim ≠ signup.

**Alternative considered:** extract a shared `derivePassphraseAndDEK`
helper inside `internal/backup/crypto`. Rejected because the helper
would need to accept the recovery-file-write callback (must happen
before the HTTP call) and the post-HTTP keychain persistence
callback, which means six parameters and two callbacks — at that
point the helper is harder to read than two parallel files.

### Bearer header via `client.Request.Headers` + `SkipAuthRefresh: true`

Direct precedent: `RecoverFinalize` (`authenticator.go:320-339`) uses
this exact pattern for the recovery bearer. The HTTP client
(`client.go:139-159`) explicitly preserves caller-supplied
`Authorization` headers — copying it onto the request *before* the
session's cached access token gets considered. The comment at
line 145-149 is explicit: the recovery-finalize flow relies on this.
Claim is the second caller of the same mechanism.

`SkipAuthRefresh: true` is essential: a 401 from `/api/auth/claim`
means the token is invalid, expired, or consumed. The default
refresh hook would attempt to refresh a (possibly nonexistent)
access token and retry, which (a) loops without progress and (b)
risks the retry succeeding with a stale local session in a way that
loses the intended substrate identity.

### Discovery field optional with issuer-relative fallback

Substrate's discovery doc (verified by reading substrate's
`src/app/api/.well-known/openid-configuration/route.ts`) advertises
six extension endpoints: signup, login, refresh, logout, recover,
backup_api_base. It does **not** advertise `auth_claim_endpoint`.

Two paths considered:

1. Make `auth_claim_endpoint` required in `validateDocument` and
   require a substrate companion PR. **Rejected**: this engine
   change is meant to unblock the GUI immediately; coupling it to
   another substrate PR delays the launch.
2. Make `auth_claim_endpoint` optional with `<issuer>/api/auth/claim`
   fallback when absent. **Chosen**: zero substrate dependency,
   matches the precedent for `Me()` and `Subscribe()` which compute
   their URLs from the issuer URL directly.

The fallback only fires when the field is *empty/missing*, not when
present-but-bogus. A future substrate version that advertises a
typo'd value will cause the HTTP call to fail loudly with a clear
error rather than silently routing to a fallback path.

### Error-code passthrough widens existing whitelist

`parseAPIError` (`client.go:296-307`) already passes
`STORAGE_QUOTA_EXCEEDED` through as `envelope.ErrStorageQuotaExceeded`
when substrate sets that in the response body. The same mechanism
extends to the four substrate claim codes — except the engine does
not define `ErrCLAIM_TOKEN_INVALID` etc. as constants. Two options:

1. Define constants and add full `defaultRemediation` /
   `defaultDocsKey` mappings. **Rejected**: the engine doesn't own
   the claim error namespace; substrate's claim endpoint is the
   source of truth. The GUI's `friendlyAuthError` map already owns
   the user-facing copy for these codes.
2. Cast the substrate code string into `envelope.ErrorCode` directly:
   `ae.Code = envelope.ErrorCode(strings.ToUpper(env.Error.Code))`.
   Allowed by the type — `envelope.ErrorCode` is `type ErrorCode
   string`. **Chosen**: minimal engine surface, the substrate code
   reaches `envelope.error.code` verbatim, and the GUI side switches
   on the wire-string. A comment in `errors.go` documents the
   passthrough so future readers don't expect a constant for these
   codes.

### `--token` on argv vs. on stdin

The GUI already wires `--token` as a CLI flag (the bridge call shape
is locked in `endstate-gui/src/lib/backup-bridge.ts`). Moving to a
stdin protocol requires a coordinated GUI change and would block
this PR. The token is:

- Single-use — consumed on first successful claim. A `ps` snapshot
  captured during the call window can replay only until the
  legitimate buyer completes their claim, which is the immediately
  following operation.
- Short-lived — 30-day expiry from substrate's `claim_tokens` row.
- Not a long-lived secret like an access token or recovery key.

V1 accepts the leak. Future work can add `--token-stdin` (read line
0 of stdin, then the existing passphrase on line 1) once both repos
ship together.

### `Email` field on `CompleteLoginResponse`

Adding an `omitempty` field to the shared response struct is benign:
the other endpoints (signup, login, recover-finalize, refresh) will
serialize the same struct with the field absent because substrate
doesn't return it in those flows. The alternative — a separate
`ClaimResponse` struct — duplicates five identical fields for one
extra and forces a bespoke `session.SetTokens` call site.

## Risks / Trade-offs

**[Discovery fallback silently masks a future substrate
misconfiguration]** → Mitigation: the fallback only fires when the
field is empty/missing. A bogus value reaches the HTTP layer
unchanged, where the request fails with a clear status code. If we
want louder feedback when substrate later advertises the field but
disagrees with the engine on the path shape, that's a v2 concern.

**[`SkipAuthRefresh: true` plus a successful claim overwrites any
existing local session]** → This is correct behaviour. A user who
invokes `backup claim` while already signed in is telling the
engine to bind credentials to the pre-account substrate has for the
claim token, which is *not* the current local session's user
(otherwise no claim token would exist for them). The substrate
session win is intentional.

**[`envelope.ErrorCode` passthrough bypasses the constant set]** →
Acceptable because substrate owns the claim error namespace. The
engine is a transparent pipe. Mitigation: add a comment block in
`internal/envelope/errors.go` documenting that the four substrate
codes are valid `ErrorCode` values produced by this engine but NOT
declared as constants, with a link to the GUI's friendly-error map
for downstream behaviour.

**[Test parity with signup means heavy Argon2id runs]** →
`backup_signup_test.go` is already `testing.Short()`-aware; the
claim suite mirrors that. CI default keeps short mode off; pre-push
hook respects `-short`.

## Migration Plan

Purely additive. No schema migrations. No keychain layout changes.
Rollback is `git revert` of the engine PR — `backup claim` simply
disappears from the dispatch table; the GUI's `backupClaim()` will
get "unknown backup subcommand: claim" until a new binary is built.

The GUI side's PR description must reference this engine PR's commit
SHA. After both merge:

1. The GUI repo's `predev` rebuild script runs the next time `npm
   run dev` / `tauri dev` fires and produces a new binary containing
   `backup claim`.
2. CI for the GUI PR re-runs against that binary and passes.
3. The GUI PR unblocks for merge.

## Open Questions

None at this time. The discovery-doc question is resolved
(optional + fallback). The error-code passthrough mechanism has a
direct precedent (`STORAGE_QUOTA_EXCEEDED`). The bearer header
mechanism has a direct precedent (`RecoverFinalize`). The shape of
the request body is fixed by substrate's contract.
