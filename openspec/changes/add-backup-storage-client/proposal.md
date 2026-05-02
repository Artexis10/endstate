## Why

`add-backup-auth-client` shipped the auth scaffold. This change layers the storage client on top: the `endstate backup push|pull|list|versions|delete|delete-version|recover` commands, plus the `endstate account delete` handler. It implements the substrate `/api/backups/*` API surface (contract §7) and the chunked upload/download orchestration (contract §3, §8).

Crypto is still STUB — push/pull/recover return `INTERNAL_ERROR` "crypto: not yet implemented" in this change. The orchestration code is real and tested with a fake-crypto test double via httptest. The crypto change (`add-backup-crypto-module`) replaces the stub bodies in a follow-up PR; nothing in this change needs to change at that point.

The two transport sentinels in the contract — `chunkIndex = -1` on the wire (presigned-URL responses) and `0xFFFFFFFF` as AAD inside the manifest blob — are documented and treated as independent throughout (§3 cryptographic binding vs. §7 transport flag).

## What Changes

- New command handlers: `runBackupPush`, `runBackupPull`, `runBackupList`, `runBackupVersions`, `runBackupDelete`, `runBackupDeleteVersion`, `runBackupRecover`, `runAccountDelete`
- New packages: `internal/backup/manifest/`, `internal/backup/upload/`, `internal/backup/download/`
- Push/pull emit phase + item events (`driver: "hosted-backup"`) for GUI progress UI
- Destructive operations (`backup delete`, `backup delete-version`, `account delete`) require `--confirm`
- `backup status` extends to populate `lastBackupAt` from the cached state (when known)

## Capabilities

### New Capabilities

- `hosted-backup-storage-client`: Engine orchestration of substrate `/api/backups/*` and the `chunkIndex=-1` manifest URL convention. Covers list/versions/push/pull/delete/recover plus chunked upload/download with bounded concurrency and SHA-256 integrity verification.

### Modified Capabilities

- `hosted-backup-auth-client`: extends `runBackupStatus` to populate `lastBackupAt` and registers the new subcommands in the dispatcher.

## Impact

- **`go-engine/internal/commands/backup_*.go`** — eight new command handler files
- **`go-engine/internal/commands/account.go`** — `delete` handler implemented (replaces stubbed-not-implemented from the auth-client change)
- **`go-engine/internal/backup/manifest/`** — manifest serialization + version selection logic
- **`go-engine/internal/backup/upload/`** — bounded-concurrency chunk upload via presigned URLs
- **`go-engine/internal/backup/download/`** — bounded-concurrency chunk download with SHA-256 integrity verify
- **`go-engine/internal/backup/backup.go`** — surfaces `Concurrency()` to upload/download (already added in `add-backup-auth-client` for forward compatibility)
- **README** — extends the Hosted Backup section with the full command surface
