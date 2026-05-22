## Why

The substrate-side Hosted Backup checkout API is shipped and live-verified: `POST /api/billing/checkout` mints a Paddle transaction for the €4/mo Hosted Backup price and returns `{ checkoutUrl, transactionId }`. But nothing user-facing can drive a subscription yet. Because the GUI is a thin presentation layer that never talks to substrate directly (cli-source-of-truth invariant), the engine must own the checkout call. The engine returns the checkout URL; the GUI opens it in the system browser, where substrate renders the Paddle `_ptxn` overlay.

## What Changes

- Add a new subcommand `endstate backup subscribe [--json]`.
- It requires a signed-in session; when signed out it returns `AUTH_REQUIRED` without any network call.
- When signed in it issues `POST <issuer>/api/billing/checkout` (no request body — substrate resolves the price server-side) using the persisted access token, reusing the existing authenticated HTTP client (bearer injection, version check, retry, one-shot 401→refresh).
- On success it returns a `data` payload of `{ checkoutUrl, transactionId }`.
- The engine does NOT open a browser; returning the URL keeps the engine/GUI boundary (consistent with manual-app `launch` URLs being GUI-only metadata).
- Update `docs/contracts/hosted-backup-contract.md` §7 to document the engine-initiated `POST /api/billing/checkout` endpoint.
- Update `endstate backup` CLI help text to list `subscribe`.

## Capabilities

### New Capabilities

- `backup-subscribe-checkout`: A `backup subscribe` command that returns a Paddle checkout URL + transaction id for the GUI to open.

### Modified Capabilities

(none — additive command only, no existing spec behavior changes)

## Impact

- `go-engine/internal/backup/auth/authenticator.go` — new `Subscribe` method + `CheckoutResponse` type (issuer-derived URL, like `Me()`).
- `go-engine/internal/commands/backup_subscribe.go` (new) — `runBackupSubscribe` + `SubscribeResult`.
- `go-engine/internal/commands/backup.go` — dispatch case + usage string.
- `go-engine/cmd/endstate/main.go` (protected) — usage/help text only.
- `docs/contracts/hosted-backup-contract.md` (protected) — additive §7 endpoint documentation.
- No schema version bump (additive command; existing output shapes unchanged).
