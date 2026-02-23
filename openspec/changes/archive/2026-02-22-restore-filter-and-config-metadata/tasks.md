# Implementation Tasks

## Task 1: Add RestoreFilter parameter to Invoke-Apply and Invoke-ApplyFromPlan

**File:** `engine/apply.ps1`
**Specs:** restore-filter REQ-1, REQ-2, REQ-3

Add `-RestoreFilter` string parameter to both functions. Parse comma-separated string into array. After collecting `$pendingRestoreActions`, compute `$restoreModulesAvailable` (unique `_fromModule` values). If RestoreFilter is non-empty, filter `$pendingRestoreActions` to only entries whose action has a `module` or `_fromModule` matching the filter. Entries without `_fromModule` (inline entries) always pass.

## Task 2: Add RestoreFilter parameter to Invoke-Restore

**File:** `engine/restore.ps1`
**Specs:** restore-filter REQ-6

Add `-RestoreFilter` string parameter to Invoke-Restore. After loading manifest restore items, if RestoreFilter is non-empty, filter items whose `_fromModule` is in the filter. Note: standalone restore may not have _fromModule since it reads raw manifest — need to handle this by expanding configModules first or matching differently.

## Task 3: Add restoreFilter and restoreModulesAvailable to JSON envelope

**File:** `engine/apply.ps1`
**Specs:** restore-filter REQ-4, REQ-5

In the OutputJson section of both Invoke-Apply and Invoke-ApplyFromPlan, add:
- `data.restoreFilter` = parsed filter array (or null)
- `data.restoreModulesAvailable` = array of unique module IDs from all pending restore actions (before filtering)

## Task 4: Enrich capture metadata with per-module config detail

**File:** `engine/capture.ps1`
**Specs:** capture-config-metadata REQ-1, REQ-2, REQ-3, REQ-4

In Invoke-Capture, after config module capture completes, build configCapture.modules array. For each captured module, include id, displayName, entries (count of restore entries), and files (dest paths from capture.files). Add this to the capture result.

## Task 5: Add --restore-filter to capabilities

**File:** `engine/json-output.ps1`
**Specs:** restore-filter REQ-7

Add "--restore-filter" to commands.apply.flags and commands.restore.flags in Get-CapabilitiesData.

## Task 6: Run verification

Verify:
1. `.\scripts\test-unit.ps1` — all existing unit tests pass
2. `npm run openspec:validate` — OpenSpec validation passes
3. No changes when RestoreFilter is not provided (backward compat)
