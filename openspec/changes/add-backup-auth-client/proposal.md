## Why

`docs/contracts/hosted-backup-contract.md` (locked v1.0, 2026-05-02) defines a cross-repo contract for Endstate Hosted Backup. Substrate has shipped the backend (PR1+PR2). This change brings the engine-side scaffold online: the `endstate backup login|logout|status` subcommands, OIDC discovery + JWKS validation, EdDSA JWT verification, and refresh-token persistence in Windows Credential Manager.

Crypto primitives are intentionally out of scope. They land in a follow-up change (`add-backup-crypto-module`) so that PR can be reviewed in isolation. Login/recover return `INTERNAL_ERROR` with a "crypto: not implemented" message until that crypto change merges. The HTTP, OIDC, JWT, keychain, and orchestration code in this change is real and tested.

## What Changes

- New `endstate backup login`, `endstate backup logout`, `endstate backup status` subcommands (`go-engine/internal/commands/backup_login.go`, `backup_logout.go`, `backup_status.go`)
- New `internal/backup/` package tree: `oidc/`, `client/`, `auth/`, `keychain/`, `crypto/` (stubs only)
- Capabilities response advertises a new `hostedBackup` feature block populated from `ENDSTATE_OIDC_ISSUER_URL` / `ENDSTATE_OIDC_AUDIENCE` env vars
- New error codes: `AUTH_REQUIRED`, `SUBSCRIPTION_REQUIRED`, `NOT_FOUND`, `RATE_LIMITED`, `BACKEND_ERROR`, `BACKEND_UNREACHABLE`, `BACKEND_INCOMPATIBLE`, `STORAGE_QUOTA_EXCEEDED`
- New dependencies: `github.com/golang-jwt/jwt/v5` for EdDSA JWT verification; `github.com/danieljoos/wincred` for Windows Credential Manager

## Capabilities

### New Capabilities

- `hosted-backup-auth-client`: Engine-side authentication client for Endstate Hosted Backup. Discovers OIDC endpoints, validates EdDSA JWTs against a cached JWKS, persists the refresh token in the OS keychain, and orchestrates the login/logout/status flows defined in contract §5.

### Modified Capabilities

<!-- None — capabilities feature flag is additive. -->

## Impact

- **`go-engine/internal/commands/`** — three new command handlers + capabilities update
- **`go-engine/internal/backup/`** — new package tree (8 subpackages)
- **`go-engine/internal/envelope/errors.go`** — new error codes
- **`go-engine/cmd/endstate/main.go`** — new `backup` and `account` dispatch cases, new flags (`--email`, `--backup-id`, `--version-id`, `--to`, `--confirm`)
- **`go-engine/go.mod`** — adds `golang-jwt/jwt/v5` and `wincred`
- **No changes to existing commands' behavior** — all changes are additive

This change pairs with `add-backup-version-check` (engine ↔ backend version compatibility); both ship in PR 1.
