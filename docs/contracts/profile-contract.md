# Endstate Profile Contract (manifest v1 and v2)

This document defines the canonical profile manifest contract for Endstate. Both the Endstate engine (CLI) and Endstate GUI adhere to this contract.

## Overview

A **profile** describes a desired machine state. Profiles exist in three formats:

1. **Zip bundle** (`<name>.zip`) — preferred format containing manifest, config payloads, and metadata
2. **Loose folder** (`<name>\manifest.jsonc`) — unzipped bundle or manually assembled folder
3. **Bare manifest** (`<name>.jsonc`) — single JSON/JSONC/JSON5 file (legacy, install-only)

The engine is the authority on what constitutes a valid profile; the GUI relies on the same contract for discovery and validation.

## Profile Formats

### Format 1: Zip Bundle (Preferred)

A `.zip` file containing either a legacy/schema-v1 layout or a generation-aware layout:

```
<name>.zip
├── manifest.jsonc          # App list (standard profile manifest)
├── configs/                # Config module payloads (optional)
│   ├── <module-id>/
│   │   └── <files...>
│   └── ...
└── metadata.json           # Capture metadata
```

Generation-aware payloads use stable capture IDs and include inspectable provenance:

```
<name>.zip
├── manifest.jsonc          # version: 2; configCaptures[]
├── configs/
│   └── <capture-id>/       # complete relative payload hierarchy
├── provenance/
│   └── modules/            # canonical, non-executable source module snapshots
└── metadata.json           # schemaVersion: "2.0"
```

**`metadata.json` schema:**

```json
{
  "schemaVersion": "1.0",
  "capturedAt": "2026-02-16T20:00:00Z",
  "machineName": "DESKTOP-ABC123",
  "endstateVersion": "0.1.0",
  "configModulesIncluded": ["vscode", "claude-desktop"],
  "configModulesSkipped": [],
  "captureWarnings": []
}
```

A zip with only `manifest.jsonc` + `metadata.json` (no `configs/`) is valid (install-only profile).

For metadata schema `2.0`, `metadata.json` additionally identifies `manifestVersion: 2`. Compatibility-relevant per-set source provenance lives in the embedded manifest's `configCaptures[]`; metadata summarizes the artifact and does not become target authority.

### Format 2: Loose Folder

A directory containing at minimum `manifest.jsonc`:

```
<name>/
├── manifest.jsonc
├── configs/                # Optional
│   └── ...
└── metadata.json           # Optional
```

An unzipped zip bundle is a valid loose folder profile.

### Format 3: Bare Manifest (Legacy)

A single `.jsonc`/`.json`/`.json5` file. Install-only — no config payloads.

---

## Manifest Schema Version

- **Generation-Aware Version:** `2`
- **Minimum Supported:** `1`

Profile versioning follows these rules:
- The `version` field is a **number** (not a string)
- Version `1` remains supported for legacy/schema-v1 flat restore payloads
- Version `2` is required when any generation-aware `configCaptures[]` record is present
- Future versions will be backward-compatible where possible

New engines explicitly dispatch and validate versions 1 and 2. Version-2 data that is malformed never falls back to version-1 restore behavior. Released version-1 engines may still process application declarations and explicit legacy lanes, so generation-aware payload safety comes from structural isolation: v2 payloads have no flat restore entry.

---

## Profile Signature (Validation)

A file is considered a **valid profile manifest** if and only if:

### Required Fields

| Field | Type | Description |
|-------|------|-------------|
| `version` | number | Must be supported integer `1` or `2` |
| `apps` | array | Array of app entries (may be empty) |

### Optional Fields

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | Profile name (human-readable identifier) |
| `captured` | string | ISO 8601 timestamp of when profile was captured |
| `includes` | array | Paths to included manifest files |
| `restore` | array | Configuration restore operations |
| `verify` | array | Verification steps |
| `configModules` | array | Config module references |
| `exclude` | `string[]` | Winget IDs to remove from merged app list (composition) |
| `excludeConfigs` | `string[]` | Config module IDs to suppress from restore (composition) |
| `configCaptures` | array | Version-2 per-instance/per-config-set provenance and payload records; required for version 2 |

### Version-2 Config Capture Schema

Each `configCaptures[]` record is independently addressable and requires:

| Field | Type | Description |
|-------|------|-------------|
| `captureId` | string | Stable ID used for payload location and explicit target mapping |
| `moduleId` | string | Current-catalog lookup identity |
| `configSetId` | string | Independently evolving settings family |
| `sourceInstance` | object | Stable instance/detector IDs, raw/normalized version evidence, and detector evidence |
| `sourceGeneration` | string | Capture-time generation ID |
| `sourceGenerationFingerprint` | string | Canonical capture-time generation fingerprint |
| `captureModule` | object | Schema version, canonical content hash, and snapshot path |
| `payloadRoot` | string | Portable `configs/<captureId>` root |
| `payloadManifest` | array | Relative path, byte size, and SHA-256 for every payload entry |

The bundle owns these immutable source facts and bytes. The pinned trusted current catalog owns target discovery, target generations, and migrations. The module snapshot is verified and inspectable but never executed or used as target authority.

