# Tasks: endstate-rebuild

## 1. Engine

- [ ] 1.1 `envelope/errors.go`: add `CONFIRMATION_REQUIRED` error code (additive, schema 1.x)
- [ ] 1.2 `commands/rebuild.go`: `RebuildFlags`, `RebuildResult`, `RunRebuild` — validate `--from` (empty → `MANIFEST_VALIDATION_ERROR`; `://` → `NOT_SUPPORTED`; missing → `MANIFEST_NOT_FOUND`); confirmation gate before any mutation; branch on `bundle.IsBundle` (extract via `bundle.ExtractBundle`, deferred temp cleanup, best-effort metadata) vs bare manifest; call `RunApply` then (unless dry-run) `RunVerify`; assemble result; verify failures are data
- [ ] 1.3 `cmd/endstate/main.go`: parse `--from` (value-taking) and `--no-restore`; usage + `commandUsage` rebuild case; dispatch `case "rebuild"` (protected area — this change is the explicit instruction)
- [ ] 1.4 `capabilities.go`: add `rebuild` to the commands map with its flag set

## 2. Contract docs

- [ ] 2.1 `docs/contracts/cli-json-contract.md`: `rebuild` command section (synopsis, flags, response shape, "verify failures are data; exit 0") + `CONFIRMATION_REQUIRED` in the error-code table (additive; no schema bump)
- [ ] 2.2 `docs/contracts/event-contract.md`: note that `rebuild` composes the apply and verify streams; no new event types; schema stays v1
- [ ] 2.3 `readme.md`: fresh-machine quickstart block + a `rebuild` row in the CLI commands table

## 3. Tests (hermetic)

- [ ] 3.1 `commands/rebuild_test.go`: capture → rebuild round-trip (restored content equals captured content; apply+verify summaries present)
- [ ] 3.2 bare `.jsonc` rebuild installs; bundle nil in result
- [ ] 3.3 confirmation gate: no `--confirm` (not dry-run/no-restore) → `CONFIRMATION_REQUIRED`, zero installs
- [ ] 3.4 `--dry-run` without confirm → succeeds, no verify, no installs
- [ ] 3.5 `--no-restore` without confirm → succeeds, restore targets untouched, restore disabled
- [ ] 3.6 input validation: URL → `NOT_SUPPORTED`; missing path → `MANIFEST_NOT_FOUND`; zip without manifest → `MANIFEST_PARSE_ERROR`
- [ ] 3.7 temp extraction dir removed after success and after a mid-pipeline install error
- [ ] 3.8 events: first event is phase, last is summary
- [ ] 3.9 `capabilities_test.go`: `commands.rebuild.flags` includes `--from`
- [ ] 3.10 verify-failure run → success envelope, verify summary fail > 0

## 4. Verification

- [ ] 4.1 `cd go-engine && go build ./cmd/endstate`
- [ ] 4.2 `cd go-engine && go test ./internal/commands/... ./internal/bundle/...`
- [ ] 4.3 `cd go-engine && go vet ./internal/commands/ ./cmd/endstate/`
- [ ] 4.4 `npm run openspec:validate`

## 5. GUI (separate endstate-gui change, after engine ships)

- [ ] 5.1 One-click rebuild affordance gated on `commands.rebuild`, passing `--from` and `--confirm`
