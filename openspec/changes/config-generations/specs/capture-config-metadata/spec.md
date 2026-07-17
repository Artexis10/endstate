## ADDED Requirements

### Requirement: Capture Metadata Includes Per-Instance Config Generations
Generation-aware capture SHALL add `configCapture.configSets[]` to the JSON envelope. Each entry SHALL include `captureId`, `moduleId`, `configSetId`, `displayName`, `sourceInstance`, `sourceGeneration`, `sourceGenerationFingerprint`, `captureModuleRevision`, `filesCaptured`, `status`, and `reason`. Existing `configCapture.modules` and `configModules` fields SHALL remain backward compatible.

#### Scenario: Generation-aware capture reports one set
- **WHEN** capture includes one generation-aware config set
- **THEN** `configCapture.configSets[]` contains its capture ID, source instance/version, source generation, module revision, file count, and capture status

#### Scenario: Side-by-side capture reports separate sets
- **WHEN** two application instances each contribute the same config-set ID
- **THEN** `configCapture.configSets[]` contains separate entries with different capture IDs and source instance IDs

#### Scenario: Legacy module keeps existing metadata
- **WHEN** capture uses a schema-v1 module
- **THEN** the existing per-module metadata remains present
- **AND** any generation-aware field is absent or explicitly marked unversioned rather than fabricated

### Requirement: Capture Envelope Identifies Bundle Compatibility Version
Capture output SHALL report the produced bundle metadata schema and embedded manifest version so GUI consumers can distinguish legacy and generation-aware artifacts without opening the zip.

#### Scenario: Version-aware bundle versions are reported
- **WHEN** capture produces a generation-aware bundle
- **THEN** the JSON envelope reports bundle schema `2.0` and manifest version `2`
