# capture-app-display-name Specification

## Purpose
Defines how winget display names are extracted during capture and surfaced in both the JSON envelope and streaming events.

## Requirements

### Requirement: Display Name Extraction from Winget List Fallback

The capture engine SHALL extract the display name from the `Name` column of `winget list` tabular output during fallback capture, and carry it on the internal app object as `_name`.

#### Scenario: Name column extracted in fallback capture

- **WHEN** capture uses the `winget list` fallback path
- **AND** the tabular output contains a `Name` column in the header
- **THEN** each parsed app object SHALL include a `_name` field containing the trimmed value from the Name column
- **AND** the `_name` value SHALL be the substring from the Name column start to the Id column start, trimmed of whitespace

#### Scenario: Missing or unparseable Name column in fallback

- **WHEN** capture uses the `winget list` fallback path
- **AND** the header line does not contain a recognizable `Name` column
- **THEN** each parsed app object SHALL have `_name` set to `$null`
- **AND** capture SHALL proceed normally without failure

### Requirement: Display Name Extraction from Winget Export

The capture engine SHALL attempt to extract a display name from the `winget export` JSON output when available, carrying it as `_name` on the internal app object.

#### Scenario: Export JSON does not include display name field

- **WHEN** capture uses the `winget export` primary path
- **AND** the export JSON package entry does not contain a display name field
- **THEN** the app object SHALL have `_name` set to `$null`
- **AND** capture SHALL proceed normally

### Requirement: Display Name in Capture Envelope appsIncluded

The capture JSON envelope SHALL include an optional `name` field in each `appsIncluded` entry, populated from the app object's `_name` when available.

#### Scenario: appsIncluded entry includes name when available

- **WHEN** capture produces a JSON envelope with `--json` flag
- **AND** an app object has a non-null `_name` value
- **THEN** the corresponding `appsIncluded` entry SHALL include a `name` field with that value

#### Scenario: appsIncluded entry omits name when unavailable

- **WHEN** capture produces a JSON envelope with `--json` flag
- **AND** an app object has a null `_name` value
- **THEN** the corresponding `appsIncluded` entry SHALL NOT include a `name` field

#### Scenario: Existing appsIncluded fields unchanged

- **WHEN** capture produces `appsIncluded` entries
- **THEN** each entry SHALL still contain `id` and `source` fields with unchanged semantics
- **AND** the addition of `name` SHALL NOT alter any existing field values or behavior

### Requirement: Display Name in Capture Item Streaming Events

`Write-ItemEvent` SHALL accept an optional `Name` parameter and include it in the emitted event JSON when provided.

#### Scenario: Item event includes name when parameter provided

- **WHEN** `Write-ItemEvent` is called with `-Name "Visual Studio Code"`
- **THEN** the emitted NDJSON event SHALL include `"name": "Visual Studio Code"`

#### Scenario: Item event omits name when parameter not provided

- **WHEN** `Write-ItemEvent` is called without the `-Name` parameter
- **THEN** the emitted NDJSON event SHALL NOT include a `name` field

#### Scenario: Capture item events pass display name

- **WHEN** capture emits item events for detected apps (status "present", reason "detected")
- **AND** the app object has a non-null `_name` value
- **THEN** the `Write-ItemEvent` call SHALL include `-Name` with the display name value
