## ADDED Requirements

### Requirement: configModuleMap covers winget and non-winget modules in Apply/Verify/Capture Envelopes
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

#### Scenario: configModuleMap present in capture with bundle

- **WHEN** `capture --json` is run and the capture result includes BundleConfigModules
- **THEN** the JSON envelope data includes a configModuleMap object built from BundleConfigModules

#### Scenario: Consistency across operations

- **GIVEN** a manifest with configModules
- **WHEN** apply, apply --dry-run, verify, and capture are each run with --json
- **THEN** all four produce identical configModuleMap content for the same module set

## REMOVED Requirements

### Requirement: configModuleMap in Apply/Verify/Capture Envelopes

**Reason**: Non-winget modules now appear in the map keyed by module ID, so the earlier scenario that omitted the map when no module resolved to a winget ref no longer holds, and the capture fallback described in the old text is no longer part of the requirement.

**Migration**: Use **configModuleMap covers winget and non-winget modules in Apply/Verify/Capture Envelopes**.
