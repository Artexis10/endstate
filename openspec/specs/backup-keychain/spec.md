# backup-keychain Specification

## Purpose
Defines where Hosted Backup secrets (refresh token, DEK) persist: the platform-native secret store only (Windows Credential Manager, macOS Keychain, Linux Secret Service) — fail-closed elsewhere, never plaintext.
## Requirements
### Requirement: Backup secrets persist only in the platform-native secret store

Hosted Backup secrets (the auth refresh token and the unwrapped DEK) SHALL be persisted between CLI invocations ONLY in the operating system's native secret store: Windows Credential Manager, the macOS login Keychain, or the Linux Secret Service (GNOME Keyring, KWallet, or any freedesktop Secret Service provider). The engine MUST NOT write these secrets to a plaintext file or any other fallback location.

The native store gates access on the OS user account, which is the trust boundary the Hosted Backup contract (§1, §5) relies on: the DEK and refresh token live in the same boundary, so a `backup push` after `backup login` does not re-prompt for the passphrase.

#### Scenario: macOS persists secrets in the login Keychain

- **WHEN** the engine runs on macOS and stores a refresh token or DEK
- **THEN** the value SHALL be written to the macOS login Keychain via the platform-native API
- **AND** a subsequent CLI invocation SHALL load the same value back

#### Scenario: Linux persists secrets in the Secret Service

- **WHEN** the engine runs on Linux with an unlocked Secret Service provider available
- **THEN** the value SHALL be written to the Secret Service over D-Bus
- **AND** a subsequent CLI invocation SHALL load the same value back

#### Scenario: No plaintext fallback is ever written

- **WHEN** the engine stores a refresh token or DEK on any platform
- **THEN** the engine SHALL NOT write that secret to a plaintext file or any non-native fallback store under any circumstance

### Requirement: The engine fails closed when no native secret store is available

On any platform without a supported native secret store, the engine SHALL fail closed: every keychain operation MUST return a clear error and the engine MUST NOT degrade to plaintext storage. A platform with a native store that is present but unreachable or locked (for example a headless or WSL Linux session with no running Secret Service) is treated the same way — the operation MUST error rather than silently succeed.

The error returned in the no-native-store and locked-store cases SHALL be actionable, naming the supported stores (or, on Linux, the requirement for an unlocked OS keyring exposing the Secret Service API) so the user understands the remediation.

#### Scenario: Unsupported platform errors instead of writing plaintext

- **WHEN** the engine runs on a platform with no supported native secret store
- **AND** a keychain Store, Load, or Delete is attempted
- **THEN** the operation SHALL return an error naming the supported stores (Windows Credential Manager, macOS Keychain, Linux Secret Service)
- **AND** SHALL NOT persist the secret anywhere

#### Scenario: Linux without a reachable Secret Service errors with an actionable hint

- **WHEN** the engine runs on Linux with no reachable or unlocked Secret Service provider
- **AND** a keychain operation is attempted
- **THEN** the operation SHALL return an error that does NOT map to the not-found sentinel
- **AND** the error SHALL include a hint naming the unlocked-OS-keyring (GNOME Keyring / KWallet via Secret Service) requirement

### Requirement: Not-found is reported via a single sentinel across all backends

Every backend SHALL report "no entry exists for this account" via the package's single `ErrNotFound` sentinel, regardless of the underlying platform store's native not-found representation. Callers MUST be able to distinguish "the account is absent" from "the store failed" using only `errors.Is(err, ErrNotFound)`.

#### Scenario: Loading an absent account returns ErrNotFound

- **WHEN** `Load` is called for an account that was never stored
- **THEN** the backend SHALL return `ErrNotFound`

#### Scenario: Deleting an absent account returns ErrNotFound

- **WHEN** `Delete` is called for an account that does not exist
- **THEN** the backend SHALL return `ErrNotFound`

#### Scenario: A store failure is not reported as not-found

- **WHEN** a keychain operation fails for a reason other than a missing account (for example the store is unreachable)
- **THEN** the returned error SHALL NOT satisfy `errors.Is(err, ErrNotFound)`

