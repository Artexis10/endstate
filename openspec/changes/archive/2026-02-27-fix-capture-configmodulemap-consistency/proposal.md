## Why

The capture command's JSON envelope always emits `"configModuleMap": {}` (empty) while apply and verify correctly emit a populated map (e.g., 8 entries mapping winget IDs to module names). This violates INV-CONFIGMAP-3 (consistency across operations) in the config-module-map spec. The capture code path has all the data needed to populate the map but gates it solely on `$captureResult.BundleConfigModules`, which is never set for non-bundle captures.

## What Changes

- Add fallback logic in the capture JSON envelope block: when `BundleConfigModules` is empty, read `configModules` from the output manifest and pass them to `Build-ConfigModuleMap`, matching the pattern used by apply and verify.
- Ensure `Read-JsoncFile` is available in the capture code path (it may need to be sourced from engine/manifest.ps1).

## Capabilities

### New Capabilities

_(none)_

### Modified Capabilities

- `config-module-map`: Capture now populates configModuleMap from the output manifest when BundleConfigModules is empty, satisfying INV-CONFIGMAP-3 consistency requirement.

## Impact

- **Code**: `bin/endstate.ps1` — capture JSON envelope block only (~15 lines changed)
- **Backward compatible**: configModuleMap field already exists in capture output (currently empty); this just populates it
- **No contract changes**: Additive fix aligning behavior with existing spec invariants
