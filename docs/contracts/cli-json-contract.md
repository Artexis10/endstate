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
| `MANIFEST_WRITE_FAILED` | Manifest file could not be written or is empty |
| `PLAN_NOT_FOUND` | Plan file does not exist |
| `PLAN_PARSE_ERROR` | Plan file is invalid |
| `WINGET_NOT_AVAILABLE` | winget is not installed or accessible |
| `REALIZER_UNAVAILABLE` | The package realizer is unavailable (e.g. the Nix daemon/store is unreachable, or `nix` is not installed) |
| `ENGINE_CLI_NOT_FOUND` | Engine CLI not found (repo root not configured) |
| `CAPTURE_FAILED` | Capture operation failed |
| `CAPTURE_BLOCKED` | Capture blocked by guardrail |
| `INSTALL_FAILED` | Package installation failed |
| `RESTORE_FAILED` | Configuration restore failed |
| `INVALID_RESTORE_TARGET` | A `--restore-target` mapping is malformed, duplicates a capture mapping, or names an unknown/non-targetable capture. Raised before installation or config mutation and includes engine-authored remediation. |
| `VERIFY_FAILED` | Verification check failed |
| `PERMISSION_DENIED` | Insufficient permissions |
| `INTERNAL_ERROR` | Unexpected internal error |
| `SCHEMA_INCOMPATIBLE` | Schema version mismatch |
| `ROLLBACK_UNSUPPORTED` | None of the selected provisioning generations has a backend that can perform native rollback or best-effort package uninstall. Additive in schema 1.x. |
| `GENERATION_NOT_FOUND` | The `rollback --to <n>` target generation does not exist, or records no backend-native rollback anchor. Additive in schema 1.x. |
| `ROLLBACK_FAILED` | The backend rollback failed (non-systemic). Raw backend text is confined to `error.detail`. Additive in schema 1.x. |
| `CONVERGENCE_UNSUPPORTED` | `apply --prune` was requested on package-driver lanes that cannot safely remove installed-but-undeclared packages, or on a host with no realizer. Nothing is removed. Additive in schema 1.x. |
| `CONFIRMATION_REQUIRED` | `rebuild` was invoked for a live run (restore on, not `--dry-run`) without `--confirm`. Raised before any mutation, so the refusal has no side effects. Additive in schema 1.x. |
| `NOT_SUPPORTED` | The requested operation is not supported on the current platform (e.g. `schedule enable` on non-Windows), or the input mode is unsupported (e.g. `rebuild --from <URL>`). Additive in schema 1.x. |
| `TASK_REGISTRATION_FAILED` | `schedule enable` could not register the Windows Scheduled Task via `schtasks.exe`. Additive in schema 1.x. |

### Command Warnings

`capture` and `apply` MAY include an additive `data.warnings` array. Each warning has this stable shape:

```json
{
  "code": "optional_driver_unavailable",
  "message": "Optional driver chocolatey is unavailable; continuing with available drivers.",
  "driver": "chocolatey",
  "ref": "vscode"
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `code` | string | Yes | Stable machine-readable warning code |
| `message` | string | Yes | Human-readable explanation |
| `driver` | string | No | Stable driver name related to the warning |
| `ref` | string | No | Package reference related to the warning |

The defined warning codes are:

- `optional_driver_unavailable`: an optional driver could not participate; independent available lanes continue.
- `possible_duplicate`: capture found equal non-empty display names, ignoring case, from different drivers. Both entries remain in the capture.

`rebootRequired` is a successful item fact, not a warning.

#### Hosted Backup error codes

These codes are produced exclusively by `endstate backup *` and `endstate account *` commands. Their precise HTTP-status mapping is locked in `hosted-backup-contract.md` §7, §11.

| Code | Description |
|------|-------------|
| `AUTH_REQUIRED` | The hosted-backup session is missing or expired. Run `endstate backup login`. |
| `SUBSCRIPTION_REQUIRED` | An active Endstate subscription is required for backup writes. Read paths remain available during `grace` and `cancelled` states. |
| `NOT_FOUND` | The requested backup or version does not exist (or is owned by a different user — server returns 404, not 403, to avoid existence leaks). |
| `RATE_LIMITED` | Backend rate limit hit. Honour the `Retry-After` hint where present. |
| `BACKEND_ERROR` | Backend returned 5xx after the engine's bounded retries; transient infrastructure issue on Endstate's side. |
| `BACKEND_UNREACHABLE` | Engine could not reach the configured `ENDSTATE_OIDC_ISSUER_URL` (DNS / TCP / TLS / timeout). |
| `BACKEND_INCOMPATIBLE` | The configured backend does not advertise the required `endstate_extensions` discovery block (or advertises KDF parameters below the v1 floor). |
| `STORAGE_QUOTA_EXCEEDED` | The user's hosted-backup storage quota would be exceeded by the operation. |

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
      "capabilities": {
        "supported": true,
        "flags": ["--json"]
      },
      "apply": {
        "supported": true,
        "flags": ["--manifest", "--dry-run", "--enable-restore", "--restore-filter", "--restore-target", "--only", "--bootstrap-backends", "--no-bootstrap", "--json", "--events"]
      },
      "verify": {
        "supported": true,
        "flags": ["--manifest", "--json", "--events"]
      },
      "capture": {
        "supported": true,
        "flags": ["--profile", "--out", "--name", "--driver", "--sanitize", "--discover", "--update", "--include-runtimes", "--include-store-apps", "--minimize", "--manifest", "--json", "--events", "--pin"]
      },
      "plan": {
        "supported": true,
        "flags": ["--manifest", "--json", "--events"]
      },
      "restore": {
        "supported": true,
        "flags": ["--manifest", "--restore-filter", "--restore-target", "--json", "--events", "--filter"]
      },
      "rebuild": {
        "supported": true,
        "flags": ["--from", "--confirm", "--dry-run", "--no-restore", "--restore-filter", "--restore-target", "--bootstrap-backends", "--no-bootstrap", "--json", "--events"]
      },
      "report": {
        "supported": true,
        "flags": ["--run-id", "--latest", "--last", "--json"]
      },
      "doctor": {
        "supported": true,
        "flags": ["--json"]
      },
      "profile": {
        "supported": true,
        "flags": ["--json"]
      },
      "bootstrap": {
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
      "drivers": ["winget", "chocolatey"]
    },
    "gitCommit": "a1b2c3d",
    "gitDirty": false,
    "bootstrapTimestamp": "2026-02-27T10:00:00Z"
  },
  "error": null
}
```

