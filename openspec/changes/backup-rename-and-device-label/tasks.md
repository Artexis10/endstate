## 1. Rename: storage client + command

- [x] 1.1 `storage.Client.UpdateBackup(ctx, backupID, name)` + `UpdatedBackup{id,name,updatedAt}` → `PATCH /api/backups/:id` with `{name}`
- [x] 1.2 `backup rename` command (`backup_rename.go`): validate `--backup-id` + non-empty `--name`, delegate to `UpdateBackup`, report the server-echoed row
- [x] 1.3 Dispatch + help: add `rename` to `RunBackup`'s switch and the subcommand list in `backup.go`
- [x] 1.4 Validation tests (`backup_rename_test.go`): missing id, blank name (no backend call)

## 2. Device-label create default

- [x] 2.1 `deviceLabel()` (`upload.go`) = trimmed `os.Hostname()`, fallback `"default"`, never errors; pure `deviceLabelFrom(host, err)` split out for tests
- [x] 2.2 Inject the default into `resolveBackupID(ctx, store, backupID, name, defaultName)`; call site passes `deviceLabel()`; resolution order unchanged
- [x] 2.3 Tests: create-default uses the injected label (not `"default"`); `deviceLabelFrom` trim/fallback table

## 3. Capability advert

- [x] 3.1 `HostedBackupFeature.Rename bool` (`capabilities.go`), set `true`; GUI gates its rename UI on it

## 4. Verify & release

- [x] 4.1 `go build ./...`, `go vet ./internal/backup/... ./internal/commands/...`, `go test ./internal/backup/... ./internal/commands/...` green
- [x] 4.2 CLI smoke: `backup rename` (no args) → validation error; with `--backup-id`+`--name` → reaches backend (405 until substrate deploys), proving dispatch + flag parsing
- [x] 4.3 `openspec validate backup-rename-and-device-label --strict`
- [ ] 4.4 Merge → release-please cuts the engine release; GUI `ENGINE_VERSION` pin auto-bumps via `engine-drift-check`

## 5. Cross-repo

- [x] 5.1 Backend: `PATCH /api/backups/:id` (substrate change — PR open; deploy before the engine release is consumed)
- [x] 5.2 Update the hosted-backup contract doc with the PATCH endpoint + read-access gating note
- [ ] 5.3 GUI follow-up (separate `endstate-gui` change): `backupRename` bridge + rename affordance gated on `hostedBackup.rename`; auto-backup relies on the device-label default
- [ ] 5.4 (Deferred) per-profile auto-backup unification — needs a stable machine/profile id (engine), tracked separately
