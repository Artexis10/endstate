# Endstate Event Contract v1

**Status:** Locked
**Version:** 1
**Last Updated:** 2026-07-16

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
| `event` | string | Event type: `"phase"`, `"progress"`, `"item"`, `"summary"`, `"error"`, `"artifact"`, `"restore-item"`, `"backup-chunk"`, `"consent"`, `"config-resolution"`, `"config-migration"` |

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
- `phase`: `"plan"` | `"apply"` | `"verify"` | `"capture"` | `"restore"`

**Guarantees:**
- First event in stream is always a phase event
- Phase events are monotonic (no backward transitions)

---

#### Capture Progress Event

Capture MAY emit additive schema-v1 progress events with no invented percentage:

```json
{"version":1,"runId":"capture-...","timestamp":"2026-07-17T12:00:00Z","event":"progress","phase":"capture","stage":"inventory"}
```

`stage` is `inventory`, `settings`, or `packaging`. The applicable subset is monotonic: `inventory` precedes package-source enumeration; `settings` appears only when matched configuration collection begins; `packaging` precedes final artifact publication. The opening capture phase remains first and summary remains last. Capture package items use `status:"present"` and `reason:"detected"`.

---

#### Capture Source and Store Lifecycle

Capture enumerates the WinGet community source and the Microsoft Store source concurrently by default. `--exclude-store-apps` opts out of Store access; `--include-store-apps` is a deprecated no-op (Store is included by default) and `--exclude-store-apps` wins when both are supplied.

- **Item `driver` for Store packages.** A Store-sourced package is still managed through WinGet, so its `item` event carries `driver:"winget"` like any other WinGet package. The `msstore` distinction is a *source*, not a driver: it is preserved in the written manifest and the capture envelope's `apps[].source` (`winget` | `msstore`), and it is threaded through plan, apply, verify, provisioning history, rollback, and uninstall so a Store app is never rerouted to the community source. Store apps dropped by `--exclude-store-apps` are not emitted as item events; they are tallied in the capture envelope's `counts.filteredStoreApps`.
- **Partial source coverage is reported honestly.** When one source fails but another yields usable inventory, capture succeeds and surfaces an envelope warning rather than silently under-reporting: `store_source_unavailable` (Store failed, community succeeded) or `winget_source_unavailable` (community failed, Store succeeded). Both sources failing is a hard `CAPTURE_FAILED`.
- **Store version pins are omitted.** Store versions are not a reliable install coordinate, so `capture --pin` omits them and emits one aggregate `store_version_unpinned` warning counting the affected packages.
- **Store display names are recovered best-effort.** `winget export` yields only the raw Store product ID (e.g. `XP89DCGQ3K6VLD`), and the concurrent `winget list --source msstore` name evidence can be lost to source flakiness or lock contention, leaving an entry labelled with its bare ID. When any captured Store entry has a missing or ID-equal `name`, capture makes one additional sequential `winget list --source msstore` call to recover friendly names ("PowerToys (Preview) x64") and fills the `item` event `name`, the envelope `apps[].name`, and the written manifest. This is additive and never fails the capture: when no name resolves, the raw ID is preserved, matching the general `name` semantics (absent `name` → consumers format the `id`).

These warnings are envelope-level `warnings[]` entries, not stream events; their codes, fields, and precise semantics are catalogued in `cli-json-contract.md`. This section is additive to schema v1 (no version bump): the progress, source, and warning fields are all optional and absent on engines or runs that do not produce them.

---

#### 2. Item Event

Tracks progress of individual items (apps, configs, etc.).

```json
{
  "version": 1,
  "runId": "apply-20250101-120000-MACHINE",
  "timestamp": "2025-01-01T12:00:01.456Z",
  "event": "item",
  "id": "git.install",
  "driver": "chocolatey",
  "status": "installed",
  "reason": null,
  "message": "Installed; restart required",
  "rebootRequired": true
}
```

**Fields:**
- `id` (string, required): Item identifier
- `driver` (string, required for package items): Resolved stable driver/backend name (e.g., `"winget"`, `"chocolatey"`, `"brew"`, or `"nix"`)
- `name` (string, optional): Human-readable display name (e.g., `"Visual Studio Code"`). When absent, consumers should format the `id` field for display.
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
- `rebootRequired` (boolean, optional): `true` when a successful package operation requires a reboot; omitted otherwise. This is a success fact, not a warning or failure.

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

