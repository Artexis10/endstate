# Tasks: backup subscribe checkout command

## Implementation Order

1. [x] OpenSpec change artifacts (proposal, spec delta, tasks)
2. [x] Engine: `Authenticator.Subscribe` + `CheckoutResponse` in authenticator.go (issuer-derived URL)
3. [x] Command: `runBackupSubscribe` + `SubscribeResult` in backup_subscribe.go
4. [x] Dispatch: add `subscribe` case + usage string in backup.go
5. [x] CLI help: list `subscribe` in cmd/endstate/main.go usage strings
6. [x] Contract: document `POST /api/billing/checkout` in hosted-backup-contract.md §7
7. [x] Tests: signed-in success, signed-out AUTH_REQUIRED, 402 SUBSCRIPTION_REQUIRED
8. [x] Verification: `go test ./internal/commands/... ./internal/backup/...` and `npm run openspec:validate`
