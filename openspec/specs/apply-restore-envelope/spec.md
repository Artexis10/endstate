# apply-restore-envelope Specification

## Purpose
Defines the JSON envelope extensions and restore journal for `apply --EnableRestore`, enabling GUI result display and revert support.
## Requirements
### Requirement: JSON Envelope Restore Extensions and Journal

The apply command SHALL extend its JSON envelope with `restoreItems[]` and `restoreSummary` when `--EnableRestore` is active, and write a restore journal for revert support.

#### Scenario: restoreItems array in JSON envelope

- **WHEN** `apply --EnableRestore --json` is run with restore entries
- **THEN** the JSON envelope `data` object contains a `restoreItems` array
- **AND** each element includes: id, module, restorer, source, target, status, reason, backupPath, targetExisted, message
- **AND** status is one of: "restored", "skipped_up_to_date", "skipped_missing_source", "failed"

#### Scenario: restoreSummary object in JSON envelope

- **WHEN** `apply --EnableRestore --json` is run with restore entries
- **THEN** the JSON envelope `data` object contains a `restoreSummary` object
- **AND** restoreSummary includes: total, restored, skipped, failed, backupLocation

#### Scenario: Existing items array unchanged

- **WHEN** `apply --EnableRestore --json` is run
- **THEN** the existing `items[]` array contains only app (install) entries
- **AND** restore results are NOT mixed into `items[]`

#### Scenario: No restore fields when EnableRestore not active

- **WHEN** `apply --json` is run WITHOUT `--EnableRestore`
- **THEN** the JSON envelope does NOT contain `restoreItems` or `restoreSummary` fields

#### Scenario: Restore journal written from apply

- **WHEN** `apply --EnableRestore` completes (non-dry-run)
- **THEN** a file `logs/restore-journal-{runId}.json` is written
- **AND** the journal uses the same schema as standalone restore's journal
- **AND** the journal is written even if some restore steps fail

#### Scenario: Apply uses restore strategy dispatch for multi-restorer support

- **WHEN** `apply --EnableRestore` processes restore entries
- **THEN** the restore package (`go-engine/internal/restore/`) dispatches each entry by its `type` field via `RunRestore`
- **AND** copy, merge-json, merge-ini, and append restorer types are supported
- **AND** requiresAdmin and requiresClosed checks are enforced
- **AND** exclude patterns are applied

#### Scenario: Consistency across apply paths

- **WHEN** `apply --Plan <plan.json> --EnableRestore --json` is run
- **THEN** the same restore convergence behavior applies as manifest-based apply
- **AND** restoreItems[], restoreSummary, and journal are produced identically

### Requirement: Restore Envelope Includes Config Generation Resolutions
When restore-capable input contains config payloads, apply, standalone restore, and rebuild JSON output SHALL include `configResolutions[]`, `configResolutionSummary`, and `restoreItems[]`. Each resolution SHALL include `captureId`, `moduleId`, `configSetId`, portable `sourceInstance`, source/target instance IDs, non-null `targetCandidates[]`, source/target generations and source-generation fingerprint when known, `resolution`, nullable `reason`, `migrationPath`, capture/restore module revisions, terminal `status`, and engine-authored `label`, `message`, and nullable `remediation`. Target candidates SHALL contain only portable, non-secret identity/version evidence; host-local roots SHALL remain internal. Legacy inputs SHALL use `legacy_unverified` rather than fabricated generation values.

When restore-capable input contains no config payloads, these config fields SHALL be omitted. When config payloads are present, `configResolutions[]`, `targetCandidates[]`, `migrationPath[]`, `resolvedTargets[]`, and `restoreItems[]` SHALL be present arrays and SHALL serialize as `[]`, never `null`, when empty. `reason` and `remediation` SHALL serialize as `null` when they have no value. Rebuild SHALL expose the canonical config fields at the top level of its command data; its nested apply result MAY mirror the same values.

The engine SHALL author the default distilled label, result message, remediation, and technical details. Consumers SHALL render those values verbatim and SHALL NOT reconstruct compatibility, presentation copy, or target evidence from module or bundle rules.

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

#### Scenario: Config-free command preserves the existing shape
- **WHEN** restore-capable input contains no generation-aware or explicit legacy module payload
- **THEN** config resolution, summary, and restore-item fields are omitted

#### Scenario: Payload with no concrete actions uses empty arrays
- **WHEN** input contains a config payload but filtering or safe non-execution produces no concrete restore action
- **THEN** the config arrays are present as `[]`
- **AND** no config array is `null`

#### Scenario: Rebuild exposes canonical config data
- **WHEN** rebuild consumes config payloads
- **THEN** its command data exposes canonical top-level config resolutions, summary, and restore items
- **AND** a nested apply result may mirror the same values without changing their semantics

### Requirement: Concrete Restore Items Link Back to Generation Plan
Concrete `restoreItems[]` produced from generation-aware config sets SHALL retain the existing restore-item fields and SHALL add optional `captureId`, `configSetId`, `targetInstanceId`, `sourceGeneration`, and `targetGeneration` fields. Application install `items[]` SHALL remain unchanged.

#### Scenario: Migrated file action is traceable
- **WHEN** a migrated config set commits a concrete restore action
- **THEN** its restore item links to the capture ID, config set, target instance, and source/target generations

### Requirement: Restore Journal Failures Are Command Failures
The JSON envelope SHALL report a command/config-set failure when durable journal intent cannot be written before target mutation. Journal write errors SHALL NOT be ignored or reported as successful restore.

#### Scenario: Journal intent cannot be persisted
- **WHEN** backup/staging succeeds but journal intent persistence fails
- **THEN** the result is failed with reason `journal_intent_failed`
- **AND** no target mutation is reported
