## ADDED Requirements

### Requirement: Linux scheduling extends one platform-neutral schedule contract
Before Linux schedule support is implemented or advertised, the active `scheduled-drift-check` change and public CLI contract SHALL be reconciled so Windows and Linux select scheduler backends behind one structured interface. The interface SHALL accept structured engine/root/manifest/cadence intent and expose availability, enable, disable, and inspect operations. Each backend SHALL own its native escaping and registration format; command handlers SHALL NOT pass a pre-quoted command string as the cross-platform contract.

The shared command family SHALL retain `schedule enable`, `disable`, `status`, `run`, and `discard-upload`. Manual `schedule run` and durable Cloud replay/discard semantics SHALL be scheduler-independent once the host supports the underlying verification/Cloud operation. A platform with no usable scheduler backend SHALL return stable unsupported behavior for registration without making the manual result model Windows-only.

#### Scenario: Windows behavior is preserved through the shared interface
- **WHEN** the structured scheduler interface replaces the Windows-shaped registrar
- **THEN** existing Windows task name, registration, disable, run, replay, and result semantics SHALL remain covered by regression tests
- **AND** the Windows backend SHALL own Windows command-line quoting

#### Scenario: Linux backend renders structured intent
- **WHEN** Linux enable receives structured engine, root, manifest, interval, time, and auto-push intent
- **THEN** the systemd backend SHALL render native unit fields without shell evaluation
- **AND** command handlers SHALL NOT pre-quote an `ExecStart` string for the backend

#### Scenario: Manual run does not require timer registration
- **WHEN** a supported Linux user invokes `schedule run` with valid saved configuration but no enabled timer
- **THEN** the command SHALL execute the shared short-lived run semantics without requiring a usable systemd user manager
- **AND** only scheduler registration commands SHALL depend on scheduler availability

#### Scenario: Ambiguous Cloud upload exists on Linux
- **WHEN** a Linux manual or timer run leaves a durable ambiguous upload record
- **THEN** `schedule discard-upload` SHALL use the same confirmation, lock, and preservation semantics as Windows
- **AND** it SHALL NOT fail merely because the host is non-Windows

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
Repeated `schedule enable` calls with the same desired configuration SHALL converge to the same units and active timer. The generated timer SHALL use persistent missed-run behavior. Exact unit contents, resolved engine path/root, manifest, cadence, ownership marker/version, content integrity, and next/last-run state SHALL be inspectable through the CLI/status details and ordinary systemd user tooling. Unit writes SHALL be atomic, and partial write/reload/enable/config-persistence failures SHALL leave or recover to an explicitly reported non-enabled state.

#### Scenario: Enable is repeated after an application update
- **WHEN** `schedule enable` is reasserted with a new resolved engine path and otherwise unchanged settings
- **THEN** the engine SHALL atomically replace its generated unit content, reload the user manager, and keep one enabled timer
- **AND** it SHALL NOT create duplicate timers or services

#### Scenario: Host resumes after a missed run
- **WHEN** the configured timer elapsed while the user manager was stopped or the machine was suspended
- **THEN** the persistent timer SHALL schedule the missed short-lived run when systemd permits

#### Scenario: Registration fails after one unit is staged
- **WHEN** service creation succeeds but timer creation, daemon reload, enable/start, or enabled-config persistence fails
- **THEN** enable SHALL report the exact stable registration phase and SHALL NOT report the schedule enabled
- **AND** retry or disable SHALL deterministically reconcile only recognizably Endstate-owned staged state

#### Scenario: Owned unit was modified by the user
- **WHEN** re-enable finds a unit with a valid Endstate ownership marker but content that differs from both the recorded generated hash and the newly desired bytes
- **THEN** enable SHALL report a modified-unit ownership conflict instead of silently overwriting it
- **AND** an explicit future conflict-resolution action SHALL be required to replace it

