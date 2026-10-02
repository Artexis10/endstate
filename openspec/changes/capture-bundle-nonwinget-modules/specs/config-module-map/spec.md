## ADDED Requirements

### Requirement: configModuleMap covers winget and non-winget modules in Apply and Capture Envelopes

The apply (including `--dry-run`) and capture JSON envelopes SHALL include a `configModuleMap` field that maps package refs to config module IDs. A module with `matches.winget` refs is keyed by each of those winget refs. A module with neither winget nor chocolatey refs (matched via `pathExists`) is keyed by a module-derived identifier so it still appears: in apply the key is the module ID without its `apps.` prefix, and in capture the key is the full module ID. In apply, the field SHALL be omitted when no module matches the manifest's apps. In capture, the field SHALL always be present, as an empty object when no modules map. The verify envelope SHALL NOT carry `configModuleMap`.

#### Scenario: Apply maps winget refs to module IDs

- **WHEN** `apply --manifest <path> --json` is run with a manifest whose apps match a config module that declares winget refs
- **THEN** the JSON envelope data includes a `configModuleMap` object
- **AND** keys are winget package ref strings (e.g. "Git.Git")
- **AND** values are config module ID strings (e.g. "apps.git")

#### Scenario: configModuleMap present in dry-run mode

- **WHEN** `apply --manifest <path> --dry-run --json` is run with the same manifest
- **THEN** the JSON envelope data includes a `configModuleMap` object with the same content as a non-dry-run

#### Scenario: Apply keys a non-winget module by its short ID

- **WHEN** `apply --json` matches a config module that has no winget and no chocolatey refs (matched via `pathExists`) such as `apps.claws-mail`
- **THEN** the `configModuleMap` includes the entry `"claws-mail": "apps.claws-mail"`

#### Scenario: Apply omits the map when no module matches

- **WHEN** no config module matches the manifest's apps
- **THEN** the `configModuleMap` field is absent from the apply JSON envelope data

#### Scenario: Non-winget module appears in capture using module ID as key

- **WHEN** a config module has no `matches.winget` entries but is matched via `pathExists`
- **AND** `capture --json` is run
- **THEN** the `configModuleMap` SHALL include an entry for that module with its ID as both key and value

#### Scenario: configModuleMap always present in capture even when empty

- **WHEN** `capture --json` is run and no config modules resolve to any refs
- **THEN** the `configModuleMap` field SHALL be present as an empty object `{}`
- **AND** the field SHALL NOT be null or missing

#### Scenario: Verify carries no configModuleMap

- **WHEN** `verify --manifest <path> --json` is run
- **THEN** the JSON envelope data does not include a `configModuleMap` field

## REMOVED Requirements

### Requirement: configModuleMap in Apply/Verify/Capture Envelopes

**Reason**: The requirement text claimed verify envelopes carry `configModuleMap`, that capture builds it from `BundleConfigModules`, and that all operations produce identical content; none holds. Non-winget modules now appear in the map, keyed by a module-derived identifier.

**Migration**: Use **configModuleMap covers winget and non-winget modules in Apply and Capture Envelopes**.