A version-2 manifest may also contain explicitly identified schema-v1 flat restore lanes. Those remain `legacy_unverified`; no flat entry may reference a generation-aware payload or act as fallback for invalid `configCaptures[]` data.

Manifest version 1 does not interpret `configCaptures[]`. Capture-generated version-2 manifests contain at least one generation-aware record; install-only/schema-v1-only captures remain version 1.

### App Entry Schema

Each entry in the `apps` array must have:

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `id` | string | Yes | Stable cross-platform app identifier |
| `driver` | string | No | Package driver selector: `winget`, `chocolatey`, or `brew` (case-insensitive) |
| `source` | string | No | For WinGet apps only: `winget` or `msstore` (case-insensitive; normalized lowercase) |
| `refs` | object | No | Platform-specific package references |
| `refs.windows` | string | No | Windows package ID interpreted by the selected Windows driver |

**Note:** For backward compatibility, app entries with missing `id` are accepted but flagged as warnings.

On Windows, omitted `driver` means `winget`. For WinGet, an explicit valid `source` is authoritative. When source is omitted, recognized Microsoft Store IDs infer `msstore` for backward compatibility and other refs infer `winget`. Source on a non-WinGet driver, or any value outside `winget`/`msstore`, is invalid.

On Windows, omitted `driver` means `winget`. An explicit driver never falls back to another manager. A globally known driver unsupported on the host is a visible skipped item; an unknown driver fails manifest validation before package mutation.

### Driver-Aware Config Module Matches

Config modules may add `matches.chocolatey` alongside `matches.winget`. Results preserve legacy `configModuleMap` (bare Winget refs) and add `packageModuleMap` with `driver:ref` keys whose values are arrays of matching module IDs; capture module metadata adds `chocolateyRefs` alongside `wingetRefs`.

---

## Candidate Files

### Valid Profile Extensions

Files with these extensions are **candidates** for profile validation:
- `.json`
- `.jsonc`
- `.json5`

### Excluded Files

The following are **never** considered profiles:
- `*.meta.json` — GUI-only metadata files (displayName, etc.)
- Files that fail JSON/JSONC parsing
- Files that fail profile signature validation

---

## Validation Rules

### Validation Function

The engine provides a canonical validation function:

```powershell
Test-ProfileManifest -Path <path>
# Returns: @{ Valid = $true/$false; Errors = @(...); Summary = @{...} }
```

### Validation Checks (in order)

1. **File exists** — Path must point to an existing file
2. **Parseable** — Content must be valid JSON/JSONC/JSON5
3. **Version field** — Must have a supported numeric integer value (`1` or `2`)
4. **Apps field** — Must have `apps` field that is an array
5. **Version-2 provenance** — Every `configCaptures[]` record is complete, structurally isolated, hierarchy-safe, and internally consistent

### Error Codes

| Code | Description |
|------|-------------|
| `FILE_NOT_FOUND` | File does not exist |
| `PARSE_ERROR` | Invalid JSON/JSONC/JSON5 syntax |
| `MISSING_VERSION` | No `version` field present |
| `INVALID_VERSION_TYPE` | `version` is not a number |
| `UNSUPPORTED_VERSION` | `version` is not a supported manifest version |
| `MISSING_APPS` | No `apps` field present |
| `INVALID_APPS_TYPE` | `apps` is not an array |
| `INVALID_APP_ENTRY` | App entry missing required `id` field (warning) |

---

## Discovery Rules (GUI)

### Profile Resolution (CLI)

When resolving `--profile "Name"`, the engine checks in order:

1. `<ProfilesDir>\Name.zip` — zip bundle
2. `<ProfilesDir>\Name\manifest.jsonc` — loose folder
3. `<ProfilesDir>\Name.jsonc` — bare manifest

