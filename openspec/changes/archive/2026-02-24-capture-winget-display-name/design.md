## Context

The capture flow has two code paths for detecting installed apps:

1. **`winget export` (primary)** — `engine/capture.ps1:887-927` — Parses JSON export. Each package entry contains `PackageIdentifier`. The export JSON schema *may* include a display name field but it is not extracted.
2. **`winget list` (fallback)** — `engine/capture.ps1:768-865` — Parses tabular CLI output. The header line `Name  Id  Version  Available  Source` is detected at line 796 and column indices are computed for `Id`, `Version`, `Source`. The `Name` column value is present in every data line but its index is never computed and the value is never extracted.

Both paths build app objects with `{ id, refs.windows, _source }` — no display name.

Downstream, `bin/endstate.ps1:2755-2764` builds the `appsIncluded` envelope array from `manifest.apps`, emitting `{ id, source }` per entry. Item events emitted at `bin/endstate.ps1:2771` pass only `Id`, `Driver`, `Status`, `Reason`, `Message` to `Write-ItemEvent`.

## Goals / Non-Goals

**Goals:**
- Extract the winget display name during both capture paths and carry it on the app object
- Include an optional `name` field in each `appsIncluded` entry in the capture JSON envelope
- Include an optional `name` field in `item` streaming events emitted during capture
- Maintain full backward compatibility — `name` is additive and optional everywhere

**Non-Goals:**
- Displaying names in the GUI (separate concern; GUI can start using the field when ready)
- Adding display names to apply or verify events (scope limited to capture)
- Persisting display names in the manifest file (the manifest schema is out of scope)

## Decisions

### D1: Extract name from both capture paths

**Decision**: Compute `$nameIndex` from the header line in the fallback parser and substring-extract the display name. For the export path, check for a display name field in the export JSON (field name TBD — `PackageName` if present, otherwise skip).

**Rationale**: The fallback path is the more reliable source since `winget list` always shows the Name column. The export path may or may not include it, so we treat it as best-effort.

**Alternative considered**: Only extract from fallback. Rejected because we want consistent behavior regardless of which path succeeds.

### D2: Carry name as `_name` on internal app object, surface as `name` in envelope

**Decision**: Store the display name as `_name` on the internal app hashtable (alongside `_source`), following the existing underscore-prefix convention for metadata fields that don't persist to the manifest. Surface it as `name` in the `appsIncluded` envelope and streaming events.

**Rationale**: The underscore prefix signals "engine metadata, not manifest schema." Using plain `name` in the public-facing envelope and events is cleaner for consumers.

### D3: Add optional `-Name` parameter to `Write-ItemEvent`

**Decision**: Add `[Parameter(Mandatory = $false)] [string]$Name = $null` to `Write-ItemEvent` and conditionally include it in the event hashtable when non-null.

**Rationale**: Mirrors the existing pattern for optional fields (see `-Reason`, `-Message`). Does not break any existing callers since the parameter is optional with a null default.

### D4: Only pass name in capture item events

**Decision**: Only the capture-phase `Write-ItemEvent` calls will pass `-Name`. Apply/verify item events will not (they don't have the data and don't need it).

**Rationale**: Display names are a capture-time discovery. Apply and verify operate from the manifest which currently doesn't store display names.

## Risks / Trade-offs

- **[Risk] `winget export` JSON may not include display name** → Mitigation: `_name` will be `$null` for export-path apps. Envelope and events tolerate null (field omitted). This is acceptable since the fallback path — which is more commonly exercised — always has access to the Name column.
- **[Risk] Name column width varies across locales** → Mitigation: Use the same header-based column indexing already used for Id/Version/Source. Name runs from index 0 (or header start) to `$idIndex`, so it's bounded by the same header detection logic.
- **[Risk] Truncated names in `winget list` output** → Mitigation: Accept truncated names as-is. A truncated display name is still more useful than no name at all. This matches how existing fields (Id, Source) are parsed.