### Capabilities Data Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `supportedSchemaVersions` | object | Yes | Min/max supported schema versions |
| `commands` | object | Yes | Supported commands with flags |
| `features` | object | Yes | Feature flags |
| `platform` | object | Yes | Host OS and available package-manager drivers (host-dependent — see note) |
| `gitCommit` | string\|null | Yes | Short git SHA of HEAD, or `null` if git unavailable |
| `gitDirty` | boolean | Yes | `true` if working tree has uncommitted changes, `false` otherwise (defaults to `false` if git unavailable) |
| `bootstrapTimestamp` | string\|null | Yes | ISO 8601 UTC timestamp of last bootstrap, or `null` if not bootstrapped |

> **`platform` is host-dependent.** `platform.os` reflects the host operating system (`windows`, `linux`, `darwin`) and `platform.drivers` lists the supported package backends in deterministic registry order. Windows reports `{ "os": "windows", "drivers": ["winget", "chocolatey"] }`; Winget remains its default. Linux reports the Nix realizer, and macOS reports Nix plus the additive Brew driver. On a host with no implemented backend, `drivers` is an empty array (`[]`). Consumers MUST NOT infer that every advertised optional driver is currently installed.

---

## Command: `capture`

Captures current machine state into a zip bundle profile.

```powershell
endstate capture --profile "My-Desktop" --json
endstate capture --profile "Chocolatey-Only" --driver chocolatey --json
```

`--driver <name>` is repeatable. With no filter, capture enumerates every available installed-package driver. An explicitly selected unavailable driver fails capture; an unavailable optional driver during unfiltered capture produces `optional_driver_unavailable` while available drivers continue.

### Response

```json
{
  "schemaVersion": "1.0",
  "cliVersion": "0.1.0",
  "command": "capture",
  "runId": "20260216-200000",
  "timestampUtc": "2026-02-16T20:00:00Z",
  "success": true,
  "data": {
    "outputPath": "C:\\Users\\user\\Documents\\Endstate\\Profiles\\My-Desktop.zip",
    "outputFormat": "zip",
    "bundleSchemaVersion": "2.0",
    "manifestVersion": 2,
    "sanitized": false,
    "isExample": false,
    "counts": {
      "totalFound": 85,
      "included": 72,
      "skipped": 13,
      "filteredRuntimes": 8,
      "filteredStoreApps": 5,
      "sensitiveExcludedCount": 3
    },
    "appsIncluded": [
      { "id": "Microsoft.VisualStudioCode", "source": "winget" },
      { "id": "git.install", "source": "chocolatey" }
    ],
    "configModules": [
      {
        "id": "apps.git",
        "appId": "git",
        "displayName": "Git",
        "status": "captured",
        "filesCaptured": 1,
        "wingetRefs": ["Git.Git"],
        "chocolateyRefs": ["git.install"]
      }
    ],
    "configsIncluded": ["vscode", "claude-desktop"],
    "configsSkipped": ["git"],
    "configsCaptureErrors": [],
    "configModuleMap": {
      "Git.Git": "apps.git",
      "Microsoft.VisualStudioCode": "apps.vscode"
    },
    "packageModuleMap": {
      "winget:Git.Git": ["apps.git"],
      "winget:Microsoft.VisualStudioCode": ["apps.vscode"],
      "chocolatey:git.install": ["apps.git"]
    },
    "captureWarnings": [],
    "warnings": [],
    "configCapture": {
      "configSets": [
        {
          "captureId": "apps.vscode-preferences-instance-a",
          "moduleId": "apps.vscode",
          "configSetId": "preferences",
          "displayName": "Preferences",
          "sourceInstance": {
            "id": "instance-a",
            "detectorId": "installed-package",
            "rawVersion": "1.92.0",
            "normalizedVersion": "1.92.0",
            "evidence": { "backend": "winget", "ref": "Microsoft.VisualStudioCode" }
          },
          "sourceGeneration": "g1",
          "sourceGenerationFingerprint": "<sha256>",
          "captureModuleRevision": "<sha256>",
          "filesCaptured": 2,
          "status": "captured",
          "reason": null
        }
      ]
    }
  },
  "error": null
}
```

### Capture Data Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `outputPath` | string | Yes | Absolute path to output file |
| `outputFormat` | string | Yes | `"zip"` for bundle, `"jsonc"` for legacy |
| `bundleSchemaVersion` | string | No | Bundle metadata schema (`"2.0"` for generation-aware capture) |
| `manifestVersion` | integer | No | Embedded manifest version (`2` for generation-aware capture) |
| `sanitized` | boolean | Yes | Whether output was sanitized |
| `isExample` | boolean | Yes | Whether this is an example manifest |
| `counts` | object | Yes | Capture statistics |
| `appsIncluded` | array | Yes | Apps included in manifest |
| `configModules` | array | Yes | Per-module capture results; entries include legacy `wingetRefs` and additive `chocolateyRefs` arrays |
| `configsIncluded` | array | No | Config module IDs bundled in zip |
| `configsSkipped` | array | No | Config module IDs that matched but were skipped |
| `configsCaptureErrors` | array | No | Config capture error descriptions |
| `configModuleMap` | object | Yes | Maps winget package refs to config module IDs (empty `{}` when no mappings) |
| `packageModuleMap` | object | Yes | Maps namespaced `driver:ref` package identities to arrays of config module IDs (empty `{}` when no mappings) |
| `captureWarnings` | array | No | General capture warnings |
| `warnings` | array | No | Structured `CommandWarning` entries |
| `configCapture.configSets` | array | No | Per-instance/per-config-set generation provenance and capture results |

