# Proposal: Restore Filter and Config Metadata

## Problem
The apply --EnableRestore command executes ALL restore entries from all config modules. Users need per-module selection for partial restores (e.g., restore only vscode configs on a shared machine). Additionally, capture output lacks per-module metadata, making it impossible for the GUI to display module-level config information.

## Solution
Add --RestoreFilter flag to apply and restore commands for per-module filtering. Enrich capture metadata with per-module detail (id, displayName, entry counts, file paths). Both changes are additive and backward compatible.

## Scope
- engine/apply.ps1 — add RestoreFilter parameter and filtering logic
- engine/restore.ps1 — add RestoreFilter parameter to standalone restore
- engine/config-modules.ps1 — ensure _fromModule tagging works for filtering (already exists)
- engine/capture.ps1 — enrich configCapture with per-module metadata
- engine/json-output.ps1 — add --restore-filter to capabilities
- docs/contracts/cli-json-contract.md — document new envelope fields

## Non-Goals
- Rich configModules objects in manifests (deferred — string array format sufficient)
- Adding displayName to all 70+ modules (already have displayName as required field)
- Modifying the event contract (no new event types needed)
