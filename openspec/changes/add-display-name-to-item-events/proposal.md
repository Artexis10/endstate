## Why

The Go engine correctly implements the event contract but omits human-readable display names from item events. The GUI shows raw winget IDs (e.g., `Microsoft.VisualStudioCode`) instead of friendly names (e.g., "Visual Studio Code"). The PowerShell engine sent display names as undocumented extra data; the Go engine needs a formal contract field to carry them.

## What Changes

- Add optional `name` field to the Item Event definition in `docs/contracts/event-contract.md`
- Add `Name` field to `ItemEvent` struct in Go engine (`internal/events/types.go`)
- Update `EmitItem` to accept and propagate a display name (`internal/events/emitter.go`)
- Extract display names from `winget list` output in the winget driver (`internal/driver/winget/detect.go`)
- Pass display names through all item event call sites in apply, verify, capture, plan commands
- Include `name` in the JSON envelope `appsIncluded` entries for capture

## Capabilities

### New Capabilities
- `item-event-display-name`: Add optional `name` field to item events across the event contract and Go engine, populated from winget display names.

### Modified Capabilities
- `capture-app-display-name`: Extend Go engine capture to populate display names in item events and JSON envelope, matching the PowerShell engine's existing behavior.

## Impact

- **Event contract**: Additive-only — new optional field `name` on item events. Non-breaking per contract versioning rules.
- **Go engine events package**: `ItemEvent` struct gains `Name` field; `EmitItem` signature changes (all call sites updated).
- **Go engine winget driver**: `Detect()` returns display name alongside boolean result.
- **Go engine commands**: All commands emitting item events (apply, verify, capture, plan, restore, export, revert, validate-export) updated to pass display name.
- **GUI**: No changes needed — already has `name?: string` on `ItemEvent` type.
