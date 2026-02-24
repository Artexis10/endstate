## MODIFIED Requirements

### Requirement: Capture Details UI Must Render App List

- Capture Details modal MUST show scrollable list of captured apps
- Canonical source: `appsIncluded` from engine envelope (preferred)
- Fallback: derive from manifest if `appsIncluded` unavailable
- Count displayed MUST equal list length shown
- If list unavailable but count > 0: show "N apps captured" with note
- Each `appsIncluded` entry MAY include an optional `name` field containing the winget display name
- GUI MAY use the `name` field for human-readable display when available, falling back to `id` when absent

#### Scenario: appsIncluded entry with name field

- **WHEN** the capture envelope contains `appsIncluded` entries with `name` fields
- **THEN** GUI MAY display the `name` value alongside or instead of the package `id`

#### Scenario: appsIncluded entry without name field

- **WHEN** the capture envelope contains `appsIncluded` entries without `name` fields
- **THEN** GUI SHALL display the `id` as before
- **AND** behavior SHALL be identical to pre-change behavior
