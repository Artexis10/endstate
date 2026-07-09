## Why

Endstate's value today fires only at a rare event — a reinstall, a new PC, a disaster. Between those events the engine is idle and the user's snapshot silently rots: apps drift from their pinned versions, configs change, new tools are installed and never captured. On-demand `verify` exists, but nobody remembers to run it.

A scheduled drift check makes Endstate continuously valuable without violating the vision boundary "Not an always-on agent — it runs on demand, not continuously" (`docs/vision/vision.md`): there is **no resident process**. The OS Task Scheduler invokes a short-lived engine run that verifies, optionally captures + pushes (`backup push --if-changed`), records its result, and exits. Each run is on-demand, deterministic, and auditable — the demander is the scheduler, visible in the Windows Task Scheduler UI.

## What Changes

- **New `schedule` command family** — `endstate schedule enable|disable|status|run`:
  - `enable --manifest <path> [--interval daily|weekly] [--time HH:MM] [--auto-push] [--json]` writes `state/schedule/config.json` (atomic temp+rename) and registers the Windows Scheduled Task `\Endstate\DriftCheck` via `schtasks.exe` (`/F`, idempotent re-assert). The task command line bakes the resolved root in as `--root` (Task Scheduler `Exec` actions cannot set env vars), so GUI-spawned and scheduled runs share one state dir.
  - `disable` deletes the task (`/F`) and sets `enabled: false` (config kept).
  - `status --json` reports `{enabled, interval, time, autoPush, manifest, lastRun}` from config + `last-run.json` — the sole drift-truth source for clients (CLI is source of truth).
  - `run [--root <path>] [--json]` is the task payload: verify in-process against the configured manifest; when `--auto-push` is configured, capture and `backup push --if-changed` via the keychain session; write `state/schedule/last-run.json` atomically. Exit 0 on drift (drift is data, not error); hard errors are recorded into `last-run.json` so clients can distinguish a stale/failing check from "no drift". Emits no NDJSON events (headless; event contract v1 untouched).
- **Capability adverts (additive, no schema bump)** — `features.schedule: { supported: <windows-only>, autoPush: true }` and `commands.schedule` with its flag list. On non-Windows, `schedule enable` returns a stable `NOT_SUPPORTED` error envelope and `features.schedule.supported` is `false`.

## Capabilities

### New Capabilities
- `scheduled-drift-check`: OS-scheduler-invoked, short-lived drift verification (and optional auto-push) with persisted, queryable results — enable/disable/status/run semantics, state files, and the capability advert.

### Modified Capabilities
<!-- None: verify, capture, and backup push are reused unchanged, in-process. -->

## Impact

- `go-engine/internal/schedule/` (new) — schtasks wrapper behind a test seam; config/last-run state file types.
- `go-engine/internal/commands/schedule*.go` (new) — enable/disable/status/run handlers.
- `go-engine/cmd/endstate/main.go` — parse/dispatch + help (protected area: this change is the explicit instruction).
- `go-engine/internal/commands/capabilities.go` — `features.schedule`, `commands.schedule`.
- Contract docs: `docs/contracts/cli-json-contract.md` (new "Command: schedule" section + commands table), `docs/contracts/gui-integration-contract.md` (capabilities example).
- **Consumer (separate `endstate-gui` change):** "Continuous protection" settings card, launch-time `schedule status` fetch + re-assert of `enable` (self-heals task path after app updates), drift chip on the intent landing. All drift logic stays in the CLI.
- Backward-compatible: purely additive surface; machines without the feature enabled are unaffected.
