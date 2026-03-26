## Context

The event contract (`docs/contracts/event-contract.md`) defines item events with `id`, `driver`, `status`, `reason`, and `message` fields. The GUI's `streaming-events.ts` already has `name?: string` on its `ItemEvent` type, expecting the engine to provide display names. The PowerShell engine sends display names via `Write-ItemEvent -Name`, and the `capture-app-display-name` spec formalizes this for capture. However, the Go engine's `EmitItem(id, driver, status, reason, message)` has no name parameter, so the GUI falls back to showing raw winget IDs.

The winget driver's `Detect()` currently returns `(bool, error)`. The display name is available from `winget list --id <ref> -e` output, which is already executed during detection.

## Goals / Non-Goals

**Goals:**
- Formally add `name` as an optional field on item events in the event contract
- Extract display names from winget list output during detection (no extra process spawns)
- Propagate display names through all Go engine item event emissions for winget-driven items
- Include `name` in capture JSON envelope `appsIncluded` entries

**Non-Goals:**
- Adding display names to non-winget drivers (restore, export, validate) — these use descriptive IDs already
- Caching or persisting display names beyond the current run
- Changing the PowerShell engine (already has this capability)
- Event schema version bump (additive optional field is non-breaking)

## Decisions

### 1. Extend `Detect()` return to include display name

**Choice:** Change `Detect(ref string) (bool, error)` to `Detect(ref string) (bool, string, error)` where the string is the display name.

**Rationale:** The display name is available in the same `winget list` output that `Detect` already parses. Returning it avoids a second winget invocation. The `Driver` interface changes, but there's only one implementation (winget), and all callers are in-tree.

**Alternative considered:** A separate `DisplayName(ref string) string` method — rejected because it would require a redundant `winget list` call or a cache layer, adding complexity for no benefit.

### 2. Add `Name` field to `EmitItem` signature

**Choice:** Add `name string` as the last parameter to `EmitItem(id, driver, status, reason, message, name string)`.

**Rationale:** Simple, explicit, grep-able. All 42 call sites must be updated, but most pass `""` for non-winget contexts. This is a mechanical change.

**Alternative considered:** Options struct or `EmitItemWithName` overload — rejected as over-engineering for a single additional field. The existing pattern of positional strings is consistent across the codebase.

### 3. Parse display name from winget list output

**Choice:** Extract the `Name` column from the tabular output of `winget list --id <ref> -e` using the same column-header parsing approach the PowerShell engine uses.

**Rationale:** Winget list output has a header line with `Name`, `Id`, `Version`, `Source` columns. The name is the substring from position 0 to the start of the `Id` column, trimmed. This is already proven reliable in the PowerShell engine.

### 4. Non-winget EmitItem calls pass empty name

**Choice:** Restore, export, validate, and revert commands pass `""` for the name parameter.

**Rationale:** These drivers don't have display names — their IDs are already human-readable (file paths, entry IDs). The `Name` field is `omitempty` in JSON, so empty strings produce no field in the output.

## Risks / Trade-offs

- **[Risk] Winget output format changes** → The column-header parsing is heuristic. Mitigation: if the Name column can't be found, return empty string (graceful degradation, same as PowerShell engine behavior).
- **[Risk] 42 call site updates** → Mechanical but large diff. Mitigation: all changes are adding a final `""` or variable parameter — easy to review, hard to get wrong.
- **[Trade-off] Driver interface break** → `Detect` signature change affects all implementations. Acceptable because there's only one driver implementation and all callers are in-tree.
