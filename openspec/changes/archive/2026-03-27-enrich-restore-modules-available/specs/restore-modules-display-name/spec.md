## ADDED Requirements

### Requirement: restoreModulesAvailable Contains Enriched Module References

The apply JSON envelope's `restoreModulesAvailable` field SHALL be an array of objects, each containing `id` (qualified module ID) and `displayName` (human-readable label). Display names SHALL be resolved from the already-loaded module catalog without additional I/O.

#### Scenario: Module with displayName field

- **WHEN** `apply --dry-run --json` is run and module `apps.vscode` has `"displayName": "Visual Studio Code"` in its module.jsonc
- **THEN** `restoreModulesAvailable` contains `{ "id": "apps.vscode", "displayName": "Visual Studio Code" }`

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

### Requirement: Display Name Resolution Order

The engine SHALL resolve display names using this precedence:
1. Module's `displayName` field from module.jsonc (if non-empty)
2. Short ID: module ID with `apps.` prefix stripped

#### Scenario: displayName takes precedence over short ID

- **WHEN** module `apps.vscode` has `displayName: "Visual Studio Code"`
- **THEN** the resolved display name is `"Visual Studio Code"`, not `"vscode"`

#### Scenario: Short ID fallback for missing displayName

- **WHEN** module `apps.custom-tool` has no `displayName` or empty `displayName`
- **THEN** the resolved display name is `"custom-tool"`
