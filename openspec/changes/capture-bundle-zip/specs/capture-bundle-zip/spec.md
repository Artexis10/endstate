## ADDED Requirements

### Requirement: Capture produces a portable zip bundle
The capture command SHALL produce a single zip artifact containing the app manifest, config module payloads, and a metadata file. The zip is the unit of portability for machine-to-machine transfer.

#### Scenario: Capture output is a zip file
- **WHEN** `endstate capture --profile "Name"` is run
- **THEN** the output SHALL be a zip file at the configured profiles directory with the given name

#### Scenario: Zip contains manifest, metadata, and optional configs
- **WHEN** a zip bundle is produced
- **THEN** it SHALL contain `manifest.jsonc` (the app list) and `metadata.json`
- **AND** if any config modules matched, it SHALL contain `configs/<module-id>/<files>` entries

### Requirement: Config module matching bundles captured configs into zip
After the app scan, the engine SHALL match each captured app against the config module catalog via `matches.winget` and copy matched modules' `capture.files` into the zip under `configs/<module-id>/`.

#### Scenario: Matched module files are bundled
- **WHEN** a captured app has a matching config module (via `matches.winget`)
- **THEN** the module's `capture.files` are copied into the zip at `configs/<module-id>/<filename>`

#### Scenario: Sensitive files are never bundled
- **WHEN** a config module's `sensitive.files` list contains a file that would otherwise be captured
- **THEN** that file SHALL NOT appear in the zip

#### Scenario: ExcludeGlobs are respected
- **WHEN** a file matches a pattern in `capture.excludeGlobs`
- **THEN** that file SHALL be skipped and SHALL NOT appear in the zip

### Requirement: Config capture failures do not block app capture
If config file collection fails for one or more modules, the manifest and metadata SHALL still be written and the zip SHALL be produced.

#### Scenario: Missing config file does not abort capture
- **WHEN** a config file listed in `capture.files` does not exist on disk
- **THEN** the failure SHALL be recorded in `metadata.captureWarnings`
- **AND** the zip SHALL still be produced with the remaining content

### Requirement: Install-only profile is always valid
A zip containing only `manifest.jsonc` and `metadata.json` (no `configs/` entries) is a valid profile and SHALL be accepted by apply and profile discovery without warnings.

#### Scenario: Zip with no configs is valid
- **WHEN** no config modules match during capture
- **THEN** the zip SHALL contain only `manifest.jsonc` and `metadata.json`
- **AND** this SHALL be a successful capture result (no errors)

### Requirement: Profile discovery resolves zip, folder, and bare manifest
Profile resolution SHALL check for profiles in this order: zip bundle → loose folder → bare manifest. The first match wins.

#### Scenario: Zip takes priority over folder and bare manifest
- **WHEN** `--profile "Name"` is specified
- **AND** both `Name.zip` and `Name.jsonc` exist
- **THEN** `Name.zip` SHALL be used

#### Scenario: Folder used when no zip exists
- **WHEN** `--profile "Name"` is specified
- **AND** `Name.zip` does not exist but `Name/manifest.jsonc` does
- **THEN** the folder manifest SHALL be used

### Requirement: Apply from zip extracts to temp directory and cleans up
When a zip profile is used with `apply`, the zip SHALL be extracted to a temporary directory, the manifest read and applied from there, and the temp directory removed after apply completes.

#### Scenario: Apply from zip extracts and applies
- **WHEN** `endstate apply --profile "Name.zip"` is run
- **THEN** the zip SHALL be extracted to a temp directory
- **AND** apps SHALL be installed from the extracted manifest
- **AND** the temp directory SHALL be removed after apply finishes

### Requirement: Capture JSON output reports bundle details
The `capture --json` envelope data SHALL include `outputPath`, `outputFormat`, `configsIncluded`, `configsSkipped`, and `configsCaptureErrors` fields.

#### Scenario: JSON output has bundle fields
- **WHEN** `capture --json` succeeds and produces a zip
- **THEN** `data.outputFormat` SHALL be `"zip"`
- **AND** `data.configsIncluded` SHALL list module IDs whose files were bundled
- **AND** `data.configsSkipped` SHALL list module IDs that were matched but produced no files
