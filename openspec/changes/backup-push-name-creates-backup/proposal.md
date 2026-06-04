## Why

`backup push` with `--name` but no `--backup-id` does not create a backup with that name once the account already has one — `resolveBackupID` returns `backups[0]` and silently ignores `--name`. This makes per-profile hosting impossible: every push collapses into the first backup. The GUI's unified per-profile model (one backup per profile, addressed by id) depends on a named push creating a distinct backup.

## What Changes

- **`backup push` resolution**: when `--backup-id` is absent and `--name` is non-empty, the engine **creates a new backup** labeled `--name` instead of appending to the first existing backup.
- Explicit `--backup-id` is unchanged (used verbatim → new version of that backup).
- Empty `--backup-id` **and** empty `--name` keeps the legacy convenience: append to the first existing backup, else create `"default"`.
- `resolveBackupID` is refactored to depend on a minimal `backupResolverStore` interface so the resolution logic is unit-tested with a fake (no behavior change from the refactor itself).

## Capabilities

### New Capabilities
- `backup-push-resolution`: how `backup push` chooses or creates the target backup from `--backup-id` / `--name`.

### Modified Capabilities
<!-- None: no existing spec covers push target resolution. -->

## Impact

- `go-engine/internal/backup/upload/upload.go` (`resolveBackupID`, `PushVersion` doc).
- `go-engine/internal/backup/upload/resolve_backup_id_test.go` (new unit tests).
- Consumer: the GUI `unified-per-profile-backups` change (separate `endstate-gui` repo) depends on this contract.
- Backward-compatible: only the previously-misbehaving "no id + named" path changes; auto-backup (which records and passes an id after first push) is unaffected.
