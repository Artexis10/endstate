# capture-config-metadata Specification

## Purpose
Defines the rich per-module metadata included in capture output, enabling GUI display of module-level config information.
## Requirements
### Requirement: Rich Config Capture Metadata

The capture command SHALL include per-module metadata in its output to enable GUI display of module-level config information.

#### Scenario: configCapture.modules in capture result

- **WHEN** `capture --WithConfig --json` is run with matched config modules
- **THEN** the result includes a configCapture object with a modules array
- **AND** each module entry includes: id (string), displayName (string), entries (integer), files (array of strings)

#### Scenario: displayName falls back to module ID

- **WHEN** a config module is captured
- **THEN** the displayName field uses the module's displayName from module.jsonc
- **AND** displayName is always present (it is a required field in the module schema)

#### Scenario: entries count reflects restore entry count

- **WHEN** a config module with 3 restore entries is captured
- **THEN** the module entry in configCapture.modules has entries: 3

#### Scenario: files array lists source paths

- **WHEN** a config module with capture.files entries is captured
- **THEN** the files array lists the relative dest paths from capture.files

### Requirement: configModules Array in Capture JSON Envelope

The capture command SHALL include a `configModules` array in the JSON envelope `data` object when bundle capture includes matched config modules, enabling GUI association of config modules with parent apps.

#### Scenario: configModules in capture JSON envelope

- **WHEN** `capture --WithConfig --json` produces a bundle with matched config modules
- **THEN** the JSON envelope `data` object contains a `configModules` array
- **AND** each element includes: id (string), appId (string), displayName (string), status (string), filesCaptured (integer), wingetRefs (string[])
- **AND** status is one of: "captured", "skipped", "error"
- **AND** wingetRefs contains the module's matches.winget array (may be empty)

#### Scenario: appId derived from module ID

- **WHEN** a config module with id "apps.vscode" is captured
- **THEN** the configModules entry has appId "vscode"
- **AND** appId is always the module id with "apps." prefix stripped

#### Scenario: wingetRefs from module matches

- **WHEN** a config module has matches.winget = ["Microsoft.VisualStudioCode"]
- **THEN** the configModules entry has wingetRefs = ["Microsoft.VisualStudioCode"]

- **WHEN** a config module has no matches.winget field
- **THEN** the configModules entry has wingetRefs = []

#### Scenario: configModules absent when no config capture

- **WHEN** `capture --json` is run WITHOUT `--WithConfig`
- **THEN** the JSON envelope `data` does NOT contain a `configModules` field

#### Scenario: configModules includes all matched modules regardless of status

- **WHEN** capture matches 3 modules but only 2 have files on disk
- **THEN** configModules contains 3 entries
- **AND** 2 have status "captured" and 1 has status "skipped"

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
