## ADDED Requirements

### Requirement: Streaming Reports Config Resolution Before Mutation
Restore-capable commands using JSONL events SHALL emit one `config-resolution` event per captured config set after final target detection and before the first config mutation for that set. The event SHALL include the same identity, generation, resolution, reason, migration-path, and module-revision data as the JSON envelope.

#### Scenario: Migration resolution precedes restore actions
- **WHEN** a config set resolves through migration
- **THEN** its terminal `config-resolution` event is emitted before any `restore-item` event reports target mutation for that capture ID

#### Scenario: Incompatible set has no mutating events
- **WHEN** a config set resolves `incompatible`
- **THEN** a terminal `config-resolution` event is emitted with its reason
- **AND** no restoring/restored `restore-item` event is emitted for that capture ID

### Requirement: Streaming Reports Migration Progress Without Executing Module Code
For a migrating config set, the engine SHALL emit `config-migration` events for staging, each generation edge, validation, commit, and rollback when applicable. Event status SHALL be engine-derived and SHALL NOT require the GUI to interpret module migration operations.

#### Scenario: Multi-edge migration reports ordered progress
- **WHEN** the engine executes `g1 -> g2 -> g3`
- **THEN** events report the `g1 -> g2` edge before `g2 -> g3`
- **AND** validation status is emitted before commit begins

#### Scenario: Failed commit reports rollback
- **WHEN** commit fails after a target mutation
- **THEN** streaming reports rollback start and terminal rollback outcome for that capture ID

### Requirement: Legacy Streaming Uses Explicit Compatibility Reason
Legacy config restore SHALL emit a config-resolution event with resolution `legacy_unverified` before legacy restore-item events.

#### Scenario: Legacy restore warning precedes action
- **WHEN** restore executes a legacy config payload with consent
- **THEN** the stream reports `legacy_unverified` before any corresponding target mutation

