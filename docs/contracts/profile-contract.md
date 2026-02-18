# Endstate Profile Contract v1.0

This document defines the canonical profile manifest contract for Endstate. Both the Endstate engine (CLI) and Endstate GUI adhere to this contract.

## Overview

A **profile** describes a desired machine state. Profiles exist in three formats:

1. **Zip bundle** (`<name>.zip`) — preferred format containing manifest, config payloads, and metadata
2. **Loose folder** (`<name>\manifest.jsonc`) — unzipped bundle or manually assembled folder
3. **Bare manifest** (`<name>.jsonc`) — single JSON/JSONC/JSON5 file (legacy, install-only)

The engine is the authority on what constitutes a valid profile; the GUI relies on the same contract for discovery and validation.

## Profile Formats

### Format 1: Zip Bundle (Preferred)

A `.zip` file containing:

```
<name>.zip
├── manifest.jsonc          # App list (standard profile manifest)
├── configs/                # Config module payloads (optional)
│   ├── <module-id>/
│   │   └── <files...>
│   └── ...
└── metadata.json           # Capture metadata
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

## Schema Version

- **Current Profile Version:** `1`
- **Minimum Supported:** `1`

Profile versioning follows these rules:
- The `version` field is a **number** (not a string)
- Version `1` is the current and only supported version
- Future versions will be backward-compatible where possible

---

## Profile Signature (Validation)

A file is considered a **valid profile manifest** if and only if:

### Required Fields

| Field | Type | Description |
|-------|------|-------------|
| `version` | number | Must be `1` (integer) |
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

### App Entry Schema

Each entry in the `apps` array must have:

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `id` | string | Yes | Stable cross-platform app identifier |
| `refs` | object | No | Platform-specific package references |
| `refs.windows` | string | No | Windows package ID (e.g., winget ID) |

**Note:** For backward compatibility, app entries with missing `id` are accepted but flagged as warnings.

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
3. **Version field** — Must have `version` field with numeric value `1`
4. **Apps field** — Must have `apps` field that is an array

### Error Codes

| Code | Description |
|------|-------------|
| `FILE_NOT_FOUND` | File does not exist |
| `PARSE_ERROR` | Invalid JSON/JSONC/JSON5 syntax |
| `MISSING_VERSION` | No `version` field present |
| `INVALID_VERSION_TYPE` | `version` is not a number |
| `UNSUPPORTED_VERSION` | `version` is not `1` |
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
   - Validate against profile signature
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
- Prints "Valid profile (v1)" on success

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

### Future Versions

When introducing new versions:
- Engine will support multiple versions simultaneously
- GUI will use engine validation (single source of truth)
- New optional fields do not require version bump

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
    }
  ],
  "restore": [],
  "verify": []
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

### Engine (PowerShell)

The engine implements `Test-ProfileManifest` in `engine/manifest.ps1`:
- Single source of truth for validation
- Used by `validate`, `apply`, `preview` commands
- Returns structured result with `Valid`, `Errors`, `Summary`

### GUI (Tauri/Rust)

The GUI exposes validation via Tauri command:
- `validate_profile` command calls engine validator
- Returns `{ valid: boolean, errors: string[], summary: object }`
- GUI discovery uses this for filtering candidates

### Synchronization

- Engine owns the validation logic
- GUI calls engine validation (preferred) or mirrors logic
- Contract document is canonical reference for both
