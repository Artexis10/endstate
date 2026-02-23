# Implementation Tasks

## Task 1: Add Write-RestoreItemEvent to engine/events.ps1

**File:** `engine/events.ps1`
**Specs:** apply-restore-streaming REQ-1, REQ-7

Add `Write-RestoreItemEvent` function patterned after `Write-ItemEvent`. Parameters: `Id`, `Module`, `Restorer`, `Source`, `Target`, `Status`, `Reason`, `BackupPath`, `TargetExisted`, `Message`. ValidateSet for Status: `"restoring"`, `"restored"`, `"skipped_up_to_date"`, `"skipped_missing_source"`, `"failed"`. ValidateSet for Restorer: `"copy"`, `"merge-json"`, `"merge-ini"`, `"append"`.

Update `Write-PhaseEvent` ValidateSet to include `"restore"`.
Update `Write-SummaryEvent` ValidateSet to include `"restore"`.

## Task 2: Refactor apply.ps1 restore case to use Invoke-RestoreAction

**File:** `engine/apply.ps1`
**Specs:** apply-restore-envelope REQ-7

Dot-source `restore.ps1` at the top of `apply.ps1` (add `. "$PSScriptRoot\restore.ps1"` after existing dot-sources, remove `. "$PSScriptRoot\..\restorers\copy.ps1"` since restore.ps1 already imports it).

Separate restore actions from the main action loop. Collect restore actions during the loop but defer processing to a dedicated restore phase. In the main loop's "restore" case: when `--EnableRestore` is not active, skip as before. When active, collect the action into a `$restoreActions` array instead of executing immediately.

## Task 3: Add restore phase emission and processing

**File:** `engine/apply.ps1`
**Specs:** apply-restore-streaming REQ-2, REQ-3, REQ-4, REQ-5

After the main action loop (app installs) completes and before verify:

1. If `$EnableRestore` and `$restoreActions.Count -gt 0`:
   - Emit `Write-PhaseEvent -Phase "restore"`
   - Get manifest directory for path resolution
   - Initialize restore counters: `$restoreSuccessCount`, `$restoreSkipCount`, `$restoreFailCount`
   - Initialize `$restoreResults` array
   - For each restore action:
     - Build action hashtable for `Invoke-RestoreAction` (id, restoreType, source, target, backup, requiresAdmin, requiresClosed, format, arrayStrategy, dedupe, newline, exclude)
     - Emit `Write-RestoreItemEvent` with status `"restoring"`
     - Call `Invoke-RestoreAction -Action $actionHash -RunId $runId -ManifestDir $manifestDir`
     - Map result status to event status and emit terminal `Write-RestoreItemEvent`
     - Update counters
   - Emit `Write-SummaryEvent -Phase "restore"` with restore counts
   - Add `backupLocation` field to summary event if backups were created

## Task 4: Write restore journal from apply

**File:** `engine/apply.ps1`
**Specs:** apply-restore-envelope REQ-5, REQ-6

After restore processing completes (and when not DryRun), write `logs/restore-journal-{runId}.json` using the same journal schema as `Invoke-Restore` in `restore.ps1`. Include all processed entries regardless of success/failure.

Journal schema:
```json
{
  "runId": "...",
  "manifestPath": "...",
  "manifestDir": "...",
  "exportRoot": null,
  "timestamp": "...",
  "entries": [
    {
      "kind": "copy",
      "source": "...",
      "target": "...",
      "resolvedSourcePath": "...",
      "targetPath": "...",
      "backupRequested": true,
      "targetExistedBefore": true,
      "backupCreated": true,
      "backupPath": "...",
      "action": "restored",
      "error": null
    }
  ]
}
```

## Task 5: Extend JSON envelope with restore data

**File:** `engine/apply.ps1`
**Specs:** apply-restore-envelope REQ-1, REQ-2, REQ-3, REQ-4

In the `$OutputJson` section of `Invoke-Apply`, after building the existing `$items` array:

1. Build `$restoreItems` array from `$restoreResults` (only when EnableRestore and results exist)
2. Build `$restoreSummary` object
3. Add to `$data` hashtable: `restoreItems` and `restoreSummary`
4. Add `restoreJournalFile` path to `$data` if journal was written
5. Do NOT modify the existing `$items` array

## Task 6: Apply same changes to Invoke-ApplyFromPlan

**File:** `engine/apply.ps1`
**Specs:** apply-restore-envelope REQ-8

Mirror the restore phase, event emission, journal writing, and JSON envelope extension from `Invoke-Apply` into `Invoke-ApplyFromPlan`. The plan-based path has the same restore case structure and needs identical convergence.

## Task 7: Update event contract documentation

**File:** `docs/contracts/event-contract.md`
**Specs:** apply-restore-streaming REQ-6

Add documentation for:
- `restore-item` event type with full field table
- `"restore"` as valid phase value in phase events and summary events
- Note that these are non-breaking additions per the contract's extensibility rules

## Task 8: Run verification

Verify:
1. `.\scripts\test-unit.ps1` — all existing unit tests pass
2. `npm run openspec:validate` — OpenSpec validation passes
3. No changes to existing `items[]` array behavior
4. No restore events emitted when `--EnableRestore` is not active
