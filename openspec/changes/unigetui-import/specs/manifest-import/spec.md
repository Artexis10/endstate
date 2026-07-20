# manifest-import Specification (delta)

## ADDED Requirements

### Requirement: UniGetUI bundle parsing

The engine SHALL parse UniGetUI bundle/backup JSON (`SerializableBundle`, `export_version` 3) including `packages[]` fields `Id`, `Name`, `Version`, `Source`, `ManagerName`, optional `InstallationOptions.Version`, and `incompatible_packages[]`. An `export_version` other than 3 SHALL produce a warning, not a failure, when required fields parse.

#### Scenario: Valid v3 bundle parses
- **WHEN** `import --from unigetui --path backup.ubundle` runs on a valid `export_version: 3` file
- **THEN** all packages SHALL be parsed with their Id, Name, Version, Source, and ManagerName

#### Scenario: Future version warns but proceeds
- **WHEN** the bundle declares `export_version: 4` and required fields parse
- **THEN** the command SHALL emit a version warning and continue

### Requirement: Winget package mapping

Packages with the winget source SHALL map to manifest app entries: winget `Id` to `refs.windows`, `Name` to `displayName`, and a deterministic slug as the app `id`. When `--pin` is passed, the app `version` SHALL be set from the package's `InstallationOptions.Version` pin when present, otherwise from the bundle's recorded `Version`. Without `--pin`, no version SHALL be written.

#### Scenario: Winget package becomes an app entry
- **WHEN** the bundle contains `{"Id": "Microsoft.VisualStudioCode", "Name": "Microsoft Visual Studio Code", "Source": "winget"}`
- **THEN** the manifest SHALL contain an app with `refs.windows` = `Microsoft.VisualStudioCode` and `displayName` = `Microsoft Visual Studio Code`

#### Scenario: Authored pin beats observed version
- **WHEN** `--pin` is passed and a package has `Version: "1.2.3"` and `InstallationOptions.Version: "1.2.0"`
- **THEN** the app entry's `version` SHALL be `1.2.0`

#### Scenario: No pin by default
- **WHEN** `--pin` is not passed
- **THEN** no app entry SHALL contain a `version` field

### Requirement: Skip transparency

Every package that is not imported SHALL be reported with its manager and a reason; `incompatible_packages` SHALL be passed through to the report. The command SHALL NOT silently drop any bundle entry.

#### Scenario: Non-winget manager is reported
- **WHEN** the bundle contains a chocolatey-sourced package
- **THEN** the import summary SHALL list it as skipped with reason indicating the unsupported manager

#### Scenario: Incompatible packages surface
- **WHEN** the bundle contains `incompatible_packages`
- **THEN** the import summary SHALL list each of them

### Requirement: Output manifest validity and purity

The emitted manifest SHALL be JSONC that loads successfully through the engine's manifest loader before being written, and import SHALL be a pure transform: no network access, no package operations, and byte-identical output for identical input.

#### Scenario: Round-trip validity gate
- **WHEN** import generates a manifest
- **THEN** the manifest SHALL load via the manifest loader before the file is written
- **AND** a load failure SHALL abort the write with an error

#### Scenario: Deterministic output
- **WHEN** import runs twice on the same bundle with the same flags
- **THEN** the two outputs SHALL be byte-identical
