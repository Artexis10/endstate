## Context

This change is the engine-side scaffold for Endstate Hosted Backup, sized to be reviewable in isolation. It deliberately stops at "everything that talks to the backend, parses tokens, and persists secrets." The cryptographic primitives that make those tokens *useful* (Argon2id KDF, AES-256-GCM, BIP39 recovery) are isolated in a follow-up change so a security reviewer can assess them in a small, focused PR.

Three properties shape the design:

1. **Crypto stays at arm's length.** `internal/backup/crypto/` exports an interface; every body returns `ErrNotImplemented`. Login/recover surface `INTERNAL_ERROR` "crypto not yet implemented" until the crypto change merges. Compile-time integration is real; run-time behaviour blocks at the well-defined boundary.
2. **One source of truth for HTTP semantics.** All backend calls go through `client.Client.Do`, which is the only place that maps HTTP status to engine error codes, retries, and validates the version header. Auth, status, and (later) push/pull/list/delete reuse it verbatim.
3. **Test seams without test-only files.** Each layer takes its dependencies as interfaces (`HTTPDoer`, `TokenProvider`, `JWKSResolver`, `Keychain`) so unit tests use httptest and an in-memory keychain. The command-handler layer adds one swappable factory (`ReplaceBackupAuthFactoryForTest`) so command tests can inject the stack pointing at a fake backend.

## Goals / Non-Goals

**Goals:**

- Implement everything required to compile and integrate hosted-backup commands without crypto bodies
- Bring `endstate backup login|logout|status` to a state where the orchestration paths are exercised end-to-end against a fake backend
- Establish error-code mappings, retry policy, and version-check rules once, so subsequent backup commands inherit them
- Add the capabilities feature flag the GUI gates on

**Non-Goals:**

- Cryptographic primitives (Argon2id, AES-256-GCM, DEK wrap, BIP39) — `add-backup-crypto-module`
- Storage client (push/pull/list/versions/delete) — `add-backup-storage-client`
- GUI changes — Prompt 4 in a separate repo
- Any change to existing engine commands' behaviour

## Decisions

### 1. Stdlib `net/http` over a third-party HTTP library

**Choice:** Use stdlib `net/http` wrapped by a thin `client` package.

**Rationale:** The engine has no other HTTP client today, and the substrate API surface is small (≤10 endpoints in v1). Adding `resty` or similar buys nothing the wrapper can't deliver. Keeping the dep surface minimal also matters because each new dep is a future review burden.

**Alternative considered:** `go-resty/resty/v2`. Rejected on dep-surface grounds. Re-evaluate if the wrapper grows past ~300 LOC.

### 2. `github.com/golang-jwt/jwt/v5` for JWT parsing

**Choice:** golang-jwt/jwt/v5.

**Rationale:** De-facto Go choice; supports EdDSA per RFC 8037, which the contract requires; v5 has cleaner API than v4.

**Alternative considered:** `lestrrat-go/jwx`. Heavier, more flexible than we need. Rejected to keep the verifier surface narrow.

### 3. `github.com/danieljoos/wincred` for Windows Credential Manager

**Choice:** wincred.

**Rationale:** Mature, single-purpose, three years of stable releases, no transitive deps. Wrapped by our own `Keychain` interface so swapping later is trivial.

**Alternative considered:** Direct CGO bindings to `wincred.h`. Rejected; we'd reinvent wincred for no gain.

### 4. Single account name per user, not per-token

**Choice:** Persist the refresh token under `endstate-refresh-<userID>`.

**Rationale:** Simplest mapping; one user = one account = one refresh token. Rotation overwrites in place. If we later need multiple-tab/concurrent sessions we can extend the key, but no current requirement.

### 5. Bounded retry policy: 3 retries, exp backoff w/ jitter, 4xx never

**Choice:** Max 3 retries, initial 500 ms, ×2 backoff with ±25% jitter, capped at 8 s. Retry on 5xx and transport errors; never on 4xx; 429 honours `Retry-After`.

**Rationale:** Standard exponential backoff. 4xx is the client's fault — retrying just amplifies the problem. The 401-refresh-then-retry hook is independent of the regular retry counter so an expired token doesn't burn a retry slot.

**Alternative considered:** Idempotency keys for write-path retries. Deferred — substrate doesn't currently honour them, and our writes are rare and short.

### 6. `client.Do` returns `*envelope.Error` directly, not `error`

**Choice:** The HTTP client wrapper returns the engine's domain error type so command handlers can return the result verbatim. Internally the retry loop tracks both API errors (typed) and transport errors (plain) and maps the boundary.

**Rationale:** Avoids round-tripping through `errors.As` at every call site. The auth package can introspect `err.Code` directly. Engine convention is `*envelope.Error` everywhere it crosses package boundaries.

**Alternative considered:** Return `error` and force callers to `errors.As` to an `*APIError`. Rejected as ergonomically worse for the common case.

### 7. Crypto stub returns `ErrNotImplemented`, not panic

**Choice:** Stub bodies return a sentinel error.

**Rationale:** Panicking would abort the engine; returning a typed error lets callers (login, recover) translate to a clear `INTERNAL_ERROR` envelope with remediation. The error message names the follow-up change explicitly so a confused engineer reading the engine logs immediately knows where to look.

## Risks / Trade-offs

- **[Risk] Login surface fails until PROMPT 3 lands.** Users running this engine cannot actually log in. Mitigation: the README and the error message both call out that crypto is in a follow-up change. The capabilities response advertises `hostedBackup.supported = true` because the *feature* exists; the GUI is expected to gate on engine version too (per contract §11).
- **[Risk] Refresh-token persistence is platform-specific.** Non-Windows targets always error from `keychain.NewSystem()`. Mitigation: Endstate is Windows-first; cross-platform support is a separate change. Test coverage uses the memory keychain on all platforms.
- **[Trade-off] `backup login` is "signup or login" but only the login path is wired.** PROMPT 2 calls out the merged surface, but the signup path needs DEK generation + recovery key + verifier production, all of which depend on PROMPT 3. The error message specifically targets the crypto stub so the GUI doesn't try to differentiate.
- **[Trade-off] `account delete` is wired in `main.go` but the handler is stubbed.** This keeps the dispatcher table cohesive with the contract surface today; the storage-client change provides the working handler in a single coordinated change with backup deletion.