**Note:** `configsIncluded`, `configsSkipped`, and `configsCaptureErrors` are only present when `outputFormat` is `"zip"`. Legacy `configModuleMap` remains Winget-only and always present. `packageModuleMap` is its driver-aware companion, uses `(driver, ref)` identity, and retains every matching module ID in a deterministic array. Capture never suppresses a cross-driver entry based on equal refs, versions, or display-name similarity; exact case-insensitive display-name equality emits `possible_duplicate` and retains both entries.

Existing schema-v1 module fields remain backward compatible. Generation-aware fields are absent for schema-v1 payloads or explicitly identify them as unversioned; the engine never fabricates source versions or generations. A generation-aware artifact reports bundle schema `2.0` and manifest version `2` without changing this stdout envelope's additive schema `1.0` contract.

---

## Command: `apply`

Executes provisioning plan.

```powershell
endstate apply --manifest ./manifest.jsonc --json
endstate apply --manifest ./manifest.jsonc --dry-run --json
endstate apply --manifest ./manifest.jsonc --only git,vscode --json
endstate apply --manifest ./manifest.jsonc --bootstrap-backends --json
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
        "driver": "winget",
        "status": "success",
        "message": "Installed"
      },
      {
        "type": "app",
        "id": "git-choco",
        "ref": "git.install",
        "driver": "chocolatey",
        "status": "success",
        "rebootRequired": true,
        "message": "Installed; restart required"
      }
    ],
    "configModuleMap": {
      "Microsoft.VisualStudioCode": "apps.vscode"
    },
    "packageModuleMap": {
      "winget:Microsoft.VisualStudioCode": ["apps.vscode"],
      "chocolatey:git.install": ["apps.git"]
    },
    "warnings": [],
    "runId": "20241220-143052",
    "stateFile": "C:\\endstate\\state\\20241220-143052.json",
    "logFile": "C:\\endstate\\logs\\apply-20241220-143052.log",
    "eventsFile": "C:\\endstate\\logs\\apply-20241220-143052.events.jsonl"
  },
  "error": null
}
```

**Note:** `eventsFile` is only included when `--events jsonl` is enabled. The engine persists events to `logs/<runId>.events.jsonl` in addition to streaming to stderr.

### Driver Selection and Bootstrap

Package items returned by plan, apply, and verify include the resolved lowercase `driver`. On Windows, an omitted app driver resolves to `winget`; an explicit `chocolatey` app uses its `refs.windows` value only with Chocolatey. The globally known manifest driver names are `winget`, `chocolatey`, and `brew` (recognized case-insensitively). A known driver unsupported on the host is a visible skipped item. A globally unknown driver fails manifest validation. An explicit driver is authoritative: unavailability or failure never falls back to another package manager.

`apply` reuses the existing backend-bootstrap flags for required optional drivers:

| Flag | Behavior |
|------|----------|
| `--bootstrap-backends` | Explicitly authorizes bootstrap of a selected missing optional backend, followed by executable verification before package mutation. |
| `--no-bootstrap` | Never bootstrap; visibly skip unavailable lanes and continue independent lanes. |
| neither | Emit the existing combined consent event, skip unavailable lanes, and continue independent lanes. |
| `--dry-run` | Report unavailable prerequisites without installing a backend or requesting mutating consent. |

Winget is operating-system provided and is never bootstrapped. A Chocolatey reboot-success remains a successful action with `rebootRequired: true`; the field is omitted when false and is not mirrored into `warnings`.

When a selected optional driver remains unavailable, `apply.data.warnings` includes `{"code":"optional_driver_unavailable","message":"...","driver":"chocolatey"}` alongside the visible skipped or failed action. The warning never authorizes fallback and does not replace the action's own status and diagnostic.

### Generation-Aware Configuration Output (Apply, Restore, and Rebuild)

When restore-capable input contains configuration payloads, apply, standalone restore, and rebuild add `configResolutions[]`, `configResolutionSummary`, and `restoreItems[]` to their command data. This is additive in stdout schema `1.0`. Application-install `items[]`/`actions[]` remain unchanged. If the input contains no config payloads, these config fields are omitted. If config payloads are present, no config field is omitted: all arrays are present and use `[]`, never `null`, when empty; `reason` and `remediation` use `null` when absent.

```json
{
  "configResolutions": [
    {
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
      "resolvedTargets": [],
      "status": "restored",
      "label": "Will be upgraded",
      "message": "Settings will be upgraded from g1 to g2 before restore.",
      "remediation": null
    }
  ],
  "configResolutionSummary": {
    "total": 1,
    "direct": 0,
    "migrate": 1,
    "incompatible": 0,
    "unknown": 0,
    "legacyUnverified": 0,
    "selected": 1,
    "skipped": 0,
    "failed": 0
  },
  "restoreItems": []
}
```

`sourceInstance` and every `targetCandidates[]` member carry portable, non-secret identity and version evidence. Host-local target roots and locators are internal engine data and never appear in command results. `targetCandidates[]`, `migrationPath[]`, `resolvedTargets[]`, and `restoreItems[]` are non-null arrays. The engine authors each row's distilled `label`, `message`, nullable `remediation`, and technical details. GUIs render those values verbatim and do not reconstruct them from versions, candidates, modules, or bundle data.

`resolution` describes source/target compatibility and is exactly `direct`, `migrate`, `incompatible`, `unknown`, or `legacy_unverified`. Application versions are evidence for selecting config generations; they are not a pairwise compatibility result. Stable reasons include `downgrade_unsupported`, `migration_path_missing`, `ambiguous_target_instance`, `ambiguous_generation`, `target_not_detected`, `mapped_target_not_detected`, `mapped_target_incompatible`, `target_collision`, `app_running`, `payload_integrity_failed`, `unsupported_module_schema`, `catalog_module_missing`, `config_set_missing`, `source_generation_unknown`, `source_generation_definition_changed`, `recovery_required`, `restore_filtered`, `restore_not_enabled`, `target_detection_failed`, `staging_validation_failed`, `backup_failed`, `journal_intent_failed`, `commit_failed`, `target_validation_failed`, `journal_completion_failed`, and `already_up_to_date`.

