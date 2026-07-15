## ADDED Requirements

### Requirement: Restore Envelope Includes Config Generation Resolutions
When restore-capable input contains config payloads, apply, standalone restore, and rebuild JSON output SHALL include `configResolutions[]` and `configResolutionSummary`. Each resolution SHALL include `captureId`, `moduleId`, `configSetId`, source/target instance IDs, source/target generations and source-generation fingerprint when known, `resolution`, `reason`, `migrationPath`, capture/restore module revisions, and terminal status. Legacy inputs SHALL use `legacy_unverified` rather than fabricated generation values.

#### Scenario: Dry-run exposes migration plan
- **WHEN** a dry-run resolves a config set through a forward migration
- **THEN** `configResolutions[]` reports `resolution: "migrate"`
- **AND** includes the ordered migration path without changing host configuration

#### Scenario: Legacy input exposes warning state
- **WHEN** apply/rebuild consumes legacy config payloads
- **THEN** `configResolutions[]` reports `legacy_unverified`
- **AND** omits unknown source/target generation values

#### Scenario: Summary counts every resolution
- **WHEN** config resolutions contain direct, migrate, unknown, incompatible, and legacy entries
- **THEN** `configResolutionSummary` reports totals for each state and selected/skipped/failed execution counts

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