#### 6. Restore-Item Event

Tracks progress of individual restore actions during apply with `--EnableRestore`. This is a non-breaking addition (new event type, no version bump required per extensibility rules).

```json
{
  "version": 1,
  "runId": "apply-20250101-120000-MACHINE",
  "timestamp": "2025-01-01T12:06:00.000Z",
  "event": "restore-item",
  "id": "vscode/settings.json",
  "module": "vscode",
  "restorer": "copy",
  "source": "./payload/apps/vscode/settings.json",
  "target": "~/AppData/Roaming/Code/User/settings.json",
  "status": "restored",
  "reason": null,
  "backupPath": "C:\\endstate\\state\\backups\\20250101-120000\\...",
  "targetExisted": true,
  "message": "restored successfully"
}
```

**Fields:**
- `id` (string, required): Restore entry identifier (e.g., `"vscode/settings.json"`)
- `module` (string, required): Config module ID (e.g., `"vscode"`)
- `restorer` (string, required): Engine restorer type: `"copy"` | `"merge-json"` | `"merge-ini"` | `"append"` | `"delete-glob"` | `"registry-import"` | `"registry-set"`
- `source` (string, required): Portable source path
- `target` (string, required): System target path
- `status` (string, required): One of:
  - `"restoring"` - In progress
  - `"restored"` - Successfully restored
  - `"skipped_up_to_date"` - Target already matches source
  - `"skipped_missing_source"` - Source file not found
  - `"failed"` - Restore failed
- `reason` (string | null, required): Reason for skip/failure, or null
- `backupPath` (string | null, required): Path to backup if created, or null
- `targetExisted` (boolean, required): Whether the target file existed before restore
- `message` (string, required): Human-readable message
- `captureId` (string, optional): Owning generation-aware config capture
- `configSetId` (string, optional): Owning config set
- `targetInstanceId` (string, optional): Selected target instance
- `sourceGeneration` (string, optional): Captured source generation
- `targetGeneration` (string, optional): Resolved target generation

**Guarantees:**
- Same `id` appears twice per restore action: first with `"restoring"`, then with terminal status
- Only emitted when `--EnableRestore` is active and restore entries exist
- Emitted during the `"restore"` phase (between `"apply"` and `"verify"`)

**Summary event extension:**
When `phase` is `"restore"`, the summary event may include an additional optional field:
- `backupLocation` (string | null): Root backup directory path

---

#### 7. Backup-Chunk Event

Tracks per-chunk progress of a hosted-backup push or pull (added in engine v2.3.0). Non-breaking addition (new event type, no version bump).

```json
{
  "version": 1,
  "runId": "backup-push-20260526-120000-MACHINE",
  "timestamp": "2026-05-26T12:00:01.456Z",
  "event": "backup-chunk",
  "chunkIndex": 46,
  "totalChunks": 95,
  "encryptedSize": 4194316,
  "status": "uploading",
  "current": 47,
  "total": 95
}
```

**Fields:**
- `chunkIndex` (integer, required): 0-based chunk index, or `-1` for the manifest blob
- `totalChunks` (integer, required): Count of data chunks (manifest excluded)
- `encryptedSize` (integer, required): On-the-wire size of the chunk in bytes
- `status` (string, required): One of:
  - `"uploading"` — push: chunk in flight
  - `"uploaded"` — push: chunk terminally succeeded
  - `"downloading"` — pull: chunk in flight
  - `"verified"` — pull: SHA-256 verified
  - `"decrypted"` — pull: AEAD decrypt succeeded
  - `"retrying"` — push: previous attempt failed with a retryable error; retry imminent (no pull-side retry today)
  - `"failed"` — terminal failure after exhausting retries
- `message` (string, optional): Non-fatal hint (e.g., error message before retry)
- `attempt` (integer, optional): 1-based current attempt number; present only when `status === "retrying"`
- `maxAttempts` (integer, optional): Inclusive upper bound on attempts; present only when `status === "retrying"`
- `current` (integer, optional): 1-based chunk-of-total position (mirrors `chunkIndex + 1` for data chunks; omitted for the manifest chunk)
- `total` (integer, optional): Mirrors `totalChunks` for forward-compat

