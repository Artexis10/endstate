# Endstate CLI JSON Contract v1.0

This document defines the stable JSON contract between Endstate CLI and its consumers (e.g., Endstate GUI).

## Overview

All Endstate CLI commands that support `--json` output follow a standardized envelope format. This enables reliable machine consumption, versioning, and compatibility checking.

## Schema Version

- **Current Schema Version:** `1.0`
- **Minimum Supported:** `1.0`

Schema versioning follows these rules:
- **Additive changes** (new optional fields) are backward-compatible and do NOT bump the major version
- **Breaking changes** (removed fields, type changes, semantic changes) require:
  - Schema major version bump (e.g., `1.0` → `2.0`)
  - Endstate CLI major version bump

## Standard Envelope

Every JSON output includes this envelope:

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

### Envelope Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `schemaVersion` | string | Yes | JSON schema version (e.g., "1.0") |
| `cliVersion` | string | Yes | Endstate CLI version (semver) |
| `command` | string | Yes | Command that produced this output |
| `runId` | string | Yes | Unique run identifier (format: `yyyyMMdd-HHmmss`) |
| `timestampUtc` | string | Yes | ISO 8601 UTC timestamp |
| `success` | boolean | Yes | Whether the command succeeded |
| `data` | object | Yes | Command-specific result data |
| `error` | object | No | Error object if `success` is false |

## Error Object

When `success` is `false`, the `error` field contains:

```json
{
  "error": {
    "code": "MANIFEST_NOT_FOUND",
    "message": "The specified manifest file does not exist.",
    "detail": {
      "path": "C:\\manifests\\missing.jsonc"
    },
    "remediation": "Check the file path and ensure the manifest exists.",
    "docsKey": "errors/manifest-not-found"
  }
}
```

### Error Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `code` | string | Yes | Stable, machine-readable error code (SCREAMING_SNAKE_CASE) |
| `message` | string | Yes | Human-readable error description |
| `detail` | object | No | Structured context (varies by error type) |
| `remediation` | string | No | Suggested action to resolve the error |
| `docsKey` | string | No | Documentation reference key |

### Standard Error Codes

| Code | Description |
|------|-------------|
| `MANIFEST_NOT_FOUND` | Manifest file does not exist |
| `MANIFEST_PARSE_ERROR` | Manifest file is invalid JSON/JSONC |
| `MANIFEST_VALIDATION_ERROR` | Manifest schema validation failed |
| `PLAN_NOT_FOUND` | Plan file does not exist |
| `PLAN_PARSE_ERROR` | Plan file is invalid |
| `WINGET_NOT_AVAILABLE` | winget is not installed or accessible |
| `INSTALL_FAILED` | Package installation failed |
| `RESTORE_FAILED` | Configuration restore failed |
| `VERIFY_FAILED` | Verification check failed |
| `PERMISSION_DENIED` | Insufficient permissions |
| `INTERNAL_ERROR` | Unexpected internal error |
| `SCHEMA_INCOMPATIBLE` | Schema version mismatch |

---

## Command: `capabilities`

Returns CLI capabilities for handshake/compatibility checking.

```powershell
endstate capabilities --json
```

