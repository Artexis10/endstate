## Why

The apply command's restore path uses a simple `Invoke-CopyRestore` call that lacks streaming events, journaling, multi-restorer dispatch, and JSON envelope integration. The standalone restore command (`restore.ps1`) has full infrastructure for all of these. This divergence means the GUI cannot show restore progress, display restore results, or offer revert when using `apply --EnableRestore`. This is the critical-path blocker for GUI config integration.

## What Changes

- Add `restore-item` streaming event type to `engine/events.ps1` for real-time restore progress
- Add `"restore"` phase value to phase events, making apply execution: plan → apply → restore → verify
- Refactor `apply.ps1` restore case to use `Invoke-RestoreAction` from `restore.ps1` (multi-restorer dispatch, backup-first journaling, requiresAdmin/requiresClosed checks, exclude patterns)
- Emit restore-item events and restore phase/summary events during apply
- Write restore journal (`restore-journal-{runId}.json`) from apply for revert support
- Extend JSON envelope with `restoreItems[]` array and `restoreSummary` object (additive, backward compatible)
- Apply same changes to `Invoke-ApplyFromPlan` for consistency

## Capabilities

### New Capabilities
- `apply-restore-streaming`: Streaming NDJSON events for restore actions during apply, including restore-item events, restore phase events, and restore summary events
- `apply-restore-envelope`: JSON envelope extensions for restore results (restoreItems[], restoreSummary) and restore journal writing from apply

### Modified Capabilities
<!-- No existing spec-level requirements are changing. The event contract explicitly allows adding new event types and phase values without a version bump. The JSON contract allows additive fields. -->

## Impact

- `engine/events.ps1` — new `Write-RestoreItemEvent` function, updated `Write-PhaseEvent` and `Write-SummaryEvent` ValidateSet to include "restore"
- `engine/apply.ps1` — refactored restore case in both `Invoke-Apply` and `Invoke-ApplyFromPlan`, new dot-source of `restore.ps1`, restore phase emission, journal writing, JSON envelope extension
- `docs/contracts/event-contract.md` — documentation of restore-item event type and restore phase value
- No breaking changes to existing CLI behavior, event schema, or JSON envelope