**Guarantees:**
- Emitted during the `"backup-push"` and `"backup-pull"` phases (alongside existing item events for log continuity)
- Per chunk, the status sequence is `uploading → (retrying* →) uploaded` for push and `downloading → verified → decrypted` for pull
- A `retrying` event is emitted **before** the backoff sleep, so the GUI shows the retry tag during the sleep window rather than after

**Consumer notes:**
- The GUI MUST handle missing `attempt` / `maxAttempts` gracefully (treat as a generic retry indicator) so a GUI built against this event can still consume events from an older engine that doesn't yet emit them
- `current` is omitted from the manifest chunk (chunkIndex == -1); the GUI should render "manifest" rather than a chunk number

---

#### 8. Consent Event

Requests the user's consent to bootstrap one or more absent package backends, including the Nix realizer/Homebrew driver on macOS/Linux and Chocolatey on Windows. Non-breaking addition (new event type, no version bump required per extensibility rules).

One event covers the **combined** set of backends a run needs and lacks, so the GUI renders a single plain-language consent dialog. The `message` is product-neutral (it never names "Nix"/"Homebrew") to keep the backend concepts invisible; the `details` carry the exact, inspectable installer commands for anyone who looks.

```json
{
  "version": 1,
  "runId": "apply-20260606-120000-MACHINE",
  "timestamp": "2026-06-06T12:00:01.000Z",
  "event": "consent",
  "backends": ["brew", "nix"],
  "message": "Endstate needs to set up the tools it uses to install and configure your software. You may be asked for your administrator password, and a background helper and a dedicated storage area may be created. See the details for the exact commands that will run.",
  "details": [
    "/bin/bash -c \"$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)\"",
    "curl --proto '=https' --tlsv1.2 -sSf -L https://install.determinate.systems/nix | sh -s -- install --no-confirm"
  ]
}
```

**Fields:**
- `backends` (array of string, required): Internal identifiers of the absent backends needing consent (e.g. `"brew"`, `"nix"`). Structured metadata the GUI maps back to the run — not user-facing copy.
- `message` (string, required): The plain-language, product-neutral consent ask. Names no backend product.
- `details` (array of string, optional): The exact installer commands the privileged step would run, one per backend — the inspectable "what will run" affordance. Omitted when empty.

