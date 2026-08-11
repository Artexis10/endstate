## ADDED Requirements

### Requirement: Scheduled Cloud delivery does not repeat ambiguous mutations

The scheduler SHALL not repeat an ambiguous Cloud create mutation. When
discovery explicitly advertises `version-create-operation-replay-v1`, it SHALL persist the resolved existing backup ID, operation ID, and
byte-exact encrypted create payload before its first create-version POST. A
restart SHALL reuse that payload and SHALL fail closed if the persisted spool
is corrupt or the capability later disappears. Without that capability it
SHALL record legacy create-started before the POST and SHALL not automatically
repeat an ambiguous response; definite pre-mutation 4xx failures clear the
marker. If no existing backup row is available, it SHALL return
`BACKUP_SETUP_REQUIRED` with auto-backup outcome `setup_required` rather than
attempting `CreateBackup`.

#### Scenario: Terminal replay drains before cleanup

- **WHEN** a replay returns `alreadyCommitted: true`
- **THEN** the queue removal SHALL be persisted before its ciphertext spool is
  deleted
- **AND** the engine SHALL send no object PUTs

#### Scenario: Explicit legacy-create resolution

- **WHEN** a pending upload has `legacyCreateStarted: true` after an uncertain create
- **THEN** `schedule discard-upload --artifact-sha256 <sha> --confirm` SHALL remove only that queue record under `run.lock`
- **AND** it SHALL preserve the local artifact and baseline while reporting that Cloud may already contain the version

### Requirement: Drift checks are scheduler-invoked, not resident

The engine SHALL provide scheduled drift checking without any resident process. `schedule enable` SHALL register an OS scheduled task (Windows Task Scheduler, task name `Endstate\DriftCheck`) that invokes a short-lived `endstate schedule run` and exits. Registration SHALL be idempotent: re-running `enable` SHALL re-assert the task with the current executable path and configuration. `schedule disable` SHALL remove the task and mark the persisted config disabled without deleting it.

`schedule enable` SHALL canonicalise explicit `--manifest` and `--root` paths to absolute-clean values before persisting or registering the task. An optional `--backup-id` SHALL be persisted and used for every scheduled Cloud delivery. When absent, the engine SHALL select an existing backup only if exactly one exists; zero or multiple backups SHALL return `BACKUP_SETUP_REQUIRED` and SHALL never select a positional list entry.

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

`schedule run` SHALL verify the machine against the configured manifest in-process and write its outcome atomically to `state/schedule/last-run.json`. After acquiring `run.lock` and before reading configuration, verification, capture, or Cloud work, it SHALL atomically replace any prior result with `{status: "running"}` for the new run. Terminal writes SHALL replace that marker with `status: "completed"` or `status: "failed"`; clients SHALL treat `running` as non-healthy. Detected drift SHALL NOT cause a non-zero exit (drift is data). When the config enables auto-push, every detected drift SHALL first publish a fresh local baseline and append it to an ordered durable upload queue, then retry queued captures oldest-first with if-changed semantics using the persisted keychain session. Authentication and transport failure SHALL leave queued local captures intact and SHALL never prompt interactively; corrupt queue entries SHALL be quarantined without blocking later entries. Hard failures (missing manifest, auth required, subscription lapsed, last-run persistence failure) SHALL be reported with a stable error code. Scheduled runs SHALL emit no NDJSON events.

#### Scenario: Interrupted run does not retain an earlier healthy result

- **WHEN** a run has acquired `run.lock` and is interrupted before terminal persistence
- **THEN** `last-run.json` reports the new run with `status: "running"`
- **AND** it SHALL not expose the previous healthy verification result

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

The capabilities envelope SHALL advertise scheduling additively: `features.schedule.supported` SHALL be `true` on Windows and `false` elsewhere, `features.schedule.autoPush` SHALL indicate auto-push support, and `commands.schedule` SHALL list the supported flags. On platforms where scheduling is unsupported, `schedule enable`, `schedule disable`, and `schedule run` SHALL fail with the stable error code `NOT_SUPPORTED`. When `schedule run` is invoked on a Windows host where no schedule has been enabled, it SHALL fail with the stable error code `SCHEDULE_DISABLED` (distinct from the platform gate, so clients can render an actionable message).

#### Scenario: Windows advertises scheduling
- **WHEN** a client invokes `capabilities --json` on Windows
- **THEN** `features.schedule.supported` is `true` and `commands.schedule.flags` includes `--manifest`, `--interval`, `--time`, `--auto-push`, `--root`, `--json`

#### Scenario: Unsupported platform stays dark and fails stably
- **WHEN** `schedule enable`, `schedule disable`, or `schedule run` is invoked on a non-Windows platform
- **THEN** it fails with error code `NOT_SUPPORTED`
- **AND** `capabilities --json` reports `features.schedule.supported: false`

#### Scenario: schedule run on disabled schedule returns SCHEDULE_DISABLED
- **WHEN** `schedule run` is invoked on Windows but no schedule has been enabled (`config.enabled: false`)
- **THEN** it fails with error code `SCHEDULE_DISABLED`
- **AND** `last-run.json` records the error with code `SCHEDULE_DISABLED`