### Response

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
      "capture": {
        "supported": true,
        "flags": ["--profile", "--out-manifest", "--include-runtimes", "--include-store-apps", "--minimize", "--discover", "--update", "--json"]
      },
      "plan": {
        "supported": true,
        "flags": ["--manifest", "--json"]
      },
      "apply": {
        "supported": true,
        "flags": ["--manifest", "--plan", "--dry-run", "--enable-restore", "--json"]
      },
      "verify": {
        "supported": true,
        "flags": ["--manifest", "--json"]
      },
      "restore": {
        "supported": true,
        "flags": ["--manifest", "--enable-restore", "--dry-run", "--json"]
      },
      "report": {
        "supported": true,
        "flags": ["--run-id", "--latest", "--last", "--json"]
      },
      "doctor": {
        "supported": true,
        "flags": ["--json"]
      },
      "capabilities": {
        "supported": true,
        "flags": ["--json"]
      }
    },
    "features": {
      "streaming": false,
      "parallelInstall": true,
      "configModules": true
    },
    "platform": {
      "os": "windows",
      "drivers": ["winget"]
    }
  },
  "error": null
}
```

---

## Command: `apply`

Executes provisioning plan.

```powershell
endstate apply --manifest ./manifest.jsonc --json
endstate apply --manifest ./manifest.jsonc --dry-run --json
```

### Response

```json
{
  "schemaVersion": "1.0",
  "cliVersion": "0.1.0",
  "command": "apply",
  "runId": "20241220-143052",
  "timestampUtc": "2024-12-20T14:30:52Z",
  "success": true,
  "data": {
    "dryRun": false,
    "manifest": {
      "path": "C:\\manifests\\my-machine.jsonc",
      "name": "my-machine",
      "hash": "abc123..."
    },
    "summary": {
      "total": 15,
      "success": 12,
      "skipped": 2,
      "failed": 1
    },
    "actions": [
      {
        "type": "app",
        "id": "vscode",
        "ref": "Microsoft.VisualStudioCode",
        "status": "success",
        "message": "Installed"
      },
      {
        "type": "app",
        "id": "git",
        "ref": "Git.Git",
        "status": "skipped",
        "message": "Already installed"
      }
    ],
    "runId": "20241220-143052",
    "stateFile": "C:\\endstate\\state\\20241220-143052.json",
    "logFile": "C:\\endstate\\logs\\apply-20241220-143052.log",
    "eventsFile": "C:\\endstate\\logs\\apply-20241220-143052.events.jsonl"
  },
  "error": null
}
```

**Note:** `eventsFile` is only included when `--events jsonl` is enabled. The engine persists events to `logs/<runId>.events.jsonl` in addition to streaming to stderr.

```json
```

---

## Command: `verify`

Verifies current machine state against manifest.

```powershell
endstate verify --manifest ./manifest.jsonc --json
```

### Response

```json
{
  "schemaVersion": "1.0",
  "cliVersion": "0.1.0",
  "command": "verify",
  "runId": "20241220-143052",
  "timestampUtc": "2024-12-20T14:30:52Z",
  "success": true,
  "data": {
    "manifest": {
      "path": "C:\\manifests\\my-machine.jsonc",
      "name": "my-machine"
    },
    "summary": {
      "total": 10,
      "pass": 9,
      "fail": 1
    },
    "results": [
      {
        "type": "app",
        "id": "vscode",
        "ref": "Microsoft.VisualStudioCode",
        "status": "pass"
      },
      {
        "type": "verify",
        "verifyType": "file-exists",
        "path": "~/.gitconfig",
        "status": "fail",
        "message": "File not found"
      }
    ],
    "runId": "20241220-143052",
    "stateFile": "C:\\endstate\\state\\verify-20241220-143052.json",
    "logFile": "C:\\endstate\\logs\\verify-20241220-143052.log",
    "eventsFile": "C:\\endstate\\logs\\verify-20241220-143052.events.jsonl"
  },
  "error": null
}
```

**Note:** `eventsFile` is only included when `--events jsonl` is enabled.

```json
```

---

## Command: `report`

Retrieves run history.

```powershell
endstate report --latest --json
endstate report --last 5 --json
endstate report --run-id 20241220-143052 --json
```

### Response

```json
{
  "schemaVersion": "1.0",
  "cliVersion": "0.1.0",
  "command": "report",
  "runId": "20241220-150000",
  "timestampUtc": "2024-12-20T15:00:00Z",
  "success": true,
  "data": {
    "reports": [
      {
        "runId": "20241220-143052",
        "timestamp": "2024-12-20T14:30:52Z",
        "command": "apply",
        "dryRun": false,
        "manifest": {
          "name": "my-machine.jsonc",
          "path": "C:\\manifests\\my-machine.jsonc",
          "hash": "abc123..."
        },
        "summary": {
          "success": 12,
          "skipped": 2,
          "failed": 1
        },
        "stateFile": "C:\\endstate\\state\\20241220-143052.json"
      }
    ]
  },
  "error": null
}
```

---

## Versioning Rules

### Semantic Versioning

Endstate CLI follows [Semantic Versioning](https://semver.org/):

- **MAJOR:** Breaking changes (including JSON schema breaking changes)
- **MINOR:** New features, backward-compatible
- **PATCH:** Bug fixes, backward-compatible

### Schema Versioning

The JSON schema has its own version (`schemaVersion`) independent of CLI version:

- Schema version uses `MAJOR.MINOR` format
- MAJOR bump = breaking change to JSON structure
- MINOR bump = additive, backward-compatible changes

### Compatibility Matrix

| CLI Version | Schema Version | Notes |
|-------------|----------------|-------|
| 0.x.x | 1.0 | Initial schema |
| 1.0.0 | 1.0 | First stable release |
| 2.0.0 | 2.0 | Breaking schema change (hypothetical) |

---

## GUI Execution Model

### Development Mode

During development, Endstate GUI resolves the CLI from the system PATH:

1. GUI calls `endstate capabilities --json`
2. GUI validates `schemaVersion` is compatible
3. If incompatible, GUI shows clear error and refuses to execute
4. If compatible, GUI proceeds with CLI invocation

### Production Mode

Production builds of Endstate GUI bundle a pinned Endstate binary:

1. GUI ships with a specific Endstate CLI version
2. GUI validates bundled CLI on startup via `capabilities`
3. Version mismatch indicates corrupted installation

### Compatibility Check

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

### Incompatibility Error

When schema versions are incompatible, GUI must display:

```
Endstate CLI Incompatible

The installed Endstate CLI (v0.1.0, schema 1.0) is not compatible 
with this version of Endstate GUI (requires schema 2.0).

Please update Endstate CLI or use a compatible GUI version.
```

---

## Design Principles

1. **Thin GUI:** Endstate GUI contains no business logic, no provisioning logic, and makes no assumptions about internal CLI implementation.

2. **CLI as Source of Truth:** All operations are executed by CLI invocation. GUI is purely a presentation layer.

3. **Explicit Versioning:** Both CLI and schema versions are explicit and machine-readable.

4. **Graceful Degradation:** Unknown fields in JSON responses should be ignored by consumers.

5. **Stable Error Codes:** Error codes are stable identifiers that can be used for programmatic handling.
