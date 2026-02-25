# config-module-map Specification

## Purpose
Defines the configModuleMap field in apply, verify, and capture JSON envelopes, enabling the GUI to show per-app settings indicators by mapping winget package refs to config module IDs.

## Requirements
### Requirement: configModuleMap in Apply/Verify/Capture Envelopes

The apply, verify, and capture JSON envelopes SHALL include a configModuleMap field that maps winget package refs to config module IDs. In apply and verify, the field is present when the manifest declares configModules with winget refs. In capture, the field is always present (empty object when no mappings exist).

#### Scenario: configModuleMap present when manifest has configModules

- **WHEN** `apply --manifest <path> --json` is run with a manifest that declares configModules
- **THEN** the JSON envelope data includes a configModuleMap object
- **AND** keys are winget package ref strings (e.g. "Git.Git")
- **AND** values are config module ID strings (e.g. "apps.git")

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

#### Scenario: configModuleMap present in capture

- **WHEN** `capture --json` is run
- **THEN** the JSON envelope data includes a configModuleMap object
- **AND** keys are winget package ref strings (e.g. "Git.Git")
- **AND** values are config module ID strings (e.g. "apps.git")

#### Scenario: configModuleMap always present in capture even when empty

- **WHEN** `capture --json` is run and no config modules resolve to winget refs
- **THEN** the configModuleMap field is present as an empty object `{}`
- **AND** the field is never null or missing

#### Scenario: Consistency across operations

- **GIVEN** a manifest with configModules
- **WHEN** apply, apply --dry-run, verify, and capture are each run with --json
- **THEN** all four produce identical configModuleMap content for the same module set

## Invariants

### INV-CONFIGMAP-1: Presence Condition
- In apply/verify: configModuleMap MUST be present when the manifest declares configModules and at least one module resolves to winget refs
- In apply/verify: configModuleMap MUST be absent when the manifest has no configModules
- In capture: configModuleMap MUST always be present (empty object `{}` when no mappings exist)

### INV-CONFIGMAP-2: Key-Value Contract
- Keys are winget package ref strings (e.g. "Git.Git")
- Values are config module ID strings (e.g. "apps.git")
- Keys correspond to matches.winget entries in the referenced module definitions

### INV-CONFIGMAP-3: Consistency Across Operations
- configModuleMap uses the same construction logic (Build-ConfigModuleMap) in apply, apply --dry-run, verify, and capture
- Given the same manifest/module set, all operations produce identical configModuleMap content

### INV-CONFIGMAP-4: Additive Field
- This is an additive optional field
- No schema version bump required
- Consumers MUST tolerate its absence (graceful degradation)

## JSON Shape

```json
{
  "data": {
    "configModuleMap": {
      "Git.Git": "apps.git",
      "Microsoft.PowerToys": "apps.powertoys"
    }
  }
}
```

## Affected Commands
- apply (including --dry-run)
- verify
- capture

## Implementation
- engine/config-modules.ps1: Build-ConfigModuleMap function
- engine/apply.ps1: conditional inclusion in $data block
- engine/verify.ps1: conditional inclusion in $data block
- bin/endstate.ps1: capture envelope construction (always present, defaults to empty object)
