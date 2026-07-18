# config-capture-provenance Specification

## Purpose
Defines versioned configuration capture provenance, payload integrity, bundle isolation, and authority boundaries for safe restoration.

## Requirements
### Requirement: Generation-Aware Capture Produces a Versioned, Structurally Isolated Bundle
A capture containing at least one generation-aware config set SHALL produce bundle metadata schema `2.0` and an embedded manifest version `2`. A generation-aware bundle SHALL encode generation-aware payloads only through `configCaptures[]` and SHALL NOT contain flat restore entries as an alternative execution path for those config sets. The bundle SHALL be structured so an engine that does not understand `configCaptures[]` has no executable legacy restore path to any generation-aware payload. Such an engine MAY still process application declarations and explicitly represented legacy lanes.

#### Scenario: Version-aware bundle declares its compatibility version
- **WHEN** capture includes a schema-v2 config set
- **THEN** the bundle metadata declares schema `2.0`
- **AND** the embedded manifest declares version `2`
- **AND** generation-aware payloads are referenced through `configCaptures[]`

#### Scenario: Legacy engine cannot execute generation-aware payloads
- **WHEN** an engine supports manifest version 1 only and opens a generation-aware bundle
- **THEN** the engine MAY process application declarations and explicitly represented legacy lanes
- **AND** no flat restore entry references a generation-aware payload
- **AND** the engine cannot execute that payload through its legacy configuration path

### Requirement: Bundle Records Immutable Per-Set Source Provenance
Each generation-aware captured config set SHALL have one `configCaptures[]` record containing a stable capture ID, module ID, config-set ID, source instance evidence, raw and normalized source app versions, source generation and canonical generation fingerprint, capture-time module schema version and content hash, payload root, and payload manifest.

#### Scenario: Multiple config sets have distinct records
- **WHEN** one application instance captures `preferences` and `presets`
- **THEN** the bundle contains two independently addressable config-capture records
- **AND** each record preserves its own source generation and payload root

#### Scenario: Side-by-side instances remain distinct
- **WHEN** two installed versions of one application are captured
- **THEN** their config sets receive distinct source instance IDs and capture IDs
- **AND** neither record is labeled as the preferred or latest instance

### Requirement: Payload Hierarchy and Integrity Are Preserved
The bundle SHALL store each config set under `configs/<captureId>/`, preserve the complete relative path hierarchy, reject duplicate destinations, and record each payload entry's relative path, byte size, and SHA-256 hash. Restore SHALL verify this manifest before compatibility planning that can lead to mutation.

#### Scenario: Nested paths do not collapse
- **WHEN** two captured files share a basename but have different relative parent directories
- **THEN** both hierarchy-preserving paths exist in the bundle
- **AND** they do not overwrite or collide with one another

#### Scenario: Payload hash mismatch blocks the set
- **WHEN** a payload file does not match its recorded size or SHA-256 hash
- **THEN** that config set reports `payload_integrity_failed`
- **AND** no target mutation occurs for it

### Requirement: Bundle and Current Catalog Have Separate Authority
The bundle SHALL own captured source facts and payload bytes. The trusted catalog pinned by the current engine SHALL own target instance detection, target generation rules, and available migration edges. Restore SHALL NOT rewrite source facts to match the current catalog and SHALL record both capture-time and restore-time module revisions.

#### Scenario: Current module adds support for a future target
- **WHEN** a bundle captured with module revision A names source generation `g1`
- **AND** current trusted module revision B declares a valid `g1` to target-generation migration
- **THEN** the engine may plan that migration
- **AND** reports revisions A and B separately

#### Scenario: Current module hash differs without changing source history
- **WHEN** the current module revision differs from the capture-time revision
- **THEN** the engine does not alter the captured source version or generation
- **AND** a revision difference alone is not an incompatibility

### Requirement: Source Module Snapshot Is Inspectable but Non-Authoritative
Generation-aware bundles SHALL include a canonical declarative snapshot of each capture-time module under `provenance/modules/` and SHALL verify it against the recorded module hash. The engine SHALL NOT execute the snapshot or use it to select target paths or migrations.

#### Scenario: Snapshot cannot introduce target behavior
- **WHEN** an edited bundle snapshot contains target or migration rules absent from the trusted current catalog
- **THEN** the engine ignores those rules for target resolution
- **AND** reports failed provenance integrity if the snapshot hash no longer matches

### Requirement: Legacy Bundles Remain Usable and Explicitly Unverified
New engines SHALL accept bundle/manifest v1. Legacy config payloads SHALL be reported as `legacy_unverified`; application installation SHALL proceed, and config restore SHALL remain available through the existing explicit consent, conflict, backup, journal, and revert behavior without an additional expert flag.

An explicit schema-v1 module lane SHALL use `configSetId: "legacy"` and the deterministic, domain-separated capture ID returned by `bundle.LegacyCaptureID(moduleId)`. Anonymous inline restore actions without a module-lane association SHALL remain ordinary restore items and SHALL NOT cause the engine to fabricate config-resolution rows, instances, versions, or generations.

#### Scenario: Legacy bundle installs and can restore
- **WHEN** a user rebuilds from a valid v1 bundle with restore consent
- **THEN** application installation proceeds
- **AND** the legacy config payload is offered with `legacy_unverified`
- **AND** existing restore safety behavior applies

#### Scenario: Legacy warning does not claim incompatibility
- **WHEN** source generation provenance is absent because the bundle predates this capability
- **THEN** the engine reports that compatibility could not be verified
- **AND** does not claim that the settings are known incompatible

#### Scenario: Explicit legacy module identity is deterministic
- **WHEN** a schema-v1 module lane is represented in a legacy or mixed bundle
- **THEN** its config result uses `configSetId: "legacy"`
- **AND** its capture ID is `bundle.LegacyCaptureID(moduleId)`

#### Scenario: Anonymous inline action stays an ordinary item
- **WHEN** a flat restore action has no explicit module-lane association
- **THEN** it may appear as an ordinary restore item
- **AND** the engine emits no fabricated config-resolution row for it

### Requirement: Mixed V2 Bundles Keep Legacy and Generation-Aware Payloads Separate
A manifest-v2 bundle MAY contain schema-v1 flat module payloads alongside generation-aware config captures. Flat restore entries SHALL be associated only with explicitly identified schema-v1 payloads, SHALL remain `legacy_unverified`, and SHALL NOT supply missing data or fallback behavior for a generation-aware config capture.

#### Scenario: Mixed bundle restores both paths distinctly
- **WHEN** a v2 bundle contains one valid generation-aware config capture and one schema-v1 flat payload
- **THEN** the generation-aware capture uses generation resolution
- **AND** the schema-v1 payload uses the legacy consent/safety path with `legacy_unverified`

#### Scenario: Legacy payload cannot rescue invalid v2 capture
- **WHEN** a generation-aware capture in a mixed bundle fails provenance validation
- **THEN** no flat restore entry is used as an alternative for that capture

### Requirement: Invalid Version-Aware Data Never Falls Back to Legacy Restore
A bundle or manifest that declares version 2 SHALL either pass generation provenance validation or fail/skip the affected config set. It SHALL NOT execute version-aware payloads through the legacy flat restore path.

#### Scenario: Missing generation field in v2 capture
- **WHEN** a v2 config-capture record omits its source generation
- **THEN** validation rejects the record
- **AND** the engine does not reinterpret it as `legacy_unverified`