`status` is independent from `resolution` and is terminal in the envelope. It is exactly:

| Status | Meaning |
|--------|---------|
| `planned` | Dry-run only; the selected set passed compatibility, integrity, preflight, staging, and validation |
| `restored` | Live transaction reached and validated desired state and durably recorded completion |
| `skipped` | No target mutation was attempted because non-execution was intentional or safely required, including filtering/consent, unknown/incompatible resolution, absent/incompatible mapped target, `app_running`, or already-up-to-date state |
| `failed` | The selected set failed before any target mutation, so rollback was unnecessary |
| `rolled_back` | Mutation began, failed, and rollback durably restored and verified complete pre-run state |
| `rollback_failed` | Mutation began and complete restoration could not be proven; no later config-set mutation starts in the run |

For `failed`, `rolled_back`, and `rollback_failed`, `reason` retains the primary execution failure. Rollback outcome is carried by `status`; it never overwrites that cause. `configResolutionSummary.selected` counts `planned`, `restored`, `failed`, `rolled_back`, and `rollback_failed`; `skipped` counts `skipped`; `failed` counts `failed`, `rolled_back`, and `rollback_failed`.

Legacy manifest/bundle v1 payloads are represented as `legacy_unverified` with unknown generation fields omitted. Every explicit schema-v1 module lane uses `configSetId: "legacy"` and the deterministic, domain-separated capture ID returned by `bundle.LegacyCaptureID(moduleId)`. Anonymous inline restore actions without a module-lane association remain ordinary `restoreItems[]`; the engine does not fabricate config-resolution rows, instances, versions, or generations for them. Legacy lanes retain explicit restore consent, conflict handling, backup, journal, and revert. Invalid manifest-v2 generation provenance never becomes `legacy_unverified` and never falls back to a flat restore path.

Concrete `restoreItems[]` retain all existing fields. Items produced from a generation-aware config set add optional `captureId`, `configSetId`, `targetInstanceId`, `sourceGeneration`, and `targetGeneration` fields so actions remain traceable to the resolution plan.

If durable journal intent cannot be written, the affected config set is `failed` before mutation with `journal_intent_failed`. A completion-record failure uses `journal_completion_failed`; journal errors are never ignored or represented as successful restore. Before a new restore-capable mutation, an unrecoverable pending intent reports `recovery_required` and prevents all new config mutation.

### Explicit Target Mapping

Apply, standalone restore, and rebuild accept repeatable `--restore-target <captureId>=<targetInstanceId>`. Module-level `--restore-filter` applies first. Malformed mappings, duplicate mappings for one capture ID, and mappings to unknown or non-targetable capture IDs return `INVALID_RESTORE_TARGET` with engine-authored message and remediation before installation or configuration mutation. After final post-install detection, a well-formed mapping to an absent/incompatible target skips only that config set with `mapped_target_not_detected` or `mapped_target_incompatible`; successful installation is not rolled back.

The engine automatically maps only one viable target or one unique exact-version target. Multiple viable side-by-side targets report `unknown` / `ambiguous_target_instance`; no newest-version rule exists.

### App-Subset Selection (`--only`)

`apply --only <id[,id,...]>` limits the run to manifest apps whose `id` is in the comma-separated list. Filtering happens at the manifest level before planning, so every downstream stage (plan generation, driver execution, config-module expansion, restore scoping, verification, event emission, and summary counts) behaves as if the manifest contained only the selected apps. Omitting `--only` leaves behaviour unchanged.

| Flag | Behavior |
|------|----------|
| `--only <ids>` | Comma-separated list of manifest app `id` values to include. Ids not found in the manifest fail the run with `MANIFEST_VALIDATION_ERROR` naming the unknown ids. An empty or blank value is likewise rejected. |
| `--dry-run` | Can be combined with `--only` to preview the subset plan without executing. This is the GUI's per-app selection preview path. |

`--only` cannot be combined with `--prune` — prune converges to the exact manifest set, and pruning against a deliberate subset would classify every unselected app as drift. The combination is rejected with `MANIFEST_VALIDATION_ERROR`.

### Convergence (`--prune`)

`apply --prune` converges the engine-managed set to *exactly* the manifest: after the install phase it removes installed-but-undeclared packages ("drift") in one atomic generation switch. Convergence is realizer-only (Nix on Linux/macOS); Winget and Chocolatey driver lanes refuse with `CONVERGENCE_UNSUPPORTED`, changing nothing. Package-stage only: prune never touches configuration restore, `state/backups/`, or the revert journal.

| Flag | Behavior |
|------|----------|
| `--prune` | After install, remove installed-but-undeclared packages. Destructive, so it requires `--confirm` (or `--dry-run`). |
| `--confirm` | Required to execute the prune. Without it (and without `--dry-run`) the command refuses with `INTERNAL_ERROR` and removes nothing — the install-phase results stand. |
| `--dry-run` | Preview only: the would-be-pruned set is reported in `data.pruned` without removing anything and without requiring `--confirm` (install and prune are both preview-only). |

On a converging run the result carries a `pruned` field — the element names removed this run (omitted when empty):

```json
"data": {
  "dryRun": false,
  "summary": { "total": 1, "success": 0, "skipped": 1, "failed": 0 },
  "actions": [ /* ... */ ],
  "pruned": ["ripgrep"]
}
```

The recorded Provisioning Generation reflects the converged set: `addedRefs` for packages installed this run and `removedRefs` for packages pruned this run, with `native` set to the final advancing generation. A no-op convergence (nothing added or removed) records no generation.

```json
```

### Version capture and pinning (Windows package drivers)

On Winget and Chocolatey, each Provisioning Generation item records the installed `version` reported by the selected driver (best-effort — empty when the manager exposes none). The Nix realizer pins exact versions through its ref, so Nix generations leave `version` empty.

