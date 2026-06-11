## Why

Endstate Hosted Backup persists two secrets between CLI invocations — the
auth **refresh token** and the unwrapped **DEK** (plus the non-secret
wrappedDEK / access-token cache) — in the OS secret store via the narrow
`internal/backup/keychain` `Keychain{Store,Load,Delete}` interface. The
trust boundary is the OS user account (contract §1: the masterKey/DEK
"never leave the device"; the DEK lives "in the same trust boundary as the
refresh token").

Only Windows had a real backend (Credential Manager, `keychain_windows.go`).
Every other platform fell through `keychain_other.go` (`//go:build !windows`),
a **fail-closed stub** that errors `"platform not supported (Windows only)"`
on every op — by design the engine NEVER falls back to plaintext (contract
§1/§5). That made `backup login/push/pull/...` unusable on macOS and Linux,
even though the rest of the backup stack (crypto, auth, upload/download) is
platform-agnostic and builds for those targets.

This change adds REAL native backends for macOS and Linux so backup works
there, while preserving fail-closed semantics on every platform that lacks a
native secret store.

## What Changes

- **macOS backend** (`keychain_darwin.go`, `//go:build darwin`) — backs the
  `Keychain` interface with the macOS login Keychain via
  `github.com/zalando/go-keyring` (pure Go; shells out to `/usr/bin/security`,
  no cgo).
- **Linux backend** (`keychain_linux.go`, `//go:build linux`) — backs the
  `Keychain` interface with the freedesktop Secret Service (GNOME Keyring,
  KWallet, …) over D-Bus via the same library.
- **Narrowed stub** — `keychain_other.go` becomes
  `//go:build !windows && !darwin && !linux`; it keeps the fail-closed
  behaviour and its error now names the three supported stores instead of
  "Windows only".
- **Error mapping** — go-keyring's `keyring.ErrNotFound` maps to the
  package's `ErrNotFound`; all other errors are wrapped with the existing
  `keychain:` prefix. On Linux a non-not-found failure (headless / WSL /
  locked keyring — the correct fail-closed path) carries an actionable hint
  naming the unlocked-OS-keyring requirement.
- **Dependency** — adds `github.com/zalando/go-keyring v0.2.8` (and its
  transitive `github.com/godbus/dbus/v5`, indirect) to `go-engine/go.mod`.
- **Hermetic tests** — `keychain_darwin_test.go` / `keychain_linux_test.go`
  (build-tagged so each platform's CI exercises its own backend) use
  go-keyring's `MockInit()` to test the wrapper, ErrNotFound mapping, binary
  round-trip, and (Linux) the daemon-failure remediation hint — never
  touching the real keychain or D-Bus.

The account-name convention (`AccountForUser` / `AccountForDEK` / …) is
unchanged; the `Keychain` interface, the `NewSystem()` constructor signature,
and all callers are unchanged.

## Capabilities

### New Capabilities
- `backup-keychain`: where Hosted Backup secrets persist between CLI
  invocations — ONLY the platform-native secret store (Windows Credential
  Manager, macOS Keychain, Linux Secret Service) — and the fail-closed
  guarantee that on any platform without one the engine errors rather than
  writing plaintext.

### Modified Capabilities
<!-- None: there is no pre-existing keychain capability in the specs baseline; this delta adds backup-keychain. -->

## Impact

- `go-engine/internal/backup/keychain/keychain_darwin.go` (new) — macOS backend.
- `go-engine/internal/backup/keychain/keychain_linux.go` (new) — Linux backend.
- `go-engine/internal/backup/keychain/keychain_other.go` — build tag narrowed
  to `!windows && !darwin && !linux`; stub error message updated.
- `go-engine/internal/backup/keychain/keychain_darwin_test.go`,
  `keychain_linux_test.go` (new) — hermetic wrapper tests via `MockInit()`.
- `go-engine/go.mod`, `go.sum` — add `github.com/zalando/go-keyring`.
- No contract edit required: contract §1/§5 describe the trust boundary
  (on-device secret, gated by the OS user account) platform-agnostically; the
  new backends uphold it. The §1 "Filen.io — Windows-first" note and the
  existing `hosted-backup-orchestration` spec's "OS keychain" wording already
  read generically.
- Backward-compatible: Windows behaviour is byte-identical; macOS/Linux gain
  function where they previously errored; truly unsupported platforms still
  fail closed.
- **Manual validation (not CI-verifiable here):** a real macOS Keychain
  round-trip and a real Secret Service round-trip on a desktop Linux session
  with an unlocked keyring. CI exercises only the hermetic `MockInit()` path.
