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
| `VERIFY_FAILED` | Verification check failed |
| `PERMISSION_DENIED` | Insufficient permissions |
| `INTERNAL_ERROR` | Unexpected internal error |
| `SCHEMA_INCOMPATIBLE` | Schema version mismatch |
| `ROLLBACK_UNSUPPORTED` | The host's package backend does not advertise native rollback (e.g. winget on Windows; any host with no realizer). Additive in schema 1.x. |
| `GENERATION_NOT_FOUND` | The `rollback --to <n>` target generation does not exist, or records no backend-native rollback anchor. Additive in schema 1.x. |
| `ROLLBACK_FAILED` | The backend rollback failed (non-systemic). Raw backend text is confined to `error.detail`. Additive in schema 1.x. |
| `CONVERGENCE_UNSUPPORTED` | `apply --prune` was requested on a backend that cannot safely remove installed-but-undeclared packages (the winget driver, or any host with no realizer). Nothing is removed. Additive in schema 1.x. |

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
        "flags": ["--manifest", "--plan", "--dry-run", "--enable-restore", "--restore-filter", "--json"]
      },
      "verify": {
        "supported": true,
        "flags": ["--manifest", "--json"]
      },
      "restore": {
        "supported": true,
        "flags": ["--manifest", "--enable-restore", "--restore-filter", "--dry-run", "--json"]
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

> **`platform` is host-dependent.** `platform.os` reflects the host operating system (`windows`, `linux`, `darwin`) and `platform.drivers` lists the package-manager backends available on that host. On Windows this is `{ "os": "windows", "drivers": ["winget"] }`. On Linux and macOS with Nix available this is `["nix"]`. On a host with no implemented backend yet, `drivers` is an empty array (`[]`). This is additive: consumers MUST NOT assume a fixed `windows` / `winget` value.

---

## Command: `capture`

Captures current machine state into a zip bundle profile.

```powershell
endstate capture --profile "My-Desktop" --json
```

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
      { "id": "Microsoft.VisualStudioCode", "source": "winget" }
    ],
    "configsIncluded": ["vscode", "claude-desktop"],
    "configsSkipped": ["git"],
    "configsCaptureErrors": [],
    "configModuleMap": {
      "Git.Git": "apps.git",
      "Microsoft.VisualStudioCode": "apps.vscode"
    },
    "captureWarnings": []
  },
  "error": null
}
```

### Capture Data Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `outputPath` | string | Yes | Absolute path to output file |
| `outputFormat` | string | Yes | `"zip"` for bundle, `"jsonc"` for legacy |
| `sanitized` | boolean | Yes | Whether output was sanitized |
| `isExample` | boolean | Yes | Whether this is an example manifest |
| `counts` | object | Yes | Capture statistics |
| `appsIncluded` | array | Yes | Apps included in manifest |
| `configsIncluded` | array | No | Config module IDs bundled in zip |
| `configsSkipped` | array | No | Config module IDs that matched but were skipped |
| `configsCaptureErrors` | array | No | Config capture error descriptions |
| `configModuleMap` | object | Yes | Maps winget package refs to config module IDs (empty `{}` when no mappings) |
| `captureWarnings` | array | No | General capture warnings |

**Note:** `configsIncluded`, `configsSkipped`, and `configsCaptureErrors` are only present when `outputFormat` is `"zip"`. `configModuleMap` is always present (empty object when no config modules resolve to winget refs).

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

### Convergence (`--prune`)

`apply --prune` converges the engine-managed set to *exactly* the manifest: after the install phase it removes installed-but-undeclared packages ("drift") in one atomic generation switch. Convergence is realizer-only (Nix on Linux/macOS); the winget driver refuses with `CONVERGENCE_UNSUPPORTED`, changing nothing. Package-stage only: prune never touches configuration restore, `state/backups/`, or the revert journal.

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

### Version capture and pinning (winget)

On the winget backend, each Provisioning Generation item records the installed `version` of the package, captured from `winget list` (best-effort — empty when winget exposes none). The Nix realizer pins exact versions through its ref, so nix generations leave `version` empty.

A manifest app MAY declare a `version` to **pin** the install:

```jsonc
{ "id": "vscode", "version": "1.89.0", "refs": { "windows": "Microsoft.VisualStudioCode" } }
```

- When `version` is declared, the winget backend installs that exact version (`winget install --version`). The recorded generation item carries the pinned `version`.
- By default, pinning is **pin-on-install only**: a package already installed at a different version is left untouched (reported `present`). Use `--repin` (below) to converge a drifted version.
- If the declared version is unavailable, that package fails as `INSTALL_FAILED` (the requested version appears in the message); no other version is installed in its place. Other packages in the run are unaffected.
- Pinning applies only to backends that support versioned install (winget). The Nix realizer ignores `version` (it pins via its ref); this is not an error.

### Version convergence (`--repin`)

`apply --repin` reinstalls a declared `version` when the installed version has drifted from it (`winget install --version <v> --force`) — the enforcement counterpart of pinning. Winget-only; the Nix realizer ignores it.

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
- `homeManager.config` is a path (resolved relative to the manifest) to a `home.nix` the engine **wraps in a generated flake** before activating it through this same stage — so the user supplies only their configuration, never the flake, inputs, pinning, identity, or activation wiring. `config` and `flake` are **mutually exclusive** (exactly one home-manager input); a manifest declaring both fails to load with a clear validation error.
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

The apply result carries a `homeManager` object when the config stage runs: `{ "flake": "<flakeref>", "generated": <bool>, "activated": <bool> }` — `generated` is `true` for a `homeManager.config` wrapper (false for a direct `homeManager.flake`), and `activated` is `false` on `--dry-run`. (Optional, omitted when no config stage runs; additive in schema 1.x.)

> **Rollback of an activated home-manager configuration is not yet supported** (a documented follow-on). home-manager keeps its own numbered generations; re-activating a prior one ships separately.

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

### Version drift (winget)

When a manifest app declares a `version` and the installed version (captured on the winget backend) differs, `verify` reports that item as a **failure** with reason **`version_drift`**, distinct from a missing package. The result item carries the installed `version` and the declared `expected` version:

```json
{
  "type": "app",
  "id": "vscode",
  "ref": "Microsoft.VisualStudioCode",
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

`backend` is `"nix"` or `"winget"`. `native` is the backend-native generation number (the Nix generation) or empty for non-atomic backends. `partial` is true when a non-atomic backend (winget) committed only a subset of the requested set. `addedRefs` lists only refs installed in that run (status `installed`); already-present refs appear in `items` but not `addedRefs`. A generation is recorded when at least one package was installed in the run **or** a home-manager configuration was activated by the config stage. `rollback` (optional, omitted when false; additive in schema 1.x) is `true` when the generation was produced by a `rollback` rather than an `apply`; such generations snapshot the now-active set and have an empty `addedRefs`. `homeManager` (optional, omitted when absent; additive in schema 1.x) records a home-manager configuration activated by `apply --enable-restore`: `{ "flake": "<flakeref>", "generation": <hm generation number> }`.

## Command: `rollback`

Reverts the installed package set to a prior Provisioning Generation. Two strategies, both keyed off the recorded generations and identified by **engine generation number** (as listed by `generations`): callers never reference a backend-native version directly. Additive in schema 1.x.

- **Native** (the Nix realizer on Linux/macOS): an atomic rollback to the backend-native anchor (`native`) recorded in the target generation.
- **Best-effort** (the winget driver on Windows): there is no native rollback, so the engine uninstalls the union of `addedRefs` of every generation recorded *after* the target. Per-package and non-atomic — it tolerates per-package failure (reporting `partial`), treats an already-absent package as removed, and **does not track package-manager-pulled transitive dependencies/co-installs** (which may remain — surfaced as a `warning`).

A backend that can neither roll back natively nor uninstall refuses with `ROLLBACK_UNSUPPORTED`. Package-stage only: rollback never touches configuration restore, `state/backups/`, or the revert journal (that is `revert`'s concern).

### Request

| Flag | Meaning |
|------|---------|
| `--to <n>` | Engine Provisioning Generation number to roll back to. Omitted ⇒ the immediately previous version. |
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
