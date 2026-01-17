# Capture Artifact Contract

## Overview

This specification defines the contract for capture operations, including fallback behavior when `winget export` fails.

## Invariants

### INV-CAPTURE-1: CLI Availability
- Capture MUST fail with `ENGINE_CLI_NOT_FOUND` if the provisioning CLI is not available
- Error MUST include actionable `hint` field

### INV-CAPTURE-2: Manifest File Existence
- `success: true` ⇒ manifest file exists AND is non-empty
- Empty file (0 bytes) MUST result in failure

### INV-CAPTURE-3: Manifest Structure
- Manifest MUST contain `apps` array and/or `version` field
- `{}` is NEVER a valid manifest output

### INV-CAPTURE-4: Fallback Capture
- If `winget export` fails OR produces empty/missing output:
  - Engine MUST attempt fallback via `winget list --source winget`
  - If fallback produces >= 1 app, capture succeeds with warning
  - Warning code: `WINGET_EXPORT_FAILED_FALLBACK_USED`

### INV-CAPTURE-5: Empty Capture Failure
- If BOTH export and fallback produce zero apps:
  - Capture MUST fail with `success: false`
  - Error code: `WINGET_CAPTURE_EMPTY`
  - Error MUST include actionable `hint` field

### INV-CONTINUITY-1: Counts Must Match Apps List Length
- `counts.included` MUST equal `appsIncluded.length`
- `counts.included` MUST equal `manifest.apps.length` (same captured set)
- Engine MUST derive both from the same source (manifest.apps)
- GUI MUST NOT filter or transform appsIncluded in ways that change count

### INV-SANITIZE-IDS-1: App IDs Must Be Sanitized Before Persistence
- All app IDs in `appsIncluded` MUST be sanitized:
  - No leading non-ASCII characters (e.g., 'ª' from encoding issues)
  - No backslashes (ARP/MSIX entries are not valid winget package IDs)
  - Trimmed of leading/trailing whitespace
- Sanitization MUST occur in engine before manifest write
- GUI relies on this contract; it does NOT re-sanitize

### INV-DETAILS-1: Capture Details UI Must Render App List
- Capture Details modal MUST show scrollable list of captured apps
- Canonical source: `appsIncluded` from engine envelope (preferred)
- Fallback: derive from manifest if `appsIncluded` unavailable
- Count displayed MUST equal list length shown
- If list unavailable but count > 0: show "N apps captured" with note

## Failure Modes Table

| Condition | UI Behavior |
|-----------|-------------|
| `appsIncluded` present, length > 0 | Show scrollable app list |
| `appsIncluded` empty or missing, count > 0 | Show "N apps captured" summary |
| `appsIncluded` empty, count = 0 | Show "No applications detected" |
| Capture failed | Show error message from envelope |

## Warning Codes

| Code | Description |
|------|-------------|
| `WINGET_EXPORT_FAILED_FALLBACK_USED` | winget export failed; fallback capture via winget list was used |

## Error Codes

| Code | Description |
|------|-------------|
| `ENGINE_CLI_NOT_FOUND` | Provisioning CLI not found at configured path |
| `MANIFEST_WRITE_FAILED` | Manifest file was not created or is empty |
| `CAPTURE_FAILED` | Generic capture failure |
| `WINGET_CAPTURE_EMPTY` | Both export and fallback produced zero apps |

## JSON Output Schema

### Success Response

```json
{
  "schemaVersion": "1.0",
  "command": "capture",
  "success": true,
  "data": {
    "outputPath": "C:\\path\\to\\manifest.jsonc",
    "sanitized": false,
    "counts": {
      "totalFound": 50,
      "included": 45,
      "skipped": 5,
      "filteredRuntimes": 3,
      "filteredStoreApps": 2,
      "sensitiveExcludedCount": 0
    },
    "appsIncluded": [
      { "id": "Git.Git", "source": "winget" }
    ],
    "captureWarnings": ["WINGET_EXPORT_FAILED_FALLBACK_USED"]
  },
  "error": null
}
```

### Failure Response

```json
{
  "schemaVersion": "1.0",
  "command": "capture",
  "success": false,
  "data": null,
  "error": {
    "code": "WINGET_CAPTURE_EMPTY",
    "message": "No applications were captured. Both winget export and fallback capture returned zero apps.",
    "hint": "Ensure winget is properly configured and has access to package sources. Run 'winget source update' and try again."
  }
}
```

## GUI Behavior

### Warning Toast
- If `captureWarnings` includes `WINGET_EXPORT_FAILED_FALLBACK_USED`:
  - Show non-blocking info toast: "Winget export failed; captured winget-managed apps only."

### Save Profile Validation
- Save MUST refuse to write:
  - Empty string
  - `{}` only
  - Metadata-only JSON (no `version` or `apps` field)
- Show clear error toast if manifest is invalid

## Test Coverage

### Engine (Pester)
- Export fails → fallback used → manifest has apps
- Export fails + fallback empty → capture fails with `WINGET_CAPTURE_EMPTY`
- No `{}` manifests ever written
- **INV-CONTINUITY-1**: `counts.included` equals `appsIncluded.length`
- **INV-SANITIZE-IDS-1**: No app IDs start with non-ASCII characters

### GUI (Vitest)
- Fallback warning toast appears when `captureWarnings` present
- Save blocked on invalid manifest (`{}`, metadata-only)
- Success path requires non-empty manifest with `version` or `apps`
- **INV-CONTINUITY-1**: Derived count equals appsIncluded length
- **INV-DETAILS-1**: Capture Details receives and can render app list
- **INV-SANITIZE-IDS-1**: Dirty IDs (non-ASCII prefix) are detected/rejected
