# schema-versioning Specification

## Purpose
Ensures that breaking changes to manifest or envelope schemas are signaled by a major version bump, preventing silent incompatibilities.

## Requirements
### Requirement: Breaking Changes Require Major Version Bump

Any change that removes, renames, or changes the type of an existing schema field SHALL be accompanied by a major version increment.

#### Scenario: Removed field triggers major version bump
- **WHEN** a previously required field is removed from the manifest schema
- **THEN** the schema major version is incremented
- **AND** the changelog documents the breaking change

#### Scenario: Renamed field triggers major version bump
- **WHEN** an existing field is renamed in the manifest or envelope schema
- **THEN** the schema major version is incremented
- **AND** consumers relying on the old field name receive a clear error

#### Scenario: Additive changes do not require major bump
- **WHEN** a new optional field is added to the schema
- **THEN** the major version is not incremented
- **AND** existing consumers continue to function without modification

### Requirement: Schema Version Is Declared

Manifests and envelopes SHALL include an explicit schema version that consumers can check.

#### Scenario: Manifest includes schema version
- **WHEN** a manifest is loaded by the engine
- **THEN** the manifest contains a schema version field
- **AND** the engine validates compatibility before processing
