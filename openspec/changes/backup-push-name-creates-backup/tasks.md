## 1. Resolution change

- [x] 1.1 Extract `backupResolverStore` interface (`ListBackups`, `CreateBackup`); change `resolveBackupID` to take it; pass `deps.Storage` at the call site
- [x] 1.2 Reorder resolution: id → named-create → list/first-or-default, so a named push with no id creates a new backup
- [x] 1.3 Update the `PushVersion` doc comment to match the new contract

## 2. Tests

- [x] 2.1 Unit tests with a fake store: named+existing → create new; explicit id → verbatim (no storage calls); no-name/no-id → first; no-name/no-id/empty → default
- [x] 2.2 `go test ./internal/backup/... ./internal/commands/...` green; `go vet` clean

## 3. Release / consumer

- [ ] 3.1 Merge → release-please cuts the engine release; GUI pin auto-bumps via `engine-drift-check`
- [ ] 3.2 GUI `unified-per-profile-backups` change implements the per-profile hosting on top of this contract
