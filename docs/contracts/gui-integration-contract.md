# Endstate GUI Integration Contract

This document serves as the **contract-level source of truth** for the integration between Endstate CLI and Endstate GUI.

## Overview

Endstate GUI is a thin presentation layer that consumes Endstate CLI via JSON output. This document defines the rules, versioning, and execution model that both projects must follow.

---

## Core Principles

### 1. Thin GUI

Endstate GUI **must not** contain:
- Business logic
- Provisioning logic
- Assumptions about internal Endstate implementation
- Direct file system operations for provisioning
- Package manager interactions

Endstate GUI **only**:
- Invokes CLI commands
- Parses JSON output
- Presents results to users
- Manages user preferences and UI state

### 2. CLI as Source of Truth

All provisioning operations are executed by CLI invocation. The GUI is purely a presentation layer that:
- Calls CLI commands with appropriate flags
- Parses structured JSON responses
- Displays results and progress to users

### 3. Explicit Versioning

Both CLI version and JSON schema version are explicit and machine-readable:
- `cliVersion`: Follows Semantic Versioning (MAJOR.MINOR.PATCH)
- `schemaVersion`: JSON schema version (MAJOR.MINOR)

---

## JSON Schema Contract v1.0

### Standard Envelope

Every `--json` output includes this envelope:

```json
{
  "schemaVersion": "1.0",
  "cliVersion": "0.1.0",
  "command": "apply",
  "runId": "20241220-143052",
  "timestampUtc": "2024-12-20T14:30:52Z",
  "success": true,
  "data": { ... },
  "error": null
}
```

### Required Envelope Fields

| Field | Type | Description |
|-------|------|-------------|
| `schemaVersion` | string | JSON schema version |
| `cliVersion` | string | CLI version (semver) |
| `command` | string | Command that produced output |
| `runId` | string | Unique run ID (yyyyMMdd-HHmmss) |
| `timestampUtc` | string | ISO 8601 UTC timestamp |
| `success` | boolean | Whether command succeeded |
| `data` | object | Command-specific result |
| `error` | object\|null | Error object if failed |

### Error Object

```json
{
  "code": "MANIFEST_NOT_FOUND",
  "message": "The specified manifest file does not exist.",
  "detail": { "path": "C:\\manifests\\missing.jsonc" },
  "remediation": "Check the file path and ensure the manifest exists.",
  "docsKey": "errors/manifest-not-found"
}
```

---

## Versioning Rules

### Semantic Versioning (CLI)