#### Scenario: Different roots target the same user timer
- **WHEN** two engine roots concurrently enable the fixed user timer
- **THEN** registration SHALL serialize in the shared user-unit namespace and recheck ownership
- **AND** one root SHALL NOT overwrite the other root's committed or staged registration

#### Scenario: Persistent timer fires during enable
- **WHEN** timer activation immediately triggers a missed run while enable holds the root state lock
- **THEN** the short-lived run SHALL wait until registration commits or fails
- **AND** it SHALL NOT consume the catch-up as a lock-contention failure

#### Scenario: Process stops before enabled configuration commits
- **WHEN** the process stops after a native change but before the final configuration commit
- **THEN** persisted pending intent SHALL retain the scope and expected unit contents
- **AND** retry or disable SHALL reconcile that state without accepting unrelated unit contents

#### Scenario: Selected unit directory is not used by the manager
- **WHEN** the caller resolves a user unit directory outside the actual manager's load path
- **THEN** registration SHALL report an unavailable namespace before writing generated units
- **AND** it SHALL NOT claim success from configuration files the manager cannot load

#### Scenario: Another unit overrides generated contents
- **WHEN** a different fragment or drop-in changes the owned execution or cadence fields
- **THEN** registration SHALL report an ownership conflict before activation
- **AND** it SHALL preserve the overriding files

#### Scenario: Inherited policy preserves the schedule
- **WHEN** resource, security, environment, or logging drop-ins preserve the owned execution and cadence fields
- **THEN** registration SHALL allow that policy and expose the drop-in paths
- **AND** it SHALL NOT require the user to remove benign manager policy

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
`schedule status` on Linux SHALL report both the saved Endstate schedule intent and the actual systemd user timer state, including backend availability, enabled/active state, next trigger when available, unit presence/ownership/content integrity, last persisted run, and drift/error state. A mismatch SHALL use stable backend-neutral reason codes and remain visible rather than silently treating configuration as registration truth.

#### Scenario: Config says enabled but timer is missing
- **WHEN** saved schedule configuration is enabled and the generated timer is absent or disabled
- **THEN** status SHALL report the registration mismatch
- **AND** it SHALL offer re-enable/reassert remediation

#### Scenario: Last run failed
- **WHEN** the short-lived service persisted a stable error result
- **THEN** status SHALL return that error state separately from package/settings drift

### Requirement: Hosts without a usable systemd user manager fail registration safely
Linux capabilities SHALL distinguish the shared manual schedule operation/result contract from OS registration availability. `features.schedule.supported` SHALL report whether manual `status`, `run`, and durable replay/discard semantics are supported on the host. `features.schedule.registration.supported` SHALL report whether the engine can use the systemd user manager and resolved user unit directory, with backend `systemd-user` when true and a stable unavailable reason when false. On hosts without registration support, enable/disable registration operations SHALL return a stable unsupported result and SHALL write no cron entry, shell-profile hook, system unit, or fallback scheduler state. Core Linux discovery/capture/apply capabilities SHALL remain unaffected.

#### Scenario: systemd user manager is unavailable
- **WHEN** schedule enable runs on Linux without a usable `systemctl --user` manager
- **THEN** the engine SHALL report schedule registration unsupported with actionable detail
- **AND** it SHALL make no scheduler mutation

#### Scenario: Capabilities on supported and unsupported Linux hosts
- **WHEN** capabilities probes Linux scheduling
- **THEN** `features.schedule.supported` SHALL describe the shared manual command/result support independently from registration
- **AND** `features.schedule.registration.supported`, backend, and unavailable reason SHALL reflect the usable user manager

#### Scenario: Manual schedule operations remain available without registration
- **WHEN** Linux supports the shared schedule command/result contract but has no usable systemd user manager
- **THEN** capabilities SHALL report `features.schedule.supported: true` and `features.schedule.registration.supported: false`
- **AND** manual `status`, `run`, and durable replay/discard SHALL remain available while `enable` reports registration unsupported

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
