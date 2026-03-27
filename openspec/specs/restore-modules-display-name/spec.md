# restore-modules-display-name Specification

## Purpose
Enriches the `restoreModulesAvailable` field in the apply JSON envelope from a flat string array of module IDs to an array of objects containing both the module ID and a human-readable display name. This allows the GUI to show meaningful labels without fragile cross-referencing through configModuleMap and winget event names.

## Requirements
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

#### Scenario: Empty restoreModulesAvailable

- **WHEN** no modules match the manifest apps
- **THEN** `restoreModulesAvailable` is omitted from the envelope (existing omitempty behavior)

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

## Invariants

### INV-RMOD-DISPLAY-1: Shape Contract
- Each entry in `restoreModulesAvailable` MUST have both `id` (string) and `displayName` (string)
- `id` is the qualified module ID (e.g. "apps.vscode")
- `displayName` is never empty

### INV-RMOD-DISPLAY-2: Display Name Resolution Order
1. Module's `displayName` field from module.jsonc (if non-empty)
2. Short ID: module ID with `apps.` prefix stripped

### INV-RMOD-DISPLAY-3: Additive Change
- This is a schema-additive change (object replaces string)
- No schema version bump required
- Consumers SHOULD handle both old (string) and new (object) shapes during transition

### INV-RMOD-DISPLAY-4: No Side Effects
- This change MUST NOT alter configModuleMap shape or behavior
- This change MUST NOT introduce new I/O — display names come from the already-loaded module catalog

## JSON Shape

```json
{
  "data": {
    "restoreModulesAvailable": [
      { "id": "apps.mpv", "displayName": "mpv" },
      { "id": "apps.vscode", "displayName": "Visual Studio Code" }
    ]
  }
}
```

## Previous Shape (Replaced)

```json
{
  "data": {
    "restoreModulesAvailable": ["apps.mpv", "apps.vscode"]
  }
}
```

## Affected Commands
- apply (including --dry-run)