Endstate CLI follows [Semantic Versioning](https://semver.org/):

- **MAJOR:** Breaking changes (including JSON schema breaking changes)
- **MINOR:** New features, backward-compatible
- **PATCH:** Bug fixes, backward-compatible

### Schema Versioning

The JSON schema has its own version independent of CLI version:

- Schema version uses `MAJOR.MINOR` format
- **MAJOR bump:** Breaking change to JSON structure
- **MINOR bump:** Additive, backward-compatible changes

### Compatibility Rules

1. **Additive JSON changes** (new optional fields) are backward-compatible
2. **Breaking JSON changes** require:
   - Schema major version bump (e.g., `1.0` → `2.0`)
   - CLI major version bump
3. GUI must validate schema version before executing commands
4. GUI must refuse execution if schema version is incompatible

---

## Execution Model

### Development Mode

During development, Endstate GUI resolves the CLI from PATH:

```
GUI starts
  │
  ├─► Call: endstate capabilities --json
  │
  ├─► Parse response
  │     │
  │     ├─► Check schemaVersion
  │     │     │
  │     │     ├─► Compatible? → Proceed
  │     │     │
  │     │     └─► Incompatible? → Show error, refuse execution
  │     │
  │     └─► Check cliVersion (informational)
  │
  └─► Ready for user commands
```

### Production Mode

Production builds of Endstate GUI bundle a pinned Endstate binary:

1. GUI ships with a specific Endstate CLI version
2. GUI validates bundled CLI on startup via `capabilities`
3. Version mismatch indicates corrupted installation

### Incompatibility Handling

When schema versions are incompatible, GUI must display:

```
Endstate CLI Incompatible

The installed Endstate CLI (v0.1.0, schema 1.0) is not compatible 
with this version of Endstate GUI (requires schema 2.0).

Please update Endstate CLI or use a compatible GUI version.
```

---

## Capabilities Handshake

The `capabilities` command is the entry point for GUI integration:

```powershell
endstate capabilities --json
```

### Response Structure

```json
{
  "schemaVersion": "1.0",
  "cliVersion": "0.1.0",
  "command": "capabilities",
  "runId": "20241220-143052",
  "timestampUtc": "2024-12-20T14:30:52Z",
  "success": true,
  "data": {
    "supportedSchemaVersions": {
      "min": "1.0",
      "max": "1.0"
    },
    "commands": {
      "apply": { "supported": true, "flags": [...] },
      "verify": { "supported": true, "flags": [...] },
      ...
    },
    "features": {
      "streaming": false,
      "parallelInstall": true,
      "configModules": true,
      "jsonOutput": true,
      "manualApps": true,
      "hostedBackup": {
        "supported": true,
        "minSchemaVersion": "1.0",
        "issuerUrl": "https://auth.example.com",
        "audience": "endstate-backup",
        "rename": true,
        "ifChanged": true
      },
      "schedule": {
        "supported": true,
        "autoPush": true
      }
    },
    "platform": {
      "os": "windows",
      "drivers": ["winget"]
    }
  },
  "error": null
}
```

> **Note:** `features.hostedBackup.ifChanged` is the canonical GUI gate for the
> conditional auto-backup flow (`backup push --if-changed`). The GUI MUST check
> this field rather than probing `commands.backup.flags` for `--if-changed`.

For generation-aware restore, `commands.apply.flags`, `commands.restore.flags`, and `commands.rebuild.flags` advertise repeatable `--restore-target`. The GUI must capability-gate target selection rather than assuming a CLI version supports it.

### GUI Responsibilities

1. Call `capabilities --json` on startup
2. Validate `schemaVersion` is within supported range
3. Cache capabilities for the session
4. Use `commands` to determine available features
5. Use `features` to enable/disable UI elements

---

## Supported Commands

| Command | JSON Flag | Description |
|---------|-----------|-------------|
| `capabilities` | `--json` | Report CLI capabilities |
| `capture` | `--json` | Capture apps and configuration into a profile artifact |
| `apply` | `--json` | Execute provisioning |
| `restore` | `--json` | Restore configuration |
| `rebuild` | `--json` | Install, restore, and verify from a capture artifact |
| `verify` | `--json` | Verify machine state |
| `report` | `--json` | Retrieve run history |

---

## Configuration Generation Presentation

The CLI is the sole authority for application/config instance discovery, raw and normalized version evidence, config-generation selection, compatibility resolution, migration paths, target collisions, and transaction outcome. The GUI must not load module or bundle snapshots as rules and must not compare versions or reconstruct a migration graph.

Restore-capable envelopes expose `configResolutions[]`, `configResolutionSummary`, and `restoreItems[]`; streams expose `config-resolution` and `config-migration`. Each resolution preserves a portable, non-secret `sourceInstance` and a non-null `targetCandidates[]` of portable target identity and version evidence. Host-local target roots and locators remain internal to the engine. The GUI correlates rows by `captureId`, renders the engine-provided candidates, and sends a user choice back as `--restore-target <captureId>=<targetInstanceId>`. It never silently selects a highest/newest side-by-side instance.

When input has no config payloads, envelopes omit all config fields. When config payloads are present, `configResolutions[]`, every row's `targetCandidates[]`, `migrationPath[]`, and `resolvedTargets[]`, and `restoreItems[]` are present and serialize as `[]`, never `null`, when empty. `reason` and `remediation` serialize as `null` when absent. Rebuild's canonical config fields are at the top level of command data; its nested apply result may mirror them.

The new event types and optional restore-item fields remain event schema version `1`; consumers follow the existing rule of ignoring unknown additive fields/types they do not yet render.

### Locked Default Labels

The engine authors each row's distilled `label`, `message`, nullable `remediation`, and technical details. The GUI renders them verbatim and does not map resolutions to replacement copy or recompute details from versions, candidates, module rules, or bundle data. The default engine labels are:

| Engine resolution | Default engine label |
|-------------------|-------------------|
| `direct` | **Compatible** |
| `migrate` | **Will be upgraded** |
| `unknown` or `legacy_unverified` | **Compatibility unknown** |
| `incompatible` | **Not supported** |

Advanced details display the engine-provided source/target instance versions, config-set and generation IDs, migration path, capture/restore module revisions, and stable reason verbatim. They are progressive disclosure of engine output, not inputs to GUI-side logic.

Explicit legacy v1 module lanes remain usable through existing consent, use `configSetId: "legacy"` plus the deterministic capture ID returned by `bundle.LegacyCaptureID(moduleId)`, and receive the engine label **Compatibility unknown** (`legacy_unverified`), never falsely incompatible. Anonymous inline actions without a module-lane association appear only as ordinary restore items; the GUI must not invent config-resolution rows for them. Invalid v2 provenance is shown as the engine's `unknown`/failure reason and never offered through a legacy fallback.

### Terminal Execution Status

The GUI treats compatibility `resolution` and terminal execution `status` as separate fields. Envelope status is exactly `planned`, `restored`, `skipped`, `failed`, `rolled_back`, or `rollback_failed`. In-progress migration events use stage `staging`, `edge`, `validation`, `commit`, or `rollback` and status `started`, `completed`, or `failed`; no other stage/status value is inferred. For failure rows, the GUI preserves `reason` as the primary execution failure and uses `status` for rollback outcome. `rollback_failed` must be surfaced as requiring attention because the engine blocks later config-set mutation.

---

## Error Codes

Standard error codes for programmatic handling:

| Code | Description |
|------|-------------|
| `MANIFEST_NOT_FOUND` | Manifest file does not exist |
| `MANIFEST_PARSE_ERROR` | Manifest is invalid JSON/JSONC |
| `MANIFEST_VALIDATION_ERROR` | Manifest schema validation failed |
| `PLAN_NOT_FOUND` | Plan file does not exist |
| `PLAN_PARSE_ERROR` | Plan file is invalid |
| `WINGET_NOT_AVAILABLE` | winget not installed |
| `INSTALL_FAILED` | Package installation failed |
| `RESTORE_FAILED` | Configuration restore failed |
| `INVALID_RESTORE_TARGET` | Restore-target input is malformed, duplicated, unknown, or non-targetable; display the engine-authored message and remediation |
| `VERIFY_FAILED` | Verification check failed |
| `PERMISSION_DENIED` | Insufficient permissions |
| `INTERNAL_ERROR` | Unexpected internal error |
| `SCHEMA_INCOMPATIBLE` | Schema version mismatch |

---

## Testing Requirements

### CLI Side

1. Schema shape tests must verify envelope structure
2. All `--json` outputs must include required envelope fields
3. Error codes must be stable and documented

### GUI Side

1. Compatibility check must run on startup
2. Schema validation must reject incompatible versions
3. Error handling must display user-friendly messages

---

## Change Management

### Adding New Fields (Non-Breaking)

1. Add field to CLI output
2. Update documentation
3. GUI ignores unknown fields (graceful degradation)
4. No version bump required

### Removing/Changing Fields (Breaking)

1. Bump schema major version
2. Bump CLI major version
3. Update GUI to support new schema
4. Document migration path
5. Consider supporting multiple schema versions during transition

---

## References

- [CLI JSON Contract](./cli-json-contract.md) - Full schema specification
- [Endstate README](../readme.md) - CLI documentation
- [Endstate GUI README](../../endstate-gui/README.md) - GUI documentation