First match wins. The default profiles directory is `Documents\Endstate\Profiles\`.

### Profile Discovery Algorithm (GUI)

1. List all items in the profiles directory
2. Discover profiles in three passes:
   - **Zip bundles:** `*.zip` files (validate by checking for `manifest.jsonc` entry)
   - **Loose folders:** directories containing `manifest.jsonc`
   - **Bare manifests:** `.json`, `.jsonc`, `.json5` files (excluding `*.meta.json`)
3. Deduplicate: if a name appears in multiple formats, prefer zip → folder → bare
4. For each candidate:
   - Parse the manifest content
   - Ask the engine to validate against the versioned profile signature
   - Only include profiles that pass validation
5. Return list of valid profiles with metadata

### Metadata Files (`.meta.json`)

Metadata files store GUI-specific information:

```json
{
  "displayName": "My Work Laptop"
}
```

- Metadata files are **never** listed as profiles
- Metadata provides `displayName` overlay for profile labels
- Metadata path: `<profile-basename>.meta.json`

---

## Display Label Resolution

When displaying a profile to the user, the label is resolved in this order:

1. **`.meta.json` displayName** — GUI-only override (highest priority)
2. **Manifest `name` field** — Profile's self-declared name
3. **Filename stem** — Fallback if neither above exists

### Example Resolution

| File | Manifest `name` | `.meta.json` displayName | Displayed As |
|------|-----------------|--------------------------|---------------|
| `work.json` | `"work-laptop"` | `"My Work Laptop"` | My Work Laptop |
| `work.json` | `"work-laptop"` | *(none)* | work-laptop |
| `work.json` | *(none)* | *(none)* | work |

---

## Rename Semantics

### Default Rename (GUI)

- **"Rename"** updates the `.meta.json` displayName only
- The profile filename is **never** renamed by the GUI
- The manifest `name` field is **never** modified by the GUI
- Users can rename files manually via file explorer; validity is determined by content, not filename

### Advanced Rename (Not Implemented)

An advanced rename could optionally:
- Rename the file on disk
- Update the manifest `name` field

This is **not implemented** in the current GUI. Power users can rename files manually.

---

## Delete Semantics

When deleting a profile:
1. Delete the profile file (`.json`/`.jsonc`/`.json5`)
2. Delete the associated `.meta.json` if it exists
3. **Cannot delete** the currently-selected profile (GUI enforces this)

---

## CLI Commands

### Validate Command

```powershell
endstate validate <path>
```

- Validates the file against the profile contract
- Prints clear errors and exits non-zero on invalid
- Prints the validated profile version on success

### Apply/Preview Commands

Both `apply` and `preview` commands validate the manifest before execution:
- Invalid manifests are rejected with clear error messages
- Validation uses the same `Test-ProfileManifest` function

---

## Backward Compatibility

### Existing Manifests

All existing manifests that match the current structure continue to work:
- `version: 1` with `apps` array → Valid
- Missing optional fields → Valid (defaults applied)
- Version-1 config payloads remain available through existing consent, conflict, backup, journal, and revert behavior and are reported `legacy_unverified`

### Future Versions

When introducing new versions:
- Engine will support multiple versions simultaneously
- GUI will use engine validation (single source of truth)
- New optional fields do not require version bump

### Generation-Aware Compatibility

- A source/target application version comparison never substitutes for config-generation resolution
- Side-by-side source and target instances remain separate; no latest-version preference is encoded in the profile
- Each config set resolves independently as `direct`, `migrate`, `incompatible`, `unknown`, or `legacy_unverified`
- Unknown or incompatible config sets do not block independent application installation

---

## Example: Valid Profile

```jsonc
{
  "version": 1,
  "name": "my-laptop",
  "captured": "2025-01-15T10:30:00Z",
  "includes": ["./includes/common.jsonc"],
  "apps": [
    {
      "id": "vscode",
      "refs": {
        "windows": "Microsoft.VisualStudioCode"
      }
    },
    {
      "id": "git-chocolatey",
      "driver": "chocolatey",
      "refs": {
        "windows": "git.install"
      }
    }
  ],
  "restore": [],
  "verify": []
}
```

## Example: Valid Generation-Aware Profile

```jsonc
{
  "version": 2,
  "name": "design-workstation",
  "apps": [
    { "id": "example", "refs": { "windows": "Vendor.Example" } }
  ],
  "configCaptures": [
    {
      "captureId": "apps.example-preferences-instance-a",
      "moduleId": "apps.example",
      "configSetId": "preferences",
      "sourceInstance": {
        "id": "instance-a",
        "detectorId": "installed-package",
        "rawVersion": "27.4",
        "normalizedVersion": "27.4",
        "evidence": { "backend": "winget", "ref": "Vendor.Example" }
      },
      "sourceGeneration": "g1",
      "sourceGenerationFingerprint": "<sha256>",
      "captureModule": {
        "schemaVersion": 2,
        "contentHash": "<sha256>",
        "snapshotPath": "provenance/modules/apps.example.json"
      },
      "payloadRoot": "configs/apps.example-preferences-instance-a",
      "payloadManifest": [
        { "relativePath": "settings/preferences.json", "size": 123, "sha256": "<sha256>" }
      ]
    }
  ]
}
```

## Example: Invalid Profiles

### Missing version
```json
{
  "apps": []
}
```
Error: `MISSING_VERSION`

### Wrong version type
```json
{
  "version": "1",
  "apps": []
}
```
Error: `INVALID_VERSION_TYPE`

### Missing apps
```json
{
  "version": 1
}
```
Error: `MISSING_APPS`

### Apps not an array
```json
{
  "version": 1,
  "apps": {}
}
```
Error: `INVALID_APPS_TYPE`

---

## Implementation Notes

### Engine

The engine owns version dispatch and validation:
- Single source of truth for validation
- Used by `validate`, `apply`, `preview` commands
- Returns structured result with `Valid`, `Errors`, `Summary`
- Pins the trusted current catalog for generation resolution and never treats bundle snapshots as executable authority

### GUI (Tauri/Rust)

The GUI exposes validation via Tauri command:
- `validate_profile` command calls engine validator
- Returns `{ valid: boolean, errors: string[], summary: object }`
- GUI discovery uses this for filtering candidates

### Synchronization

- Engine owns the validation logic
- GUI calls engine validation and does not mirror manifest-v2 provenance or generation rules
- Contract document is canonical reference for both
