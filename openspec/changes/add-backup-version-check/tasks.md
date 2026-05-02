## 1. Version constants

- [x] 1.1 Define `EngineSchemaMajor=1`, `EngineSchemaMinor=0`, `versionHeader="X-Endstate-API-Version"` in `internal/backup/client/version.go`

## 2. Header parsing

- [x] 2.1 `parseVersionHeader(v)` extracts `MAJOR.MINOR`; rejects malformed values; returns wrapped error
- [x] 2.2 Malformed header logs a warning via `slog.Warn` and proceeds (does not block the response)

## 3. Mismatch policy

- [x] 3.1 Major mismatch → `SCHEMA_INCOMPATIBLE` always, on both reads and writes
- [x] 3.2 Higher minor on read-only request → `slog.Warn` and proceed (returns `(nil, true)` from `versionMismatch`)
- [x] 3.3 Higher minor on write request → `SCHEMA_INCOMPATIBLE`
- [x] 3.4 Same major + lower-or-equal minor → no action

## 4. Wiring

- [x] 4.1 `client.Client.processResponse` reads `X-Endstate-API-Version` before status mapping
- [x] 4.2 Mismatch returns from `processResponse` immediately so retries do not consume slots on an incompatible backend
- [x] 4.3 `Request.ReadOnly` flag controls minor-mismatch tolerance (true → tolerant; false → strict)

## 5. Tests

- [x] 5.1 Major mismatch → `SCHEMA_INCOMPATIBLE` for both `ReadOnly: true` and `ReadOnly: false`
- [x] 5.2 Minor mismatch on `ReadOnly: true` → request succeeds (cmd handler receives parsed body)
- [x] 5.3 Minor mismatch on `ReadOnly: false` → `SCHEMA_INCOMPATIBLE`
- [x] 5.4 Matching version (`1.0`) → no error, no warning
