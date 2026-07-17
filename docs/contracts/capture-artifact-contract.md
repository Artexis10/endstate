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
- Sensitive files (listed in `module.secrets.files`) are NEVER included in the zip
- A capture containing any schema-v2 config set uses bundle metadata schema `2.0` and embedded manifest version `2`
- Generation-aware payloads exist only under `configs/<captureId>/` and are referenced only by `configCaptures[]`; they never receive a flat restore entry

### INV-CAPTURE-6: Config Capture Failures Don't Block

**Config file capture failures MUST NOT prevent successful app capture.**

- If a config file is missing or inaccessible, capture still succeeds
- Failed config captures are reported in `configsCaptureErrors` (JSON output) and `metadata.json`
- The manifest is always written to the zip

### INV-CAPTURE-7: Install-Only Is Valid

**A zip with only `manifest.jsonc` + `metadata.json` (no configs) is a valid, successful capture.**

- No warnings, no errors for missing configs
- Matches UX guardrail: install-only profiles are successful outcomes

### INV-CAPTURE-8: Module Schemas Remain Distinct

**Existing modules without `moduleSchemaVersion` remain schema v1 and retain flat capture/restore behavior.**

- Generation-aware declarations require `moduleSchemaVersion: 2`
- A schema-v1 payload is reported as unversioned/`legacy_unverified`; the engine never fabricates an application version or config generation for it
- A manifest-v2 bundle may contain explicit schema-v1 flat lanes beside `configCaptures[]`, but a flat lane can never supply data or fallback behavior for an invalid schema-v2 capture

### INV-CAPTURE-9: Per-Set Source Provenance Is Immutable

Every successful generation-aware config-set capture records:

- `captureId`, `moduleId`, and `configSetId`
- source instance ID, detector ID, raw version evidence, and normalized numeric dotted version when available
- source generation and canonical source-generation fingerprint
- capture-time module schema version, canonical content hash, and inspectable snapshot path
- payload root and a manifest of relative path, byte size, and SHA-256 for every payload entry

Capture keeps side-by-side instances separate. It never labels one preferred/latest or silently collapses two detected roots into one record.

### INV-CAPTURE-10: Payload Layout and Provenance Are Verifiable

- Complete relative path hierarchy is preserved under `configs/<captureId>/`
- Duplicate portable destinations reject the affected config-set capture rather than overwriting by basename
- Canonical module snapshots live under `provenance/modules/`, are non-executable, and are verified against their recorded hash
- Hashes detect bundle corruption or internal inconsistency; they do not provide authenticity or signing

### INV-CAPTURE-11: Legacy Engines Cannot Reach V2 Payloads

A released manifest-v1 engine may still process application declarations or explicitly represented legacy lanes in a v2 bundle. Structural isolation is therefore mandatory: because generation-aware bytes have no flat restore entry, an engine that does not understand `configCaptures[]` has no executable legacy path to them.

### INV-CAPTURE-12: Capture Envelope Reports Compatibility Version

Generation-aware capture JSON adds `configCapture.configSets[]` and reports bundle schema `2.0` plus manifest version `2`. Each config-set row includes its capture/source identity, source generation and fingerprint, capture module revision, file count, status, and reason. Existing `configCapture.modules`, `configModules`, and schema-v1 capture fields remain backward compatible.

## Related Documents

- [CLI JSON Contract](./cli-json-contract.md) - Standard error codes
- [GUI Integration Contract](./gui-integration-contract.md) - GUI behavior requirements
- [Profile Contract](./profile-contract.md) - Profile format and discovery rules
