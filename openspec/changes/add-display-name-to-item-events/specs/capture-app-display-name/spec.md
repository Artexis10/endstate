## MODIFIED Requirements

### Requirement: Display Name in Capture Envelope appsIncluded

The capture JSON envelope SHALL include an optional `name` field in each `appsIncluded` entry, populated from the app object's display name when available. This requirement extends to the Go engine's capture command.

#### Scenario: appsIncluded entry includes name when available

- **WHEN** the Go engine capture produces a JSON envelope
- **AND** a captured app has a non-empty display name
- **THEN** the corresponding `appsIncluded` entry SHALL include a `name` field with that value

#### Scenario: appsIncluded entry omits name when unavailable

- **WHEN** the Go engine capture produces a JSON envelope
- **AND** a captured app has an empty display name
- **THEN** the corresponding `appsIncluded` entry SHALL NOT include a `name` field

#### Scenario: Existing appsIncluded fields unchanged

- **WHEN** capture produces `appsIncluded` entries
- **THEN** each entry SHALL still contain `id` and `source` fields with unchanged semantics
- **AND** the addition of `name` SHALL NOT alter any existing field values or behavior

### Requirement: Display Name in Capture Item Streaming Events

The Go engine capture command SHALL pass display names to `EmitItem` for captured apps.

#### Scenario: Capture item events pass display name from Go engine

- **WHEN** the Go engine capture emits item events for detected apps
- **AND** the captured app has a non-empty `Name` field
- **THEN** the `EmitItem` call SHALL include the display name value

#### Scenario: Capture item events omit name when unavailable in Go engine

- **WHEN** the Go engine capture emits item events for detected apps
- **AND** the captured app has an empty `Name` field
- **THEN** the `EmitItem` call SHALL pass empty string for name

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
