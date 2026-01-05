# Endstate Event Contract v1

**Status:** Locked  
**Version:** 1  
**Last Updated:** 2025-12-31

## Purpose

This document defines the **immutable event contract** between the Endstate engine and GUI. Events enable real-time progress streaming and deterministic replay of completed runs.

## Event Fundamentals

### What is an Event?

An **event** is a single-line JSON object emitted by the engine to `stderr` during execution. Events are:

- **Ephemeral:** UI-only, not authoritative state
- **Ordered:** Emitted in strict chronological order
- **Immutable:** Once emitted, never modified
- **Persisted:** Written to `logs/<runId>.events.jsonl` for replay

Events do **NOT** replace the authoritative stdout JSON envelope (see `cli-json-contract.md`).

### Transport

- **Live streaming:** Events written to process `stderr` via `[Console]::Error.WriteLine()`
- **Persistence:** Events appended to `logs/<runId>.events.jsonl` (NDJSON format)
- **Activation:** Enabled via `--events jsonl` flag

### Format

Events use **NDJSON** (Newline-Delimited JSON):
- One event per line
- No pretty-printing
- UTF-8 encoding
- Blank lines ignored by consumers

## Schema v1

### Required Fields (All Events)

Every event **MUST** include:

| Field | Type | Description |
|-------|------|-------------|
| `version` | integer | Event schema version (always `1` for this contract) |
| `runId` | string | Run identifier (e.g., `"apply-20250101-120000-MACHINE"`) |
| `timestamp` | string | RFC3339 UTC timestamp (informational; NDJSON line order is authoritative) |
| `event` | string | Event type: `"phase"`, `"item"`, `"summary"`, `"error"`, `"artifact"` |

### Event Types

#### 1. Phase Event

Signals transition between engine phases.

```json
{
  "version": 1,
  "runId": "apply-20250101-120000-MACHINE",
  "timestamp": "2025-01-01T12:00:00.123Z",
  "event": "phase",
  "phase": "apply"
}
```

**Fields:**
- `phase`: `"plan"` | `"apply"` | `"verify"` | `"capture"`

**Guarantees:**
- First event in stream is always a phase event
- Phase events are monotonic (no backward transitions)

---

#### 2. Item Event

Tracks progress of individual items (apps, configs, etc.).

```json
{
  "version": 1,
  "runId": "apply-20250101-120000-MACHINE",
  "timestamp": "2025-01-01T12:00:01.456Z",
  "event": "item",
  "id": "Microsoft.VisualStudioCode",
  "driver": "winget",
  "status": "installed",
  "reason": null,
  "message": "Installed successfully"
}
```

**Fields:**
- `id` (string, required): Item identifier
- `driver` (string, required): Driver name (e.g., `"winget"`)
- `status` (string, required): One of:
  - `"to_install"` - Preview: will be installed
  - `"installing"` - In progress
  - `"installed"` - Successfully installed
  - `"present"` - Already on system
  - `"skipped"` - Skipped by filter/policy
  - `"failed"` - Failed
- `reason` (string | null, required): Optional reason code:
  - `"already_installed"`
  - `"filtered"`
  - `"filtered_runtime"`
  - `"filtered_store"`
  - `"sensitive_excluded"`
  - `"detected"`
  - `"install_failed"`
  - `"user_denied"` - User cancelled/denied installation (heuristic, unreliable)
  - `"missing"` - App not installed (verify phase)
  - `null` (no specific reason)

**Note on `user_denied`:** Detection is heuristic and unreliable. Winget provides no standardized exit code for user cancellation. Pattern matching on output text may misclassify some user cancellations as `install_failed`.
- `message` (string, optional): Human-readable message

**Guarantees:**
- Same `id` may appear multiple times (status transitions)
- Terminal statuses (`installed`, `present`, `skipped`, `failed`) indicate no further transitions within the same phase

---

#### 3. Summary Event

Emitted at end of each phase with aggregate counts.

```json
{
  "version": 1,
  "runId": "apply-20250101-120000-MACHINE",
  "timestamp": "2025-01-01T12:05:00.789Z",
  "event": "summary",
  "phase": "apply",
  "total": 15,
  "success": 12,
  "skipped": 2,
  "failed": 1
}
```

**Fields:**
- `phase` (string, required): Phase being summarized
- `total` (integer, required): Total items processed
- `success` (integer, required): Successfully completed
- `skipped` (integer, required): Skipped items
- `failed` (integer, required): Failed items

**Guarantees:**
- One summary event emitted per phase
- Last event in stream is always a summary event (for the final phase)
- `total = success + skipped + failed`

---

#### 4. Error Event

Reports errors (item-level or engine-level).

```json
{
  "version": 1,
  "runId": "apply-20250101-120000-MACHINE",
  "timestamp": "2025-01-01T12:03:00.000Z",
  "event": "error",
  "scope": "item",
  "message": "Failed to install package",
  "id": "Some.Package"
}
```

**Fields:**
- `scope` (string, required): `"item"` | `"engine"`
- `message` (string, required): Error description
- `id` (string, optional): Item ID if scope is `"item"`

---

#### 5. Artifact Event

Reports generated artifacts (e.g., captured manifests).

