# Autosuite Project Ruleset

This document is the **authoritative source of truth** for the Autosuite CLI ↔ GUI contract.

---

## CLI ↔ GUI Contract

### Core Principles

1. **Thin GUI:** Autosuite GUI contains no business logic, no provisioning logic, and makes no assumptions about internal CLI implementation.

2. **CLI as Source of Truth:** All operations are executed by CLI invocation. GUI is purely a presentation layer.

3. **Explicit Versioning:** Both CLI version and JSON schema version are explicit and machine-readable.

4. **Graceful Degradation:** Unknown fields in JSON responses are ignored by the GUI.

---

## Capabilities Handshake Requirement

Before executing any CLI command, the GUI **must** perform a capabilities handshake:

`
autosuite capabilities --json
`

### Handshake Flow

1. GUI calls `autosuite capabilities --json`
2. GUI parses the JSON envelope
3. GUI validates `schemaVersion` is within supported range
4. If incompatible: GUI shows clear error and refuses execution
5. If compatible: GUI proceeds with CLI invocation

### Required Validation

- `schemaVersion` must be checked against GUI's supported range
- GUI must refuse execution if schema version is incompatible
- GUI must cache capabilities for the session

---

## Schema Compatibility Enforcement

### Versioning Rules

- **CLI Version:** Follows Semantic Versioning (MAJOR.MINOR.PATCH)
- **Schema Version:** Uses MAJOR.MINOR format

### Compatibility Rules

| Change Type | Schema Version | CLI Version |
|-------------|----------------|-------------|
| Additive (new optional fields) | No change | MINOR bump |
| Breaking (removed/changed fields) | MAJOR bump | MAJOR bump |

### GUI Behavior

- GUI must validate schema version before any command execution
- GUI must display clear error message for incompatible versions
- GUI must not attempt to parse incompatible responses

---

## Execution Model

### Development Mode

During development, Autosuite GUI resolves the CLI from the system PATH:

- **CLI Resolution:** `autosuite` command resolved from PATH
- **Execution:** Node.js `child_process.spawn`
- **Validation:** Capabilities handshake on startup

### Production Mode (Model B)

Production builds of Autosuite GUI bundle a pinned Autosuite CLI binary:

- **CLI Resolution:** Bundled binary at known path
- **Execution:** Tauri/Rust Command API
- **Validation:** Capabilities handshake on startup

### Execution Boundary

The `cli-bridge.ts` module defines the platform-agnostic contract:
- Types and interfaces for JSON responses
- Schema validation functions
- Abstract execution boundary

Platform-specific implementation is provided by the runtime layer:
- Development: Node.js child_process
- Production: Tauri/Rust backend

---

## JSON Contract v1.0

### Standard Envelope

Every `--json` output includes this envelope:

`json
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
`

### Required Fields

| Field | Type | Description |
|-------|------|-------------|
| `schemaVersion` | string | JSON schema version |
| `cliVersion` | string | CLI version (semver) |
| `command` | string | Command that produced output |
| `runId` | string | Unique run ID (yyyyMMdd-HHmmss) |
| `timestampUtc` | string | ISO 8601 UTC timestamp |
| `success` | boolean | Whether command succeeded |
| `data` | object | Command-specific result |
| `error` | object/null | Error object if failed |

### Error Object

`json
{
  "code": "MANIFEST_NOT_FOUND",
  "message": "The specified manifest file does not exist.",
  "detail": { "path": "C:\\manifests\\missing.jsonc" },
  "remediation": "Check the file path and ensure the manifest exists.",
  "docsKey": "errors/manifest-not-found"
}
`

---

## Supported Commands

| Command | JSON Flag | Description |
|---------|-----------|-------------|
| `capabilities` | `--json` | Report CLI capabilities for handshake |
| `apply` | `--json` | Execute provisioning plan |
| `verify` | `--json` | Verify machine state against manifest |
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

## References

- [CLI JSON Contract](../../docs/cli-json-contract.md) - Full schema specification
- [GUI Integration Contract](../../docs/gui-integration-contract.md) - Detailed integration guide
- [Autosuite GUI cli-bridge.ts](../../../autosuite-gui/src/cli-bridge.ts) - TypeScript contract types
