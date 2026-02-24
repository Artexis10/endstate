## Why

The capture flow detects winget display names (the `Name` column from `winget list`) but discards them. The GUI and downstream consumers currently receive only opaque package IDs like `Microsoft.VisualStudioCode` in the `appsIncluded` array and streaming `item` events. Adding the human-readable display name alongside the ID enables the GUI to show friendly app names without maintaining a separate lookup table.

## What Changes

- Extract the display name from `winget list` fallback parsing (Name column is already header-detected but value is discarded)
- Carry display name through the app object during capture
- Include an optional `name` field in each `appsIncluded` entry in the capture JSON envelope
- Add an optional `Name` parameter to `Write-ItemEvent` and include it in streaming item events during capture

## Capabilities

### New Capabilities
- `capture-app-display-name`: Winget display names are extracted during capture and surfaced in both the JSON envelope `appsIncluded` array and streaming `item` events as an additive optional field.

### Modified Capabilities
- `capture-artifact-contract`: The `appsIncluded` entry schema gains an optional `name` field (additive, non-breaking). Existing consumers that ignore unknown fields are unaffected.

## Impact

- **Engine**: `engine/capture.ps1` — extract name from winget list fallback; carry on app object
- **CLI envelope**: `bin/endstate.ps1` — forward `name` into `appsIncluded` entries
- **Events**: `engine/events.ps1` — add optional `Name` parameter to `Write-ItemEvent`
- **Spec**: `capture-artifact-contract.md` — update JSON schema example to include optional `name`
- **Tests**: New/updated Pester tests for name extraction, envelope inclusion, and event emission
- **No breaking changes**: `name` is additive and optional throughout
