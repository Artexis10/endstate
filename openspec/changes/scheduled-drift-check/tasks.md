# Tasks: scheduled-drift-check

## 1. Engine — schedule package

- [ ] 1.1 `internal/schedule`: task-registrar interface (test seam) + real `schtasks.exe` implementation (`/Create /F`, `/Delete /F`, daily/weekly, interactive-only)
- [ ] 1.2 `internal/schedule`: config.json + last-run.json types and atomic read/write (temp+rename, per `internal/state` pattern)
- [ ] 1.3 Unit tests against the seam: idempotent enable, disable-keeps-config, registration-failure does not half-enable

## 2. Engine — command handlers

- [ ] 2.1 `schedule enable` (validate manifest path, write config, register task with baked `--root`)
- [ ] 2.2 `schedule disable`, `schedule status` (compose config + last-run)
- [ ] 2.3 `schedule run` (`--root` override → verify in-process → optional capture + push `--if-changed` → last-run.json; exit 0 on drift; stable error codes `NOT_SUPPORTED`, `TASK_REGISTRATION_FAILED`; no NDJSON)
- [ ] 2.4 `cmd/endstate/main.go` dispatch + help text (protected area — this change is the explicit instruction)
- [ ] 2.5 Capabilities: `features.schedule.{supported,autoPush}` + `commands.schedule` (additive), non-Windows dark
- [ ] 2.6 Unit tests: handlers, capabilities shape, last-run composition, error envelopes

## 3. Contract docs

- [ ] 3.1 `docs/contracts/cli-json-contract.md`: "Command: schedule" section + commands table row
- [ ] 3.2 `docs/contracts/gui-integration-contract.md`: capabilities example gains `features.schedule`

## 4. Verification

- [ ] 4.1 `cd go-engine && go test ./...`
- [ ] 4.2 `npm run openspec:validate`
- [ ] 4.3 Manual Windows E2E: enable → task visible in Task Scheduler UI → force-run → `last-run.json` written → `status --json` reflects drift after mutating a tracked app → disable removes task; check state-dir ACLs under the installed location

## 5. GUI (separate endstate-gui change, after engine ships)

- [ ] 5.1 `schedule-bridge.ts` (mirrors backup-bridge), types for `ScheduleStatusData`
- [ ] 5.2 Settings "Continuous protection" card (toggle + time; auto-push sub-toggle gated on `autoBackupAvailable`; requires a saved capture)
- [ ] 5.3 Launch: fetch `schedule status`, re-assert `schedule enable` when on (self-heal), drift chip on intent-landing "Save this computer" card
- [ ] 5.4 Vitest coverage per `backup-bridge.test.ts` patterns
