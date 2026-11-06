## MODIFIED Requirements

### Requirement: Drift checks are scheduler-invoked, not resident

The engine SHALL provide opt-in scheduled drift checks through a short-lived
`schedule run` process. Windows SHALL retain Task Scheduler and the task name
`Endstate\DriftCheck`. Supported Linux sessions SHALL use the systemd user
backend defined by `linux-scheduled-drift-check`. No backend SHALL require a
resident Endstate process.

The shared registrar SHALL receive structured executable, argument, root,
manifest, interval, and time values. It SHALL expose availability, enable,
disable, and inspection operations. Each backend SHALL render its native
argument syntax; the command handler SHALL NOT supply a pre-quoted command.

Enable SHALL canonicalize explicit manifest and root coordinates before
persistence or registration. Repeated enable SHALL reassert one registration
with the current executable and desired configuration. Disable SHALL retain
configuration, run history, pending uploads, and replay evidence.

An optional backup ID SHALL retain its existing meaning. Without one, scheduled
Cloud delivery SHALL select an existing backup only when exactly one exists.
Zero or multiple backups SHALL return `BACKUP_SETUP_REQUIRED`; registration
changes SHALL NOT create a backup or change ambiguous-create replay semantics.

#### Scenario: Enable registers an idempotent task

- **WHEN** Windows invokes enable twice with the same daily schedule
- **THEN** exactly one task `Endstate\DriftCheck` SHALL reflect the current executable and configuration
- **AND** `config.json` SHALL record the enabled manifest, interval, and time

#### Scenario: Disable removes the task but keeps config

- **WHEN** Windows disables a successfully enabled schedule
- **THEN** the native task SHALL no longer exist
- **AND** `config.json` SHALL remain with enabled false

#### Scenario: Registration failure does not half-enable

- **WHEN** initial native task registration fails
- **THEN** the command SHALL fail with `TASK_REGISTRATION_FAILED`
- **AND** the persisted configuration SHALL NOT report the failed registration as enabled

#### Scenario: Windows uses structured registration

- **WHEN** Windows enables an existing schedule through the shared registrar
- **THEN** the backend SHALL preserve the task name, current-user scope, cadence, and short-lived command
- **AND** it SHALL encode each structured argument with Windows command-line rules

#### Scenario: Registration fails without claiming success

- **WHEN** a unit write, native registration operation, or configuration commit fails
- **THEN** the command SHALL return `TASK_REGISTRATION_FAILED` with a stable phase and reason
- **AND** it SHALL retain enough evidence for retry or disable without claiming that registration succeeded

#### Scenario: Initial configuration persistence fails

- **WHEN** the initial atomic configuration write fails during reconfiguration
- **THEN** the engine SHALL leave the existing native registration unchanged
- **AND** it SHALL preserve the prior configuration and pending upload evidence

### Requirement: Scheduled runs resolve the same state root as interactive runs

The registrar SHALL receive an absolute resolved engine root and executable.
Its native command SHALL invoke `schedule run --root <root> --json` through
structured arguments. The run SHALL resolve its saved manifest from that root
unless the existing manual manifest override applies. Timer, task, GUI, and
manual invocations SHALL use the same state and durable upload queue.

#### Scenario: Baked root matches the enabling process

- **WHEN** enable resolves the engine root to `R`
- **THEN** the registered argument vector SHALL contain `schedule run --root R --json`
- **AND** the invoked run SHALL read and write state beneath `R`

#### Scenario: Native arguments preserve the root

- **WHEN** a root contains spaces or characters with native expansion meaning
- **THEN** the invoked process SHALL receive the original root as one literal argument
- **AND** the backend SHALL NOT evaluate it through a shell

### Requirement: schedule status is the sole client-facing drift source

`schedule status --json` SHALL retain its existing configuration, pending-upload,
and last-run fields. It SHALL add backend-neutral registration observations
that distinguish saved intent from native availability and observed state.
Unavailable or failed inspection SHALL remain visible; unknown enabled, active,
or trigger values SHALL NOT become false values or invented timestamps.

