## Why

Contract §11 defines the engine ↔ backend version compatibility protocol: the backend stamps every response with `X-Endstate-API-Version: MAJOR.MINOR`; the engine refuses to write across a major mismatch and warns on a higher minor for read-only operations. Without a structured check the engine would silently send v1 requests to a v2 backend (or vice versa) and surface inscrutable parse errors instead of a clear "update the engine" remediation.

## What Changes

- The hosted-backup HTTP client wrapper (`internal/backup/client/`) inspects `X-Endstate-API-Version` on every response
- Major mismatch → `SCHEMA_INCOMPATIBLE` (already defined; reused here)
- Minor mismatch on a read-only request → warn via `slog`, proceed
- Minor mismatch on a write → `SCHEMA_INCOMPATIBLE`
- Engine's expected version is `1.0` (constants in `version.go`)

## Capabilities

### New Capabilities

- `hosted-backup-version-compatibility`: Engine policy for evaluating the backend-advertised hosted-backup API schema version on every response.

### Modified Capabilities

<!-- None — the rule is new and lives entirely inside the new client wrapper. -->

## Impact

- **`go-engine/internal/backup/client/version.go`** — new file with the version-check rule
- **`go-engine/internal/backup/client/client.go`** — calls into the rule from `processResponse`
- **No public API change** — `Request.ReadOnly` is the only surface, used internally by every backup command
- **Pairs with `add-backup-auth-client`** — the auth client is the first consumer; the storage client (later change) inherits the rule transparently.
