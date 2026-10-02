## MODIFIED Requirements

### Requirement: configModuleMap in Apply/Verify/Capture Envelopes
The apply, verify, and capture JSON envelopes SHALL include a `configModuleMap` field that maps package refs (winget or module ID for non-winget modules) to config module IDs. For modules without winget refs, the module ID itself is used as the map key, ensuring non-winget modules appear in the envelope.

#### Scenario: configModuleMap present when manifest has configModules
- **WHEN** `apply --manifest <path> --json` is run with a manifest that declares configModules
- **THEN** the JSON envelope data includes a `configModuleMap` object
- **AND** keys are winget package ref strings for winget-matched modules (e.g. `"Git.Git"`)
- **AND** keys are module ID strings for non-winget modules (e.g. `"apps.claude-desktop"`)
- **AND** values are config module ID strings (e.g. `"apps.git"`)

#### Scenario: Non-winget module appears in configModuleMap using module ID as key
- **WHEN** a config module has no `matches.winget` entries but is matched via `pathExists`
- **AND** `capture --json` is run
- **THEN** the `configModuleMap` SHALL include an entry for that module with its ID as both key and value

#### Scenario: configModuleMap always present in capture even when empty
- **WHEN** `capture --json` is run and no config modules resolve to any refs
- **THEN** the `configModuleMap` field SHALL be present as an empty object `{}`
- **AND** the field SHALL NOT be null or missing

#### Scenario: configModuleMap present in dry-run mode

- **WHEN** `apply --manifest <path> --dry-run --json` is run with a manifest that declares configModules
- **THEN** the JSON envelope data includes a configModuleMap object with the same content as a non-dry-run

#### Scenario: configModuleMap present in verify

- **WHEN** `verify --manifest <path> --json` is run with a manifest that declares configModules
- **THEN** the JSON envelope data includes a configModuleMap object

#### Scenario: configModuleMap omitted when no configModules

- **WHEN** a manifest has no configModules array
- **THEN** the configModuleMap field is absent from the JSON envelope data

#### Scenario: configModuleMap omitted when no winget matches (apply/verify)

- **WHEN** a manifest declares configModules but none resolve to winget refs
- **THEN** the configModuleMap field is absent from the apply/verify JSON envelope data

#### Scenario: configModuleMap present in capture with bundle

- **WHEN** `capture --json` is run and the capture result includes BundleConfigModules
- **THEN** the JSON envelope data includes a configModuleMap object built from BundleConfigModules

#### Scenario: configModuleMap present in capture without bundle (fallback)

- **WHEN** `capture --json` is run and BundleConfigModules is empty
- **AND** the output manifest contains a configModules array
- **THEN** the JSON envelope data includes a configModuleMap object built from the output manifest's configModules
- **AND** keys are winget package ref strings (e.g. "Git.Git")
- **AND** values are config module ID strings (e.g. "apps.git")

#### Scenario: Consistency across operations

- **GIVEN** a manifest with configModules
- **WHEN** apply, apply --dry-run, verify, and capture are each run with --json
- **THEN** all four produce identical configModuleMap content for the same module set
