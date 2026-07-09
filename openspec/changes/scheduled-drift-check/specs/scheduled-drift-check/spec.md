## ADDED Requirements

### Requirement: Drift checks are scheduler-invoked, not resident

The engine SHALL provide scheduled drift checking without any resident process. `schedule enable` SHALL register an OS scheduled task (Windows Task Scheduler, task name `Endstate\DriftCheck`) that invokes a short-lived `endstate schedule run` and exits. Registration SHALL be idempotent: re-running `enable` SHALL re-assert the task with the current executable path and configuration. `schedule disable` SHALL remove the task and mark the persisted config disabled without deleting it.

#### Scenario: Enable registers an idempotent task
- **WHEN** `schedule enable --manifest <path> --interval daily --time 09:00` is invoked twice
- **THEN** exactly one scheduled task `Endstate\DriftCheck` exists, reflecting the latest executable path and configuration
- **AND** `state/schedule/config.json` records `{enabled: true, manifest, interval, time}`

#### Scenario: Disable removes the task but keeps config
- **WHEN** `schedule disable` is invoked after a successful enable
- **THEN** the scheduled task no longer exists
- **AND** `config.json` remains with `enabled: false`

#### Scenario: Registration failure does not half-enable
- **WHEN** task registration fails
- **THEN** the command fails with error code `TASK_REGISTRATION_FAILED`
- **AND** the persisted config is not marked enabled

### Requirement: Scheduled runs resolve the same state root as interactive runs

Because scheduler-executed actions cannot set environment variables, `schedule enable` SHALL bake the resolved engine root into the registered command line as `--root <path>`, and `schedule run --root <path>` SHALL treat that value as an `ENDSTATE_ROOT` override, so scheduled and interactively-spawned runs share one state directory.

#### Scenario: Baked root matches the enabling process
- **WHEN** `schedule enable` runs with an effective engine root `R`
- **THEN** the registered task command line contains `schedule run --root "R"`
- **AND** a run invoked by the task reads and writes state under `R`

### Requirement: schedule run verifies, optionally pushes, and records its result

`schedule run` SHALL verify the machine against the configured manifest in-process and write its outcome atomically to `state/schedule/last-run.json`. Detected drift SHALL NOT cause a non-zero exit (drift is data). When the config enables auto-push, the run SHALL capture and push with if-changed semantics using the persisted keychain session, recording the outcome; it SHALL never prompt interactively. Hard failures (missing manifest, auth required, subscription lapsed) SHALL be recorded in `last-run.json` with a stable error code. Scheduled runs SHALL emit no NDJSON events.

#### Scenario: Drift is recorded, exit stays zero
- **WHEN** `schedule run` finds 3 items drifted from the configured manifest
- **THEN** the process exits 0
- **AND** `last-run.json` records the run id, timestamp, and the 3 drifted items with reasons

#### Scenario: Auto-push reuses if-changed semantics
- **WHEN** `schedule run` executes with `autoPush: true` and the capture content hash is unchanged
- **THEN** the push outcome recorded in `last-run.json` is `skipped`

#### Scenario: Auth loss is recorded, not prompted
- **WHEN** `schedule run` executes with `autoPush: true` and no valid keychain session exists
- **THEN** the run completes verify, records an auth-required auto-backup outcome, and exits without prompting

### Requirement: schedule status is the sole client-facing drift source

`schedule status --json` SHALL report the persisted configuration and the last run's outcome (`{enabled, interval, time, autoPush, manifest, lastRun}`) composed from `config.json` and `last-run.json`. Clients SHALL be able to distinguish: never-run, last-run-succeeded-no-drift, last-run-found-drift, and last-run-failed.

#### Scenario: Status surfaces drift for clients
- **WHEN** the last scheduled run recorded 3 drifted items
- **THEN** `schedule status --json` returns those items and the run timestamp under `lastRun`

#### Scenario: Status distinguishes a failing check from no drift
- **WHEN** the last scheduled run recorded a hard error
- **THEN** `schedule status --json` exposes the error code under `lastRun` rather than reporting a clean state

### Requirement: Scheduling support is advertised as a capability

The capabilities envelope SHALL advertise scheduling additively: `features.schedule.supported` SHALL be `true` on Windows and `false` elsewhere, `features.schedule.autoPush` SHALL indicate auto-push support, and `commands.schedule` SHALL list the supported flags. On platforms where scheduling is unsupported, `schedule enable` SHALL fail with the stable error code `NOT_SUPPORTED`.

#### Scenario: Windows advertises scheduling
- **WHEN** a client invokes `capabilities --json` on Windows
- **THEN** `features.schedule.supported` is `true` and `commands.schedule.flags` includes `--manifest`, `--interval`, `--time`, `--auto-push`, `--root`, `--json`

#### Scenario: Unsupported platform stays dark and fails stably
- **WHEN** `schedule enable` is invoked on a non-Windows platform
- **THEN** it fails with error code `NOT_SUPPORTED`
- **AND** `capabilities --json` reports `features.schedule.supported: false`
