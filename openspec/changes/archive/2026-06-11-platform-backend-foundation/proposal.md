## Why

The engine is hardwired to Windows + winget at several points: the driver factory hardcodes `winget.New()` (`internal/commands/verify.go:24`), package-reference resolution only reads `refs["windows"]` (`resolveWindowsRef`), `capabilities` reports a literal `["winget"]`, and profile/path resolution assumes Windows conventions. Adding any second backend — Nix on Linux/macOS, whose realizer model the spike validated — is blocked on these hardcodes. This change makes backend selection and the surrounding ref/capabilities/path logic platform-aware, with **zero behavior change on Windows**, so a Nix backend can slot in additively in a later change.

## What Changes

- Add a GOOS-keyed backend selector (`SelectBackend`) — `windows` returns the winget driver; any other host returns an explicit "no backend available" error (filled in by the Nix change).
- Generalize package-reference resolution from `resolveWindowsRef` to `resolveRef`, keyed by `runtime.GOOS` with the same fallback to the first non-empty ref.
- `capabilities` reports the host OS and available backends dynamically instead of literal `windows` / `["winget"]`.
- `ProfileDir()` follows the XDG Base Directory spec on Linux (Documents convention preserved on Windows); env-var expansion becomes platform-aware (`%VAR%` on Windows, `$VAR` elsewhere) behind one dispatch.
- **No Nix code and no new package-manager backend** — non-Windows backend selection returns a clear error until the Nix change lands.

## Capabilities

### New Capabilities

- `platform-backend-selection`: select the package backend by host OS, resolve refs per platform, report host OS/backends in capabilities, and follow platform path/env conventions — all preserving existing Windows behavior.

### Modified Capabilities

(none — Windows behavior is invariant; every addition is platform-gated and additive)

## Impact

- `internal/driver/select.go` (new) — `SelectBackend(goos) (Driver, error)`
- `internal/commands/verify.go:24` — `newDriverFn` routes through `SelectBackend`; `apply.go` / `plan.go` call sites likewise
- `internal/planner/planner.go` / `internal/commands/verify.go` — `resolveWindowsRef` → `resolveRef` (GOOS-keyed)
- `internal/commands/capabilities.go` — dynamic OS + drivers
- `internal/config/paths.go` — XDG `ProfileDir`; platform-aware env expansion (and its 4 callers)
- `docs/contracts/cli-json-contract.md` — additive only (capabilities `os`/`drivers` now host-dependent). **Protected area — flagged for explicit go-ahead at implementation time.**
- No schema version bump: adding `refs["linux"]` / `refs["darwin"]` keys is additive per `schema-versioning`.