`capture --pin` writes each captured package driver's installed version into the emitted manifest's `version` field (best-effort — omitted when the manager exposes none), producing a manifest that reproduces the exact installed set on `apply`.

A manifest app MAY declare a `version` to **pin** the install:

```jsonc
{ "id": "vscode", "version": "1.89.0", "refs": { "windows": "Microsoft.VisualStudioCode" } }
```

- When `version` is declared, Winget or Chocolatey installs that exact version through the selected driver. The recorded generation item carries the pinned `version`.
- By default, pinning is **pin-on-install only**: a package already installed at a different version is left untouched (reported `present`). Use `--repin` (below) to converge a drifted version.
- If the declared version is unavailable, that package fails as `INSTALL_FAILED` (the requested version appears in the message); no other version is installed in its place. Other packages in the run are unaffected.
- Pinning applies only to backends that support versioned install (Winget and Chocolatey). The Nix realizer ignores `version` because its ref is already the pin; this is not an error.

### Version convergence (`--repin`)

`apply --repin` reinstalls a declared `version` when the installed version has drifted from it through the selected version-capable driver — the enforcement counterpart of pinning. Winget and Chocolatey support it; the Nix realizer ignores it.

| Flag | Behavior |
|------|----------|
| `--repin` | Reinstall the declared version over an already-installed drifted one. Destructive (a reinstall / possible downgrade), so it requires `--confirm` (or `--dry-run`). |
| `--confirm` | Required to execute the re-pin. Without it (and without `--dry-run`) the command refuses with `INTERNAL_ERROR` and reinstalls nothing — the install-phase results stand. |
| `--dry-run` | Preview only: drifted apps are reported (as actions with reason `version_drift`) without reinstalling and without requiring `--confirm`. |

A converged app is recorded `installed` at the declared version in the Provisioning Generation. Drift is evaluated only for apps that declare a `version`; default `apply` (no `--repin`) leaves a drifted version untouched.

### Configuration stage (home-manager, realizer-only)

