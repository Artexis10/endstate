## ADDED Requirements

### Requirement: Restore Envelope Includes Config Generation Resolutions
When restore-capable input contains config payloads, apply, standalone restore, and rebuild JSON output SHALL include `configResolutions[]` and `configResolutionSummary`. Each resolution SHALL include `captureId`, `moduleId`, `configSetId`, source/target instance IDs, source/target generations and source-generation fingerprint when known, `resolution`, `reason`, `migrationPath`, capture/restore module revisions, and terminal `status`. Legacy inputs SHALL use `legacy_unverified` rather than fabricated generation values.

`resolution` and `status` SHALL be independent: resolution describes compatibility, while status describes the terminal execution outcome for the invocation. Status SHALL be exactly one of `planned`, `restored`, `skipped`, `failed`, `rolled_back`, or `rollback_failed`; in-progress values SHALL NOT appear in the envelope. `planned` SHALL be dry-run-only for a selected set that passed compatibility, integrity, preflight, and staging validation. `restored` SHALL mean a live transaction reached and validated the desired target state and durably recorded completion. `skipped` SHALL mean no target mutation was attempted and non-execution was intentional or safely required, including filtering/consent, unknown or incompatible resolution, absent/incompatible mapped target, `app_running`, or an already-up-to-date target. `failed` SHALL mean the selected set could not complete before any target mutation occurred. `rolled_back` SHALL mean mutation began and rollback durably restored and verified the complete pre-run state. `rollback_failed` SHALL mean mutation began and complete restoration could not be proven; the engine SHALL start no later config-set mutation in that run.

For `failed`, `rolled_back`, and `rollback_failed`, `reason` SHALL retain the primary execution failure; rollback outcome SHALL be represented by `status`. `configResolutionSummary.selected` SHALL count `planned`, `restored`, `failed`, `rolled_back`, and `rollback_failed`; `skipped` SHALL count `skipped`; and `failed` SHALL count `failed`, `rolled_back`, and `rollback_failed`.

#### Scenario: Dry-run exposes migration plan
- **WHEN** a dry-run resolves a config set through a forward migration
- **THEN** `configResolutions[]` reports `resolution: "migrate"`
- **AND** reports `status: "planned"`
- **AND** includes the ordered migration path without changing host configuration

#### Scenario: Pre-mutation execution failure is failed
- **WHEN** a selected config set fails integrity, target-collision, backup, journal-intent, or staging validation before any target mutation
- **THEN** `configResolutions[]` reports `status: "failed"`
- **AND** `reason` reports the primary failure
- **AND** no rollback outcome is implied

#### Scenario: Failed transaction is restored by rollback
- **WHEN** a config-set transaction fails after target mutation begins
- **AND** rollback durably restores and verifies the complete pre-run state
- **THEN** `configResolutions[]` reports `status: "rolled_back"`
- **AND** `reason` retains the primary transaction failure

#### Scenario: Failed rollback stops later config mutation
- **WHEN** a config-set transaction fails after target mutation begins
- **AND** rollback cannot prove complete restoration
- **THEN** `configResolutions[]` reports `status: "rollback_failed"`
- **AND** `reason` retains the primary transaction failure
- **AND** the engine starts no later config-set mutation in that run

#### Scenario: Legacy input exposes warning state
- **WHEN** apply/rebuild consumes legacy config payloads
- **THEN** `configResolutions[]` reports `legacy_unverified`
- **AND** omits unknown source/target generation values
- **AND** uses the same terminal status vocabulary as generation-aware input

#### Scenario: Summary counts every resolution
- **WHEN** config resolutions contain direct, migrate, unknown, incompatible, and legacy entries
- **THEN** `configResolutionSummary` reports totals for each state and selected/skipped/failed execution counts
- **AND** `failed`, `rolled_back`, and `rollback_failed` all contribute to the failed execution count

#### Scenario: Standalone restore uses identical vocabulary
- **WHEN** standalone restore consumes generation-aware or legacy config payloads with JSON output
- **THEN** it emits the same `configResolutions[]` fields, states, reasons, and summary semantics as apply/rebuild

### Requirement: Concrete Restore Items Link Back to Generation Plan
Concrete `restoreItems[]` produced from generation-aware config sets SHALL retain the existing restore-item fields and SHALL add optional `captureId`, `configSetId`, `targetInstanceId`, `sourceGeneration`, and `targetGeneration` fields. Application install `items[]` SHALL remain unchanged.

#### Scenario: Migrated file action is traceable
- **WHEN** a migrated config set commits a concrete restore action
- **THEN** its restore item links to the capture ID, config set, target instance, and source/target generations

### Requirement: Restore Journal Failures Are Command Failures
The JSON envelope SHALL report a command/config-set failure when durable journal intent cannot be written before target mutation. Journal write errors SHALL NOT be ignored or reported as successful restore.

#### Scenario: Journal intent cannot be persisted
- **WHEN** backup/staging succeeds but journal intent persistence fails
- **THEN** the result is failed with a structured journal reason
- **AND** no target mutation is reported
