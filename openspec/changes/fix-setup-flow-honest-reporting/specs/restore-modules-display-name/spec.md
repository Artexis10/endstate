## MODIFIED Requirements

### Requirement: restoreModulesAvailable Contains Enriched Module References

The apply JSON envelope's `restoreModulesAvailable` field SHALL be an array of objects, each containing `id` (qualified module ID), `displayName` (human-readable label), and `entryCount` (number of restore entries resolved to that module). Display names SHALL be resolved from the already-loaded module catalog without additional I/O.

#### Scenario: Module with displayName field

- **WHEN** `apply --dry-run --json` is run and module `apps.vscode` has `"displayName": "Visual Studio Code"` in its module.jsonc
- **THEN** `restoreModulesAvailable` contains an entry with `"id": "apps.vscode"` and `"displayName": "Visual Studio Code"`

#### Scenario: Module without displayName falls back to short ID

- **WHEN** `apply --dry-run --json` is run and a matched module has an empty `displayName` field with ID `apps.myapp`
- **THEN** the entry's `displayName` SHALL be `"myapp"` (module ID with `apps.` prefix stripped)

#### Scenario: Multiple modules produce deterministic output

- **WHEN** `apply --dry-run --json` is run with modules `apps.git` (displayName: "Git") and `apps.vscode` (displayName: "Visual Studio Code")
- **THEN** `restoreModulesAvailable` contains both entries with their respective display names
- **AND** entries are ordered deterministically by module ID

#### Scenario: displayName is never empty

- **WHEN** any module appears in `restoreModulesAvailable`
- **THEN** its `displayName` field SHALL be a non-empty string

#### Scenario: Non-dry-run apply also enriched

- **WHEN** `apply --json` (non-dry-run) is run with matched modules
- **THEN** `restoreModulesAvailable` contains the same enriched objects as in dry-run mode

#### Scenario: Empty restoreModulesAvailable

- **WHEN** no module in the manifest carries restorable payload
- **THEN** `restoreModulesAvailable` is omitted from the envelope (existing omitempty behavior)

#### Scenario: Every entry carries an entry count

- **WHEN** any module appears in `restoreModulesAvailable`
- **THEN** the entry SHALL include an `entryCount` field
- **AND** `entryCount` SHALL be a positive integer

## ADDED Requirements

### Requirement: Entry Shape Includes entryCount

The `restoreModulesAvailable` entry shape SHALL be `{ id, displayName, entryCount }`. `entryCount` is an additive field; per `schema-versioning` it does not require a schema version bump, and consumers that do not read it are unaffected.

#### Scenario: Additive field does not break existing consumers

- **WHEN** a consumer reads `restoreModulesAvailable` entries for `id` and `displayName` only
- **THEN** the presence of `entryCount` SHALL NOT alter that consumer's behavior

#### Scenario: JSON shape

- **WHEN** `apply --dry-run --json` resolves two restore entries to `apps.mpv` and five to `apps.vscode`
- **THEN** `restoreModulesAvailable` SHALL contain `{ "id": "apps.mpv", "displayName": "mpv", "entryCount": 2 }` and `{ "id": "apps.vscode", "displayName": "Visual Studio Code", "entryCount": 5 }`
