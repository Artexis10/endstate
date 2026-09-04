## ADDED Requirements

### Requirement: Linux schedule registration uses systemd user scope
On Linux with a usable systemd user manager, `schedule enable` SHALL register the shared short-lived drift-check command through Endstate-owned user units and SHALL NOT require root, write a system unit, or start a resident Endstate process. The generated service SHALL invoke `schedule run` and exit; the timer SHALL trigger it at the configured cadence.

#### Scenario: Enable on a supported Linux session
- **WHEN** the user enables a valid Endstate schedule on Linux and `systemctl --user` is usable
- **THEN** the engine SHALL write inspectable Endstate-owned service/timer units under the resolved user unit directory
- **AND** it SHALL enable the timer through the user manager without invoking sudo or system scope

#### Scenario: Scheduled run is short-lived
- **WHEN** the timer fires
- **THEN** systemd SHALL start one `schedule run` process using the saved Endstate root/manifest coordinates
- **AND** the process SHALL exit after persisting the result

### Requirement: Linux timer registration is idempotent, persistent, and inspectable
Repeated `schedule enable` calls with the same desired configuration SHALL converge to the same units and active timer. The generated timer SHALL use persistent missed-run behavior. Exact unit contents, resolved engine path/root, manifest, cadence, and next/last-run state SHALL be inspectable through the CLI/status details and ordinary systemd user tooling.

#### Scenario: Enable is repeated after an application update
- **WHEN** `schedule enable` is reasserted with a new resolved engine path and otherwise unchanged settings
- **THEN** the engine SHALL atomically replace its generated unit content, reload the user manager, and keep one enabled timer
- **AND** it SHALL NOT create duplicate timers or services

#### Scenario: Host resumes after a missed run
- **WHEN** the configured timer elapsed while the user manager was stopped or the machine was suspended
- **THEN** the persistent timer SHALL schedule the missed short-lived run when systemd permits

### Requirement: Disable and cleanup affect only Endstate-owned user units
`schedule disable` SHALL disable the Endstate timer, remove only the generated Endstate service/timer files it owns, reload the user manager, and preserve the schedule configuration needed for status/history unless the shared command contract explicitly requests deletion. It SHALL NOT remove similarly named user-authored or system units.

#### Scenario: Disable an enabled schedule
- **WHEN** the user disables an Endstate Linux schedule
- **THEN** the timer SHALL become disabled and inactive
- **AND** the engine SHALL remove its generated user-unit files while retaining shared schedule history/config as defined by the command contract

#### Scenario: Generated unit identity does not match
- **WHEN** a target unit file exists but is not recognizably owned by Endstate's generated-unit format
- **THEN** cleanup SHALL refuse to overwrite or delete it
- **AND** it SHALL report an ownership conflict with remediation

### Requirement: Status reconciles saved intent with live user-manager state
`schedule status` on Linux SHALL report both the saved Endstate schedule intent and the actual systemd user timer state, including enabled/active state, next trigger when available, last persisted run, and drift/error state. A mismatch SHALL be visible rather than silently treating configuration as registration truth.

#### Scenario: Config says enabled but timer is missing
- **WHEN** saved schedule configuration is enabled and the generated timer is absent or disabled
- **THEN** status SHALL report the registration mismatch
- **AND** it SHALL offer re-enable/reassert remediation

#### Scenario: Last run failed
- **WHEN** the short-lived service persisted a stable error result
- **THEN** status SHALL return that error state separately from package/settings drift

### Requirement: Hosts without a usable systemd user manager fail safely
Linux scheduling SHALL advertise supported only when the engine can use the systemd user manager and resolved user unit directory. On unsupported hosts, schedule mutation commands SHALL return a stable unsupported result and SHALL write no cron entry, shell-profile hook, system unit, or fallback scheduler state. Core Linux discovery/capture/apply capabilities SHALL remain unaffected.

#### Scenario: systemd user manager is unavailable
- **WHEN** schedule enable runs on Linux without a usable `systemctl --user` manager
- **THEN** the engine SHALL report scheduling unsupported with actionable detail
- **AND** it SHALL make no scheduler mutation

#### Scenario: Capabilities on supported and unsupported Linux hosts
- **WHEN** capabilities probes Linux scheduling
- **THEN** `features.schedule.supported` SHALL reflect the usable user manager rather than Linux OS identity alone
- **AND** discovery/apply backend capabilities SHALL be reported independently

### Requirement: Linux scheduled-run semantics match the shared schedule contract
The Linux timer payload SHALL use the same saved schedule configuration, verification, optional capture/push, last-run schema, exit semantics, and stable error vocabulary as manual and Windows scheduled runs. Linux registration details SHALL NOT fork the user-facing command/result model.

#### Scenario: Timer and manual run produce the same result shape
- **WHEN** the same enabled schedule is run once by the timer and once through `schedule run`
- **THEN** both executions SHALL persist and return the shared last-run schema
- **AND** drift SHALL remain data rather than a process failure

#### Scenario: Auto-push needs unavailable authentication
- **WHEN** a headless scheduled run cannot use the required user credential/session
- **THEN** it SHALL record the shared actionable auto-backup outcome
- **AND** it SHALL not prompt, hang, or expose credential material in logs
