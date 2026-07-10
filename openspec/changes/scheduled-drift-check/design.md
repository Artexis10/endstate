# Design: scheduled-drift-check

## Vision reconciliation

`docs/vision/vision.md` states: "Not an always-on agent — it runs on demand, not continuously." This design keeps that property structurally: no daemon, no tray process, no long-lived listener. The Windows Task Scheduler invokes `endstate schedule run`, which does its work and exits — the same lifecycle as a user-invoked command. The schedule itself is user-created (opt-in), user-visible (Task Scheduler UI, `\Endstate\DriftCheck`), and user-removable (`schedule disable` or the Task Scheduler UI directly).

## The root problem: env vars vs Task Scheduler

The GUI spawns the engine with `ENDSTATE_ROOT=<exe_dir>\engine` (see `endstate-gui/src-tauri/engine-core/src/engine.rs`, `resolve_engine_path`). Task Scheduler `Exec` actions cannot set environment variables, so a scheduled run would resolve a different state dir than GUI-spawned runs — split-brain state.

**Decision:** `schedule enable` bakes the resolved root into the registered command line:

```
"<current exe>" schedule run --root "<resolved root>" --json
```

`schedule run --root <path>` treats the value exactly as an `ENDSTATE_ROOT` override. One state dir for both invocation paths.

**Consequence:** the baked path breaks if the user relocates the install. Mitigation: `enable` is idempotent (`schtasks /F`) and cheap, so the GUI re-asserts it on every launch when the user has the feature on — self-healing after app updates or moves.

## State files

Both under `state.StateDir()` (pattern: `go-engine/internal/state/state.go`), written with the existing atomic temp+rename discipline:

- `state/schedule/config.json`
  `{schemaVersion, enabled, manifest, interval, time, autoPush, taskName, root, registeredAt}`
- `state/schedule/last-run.json`
  `{schemaVersion, runId, timestampUtc, verify: {summary: {total, pass, fail}, drifted: [{id, name, status, reason}]}, autoBackup?: {outcome, backupId?, versionId?, skipped?}, error?: {code, message}}`

`schedule status` composes both and is the **only** interface clients read; the GUI never opens these files directly (CLI is source of truth).

## `schedule run` semantics

1. Load config (missing → `INTERNAL_ERROR`; disabled → `SCHEDULE_DISABLED`, recorded to `last-run.json` where possible).
2. Verify **in-process** (call into the same code path as `RunVerify`, `go-engine/internal/commands/verify.go`) against `config.manifest`. Drift is data: the process exits 0 and records the drifted set.
3. If `config.autoPush`: capture in-process, then push with `IfChanged: true` (reuses `go-engine/internal/backup/upload` and the keychain session from `go-engine/internal/backup/auth/session.go`). Auth-required / subscription-lapsed outcomes are recorded in `last-run.json` — never interactive, never a prompt.
4. Write `last-run.json` atomically. Hard errors (manifest missing, task misconfigured) are recorded with a stable `error.code` so clients can render "check failing" distinct from "no drift".

No NDJSON: a scheduled run has no attached consumer; the event contract (`docs/contracts/event-contract.md`, schema v1) is untouched. `--json` on `run` prints the last-run document as its envelope for manual/debug invocation.

## schtasks integration

- Wrapped in a new `go-engine/internal/schedule` package behind an interface (test seam) so command handlers are unit-testable without touching the real scheduler; the real implementation shells `schtasks.exe /Create /F /SC DAILY /ST <time> /TN Endstate\DriftCheck /TR "..."` (weekly: `/SC WEEKLY`).
- Runs as the current user, **interactive-only** ("run only when user is logged on"): the keychain (Credential Manager) needs the interactive session. Missed runs while powered off/asleep are accepted as daily best-effort in v1; XML-import registration (`StartWhenAvailable`) is the v2 escape hatch.
- Failure to register (`schtasks` non-zero) → stable `TASK_REGISTRATION_FAILED` error envelope; config is not marked enabled.

## Platform gating

- `features.schedule.supported` is `true` only on Windows (`GOOS == windows`); `schedule enable|disable|run` on other platforms return the stable `NOT_SUPPORTED` error envelope. Linux/macOS (cron/launchd) is deliberately out of scope for v1.
- `schedule run` on a disabled-but-configured schedule (Windows only, after the platform gate) returns `SCHEDULE_DISABLED` — a distinct code so clients can display an actionable message rather than a generic platform error.

## Cut line

If the schedule slips, `--auto-push` in `schedule run` is the cut: verify-only scheduled runs still deliver the drift badge (the flagship UX), and auto-push remains capture-triggered in the GUI as today.

## Risks

- **Baked task path vs app updates** → GUI launch re-assert (above).
- **Keychain from a non-interactive session** → interactive-only task; recorded outcome, no prompt.
- **State dir under an ACL-restricted install dir** → surfaced more by scheduled runs than GUI runs; verify during Windows E2E before merge.
- **In-flight churn** in `capabilities.go` (uncommitted darwin driver work) and `engine-core` — rebase before this branch touches shared files.
