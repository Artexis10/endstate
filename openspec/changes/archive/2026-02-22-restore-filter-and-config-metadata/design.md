# Design: Restore Filter and Config Metadata

## Architecture

### RestoreFilter Flow
1. User passes `--RestoreFilter apps.vscode,apps.git` (comma-separated module IDs)
2. CLI parses into array of module IDs
3. After Expand-ManifestConfigModules runs (which already sets `_fromModule` on each entry), filter pending restore actions to only those whose `_fromModule` matches a filter entry
4. When filter is null/empty, all entries pass (backward compatible)

### Module ID Format
Module IDs use the `apps.<name>` format (e.g., `apps.vscode`, `apps.git`). The filter matches against the `_fromModule` property already set during config module expansion.

### JSON Envelope Extensions (Apply)
When RestoreFilter is active:
- `data.restoreFilter`: array of requested module IDs (or null if no filter)
- `data.restoreModulesAvailable`: array of all module IDs that had restore entries (before filtering)

### Capture Metadata Enrichment
Extend capture result with per-module detail:
- `data.configCapture.modules`: array of `{ id, displayName, entries, files }`

### Capabilities Extension
Add `--restore-filter` to apply and restore command flags in Get-CapabilitiesData.

## Key Decisions
1. Filter uses `_fromModule` property (already set by Expand-ManifestConfigModules)
2. Inline restore entries (not from configModules) have no `_fromModule` — they always pass the filter
3. `restoreModulesAvailable` is computed before filtering, showing what's available
4. Capture metadata is built from the same config module expansion that already runs

## Files Modified
- engine/apply.ps1 — RestoreFilter param + filtering logic (Invoke-Apply, Invoke-ApplyFromPlan)
- engine/restore.ps1 — RestoreFilter param (Invoke-Restore)
- engine/capture.ps1 — configCapture.modules metadata
- engine/json-output.ps1 — capabilities flags
- docs/contracts/cli-json-contract.md — document new fields
