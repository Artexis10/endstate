## ADDED Requirements

### Requirement: Item Event Name Field in Event Contract

The event contract SHALL define an optional `name` field on item events, carrying the human-readable display name of the item.

#### Scenario: Name field documented in event contract

- **WHEN** the event contract item event definition is read
- **THEN** it SHALL list `name` (string, optional) as a field
- **AND** the description SHALL state: human-readable display name; when absent, consumers should format the `id` field for display

#### Scenario: Name field is non-breaking addition

- **WHEN** the `name` field is added to item events
- **THEN** the event schema version SHALL remain `1`
- **AND** no version bump is required per the contract's non-breaking change rules

### Requirement: Go Engine ItemEvent Struct Includes Name

The Go engine `ItemEvent` struct SHALL include a `Name` field serialized as `name` with `omitempty`, so it appears in JSON only when populated.

#### Scenario: ItemEvent JSON includes name when populated

- **WHEN** an item event is emitted with a non-empty display name
- **THEN** the JSON output SHALL include a `"name"` field with the display name value

#### Scenario: ItemEvent JSON omits name when empty

- **WHEN** an item event is emitted with an empty display name
- **THEN** the JSON output SHALL NOT include a `"name"` field

### Requirement: Go Engine EmitItem Accepts Name Parameter

The `EmitItem` method SHALL accept a `name` string parameter and propagate it to the `ItemEvent.Name` field.

#### Scenario: EmitItem with name populates ItemEvent.Name

- **WHEN** `EmitItem` is called with name `"Visual Studio Code"`
- **THEN** the emitted event SHALL have `Name` set to `"Visual Studio Code"`

#### Scenario: EmitItem with empty name leaves Name empty

- **WHEN** `EmitItem` is called with name `""`
- **THEN** the emitted event SHALL have `Name` as empty string (omitted from JSON)

### Requirement: Winget Driver Detect Returns Display Name

The winget driver `Detect` method SHALL return the display name extracted from `winget list` output alongside the installed boolean.

#### Scenario: Detect returns display name for installed package

- **WHEN** `Detect` is called for an installed package
- **AND** `winget list --id <ref> -e` output contains a Name column
- **THEN** `Detect` SHALL return `(true, "<display name>", nil)`
- **AND** the display name SHALL be the trimmed value from the Name column

#### Scenario: Detect returns empty name when Name column unparseable

- **WHEN** `Detect` is called for an installed package
- **AND** the winget list output does not contain a recognizable Name column header
- **THEN** `Detect` SHALL return `(true, "", nil)`

#### Scenario: Detect returns empty name for uninstalled package

- **WHEN** `Detect` is called for a package that is not installed
- **THEN** `Detect` SHALL return `(false, "", nil)`

### Requirement: Apply Command Propagates Display Names

The apply command SHALL pass display names from winget detection through to all item events for winget-driven items.

#### Scenario: Plan phase item events include display name

- **WHEN** apply runs the plan phase and detects an installed package
- **THEN** the emitted item event SHALL include the display name from `Detect`

#### Scenario: Apply phase item events include display name

- **WHEN** apply installs a package and emits status events
- **THEN** the item events SHALL include the display name resolved during the plan phase

#### Scenario: Verify phase item events include display name

- **WHEN** apply runs the verify phase and re-detects packages
- **THEN** the emitted item events SHALL include the display name from the verification `Detect` call

### Requirement: Verify Command Propagates Display Names

The standalone verify command SHALL pass display names from winget detection to item events.

#### Scenario: Verify item events include display name

- **WHEN** verify detects an installed package
- **THEN** the emitted item event SHALL include the display name from `Detect`

#### Scenario: Verify item events for missing packages have empty name

- **WHEN** verify finds a package is not installed
- **THEN** the emitted item event SHALL have an empty display name

### Requirement: Capture Command Propagates Display Names

The capture command SHALL pass display names to item events for captured apps.

#### Scenario: Capture item events include display name

- **WHEN** capture emits item events for detected apps
- **AND** the captured app has a non-empty `Name` field
- **THEN** the item event SHALL include the display name

### Requirement: Plan Command Propagates Display Names

The plan command SHALL pass display names from detection to item events.

#### Scenario: Plan item events include display name

- **WHEN** plan detects packages and emits item events
- **THEN** the item events SHALL include display names from `Detect`

### Requirement: Non-Winget Item Events Pass Empty Name

Commands using non-winget drivers (restore, export, validate, revert) SHALL pass empty string for the name parameter.

#### Scenario: Restore item events have no display name

- **WHEN** restore emits item events
- **THEN** the name parameter SHALL be empty string
- **AND** the JSON output SHALL NOT include a `"name"` field
