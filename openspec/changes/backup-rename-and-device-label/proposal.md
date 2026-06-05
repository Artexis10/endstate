## Why

A hosted backup's **label is write-once**: `name` is set at create (`CreateBackup`) and there is no way to change it afterward — no rename/relabel anywhere in the engine, storage client, or backend. Two consequences:

- The GUI's automatic per-machine backup is stuck showing the misleading `"This computer"` (on a *new* machine, restoring "This computer" is actually your *old* machine), and users cannot name a backup `"Gaming Rig"`.
- Any baked-in default (e.g. the OS hostname) would be permanent.

The clean fix is to make the label **mutable metadata** decoupled from the immutable backend **id** (a UUID). Identity stays the id; the label becomes editable. With that foundation, a better *default* label (the device/host name) is just the initial value, and future per-backup metadata (description, tags, …) is additive.

## What Changes

- **Mutable metadata via rename** — a new `backup rename --backup-id <id> --name <label>` command, backed by `storage.Client.UpdateBackup` → `PATCH /api/backups/:id` (substrate, separate change). The backend route is a **partial-metadata update**, so future fields extend it without a new endpoint/command.
- **Device-label create default** — when `backup push` creates a backup with neither `--backup-id` nor `--name` (the path that hardcoded `"default"`), it labels the new backup with the host name (`os.Hostname()`, trimmed), falling back to `"default"` when unavailable. The default-name is injected into `resolveBackupID` so the resolution logic stays unit-testable.
- **Capability advert** — `capabilities.features.hostedBackup.rename = true`, so the GUI gates its rename affordance on engine support (rename adds no new flag — it reuses `--backup-id`/`--name` — so a flag-based probe wouldn't detect it).

## Capabilities

### New Capabilities
- `backup-metadata`: a backup's mutable metadata (today: the display label) — how it is changed (`backup rename` → `PATCH`), the device-label create-default, and the capability advert. Identity remains the backend id; only the label moves.

### Modified Capabilities
<!-- None: backup-push-resolution (from the in-flight backup-push-name-creates-backup change) is not yet in the specs baseline, so the create-default label change is specced here under backup-metadata. The resolution ORDER is unchanged — only the create-default label. -->

## Impact

- `go-engine/internal/backup/storage/storage.go` — `UpdateBackup(ctx, id, name)` + `UpdatedBackup` (PATCH).
- `go-engine/internal/commands/backup_rename.go` (new) + dispatch/help in `backup.go`.
- `go-engine/internal/commands/capabilities.go` — `HostedBackupFeature.Rename`.
- `go-engine/internal/backup/upload/upload.go` — `deviceLabel()` + inject the default into `resolveBackupID`.
- Tests: `resolve_backup_id_test.go` (device-label default + `deviceLabelFrom`), `backup_rename_test.go` (validation).
- **Backend dependency (separate `substrate` change, PR open):** `PATCH /api/backups/:id` (`updateBackupOwned` + route). The engine emits `PATCH`; until substrate deploys it the backend returns 405.
- **Consumer (separate `endstate-gui` change):** a rename affordance gated on `hostedBackup.rename`; auto-backup keeps `"This computer"` until this ships, then relies on the device-label default / lets users rename.
- Cross-repo contract coupling (engine ↔ substrate ↔ GUI), per `CLAUDE.md`.
- Backward-compatible: existing backups (incl. legacy `"This computer"`, `"default"`) keep their labels; only newly-created default-named backups get the device label, and rename is opt-in.
