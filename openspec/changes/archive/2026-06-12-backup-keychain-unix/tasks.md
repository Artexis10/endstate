## 1. Dependency

- [x] 1.1 Add `github.com/zalando/go-keyring` to `go-engine/go.mod`; `go mod tidy` (pins v0.2.8 + indirect `github.com/godbus/dbus/v5`)

## 2. Native backends

- [x] 2.1 `keychain_darwin.go` (`//go:build darwin`): `NewSystem()` returns a go-keyring-backed `Keychain`; single service name `"Endstate"`, account string passed through as the per-entry user
- [x] 2.2 `keychain_linux.go` (`//go:build linux`): same wrapper over the Secret Service; mirror the Windows naming/labeling convention
- [x] 2.3 Map `keyring.ErrNotFound` → package `ErrNotFound`; wrap all other errors with the `keychain:` prefix
- [x] 2.4 Linux: wrap non-not-found failures with an actionable hint (unlocked OS keyring / GNOME Keyring / KWallet via Secret Service) — kept in the keychain layer so the command-layer envelope remediation surfaces it verbatim
- [x] 2.5 Document concurrency-safety (go-keyring is stateless; zero-value struct is safe for concurrent use)

## 3. Narrow the fail-closed stub

- [x] 3.1 `keychain_other.go`: build tag `!windows && !darwin && !linux`
- [x] 3.2 Update the stub error to name the three supported stores (Windows Credential Manager, macOS Keychain, Linux Secret Service) instead of "Windows only"

## 4. Hermetic tests (per-platform build tags)

- [x] 4.1 `keychain_darwin_test.go` (`//go:build darwin`): `MockInit()`-backed round-trip, ErrNotFound mapping (load + delete), binary-DEK round-trip, overwrite; compile-time `var _ Keychain` assertion
- [x] 4.2 `keychain_linux_test.go` (`//go:build linux`): same coverage; plus a `MockInitWithError` test proving a simulated daemon failure carries the remediation hint and does NOT map to ErrNotFound
- [x] 4.3 Confirm `go test ./...` passes on a Linux box with NO D-Bus session (MockInit guarantees hermeticity)

## 5. Sweep + verification

- [x] 5.1 Grep `internal/backup/` and `internal/commands/backup*.go` for other Windows-only / GOOS gates; confirm the keychain split is the only one (engine never opens a browser — authenticator returns URLs)
- [x] 5.2 `go test ./...` (Linux), `go vet ./...`, `GOOS=windows go build ./...`, `GOOS=darwin go build ./...`, `GOOS=darwin go vet ./internal/backup/keychain/...` all green
- [x] 5.3 `npm run openspec:validate` (strict) green

## 6. Manual validation (deferred — not CI-verifiable in this environment)

- [ ] 6.1 (DEFERRED → docs/roadmap/roadmap.md §6 real-machine validation queue) Real macOS Keychain round-trip (`backup login` → `backup push` on a Mac with an unlocked login keychain)
- [ ] 6.2 (DEFERRED → docs/roadmap/roadmap.md §6 real-machine validation queue) Real Secret Service round-trip on a desktop Linux session with an unlocked GNOME Keyring / KWallet