When the manifest declares a `homeManager` block and `apply` runs with `--enable-restore` on the Nix realizer, `apply` activates a [home-manager](https://github.com/nix-community/home-manager) configuration as a **configuration stage** after the package phases. This is the realizer's config story (the winget driver path uses the restore-module layer instead).

```jsonc
{
  "homeManager": { "flake": "/home/me/dotfiles#hugo" }  // a home-manager flakeref
}
```

- `homeManager.flake` is a home-manager flakeref (e.g. `/home/me/dotfiles#hugo` or `github:me/dotfiles#hugo`) — a power-user escape hatch. The engine activates whatever flakeref it is given; engine-generated inputs may produce this flakeref in a later release without changing the stage.
- `homeManager.config` is a path (resolved relative to the manifest) to a `home.nix` the engine **wraps in a generated flake** before activating it through this same stage — so the user supplies only their configuration, never the flake, inputs, pinning, identity, or activation wiring.
- `homeManager.settings` is a **declarative, Endstate-native config block** the engine compiles into a `home.nix` (then wrapped exactly like `homeManager.config`) — so the user declares configuration in Endstate's own format and never writes Nix at all. See *Declarative catalog* below.
- `settings`, `config`, and `flake` are **mutually exclusive** (exactly one home-manager input); a manifest declaring more than one fails to load with a clear validation error.
- **Engine-owned and gated.** The engine itself runs the activation (`nix run <pinned home-manager> -- switch --flake <ref> -b endstate-backup`); the user does not install or run home-manager. The home-manager version is pinned by the engine (overridable via `ENDSTATE_HOME_MANAGER_PIN`, mirroring `ENDSTATE_NIXPKGS_PIN`). The stage runs **only** when both `--enable-restore` is set and `homeManager.flake` is non-empty; otherwise apply is byte-identical to a package-only apply.
- **Backup-on-clobber.** `-b endstate-backup` makes home-manager move any pre-existing file it would replace to `<file>.endstate-backup` instead of failing (honors *backup-before-overwrite*).
- **Recorded.** A successful activation is recorded in the Provisioning Generation under `homeManager` (the activated flakeref and the resulting home-manager generation number). A config-only apply (no package changed) still records a generation.
- **Realizer-only.** The winget/driver path never activates home-manager (a declared `homeManager` block is ignored there).
- **Errors (the moat).** A systemic failure (daemon unavailable → `REALIZER_UNAVAILABLE`, permission → `PERMISSION_DENIED`) surfaces as a top-level envelope error; any other activation failure surfaces as `INSTALL_FAILED`. Raw home-manager / Nix output appears only in `error.detail`, never in `error.message`.

#### Generated flake (the `homeManager.config` wrapper)

When the manifest uses `homeManager.config`, the engine generates the surrounding flake itself and feeds the resulting flakeref to the **same** activation (no new activation path, classification, backup, or recording):

```jsonc
{
  "homeManager": { "config": "./home.nix" }  // a path to a home-manager config file
}
```

- **Engine-generated and pinned.** The engine renders a `flake.nix` that pins `nixpkgs` (`ENDSTATE_NIXPKGS_PIN`) and `home-manager` (`ENDSTATE_HOME_MANAGER_PIN`, with `nixpkgs` following), pins `pkgs` to the host system, and injects the machine identity — `home.username`, `home.homeDirectory`, and `home.stateVersion` (the last overridable via `ENDSTATE_HM_STATE_VERSION`) — so the user's `home.nix` never hardcodes machine-specific values.
- **Inspectable + ejectable.** The generated flake is written in plain, commented form to a stable engine-state location (`state/home-manager/<name>/`, where `<name>` is the current user). The user's `home.nix` is **copied in** beside the generated `flake.nix` and referenced as `./home.nix` (a flake evaluates in pure mode, which forbids an absolute path outside the flake tree), so the directory is a self-contained flake a power user can take over and run by hand (`nix run home-manager -- switch --flake <that dir>`). It persists after the run for inspection; the user's `home.nix` is the source of truth and the flake is regenerated each apply.
- **Recorded under the generated flakeref.** A successful activation records the generated `<dir>#<name>` flakeref (and the resulting home-manager generation) in the Provisioning Generation, exactly as a direct `homeManager.flake` is recorded.
- **`--dry-run` reveals without activating.** On `--dry-run` the engine still generates the inspectable flake and reports it in the apply result (`homeManager.generated: true`, `homeManager.activated: false`), but activates nothing.

The apply result carries a `homeManager` object when the config stage runs: `{ "flake": "<flakeref>", "generated": <bool>, "activated": <bool> }` — `generated` is `true` for an engine-generated input (`homeManager.settings` or `homeManager.config`) and `false` for a direct `homeManager.flake`; `activated` is `false` on `--dry-run`. (Optional, omitted when no config stage runs; additive in schema 1.x.)

#### Declarative catalog (the `homeManager.settings` input)

When the manifest uses `homeManager.settings`, the engine compiles the declaration into a `home.nix` and then wraps + activates it exactly like `homeManager.config` above (same generated flake, pinning, identity, recording, inspectability, and `--dry-run` reveal). The user declares configuration in Endstate's own format and never writes Nix.

```jsonc
{
  "homeManager": {
    "settings": {
      "git":      { "userName": "Hugo", "userEmail": "h@x.com", "defaultBranch": "main" },  // curated
      "shell":    { "aliases": { "ll": "ls -la" }, "sessionVariables": { "EDITOR": "nvim" } },
      "direnv":   { "enable": true },
      "starship": { "enable": true },
      "programs": { "bat": { "enable": true } },          // raw home-manager passthrough
      "files":    { "~/.config/foo/bar.conf": "./payload/bar.conf" }  // any file, text or binary
    }
  }
}
```

- **Hybrid schema.** A curated set of Endstate-native concepts (v1: `git`, `shell`, `direnv`, `starship`) is mapped by the engine to the correct home-manager options; an embedded `programs` object is forwarded to home-manager **verbatim**. The curated layer insulates the declaration from home-manager option renames (e.g. `git.userName` is emitted via the stable `programs.git.extraConfig`). An **unknown key inside a curated concept** (a typo) fails to load with a clear error; the raw `programs` block stays permissive, and any mistake there surfaces as a classified activation error with raw Nix in `error.detail`.
- **A raw `programs` entry may not collide with a curated concept** (e.g. raw `programs.git` alongside curated `git`) — that fails to generate with a clear error rather than producing a double definition.
- **`files` places arbitrary files (text or binary).** Each `target → source` (source resolved relative to the manifest) is **staged into the generated flake directory** (binary-safe; kept inside the flake tree so pure evaluation can read it) and placed via home-manager's `home.file`. This matches the breadth of the Windows module catalog. **Place/restore only**; capturing files back into `settings` is a separate, deferred capability.

> **Rollback of an activated home-manager configuration is not yet supported** (a documented follow-on). home-manager keeps its own numbered generations; re-activating a prior one ships separately.

---

## Command: `rebuild`

Rebuilds a machine from a capture bundle (`.zip`) or a bare manifest (`.jsonc`) in one command: it (optionally) extracts the bundle, installs the declared apps, restores configuration, then verifies the result. Restore is ON by default. Local file input only — URL input is rejected.

```powershell
endstate rebuild --from ./MyProfile.zip --dry-run --json
endstate rebuild --from ./MyProfile.zip --confirm --json
endstate rebuild --from ./machine.jsonc --no-restore --json
```

### Flags

| Flag | Behavior |
|------|----------|
| `--from <path>` | Required. A local `.zip` capture bundle or a `.jsonc` manifest. A value containing a URL scheme (`://`) is rejected with `NOT_SUPPORTED`; a missing path returns `MANIFEST_NOT_FOUND`; an empty value returns `MANIFEST_VALIDATION_ERROR`; a `.zip` without `manifest.jsonc` returns `MANIFEST_PARSE_ERROR`. |
| `--confirm` | Required for a live run (restore on, not `--dry-run`). Without it a live rebuild refuses with `CONFIRMATION_REQUIRED` **before any mutation** (before extraction, planning, install, or restore). |
| `--dry-run` | Preview only: previews the plan without installing, restoring, or verifying. Needs no `--confirm`. The result carries no `verify` data. |
| `--no-restore` | Install and verify without restoring configuration. Non-destructive, so it needs no `--confirm`. The result reports `restore: "disabled"`. |
| `--bootstrap-backends` | Propagated unchanged to the underlying apply stage; authorizes bootstrap and verification of selected missing optional backends. |
| `--no-bootstrap` | Propagated unchanged to the underlying apply stage; unavailable backend lanes are visibly skipped. |
| `--events jsonl` | Stream events to stderr. `rebuild` composes the apply stream (including config-resolution/config-migration events when applicable) and verify stream unchanged; it defines no rebuild-only event type. |

### Response

```json
{
  "schemaVersion": "1.0",
  "cliVersion": "0.1.0",
  "command": "rebuild",
  "runId": "20241220-143052",
  "timestampUtc": "2024-12-20T14:30:52Z",
  "success": true,
  "data": {
    "from": "C:\\Users\\me\\MyProfile.zip",
    "bundle": {
      "extracted": true,
      "schemaVersion": "1.0",
      "capturedAt": "2024-12-19T10:00:00Z",
      "machineName": "OLD-PC",
      "endstateVersion": "0.1.0",
      "configModulesIncluded": []
    },
    "dryRun": false,
    "restore": "enabled",
    "apply": { "dryRun": false, "summary": { "total": 12, "success": 10, "skipped": 2, "failed": 0 }, "actions": [] },
    "verify": { "summary": { "total": 12, "pass": 12, "fail": 0 }, "results": [] }
  },
  "error": null
}
```

- `bundle` is present only for a `.zip` input (with `extracted: true`); it is omitted for a bare-manifest rebuild. The metadata fields under `bundle` are best-effort (read from the bundle's `metadata.json`) and omitted when unavailable.
- For bundle input containing config payloads, `configResolutions[]`, `configResolutionSummary`, and `restoreItems[]` at the top level of rebuild `data` are canonical. The nested `apply` result may mirror them. Config-free input omits all three fields; payload-bearing input keeps the arrays present as `[]` when empty.
- `apply` carries the underlying `apply` command result; `verify` carries the underlying `verify` command result and is omitted on `--dry-run`.
- `restore` reflects the configured posture (`"enabled"` unless `--no-restore`), not whether restore executed — a `--dry-run` reports `"enabled"` while executing nothing.

**Note:** Verify failures are **data**, not a command error. A rebuild whose post-install verification reports drift (a missing app, a failed assertion, or version drift) still returns `success: true` and **exit 0**; the failures live in `data.verify.summary.fail`. Only infrastructure or input errors (e.g. `MANIFEST_PARSE_ERROR`, `CONFIRMATION_REQUIRED`) flip `success` to `false`.

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
        "driver": "winget",
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

Every app `VerifyItem` includes its resolved `driver`; non-package verification items omit it. `packageModuleMap` may accompany `configModuleMap` using the same driver-aware and legacy shapes documented under `capture`.

```json
```

### Version drift (Windows package drivers)

When a manifest app declares a `version` and the installed version reported by Winget or Chocolatey differs, `verify` reports that item as a **failure** with reason **`version_drift`**, distinct from a missing package. The result item carries the installed `version` and the declared `expected` version:

```json
{
  "type": "app",
  "id": "vscode",
  "ref": "Microsoft.VisualStudioCode",
  "driver": "winget",
  "status": "fail",
  "reason": "version_drift",
  "version": "1.92.0",
  "expected": "1.89.0",
  "message": "installed 1.92.0, want 1.89.0"
}
```

Drift is evaluated only for apps that declare a `version`; comparison is exact (whitespace-trimmed — older or newer both count as drift). When the backend exposes no installed version, no drift is flagged. The Nix realizer pins versions through its ref and does not report per-app version drift. `apply --repin --confirm` converges a drifted version (see the `apply` command).

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

## Command: `generations`

Lists recorded Provisioning Generations, newest first. Read-only. Additive in schema 1.x.

### Response

```json
{
  "schemaVersion": "1.0",
  "cliVersion": "0.1.0",
  "command": "generations",
  "runId": "20241220-143052",
  "timestampUtc": "2024-12-20T14:30:52Z",
  "success": true,
  "data": {
    "generations": [
      {
        "schemaVersion": "1.0",
        "number": 2,
        "runId": "apply-20241220-143052",
        "timestamp": "2024-12-20T14:30:52Z",
        "backend": "nix",
        "items": [
          { "id": "ripgrep", "ref": "nixpkgs#ripgrep", "status": "installed" }
        ],
        "addedRefs": ["nixpkgs#ripgrep"],
        "native": "2",
        "partial": false
      }
    ]
  }
}
```

`backend` is the stable backend name (`"nix"`, `"winget"`, `"chocolatey"`, or `"brew"`). `native` is the backend-native generation number (the Nix generation) or empty for non-atomic drivers. `partial` is true when a non-atomic driver committed only a subset of the requested set. Mixed-driver applies write a separate backend-scoped generation per driver and give those generations the same apply `runId`; refs never cross backend records. `addedRefs` lists only refs installed in that run (status `installed`); already-present refs appear in `items` but not `addedRefs`. A generation is recorded when at least one package was installed in the run **or** a home-manager configuration was activated by the config stage. `rollback` (optional, omitted when false; additive in schema 1.x) is `true` when the generation was produced by a `rollback` rather than an `apply`; such generations snapshot the now-active set and have an empty `addedRefs`. `homeManager` (optional, omitted when absent; additive in schema 1.x) records a home-manager configuration activated by `apply --enable-restore`: `{ "flake": "<flakeref>", "generation": <hm generation number> }`.

## Command: `rollback`

Reverts the installed package set to a prior Provisioning Generation. Two strategies, both keyed off the recorded generations and identified by **engine generation number** (as listed by `generations`): callers never reference a backend-native version directly. Additive in schema 1.x.

- **Native** (the Nix realizer on Linux/macOS): an atomic rollback to the backend-native anchor (`native`) recorded in the target generation.
- **Best-effort** (Winget, Chocolatey, and Brew drivers): there is no native rollback, so the engine uninstalls later `addedRefs` through the backend recorded on each generation. Per-package and non-atomic — it tolerates per-package failure (reporting `partial`), treats an already-absent package as removed, and **does not track package-manager-pulled transitive dependencies/co-installs** (which may remain — surfaced as a `warning`).

A backend that can neither roll back natively nor uninstall refuses with `ROLLBACK_UNSUPPORTED`. Package-stage only: rollback never touches configuration restore, `state/backups/`, or the revert journal (that is `revert`'s concern).

### Request

| Flag | Meaning |
|------|---------|
| `--to <n>` | Engine Provisioning Generation number to roll back to. Every later generation participates, grouped by its recorded backend. |
| `--confirm` | Required to execute (rollback changes the installed set). Without it the command refuses. |
| `--dry-run` | Resolve and report the target without changing state; does not require `--confirm`. |

### Response

```json
{
  "schemaVersion": "1.0",
  "cliVersion": "0.1.0",
  "command": "rollback",
  "runId": "20241220-143052",
  "timestampUtc": "2024-12-20T14:30:52Z",
  "success": true,
  "data": {
    "dryRun": false,
    "backend": "nix",
    "targetGeneration": 2,
    "fromNative": "5",
    "toNative": "4",
    "newGeneration": 4
  }
}
```

`targetGeneration` is the engine generation resolved from `--to` (omitted/0 when rolling back to the previous version). `fromNative`/`toNative` are the backend-native versions before and after (on `--dry-run`, `toNative` is the resolved target, or `"previous"`). `newGeneration` is the number of the new rollback-marked Provisioning Generation appended on success (omitted on `--dry-run`). On failure, raw backend text appears only in `error.detail`.

### Response (best-effort / winget)

```json
{
  "schemaVersion": "1.0",
  "cliVersion": "0.1.0",
  "command": "rollback",
  "runId": "20241220-143052",
  "timestampUtc": "2024-12-20T14:30:52Z",
  "success": true,
  "data": {
    "dryRun": false,
    "backend": "winget",
    "targetGeneration": 1,
    "removedRefs": ["Some.AppC", "Some.AppD"],
    "failedRefs": ["Some.AppB"],
    "partial": true,
    "newGeneration": 4,
    "warning": "Package-manager-pulled transitive dependencies and co-installs are not tracked and may remain installed."
  }
}
```

For the best-effort path, `removedRefs` lists the refs uninstalled (on `--dry-run`, the refs that *would* be uninstalled — an already-absent package counts as removed). `failedRefs` lists refs whose uninstall failed (for example, another installed package still depends on one); `partial` is `true` when any failed. `newGeneration` is the appended rollback-marked generation (it records `removedRefs` and carries an empty `addedRefs`; omitted on `--dry-run` and when nothing was removed). `warning` is the untracked-dependency caveat. When **every** targeted uninstall fails the command returns `ROLLBACK_FAILED`; a missing winget binary returns `WINGET_NOT_AVAILABLE`; an unknown `--to` returns `GENERATION_NOT_FOUND`. No new error codes are introduced.

With no `--to`, rollback finds the newest non-rollback generation and selects every generation sharing that generation's `runId`; this makes all backend generations written by one mixed-driver apply a single rollback unit. Backend groups run newest-generation first, with deterministic ref order inside each group. Each ref is sent only to the uninstaller named by its recorded `backend`; an unknown or unavailable recorded backend fails only that backend's refs, contributes to `partial`, and is never substituted with the platform default. Chocolatey rollback never requests recursive dependency removal. A result spanning more than one backend reports `backend: "mixed"`; `newGeneration` is the newest backend-scoped rollback generation written by that operation.

---

## Command: `schedule`

Manages the Endstate scheduled drift-check feature via Windows Task Scheduler.
Four subcommands: `enable`, `disable`, `status`, `run`. Additive in schema 1.x.

```powershell
endstate schedule enable --manifest ./manifest.jsonc [--interval daily|weekly] [--time HH:MM] [--auto-push] [--json]
endstate schedule disable [--json]
endstate schedule status [--json]
endstate schedule run [--manifest <path>] [--root <path>] [--json]
```

### Platform gating

`schedule enable`, `schedule disable`, and `schedule run` are **Windows-only**. On other platforms they return error code `NOT_SUPPORTED`. `schedule status` works on all platforms (returns the persisted config and last-run).

### `schedule enable` response

```json
{
  "success": true,
  "data": {
    "enabled": true,
    "manifest": "C:\\manifests\\my-machine.jsonc",
    "interval": "daily",
    "time": "09:00",
    "autoPush": false,
    "taskName": "Endstate\\DriftCheck",
    "root": "C:\\Users\\user\\AppData\\Local\\Endstate"
  }
}
```

Registration is idempotent (`schtasks /F`): re-running `enable` re-asserts the task with the current executable path and configuration. The task command line bakes `--root` so scheduled runs and GUI-spawned runs share one state directory.

### `schedule disable` response

```json
{
  "success": true,
  "data": {
    "enabled": false,
    "taskName": "Endstate\\DriftCheck"
  }
}
```

The task is removed (`schtasks /Delete /F`) and `state/schedule/config.json` is updated with `enabled: false` (file is retained).

### `schedule status` response

```json
{
  "success": true,
  "data": {
    "enabled": true,
    "manifest": "C:\\manifests\\my-machine.jsonc",
    "interval": "daily",
    "time": "09:00",
    "autoPush": false,
    "taskName": "Endstate\\DriftCheck",
    "lastRun": {
      "schemaVersion": "1.0",
      "runId": "schedule-20260710-090000",
      "timestampUtc": "2026-07-10T09:00:00Z",
      "verify": {
        "summary": { "total": 10, "pass": 9, "fail": 1 },
        "drifted": [
          { "id": "vscode", "name": "Visual Studio Code", "status": "fail", "reason": "missing" }
        ]
      },
      "autoBackup": null,
      "error": null
    }
  }
}
```

`lastRun` is `null` when the schedule has never run. Clients use `lastRun` to distinguish: never-run, last-run-succeeded-no-drift, last-run-found-drift, and last-run-failed (hard error in `lastRun.error`).

### `schedule run` response

`schedule run` verifies in-process, writes `state/schedule/last-run.json` atomically, and **exits 0 on drift** (drift is data, not error). When `--json` is passed for manual/debug invocation, the last-run document is returned as the envelope `data`:

```json
{
  "success": true,
  "data": {
    "runId": "schedule-20260710-090000",
    "timestampUtc": "2026-07-10T09:00:00Z",
    "verify": {
      "summary": { "total": 10, "pass": 9, "fail": 1 },
      "drifted": [
        { "id": "vscode", "name": "Visual Studio Code", "status": "fail", "reason": "missing" }
      ]
    },
    "autoBackup": null,
    "error": null
  }
}
```

No NDJSON events are emitted by `schedule run` (headless; event contract v1 is untouched).

### Stable error codes

| Code | Trigger |
|------|---------|
| `NOT_SUPPORTED` | `schedule enable/disable/run` on non-Windows |
| `TASK_REGISTRATION_FAILED` | `schtasks.exe` failed; config is not marked enabled |
| `SCHEDULE_DISABLED` | `schedule run` was invoked but no schedule is enabled; run `schedule enable` first |
| `MANIFEST_NOT_FOUND` | `--manifest` missing or path does not exist |

### State files

Both files are written atomically (temp+rename):

- `state/schedule/config.json` — `{schemaVersion, enabled, manifest, interval, time, autoPush, taskName, root, registeredAt}`
- `state/schedule/last-run.json` — `{schemaVersion, runId, timestampUtc, verify, autoBackup, error}`

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
