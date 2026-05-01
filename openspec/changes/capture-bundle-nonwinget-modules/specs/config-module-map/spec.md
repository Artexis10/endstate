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