The additive `registration` object SHALL expose `supported`, `backend`, optional
availability `reason`/`detail`, and independent `stateReason`/`stateDetail`.
Native `enabled`, `active`, and `nextTriggerUtc` SHALL be absent when unknown.
The object SHALL expose per-unit path, presence, ownership, integrity, SHA256,
fragment path, and drop-in paths when observed. Native service result, active
state, and exit status SHALL remain separate from the saved last-run result.
A failed result read SHALL expose `lastRunError.code: LAST_RUN_READ_FAILED`.

Status SHALL remain read-only. It SHALL preserve the distinction between no
saved run, an unreadable saved run, an interrupted running check, drift, and a
terminal failure. Linux inspection SHALL additionally satisfy the unit
ownership and integrity requirements in `linux-scheduled-drift-check`.

#### Scenario: Status surfaces drift for clients

- **WHEN** the last run records three drifted items
- **THEN** status SHALL return those items and the run timestamp under `lastRun`

#### Scenario: Status distinguishes a failing check from no drift

- **WHEN** the last run records a hard failure
- **THEN** status SHALL expose its code under `lastRun`
- **AND** it SHALL NOT describe the failed check as clean

#### Scenario: Native registration differs from saved intent

- **WHEN** saved intent is enabled but native inspection finds no enabled registration
- **THEN** status SHALL expose the mismatch with a stable reason
- **AND** it SHALL retain the last run and pending-upload observations independently

#### Scenario: Saved result is unreadable

- **WHEN** status cannot read or decode an existing last-run document
- **THEN** it SHALL expose a result-read failure
- **AND** it SHALL NOT describe that state as never run or healthy

### Requirement: Scheduling support is advertised as a capability

`features.schedule.supported` SHALL describe the shared manual operation and
result contract on Windows and Linux. `features.schedule.autoPush` and
`bundleManifestSupported` SHALL retain their existing meanings.
`features.schedule.registration` SHALL separately expose native registration
support, its backend when selected, and a stable unavailable reason.

`enable` and `disable` SHALL depend on usable native registration.
`status`, manual `run`, and durable replay/discard SHALL NOT require a running
scheduler. Run SHALL still require enabled saved configuration and SHALL retain
`SCHEDULE_DISABLED` and the existing durable result semantics when disabled.
Other unsupported host platforms SHALL retain `NOT_SUPPORTED` for operations
that they cannot execute. Status SHALL remain available without a backend.

#### Scenario: Windows advertises scheduling

- **WHEN** a client invokes capabilities on Windows
- **THEN** shared scheduling support SHALL remain true
- **AND** the command SHALL retain its manifest, interval, time, auto-push, root, and JSON flags

#### Scenario: Unsupported platform stays dark and fails stably

- **WHEN** a host supports neither the shared manual operations nor a native registrar
- **THEN** enable, disable, and run SHALL return `NOT_SUPPORTED`
- **AND** capabilities SHALL report shared scheduling support as false

#### Scenario: schedule run on disabled schedule returns SCHEDULE_DISABLED

- **WHEN** a supported host invokes run without enabled saved configuration
- **THEN** the command SHALL fail with `SCHEDULE_DISABLED`
- **AND** the last-run document SHALL record that error code

#### Scenario: Linux manager is unavailable

- **WHEN** Linux supports manual scheduling operations but cannot use a systemd user manager
- **THEN** capabilities SHALL report shared support and unavailable registration separately
- **AND** enable SHALL return `NOT_SUPPORTED` without installing a fallback scheduler

#### Scenario: Manual run is independent from native registration

- **WHEN** enabled saved configuration exists but the native timer is absent or unavailable
- **THEN** manual run SHALL verify and persist the shared result without registering a timer
- **AND** replay and confirmed discard SHALL retain the existing upload-preservation contract
