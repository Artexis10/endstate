# Autosuite GUI Integration Contract

This document serves as the **contract-level source of truth** for the integration between Autosuite CLI and Autosuite GUI.

## Overview

Autosuite GUI is a thin presentation layer that consumes Autosuite CLI via JSON output. This document defines the rules, versioning, and execution model that both projects must follow.

---

## Core Principles

### 1. Thin GUI

Autosuite GUI **must not** contain:
- Business logic
- Provisioning logic
- Assumptions about internal Autosuite implementation
- Direct file system operations for provisioning
- Package manager interactions

Autosuite GUI **only**:
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

Autosuite CLI follows [Semantic Versioning](https://semver.org/):

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

During development, Autosuite GUI resolves the CLI from PATH:

```
GUI starts
  │
  ├─► Call: autosuite capabilities --json
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

Production builds of Autosuite GUI bundle a pinned Autosuite binary:

1. GUI ships with a specific Autosuite CLI version
2. GUI validates bundled CLI on startup via `capabilities`
3. Version mismatch indicates corrupted installation

### Incompatibility Handling

When schema versions are incompatible, GUI must display:

```
Autosuite CLI Incompatible

The installed Autosuite CLI (v0.1.0, schema 1.0) is not compatible 
with this version of Autosuite GUI (requires schema 2.0).

Please update Autosuite CLI or use a compatible GUI version.
```

---

## Capabilities Handshake

The `capabilities` command is the entry point for GUI integration:

```powershell
autosuite capabilities --json
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
      "jsonOutput": true
    },
    "platform": {
      "os": "windows",
      "drivers": ["winget"]
    }
  },
  "error": null
}
```

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
| `apply` | `--json` | Execute provisioning |
| `verify` | `--json` | Verify machine state |
| `report` | `--json` | Retrieve run history |

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
- [Autosuite README](../readme.md) - CLI documentation
- [Autosuite GUI README](../../autosuite-gui/README.md) - GUI documentation
