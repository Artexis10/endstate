# Tasks: scheduled-drift-check

## 1. Engine — schedule package

- [x] 1.1 `internal/schedule`: task-registrar interface (test seam) + real `schtasks.exe` implementation (`/Create /F`, `/Delete /F`, daily/weekly, interactive-only)
- [x] 1.2 `internal/schedule`: config.json + last-run.json types and atomic read/write (temp+rename, per `internal/state` pattern)
- [x] 1.3 Unit tests against the seam: idempotent enable, disable-keeps-config, registration-failure does not half-enable

## 2. Engine — command handlers

- [x] 2.1 `schedule enable` (validate manifest path, write config, register task with baked `--root`)
- [x] 2.2 `schedule disable`, `schedule status` (compose config + last-run)
- [x] 2.3 `schedule run` (`--root` override → verify in-process → optional capture + push `--if-changed` → last-run.json; exit 0 on drift; stable error codes `NOT_SUPPORTED`, `TASK_REGISTRATION_FAILED`; no NDJSON)
- [x] 2.4 `cmd/endstate/main.go` dispatch + help text (protected area — this change is the explicit instruction)
- [x] 2.5 Capabilities: `features.schedule.{supported,autoPush}` + `commands.schedule` (additive), non-Windows dark
- [x] 2.6 Unit tests: handlers, capabilities shape, last-run composition, error envelopes

## 3. Contract docs

- [x] 3.1 `docs/contracts/cli-json-contract.md`: "Command: schedule" section + commands table row
- [x] 3.2 `docs/contracts/gui-integration-contract.md`: capabilities example gains `features.schedule`

## 4. Verification

- [ ] 4.1 `cd go-engine && go test ./...`
- [x] 4.2 `npm run openspec:validate`
- [ ] 4.3 Manual Windows E2E: enable → task visible in Task Scheduler UI → force-run → `last-run.json` written → `status --json` reflects drift after mutating a tracked app → disable removes task; check state-dir ACLs under the installed location

## 5. GUI (separate endstate-gui change, after engine ships)

- [ ] 5.1 `schedule-bridge.ts` (mirrors backup-bridge), types for `ScheduleStatusData`
- [ ] 5.2 Scheduled drift-check settings card (toggle + time; auto-push sub-toggle gated on `autoBackupAvailable`; requires a saved capture)
- [ ] 5.3 Launch: fetch `schedule status`, re-assert `schedule enable` when on (self-heal), drift chip on intent-landing "Save this computer" card
- [ ] 5.4 Vitest coverage per `backup-bridge.test.ts` patterns

## Archive reconciliation — 2026-11-04

The engine implementation is present at `d76d3e5`. Checked engine tasks record
implemented code and existing tests, not an installed Windows acceptance result.
`internal/schedule/schedule.go` provides registration and atomic state files;
`internal/commands/schedule.go` provides enable, disable, status, run, and
`discard-upload`. `cmd/endstate/main.go` dispatches them. `capabilities.go` and
the two contract documents contain the additive schedule surface.

The current implementation also includes a locked run marker, an ordered local
capture queue, capability-gated Cloud replay, and explicit legacy-upload discard.
`schedule_test.go` and `schedule_replay_test.go` cover these engine boundaries.
The delta records these shipped additions and is synced before archive.
Authentication, subscription, and transport failures record stable auto-backup
outcomes; manifest and persistence failures use error codes. The reconciled
delta preserves this distinction rather than promising an auth error envelope.

Task 4.2 passes with OpenSpec 1.14.0 strict validation during this archive batch.
Task 4.1 remains unverified here: this documentation batch does not rerun the
full Go corpus. Existing test files establish coverage, not a fresh full-suite
result. The integration owner retains the full-corpus completion boundary.

Task 4.3 remains open for the Windows installed-product acceptance owner.
No Task Scheduler UI, installed-location ACL, or real Windows enable/run/disable
result was observed in this batch. Archive does not waive that acceptance.
The English-output matching in `internal/schedule/schedule.go:isNotFoundOutput`
is existing debt; the Linux shared-registrar work must preserve Windows behavior
without copying that error-classification mechanism.

Tasks 5.1–5.4 belong to the separate `endstate-gui` repository. Read-only source
inspection at `ffd500c` finds `src/lib/schedule-bridge.ts`, its test file,
`src/components/app/settings/continuous-protection-setting.tsx`, and schedule
status, launch reassertion, and drift presentation wiring in `src/App.tsx`.
Those bytes do not establish current rendered acceptance or a fresh Vitest run.
The GUI owner retains these unchecked acceptance tasks; this engine archive
neither edits that repository nor claims its delivery complete.

The Linux successor is `add-linux-product-parity`, section 11. Its task 11.1
owns the shared scheduler contract and the later systemd-user implementation.
This archive establishes the shipped Windows spec before that work begins.

The scoped Go recheck passes in `internal/schedule` (10 cases) and the
selected schedule/capability/replay tests in `internal/commands` (33 cases).
Eight Windows-only handler cases skip on Linux; no Windows execution is claimed.
