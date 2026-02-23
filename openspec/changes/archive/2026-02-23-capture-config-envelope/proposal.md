## Why

The capture `--json` envelope currently surfaces config module information as flat string arrays (`configsIncluded`, `configsSkipped`, `configsCaptureErrors`). The GUI needs structured per-module metadata to associate config modules with their parent apps without heuristic string matching. The data already exists internally in the engine — it just needs to be surfaced in the JSON envelope.

## What Changes

- Enrich `New-CaptureBundle` return value with a `ConfigModulesDetail` array containing structured per-module metadata (id, appId, displayName, status, filesCaptured, wingetRefs)
- Track per-module file counts in `Invoke-CollectConfigFiles`
- Surface `configModules` array in the capture JSON envelope when bundle capture is used
- Keep existing flat `configsIncluded`/`configsSkipped`/`configsCaptureErrors` fields for backward compatibility

## Capabilities

### New Capabilities
_(none — this extends an existing capability)_

### Modified Capabilities
- `capture-config-metadata`: Extend to cover the JSON envelope surface, adding `configModules` array with structured per-module metadata including wingetRefs for GUI matching

## Impact

- `engine/bundle.ps1` — `New-CaptureBundle` and `Invoke-CollectConfigFiles` return values enriched
- `bin/endstate.ps1` — `Invoke-CaptureCore` wires through new field; JSON envelope handler surfaces `configModules`
- `openspec/specs/capture-config-metadata/spec.md` — extended with envelope scenarios
- Additive change only — no breaking changes to existing envelope fields
