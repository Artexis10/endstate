# Capture Artifact Contract

This document defines the invariants for capture operations and their artifact outputs.

## Invariants

### INV-CAPTURE-1: CLI Availability

**If the engine CLI is not found, capture MUST fail with a structured error.**

- Error code: `ENGINE_CLI_NOT_FOUND`
- Error must include `hint` field with actionable remediation
- GUI must surface this error with Settings action

```json
{
  "success": false,
  "error": {
    "code": "ENGINE_CLI_NOT_FOUND",
    "message": "Engine CLI not found. Repo root not configured.",
    "hint": "Run 'endstate bootstrap -RepoRoot <path>' or configure Engine path in Settings."
  }
}
```

### INV-CAPTURE-2: Manifest File Existence

**Capture success (`success: true`) requires the manifest file to exist and be non-empty.**

- If manifest file does not exist after capture: `success: false`
- If manifest file is empty (0 bytes): `success: false`
- Error code: `MANIFEST_WRITE_FAILED`

```json
{
  "success": false,
  "error": {
    "code": "MANIFEST_WRITE_FAILED",
    "message": "Capture completed but manifest file was not created at: <path>",
    "hint": "Check disk space and write permissions."
  }
}
```

### INV-CAPTURE-3: GUI Error Surfacing

**GUI must display specific, actionable messages for known error codes.**

| Error Code | GUI Toast Message |
|------------|-------------------|
| `ENGINE_CLI_NOT_FOUND` | Use `error.hint` or "Engine CLI not found. Configure Engine path in Settings." |
| `MANIFEST_WRITE_FAILED` | Use `error.message` |
| `CAPTURE_BLOCKED` | Use `error.message` |
| Other | Use `error.message` or "Capture failed" |

### INV-CAPTURE-4: No Draft on Failure

**GUI must NOT create or persist a draft if capture failed.**

- If `success: false`: do not call `saveDraft()`
- If `success: false`: do not set `pendingCaptureDraft`
- Error state must be surfaced via toast and action status

## Error Code Reference

| Code | Cause | Remediation |
|------|-------|-------------|
| `ENGINE_CLI_NOT_FOUND` | Repo root not configured or CLI path invalid | Run `endstate bootstrap -RepoRoot <path>` or configure in Settings |
| `MANIFEST_WRITE_FAILED` | Disk full, permissions, or capture subprocess failure | Check disk space and permissions |
| `CAPTURE_BLOCKED` | Guardrail blocked operation (e.g., non-sanitized write to examples) | Use correct flags or output path |
| `CAPTURE_FAILED` | Generic capture failure | Check engine logs |

## Verification

To verify these invariants:

```powershell
# Test INV-CAPTURE-1: CLI not found
$env:ENDSTATE_PROVISIONING_CLI = "C:\nonexistent\cli.ps1"
.\bin\endstate.ps1 capture --json
# Expected: success=false, error.code=ENGINE_CLI_NOT_FOUND

# Test INV-CAPTURE-2: Manifest existence
# (Requires mocking Invoke-ProvisioningCli to not create file)
```

### INV-CAPTURE-5: Zip Bundle Output

**Capture produces a zip bundle as the default output format.**

- Output path: `Documents\Endstate\Profiles\<ProfileName>.zip`
- Zip contains at minimum: `manifest.jsonc` + `metadata.json`
- Config payloads are automatically bundled when config modules match captured apps
- Sensitive files (listed in `module.sensitive.files`) are NEVER included in the zip

### INV-CAPTURE-6: Config Capture Failures Don't Block

**Config file capture failures MUST NOT prevent successful app capture.**

- If a config file is missing or inaccessible, capture still succeeds
- Failed config captures are reported in `configsCaptureErrors` (JSON output) and `metadata.json`
- The manifest is always written to the zip

### INV-CAPTURE-7: Install-Only Is Valid

**A zip with only `manifest.jsonc` + `metadata.json` (no configs) is a valid, successful capture.**

- No warnings, no errors for missing configs
- Matches UX guardrail: install-only profiles are successful outcomes

## Related Documents

- [CLI JSON Contract](./cli-json-contract.md) - Standard error codes
- [GUI Integration Contract](./gui-integration-contract.md) - GUI behavior requirements
- [Profile Contract](./profile-contract.md) - Profile format and discovery rules