**Guarantees:**
- At most ONE consent event per run, covering the combined set of absent needed backends (combined consent).
- Emitted only by a mutating apply stage (including `rebuild`'s apply stage), and only when a needed backend is absent and consent has not yet been given. A present/working backend never emits it; `--bootstrap-backends` installs without emitting a request; `--no-bootstrap` skips without emitting a request.
- The engine never installs a backend without explicit consent; absent a consent decision it defaults to skipping that backend's lane and continuing the run.

**Consumer notes:**
- The GUI renders `message` as the dialog body and may reveal `details` behind a "show details" affordance; on the user's affirmative it re-invokes apply or rebuild with the existing `--bootstrap-backends` flag.
- A consumer built against an older engine that does not emit `consent` is unaffected (forward compatibility — unknown event types are ignored).

---

#### 9. Config-Resolution Event

Reports the engine's final compatibility/target decision for one captured config set after final target detection and before the first target mutation for that set. This is an additive event type; schema remains version `1`.

```json
{
  "version": 1,
  "runId": "apply-20260716-120000-MACHINE",
  "timestamp": "2026-07-16T12:00:01.000Z",
  "event": "config-resolution",
  "captureId": "apps.example-preferences-instance-a",
  "moduleId": "apps.example",
  "configSetId": "preferences",
  "sourceInstance": {
    "id": "instance-a",
    "detectorId": "photoshop-install",
    "rawVersion": "25.0.0",
    "normalizedVersion": "25.0.0",
    "evidence": { "kind": "installed-app", "value": "Adobe Photoshop 2024" }
  },
  "sourceInstanceId": "instance-a",
  "targetInstanceId": "instance-b",
  "targetCandidates": [
    {
      "id": "instance-b",
      "moduleId": "apps.example",
      "detectorId": "photoshop-install",
      "rawVersion": "26.0.0",
      "normalizedVersion": "26.0.0",
      "evidence": { "kind": "installed-app", "value": "Adobe Photoshop 2025" },
      "targetGeneration": "g2",
      "restoreModuleRevision": "<sha256-b>"
    }
  ],
  "sourceGeneration": "g1",
  "sourceGenerationFingerprint": "<sha256>",
  "targetGeneration": "g2",
  "resolution": "migrate",
  "reason": null,
  "migrationPath": ["g1", "g2"],
  "captureModuleRevision": "<sha256-a>",
  "restoreModuleRevision": "<sha256-b>",
  "label": "Will be upgraded",
  "message": "Settings will be upgraded from g1 to g2 before restore.",
  "remediation": null
}
```

**Fields:**

- Identity fields (`captureId`, `moduleId`, `configSetId`) are required.
- `sourceInstance` preserves portable, non-secret capture identity and version evidence. `targetCandidates` is a required, non-null array of portable, non-secret target identity and version evidence; it is `[]` when no candidate exists. Host-local roots and locators remain internal and are never emitted.
- Instance, generation, fingerprint, and module-revision fields are present when known; legacy payloads omit unknown generation fields.
- `resolution` (required) is `direct`, `migrate`, `incompatible`, `unknown`, or `legacy_unverified`.
- `reason` is a stable machine reason or `null`; the GUI does not derive it from module data.
- `migrationPath` is the ordered generation path and is `[]` unless a migration is planned.
- `label`, `message`, nullable `remediation`, and all technical detail are engine-authored. Consumers render them verbatim and do not recompute copy, compatibility, or evidence.

**Guarantees:**

- Exactly one final config-resolution event is emitted per captured config set in restore-capable input.
- It precedes any mutating `restore-item` or commit-stage `config-migration` event for the same capture ID.
- `incompatible` and `unknown` resolutions have no corresponding mutating event.
- An explicit legacy module lane uses `configSetId: "legacy"` and the deterministic capture ID returned by `bundle.LegacyCaptureID(moduleId)`, and emits `legacy_unverified` before any corresponding legacy restore-item event.
- Anonymous inline restore actions without a module-lane association remain ordinary restore-item events and do not receive fabricated config-resolution events, instances, versions, or generations.

---

#### 10. Config-Migration Event

Reports engine-owned progress for staging, each ordered migration edge, validation, commit, and rollback. Module and bundle data never provide executable event behavior.

```json
{
  "version": 1,
  "runId": "apply-20260716-120000-MACHINE",
  "timestamp": "2026-07-16T12:00:02.000Z",
  "event": "config-migration",
  "captureId": "apps.example-preferences-instance-a",
  "configSetId": "preferences",
  "stage": "edge",
  "fromGeneration": "g1",
  "toGeneration": "g2",
  "status": "completed",
  "reason": null,
  "message": "migration edge validated",
  "remediation": null
}
```

**Fields:**

- `captureId` and `configSetId` are required correlation identities.
- `stage` is exactly `staging`, `edge`, `validation`, `commit`, or `rollback`.
- `fromGeneration` and `toGeneration` are present for an edge.
- `status` is exactly `started`, `completed`, or `failed`.
- `reason` and `remediation` are nullable and serialize as `null` when absent. `status`, `reason`, `message`, and `remediation` are engine-derived; consumers render them verbatim and do not interpret migration operations.

**Guarantees:**

- Multi-edge paths emit edges in declared plan order and validation completes before commit begins.
- When failure occurs after target mutation, rollback start and terminal outcome are emitted for the same capture ID.
- Streaming progress may be in-progress; authoritative envelope terminal status remains exactly `planned`, `restored`, `skipped`, `failed`, `rolled_back`, or `rollback_failed`.
- A `rollback_failed` outcome is followed by no later config-set mutation in that run.

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

### Composed streams (`rebuild`)

`endstate rebuild` does **not** define new event types. It composes the **existing** `apply` and `verify` event streams unchanged: apply's stream (opening with a `phase` event, closing with a `summary`) is followed by verify's stream (also opening `phase`, closing `summary`). Whenever events are emitted, the concatenation satisfies the ordering invariant — the first event is a phase and the last is a summary; an input error before planning (e.g. the extracted manifest fails to load) yields an empty stream plus the envelope error, the same as every other command. Schema stays **v1**. (Known cosmetic divergence: the streamed sub-run `runId`s are `apply-<ts>`/`verify-<ts>` while the `rebuild` JSON envelope carries its own `rebuild-<ts>` — unifying them is a deferred GUI-facing follow-up.)

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
