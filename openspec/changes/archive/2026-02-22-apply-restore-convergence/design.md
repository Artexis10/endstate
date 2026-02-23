## Approach

Converge apply's restore path with the standalone restore infrastructure by:
1. Adding a new `Write-RestoreItemEvent` function to the events module
2. Expanding `Write-PhaseEvent` and `Write-SummaryEvent` to accept `"restore"` phase
3. Dot-sourcing `restore.ps1` functions in `apply.ps1` to reuse `Invoke-RestoreAction`
4. Separating restore processing into its own phase between apply and verify
5. Extending the JSON envelope with additive `restoreItems[]` and `restoreSummary` fields
6. Writing a restore journal from apply using the same schema as standalone restore

## Key Decisions

### 1. New event type: `restore-item`

A new `restore-item` event type (not reusing `item` events). The event contract explicitly allows adding new event types without a version bump.

```json
{
  "version": 1,
  "runId": "apply-...",
  "timestamp": "...",
  "event": "restore-item",
  "id": "vscode/settings.json",
  "module": "vscode",
  "restorer": "copy",
  "source": "./payload/apps/vscode/settings.json",
  "target": "~/AppData/Roaming/Code/User/settings.json",
  "status": "restored",
  "reason": null,
  "backupPath": "C:\\endstate\\state\\backups\\...",
  "targetExisted": true,
  "message": "restored successfully"
}
```

Status values: `"restoring"` | `"restored"` | `"skipped_up_to_date"` | `"skipped_missing_source"` | `"failed"`

### 2. New phase value: `"restore"`

Apply execution becomes: plan → apply → restore → verify. Each phase gets its own phase event and summary event. The restore phase only appears when `--EnableRestore` is active and restore entries exist.

### 3. Restore summary event

Standard summary fields plus `backupLocation`:

```json
{
  "event": "summary",
  "phase": "restore",
  "total": 5,
  "success": 4,
  "skipped": 1,
  "failed": 0,
  "backupLocation": "C:\\endstate\\state\\backups\\20260222-120000"
}
```

### 4. JSON envelope extension (backward compatible)

Add `restoreItems[]` alongside existing `items[]`. Add `restoreSummary` object. Do NOT modify existing `items[]` array — it stays app-only.

```json
{
  "data": {
    "items": [...],
    "restoreItems": [
      {
        "id": "vscode/settings.json",
        "module": "vscode",
        "restorer": "copy",
        "source": "./payload/apps/vscode/settings.json",
        "target": "~/AppData/...",
        "status": "restored",
        "reason": null,
        "backupPath": "...",
        "targetExisted": true,
        "message": "restored successfully"
      }
    ],
    "restoreSummary": {
      "total": 5,
      "restored": 4,
      "skipped": 1,
      "failed": 0,
      "backupLocation": "..."
    }
  }
}
```

### 5. Restore journal from apply

`apply.ps1` writes `logs/restore-journal-{runId}.json` using the same schema as standalone restore. This enables revert after apply-with-restore.

## Integration Points

- `engine/events.ps1`: Add `Write-RestoreItemEvent`, extend `ValidateSet` on `Write-PhaseEvent` and `Write-SummaryEvent`
- `engine/apply.ps1`: Dot-source `restore.ps1` (for `Invoke-RestoreAction`, `Get-RestoreActionId`, `Expand-RestorePath`), separate restore loop, emit events, write journal, extend JSON envelope
- `engine/restore.ps1`: No changes — functions are consumed as-is
- `docs/contracts/event-contract.md`: Document new event type and phase value

## Risks & Mitigations

| Risk | Mitigation |
|------|-----------|
| Dot-sourcing `restore.ps1` in `apply.ps1` causes function name collisions | Only the needed functions are consumed; both files already share `logging.ps1`, `manifest.ps1`, `state.ps1` dot-sources |
| Restore phase breaks existing event consumers | Event contract explicitly allows new event types and new enum values; consumers must tolerate unknown events |
| JSON envelope extension breaks GUI | JSON contract specifies additive fields are backward-compatible; old GUI versions ignore unknown fields |
| `Invoke-RestoreAction` behavior differs from old `Invoke-CopyRestore` | `Invoke-RestoreAction` is a superset — it dispatches to copy (default), merge-json, merge-ini, append; copy behavior is equivalent |