```json
{
  "version": 1,
  "runId": "capture-20250101-120000-MACHINE",
  "timestamp": "2025-01-01T12:10:00.000Z",
  "event": "artifact",
  "phase": "capture",
  "kind": "manifest",
  "path": "C:\\manifests\\captured.jsonc"
}
```

**Fields:**
- `phase` (string, required): Always `"capture"`
- `kind` (string, required): Always `"manifest"`
- `path` (string, required): Absolute filesystem path (opaque text, consumer should not parse)

---

## Invariants

### Ordering

1. **First event:** Phase event
2. **Last event:** Summary event (for final phase)
3. **Authoritative order:** NDJSON line order (timestamps are informational only)
4. **Phase boundaries:** Summary event closes each phase
5. **RunId consistency:** All events in a single file share the same `runId`

### Determinism

- Same manifest + system state → same event sequence (modulo timestamps and external dependencies)
- Event order is reproducible for replay purposes
- External factors (network, package availability) may affect specific events but not ordering guarantees

### Idempotence

- Replaying events produces identical UI state
- No side effects from event processing

### Replay Safety

- Events are **read-only** during replay
- Consumers must tolerate:
  - Unknown fields (forward compatibility within same version)
  - Missing optional fields
  - Duplicate events (rare, but possible)

---

## Versioning

### Schema Version

- **Current:** `1`
- **Type:** Integer (not semver)
- **Field:** `version` in every event
- **Scope:** Event schema version, independent of engine or application version

### Version Compatibility

**Engine (Producer):**
- Always emits current schema version
- Never emits multiple versions in same run

**GUI (Consumer):**
- **MUST** validate `version` field
- **MUST** reject events with unsupported versions
- **MUST** log warning and skip unsupported events (no crash)

### Breaking Changes

A **breaking change** requires a version bump:
- Removing required fields
- Changing field types
- Changing field semantics
- Removing event types
- Changing ordering guarantees

### Non-Breaking Changes

These do **NOT** require a version bump:
- Adding optional fields
- Adding new event types
- Adding new enum values (if documented as extensible)

---

## Replay Guarantees

### What Replay Provides

1. **State reconstruction:** Rebuild Live Activity from persisted events
2. **Audit trail:** Inspect what happened during a run
3. **Debugging:** Reproduce UI state for troubleshooting

### Replay Process

```
1. Read logs/<runId>.events.jsonl
2. Parse each line as JSON
3. Validate version field
4. Skip invalid/unsupported events
5. Apply events to state in order
6. Reconstruct final UI state
```

### Replay Limitations

- **No time travel:** Cannot replay partial runs
- **No editing:** Events are immutable
- **No interpolation:** Missing events = incomplete state

---

## What Can Change

### Allowed (No Version Bump)

- Adding new optional fields to existing events
- Adding new event types
- Improving timestamp precision
- Adding new `status` or `reason` values (documented as extensible)

### Forbidden (Requires Version Bump)

- Removing `version`, `runId`, `timestamp`, or `event` fields
- Changing field types (e.g., `version` from integer to string)
- Removing event types
- Breaking ordering guarantees (first=phase, last=summary)
- Changing `status` or `reason` semantics

---

## Enforcement

### Engine

- `engine/events.ps1` module enforces:
  - `version` = 1 (integer)
  - `runId` from `Enable-StreamingEvents -RunId`
  - `timestamp` via `Get-Rfc3339Timestamp`
  - All events written via `Write-StreamingEvent`

### GUI

- `src/lib/streaming-events.ts` enforces:
  - `version` validation (reject unsupported)
  - Required field validation
  - Event type validation
  - NDJSON parsing with error tolerance

### Tests

- `tests/contract/EventsContract.Tests.ps1` verifies:
  - All events have required fields
  - First event is phase
  - Last event is summary
  - NDJSON format compliance
  - Native stderr redirection works

---

## Migration Path

If a breaking change is required:

1. Bump `version` to `2`
2. Update engine to emit v2 events
3. Update GUI to support v1 and v2
4. Deprecate v1 support after transition period
5. Update this document with v2 schema

---

## UI Semantics

This contract defines the **low-level JSONL event schema** (fields, types, ordering).

For **UI status/phase semantics** (how events map to labels, colors, and user-facing language), see:

**→ `../endstate-gui/docs/UX_LANGUAGE.md` (single source of truth)**

Key UI semantic rules not duplicated here:
- `verify` + `status=failed` + `reason=missing` → UI displays **MISSING** (warn), not FAILED (error)
- `apply` + `status=skipped` + `reason=user_denied` → UI displays **CANCELLED** (warn), not FAILED (error)
- `verify` + `status=present` → UI displays **CONFIRMED**, not "Already present"
- **INSTALLED** vs **CONFIRMED**: Installed = installed this run; Confirmed = verified present

See `UX_LANGUAGE.md` for complete phase-aware mapping tables and critical distinctions.

---

## References

- **CLI Contract:** `cli-json-contract.md` (authoritative stdout JSON)
- **UI Semantics Contract:** `../endstate-gui/docs/UX_LANGUAGE.md` (status/phase mappings)
- **Engine Implementation:** `engine/events.ps1`
- **GUI Implementation:** `src/lib/streaming-events.ts`
- **Contract Tests:** `tests/contract/EventsContract.Tests.ps1`
