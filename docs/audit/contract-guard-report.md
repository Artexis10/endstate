# ContractGuard Audit Report

**Auditor:** ContractGuard (automated agent)
**Date:** 2026-02-22
**Scope:** All 26 files in `engine/` audited against all 7 contracts in `docs/contracts/`

---

## Executive Summary

| Severity | Count |
|----------|-------|
| CRITICAL | 1 |
| MEDIUM | 3 |
| LOW | 2 |
| PASS | 6 contracts with compliant areas |

6 violations found. 1 is a runtime-breaking bug (VIOLATION-1). 3 are structural gaps that should be addressed before GUI integration ships. 2 are minor misalignments between contract text and implementation.

---

## Contract Compliance Matrix

| Contract | Primary Engine Files | Status |
|----------|---------------------|--------|
| cli-json-contract.md | json-output.ps1, apply.ps1, verify.ps1, report.ps1 | **2 violations** (V2, V3) |
| event-contract.md | events.ps1, capture.ps1, apply.ps1 | **1 violation** (V1) |
| profile-contract.md | manifest.ps1, bundle.ps1, profile-commands.ps1 | PASS |
| capture-artifact-contract.md | capture.ps1, bundle.ps1, json-output.ps1 | **1 violation** (V2 overlap) |
| config-portability-contract.md | restore.ps1, export-capture.ps1, export-revert.ps1, export-validate.ps1 | PASS |
| gui-integration-contract.md | json-output.ps1 | **1 violation** (V3 overlap) |
| restore-safety-contract.md | restore.ps1 | **2 violations** (V5, V6) |

**Cross-cutting:** VIOLATION-4 (state atomicity) affects state.ps1, plan.ps1, restore.ps1.

---

## Violations

### VIOLATION-1: ArtifactEvent Kind ValidateSet too restrictive [CRITICAL]

**Contract:** event-contract.md, lines 196-199
> `kind` (string, required): Always `"manifest"`

**Engine:**
- `engine/events.ps1:308` -- `[ValidateSet("manifest")]` for the Kind parameter of `Write-ArtifactEvent`
- `engine/capture.ps1:630` -- calls `Write-ArtifactEvent -Phase "capture" -Kind "bundle"`

**Impact:** This is a **runtime error**. When capture completes and attempts to emit a bundle artifact event, PowerShell's ValidateSet will reject `"bundle"` and throw a parameter validation exception. This means the streaming event for bundle creation silently fails or crashes the capture pipeline depending on error handling context.

**Resolution options:**
1. If bundles should emit artifact events: expand ValidateSet to `("manifest", "bundle")` and update event-contract.md to document `"bundle"` as an allowed kind value.
2. If the contract is authoritative: change `capture.ps1:630` to use `Kind "manifest"` (but this loses semantic precision about what artifact was produced).

**Recommendation:** Option 1. The contract's "Always manifest" was written before bundle support existed. Update both.

---

### VIOLATION-2: Missing error codes in json-output.ps1 [MEDIUM]

**Contract:** cli-json-contract.md, lines 80-97 defines 16 standard error codes.
**Contract:** capture-artifact-contract.md, lines 11, 32, 53, 71 references `ENGINE_CLI_NOT_FOUND`, `MANIFEST_WRITE_FAILED`, `CAPTURE_BLOCKED`, `CAPTURE_FAILED`.

**Engine:** `engine/json-output.ps1:271-286` defines only 13 error codes in `$script:ErrorCodes`:

| Error Code | In Contract | In Engine |
|------------|-------------|-----------|
| MANIFEST_NOT_FOUND | Yes | Yes |
| MANIFEST_PARSE_ERROR | Yes | Yes |
| MANIFEST_VALIDATION_ERROR | Yes | Yes |
| MANIFEST_WRITE_FAILED | Yes | **No** |
| PLAN_NOT_FOUND | Yes | Yes |
| PLAN_PARSE_ERROR | Yes | Yes |
| WINGET_NOT_AVAILABLE | Yes | Yes |
| ENGINE_CLI_NOT_FOUND | Yes | **No** |
| CAPTURE_FAILED | Yes | **No** |
| CAPTURE_BLOCKED | Yes | **No** |
| INSTALL_FAILED | Yes | Yes |
| RESTORE_FAILED | Yes | Yes |
| VERIFY_FAILED | Yes | Yes |
| PERMISSION_DENIED | Yes | Yes |
| INTERNAL_ERROR | Yes | Yes |
| SCHEMA_INCOMPATIBLE | Yes | Yes |

**Impact:** Any code path that calls `Get-ErrorCode -Name "ENGINE_CLI_NOT_FOUND"` (or the other 3 missing codes) will silently fall back to `"INTERNAL_ERROR"` (line 303). The GUI will not be able to match on the correct error codes for capture-specific failures.

**Resolution:** Add the 4 missing codes to `$script:ErrorCodes` in `json-output.ps1`:
```powershell
MANIFEST_WRITE_FAILED = "MANIFEST_WRITE_FAILED"
ENGINE_CLI_NOT_FOUND = "ENGINE_CLI_NOT_FOUND"
CAPTURE_FAILED = "CAPTURE_FAILED"
CAPTURE_BLOCKED = "CAPTURE_BLOCKED"
```

---

### VIOLATION-3: Capabilities `streaming` field value mismatch [LOW]

**Contract:** gui-integration-contract.md, line 194 shows the capabilities example with:
```json
"streaming": false
```

**Engine:** `engine/json-output.ps1:255` returns:
```powershell
streaming = $true
```

**Analysis:** The engine now supports NDJSON streaming events (events.ps1 is fully implemented). The contract example appears stale -- it was written before streaming was implemented. The `streaming = $true` value in the engine is likely correct.

**Resolution:** Update gui-integration-contract.md line 194 to show `"streaming": true` to match current engine capability. Alternatively, if streaming is still considered experimental, document the discrepancy.

---

### VIOLATION-4: State writes do NOT use temp+move atomic pattern [MEDIUM]

**Contract:** CLAUDE.md states: "State writes use temp file + move pattern"

**Engine violations:**
- `engine/state.ps1:168` -- `$state | ConvertTo-Json -Depth 10 | Out-File -FilePath $stateFile -Encoding UTF8`
- `engine/plan.ps1` -- plan save uses direct `Out-File`
- `engine/restore.ps1:967` -- restore state uses direct `Out-File`
- `engine/restore.ps1:1033` -- restore journal uses direct `Out-File`

**Contrast:** `engine/bundle.ps1` (zip creation) correctly uses temp file + Move-Item for atomic writes.

**Impact:** If the process is interrupted during a state write (power loss, crash, kill), the state file could be left in a partial/corrupt state. This affects:
- Run state persistence (state.ps1)
- Plan files (plan.ps1)
- Restore state and journals (restore.ps1)

**Resolution:** Create a helper function (e.g., `Write-AtomicFile`) that writes to a `.tmp` file then uses `Move-Item -Force` to atomically replace the target. Apply to all state/journal writes. The pattern already exists in bundle.ps1 and can be extracted.

---

### VIOLATION-5: onConflict field not implemented in restore engine [MEDIUM]

**Contract:** restore-safety-contract.md, lines 28-37 defines the `onConflict` field:

| Value | Behavior | Default |
|-------|----------|---------|
| `skip` | Only restore if target does not exist | **Yes (default)** |
| `backup-and-overwrite` | Create backup, then overwrite | No |
| `overwrite` | Overwrite without backup (destructive) | No |

**Engine:** `engine/restore.ps1` has no `onConflict` field handling. The engine currently:
- Always attempts to restore (does not skip existing files by default)
- Always creates backups when overwriting (line 328: `$backup = if ($null -eq $Action.backup) { $true } else { $Action.backup }`)

**Analysis:** The restore-safety-contract.md is marked **"Draft"** status. The engine's current behavior (always restore, always backup) is closer to `backup-and-overwrite` than the contract's default of `skip`. This means the engine is MORE aggressive than the contract specifies -- it will overwrite existing files (with backup) rather than skipping them.

**Impact:** When the GUI implements the restore safety flow described in the contract, the engine behavior will not match the `skip` default. Files that should be skipped (because they already exist) will be overwritten instead.

**Resolution:** Implement `onConflict` dispatch in `restore.ps1`:
1. Read `onConflict` from restore action (default to `"skip"`)
2. Before restoring, check if target exists
3. If target exists and `onConflict` is `"skip"`, skip with status `skipped_exists`
4. If target exists and `onConflict` is `"backup-and-overwrite"`, proceed with backup
5. The `"overwrite"` mode should remain engine-internal only (per contract)

---

### VIOLATION-6: Restore result per-entry schema differences [LOW]

**Contract:** restore-safety-contract.md, lines 43-50 defines per-entry result fields:

| Field | Type | Description |
|-------|------|-------------|
| `source` | string | Source path in bundle |
| `target` | string | Target path on system |
| `action` | string | `restored`, `skipped_exists`, `skipped_up_to_date`, `skipped_missing_source`, `failed` |
| `targetExistedBefore` | boolean | Whether target existed before restore |
| `backupCreated` | boolean | Whether a backup was created |
| `backupPath` | string or null | Backup location if created |

**Engine:** `engine/restore.ps1:1006-1018` journal entries use a superset of fields with slightly different naming:

| Contract Field | Engine Journal Field | Match |
|----------------|---------------------|-------|
| source | source | Yes |
| target | target | Yes |
| action | action | Yes (same values) |
| targetExistedBefore | targetExistedBefore | Yes |
| backupCreated | backupCreated | Yes |
| backupPath | backupPath | Yes |
| *(not in contract)* | kind | Extra |
| *(not in contract)* | resolvedSourcePath | Extra |
| *(not in contract)* | targetPath | Extra |
| *(not in contract)* | backupRequested | Extra |
| *(not in contract)* | error | Extra |

**Analysis:** The engine journal contains all required fields plus additional ones. The extra fields come from config-portability-contract.md Section 5 (the primary authority for journal schema). The restore-safety-contract.md result schema is a simplified view for GUI consumption.

**Resolution:** This is not a functional violation -- the engine provides a superset. Update restore-safety-contract.md to reference config-portability-contract.md as the authoritative journal schema, or note that additional fields may be present.

---

## Passes

### Profile Contract (profile-contract.md) -- PASS

- `engine/manifest.ps1`: `Test-ProfileManifest` (line ~1254) correctly validates:
  - `version` field exists, is numeric, equals 1
  - `apps` field exists, is array
  - Returns structured result with `Valid`, `Errors`, `Summary`
  - Error codes match contract (FILE_NOT_FOUND, PARSE_ERROR, MISSING_VERSION, etc.)
- `engine/bundle.ps1`: `Resolve-ProfilePath` implements zip -> folder -> bare priority correctly
- `engine/profile-commands.ps1`: Correctly enforces bare-only mutability

### Config Portability Contract (config-portability-contract.md) -- PASS

- `engine/restore.ps1`: Model B source resolution implemented (lines ~619-635)
  - ExportRoot checked first, ManifestDir fallback
- `engine/restore.ps1`: Journal written for non-dry-run restores (lines 969-1034)
  - All 10 required journal fields present (runId, timestamp, manifestPath, manifestDir, exportRoot, entries with resolvedSourcePath, targetPath, targetExistedBefore, backupRequested, backupCreated, backupPath, action, error)
- `engine/export-revert.ps1`: Processes journal entries in reverse order
  - Handles all 3 revert cases: backup restore, created-target deletion, no-op
- `engine/export-capture.ps1`: Implements export (target -> source copy)
- `engine/export-validate.ps1`: Validates export integrity

### Event Contract (event-contract.md) -- PASS (except V1)

- `engine/events.ps1`: Implements all 5 event types (phase, item, summary, error, artifact)
- Required fields (version=1, runId, timestamp, event) added automatically by `Write-StreamingEvent`
- Events written to stderr via `[Console]::Error.WriteLine()`
- NDJSON format with `ConvertTo-Json -Compress`

### CLI JSON Envelope (cli-json-contract.md) -- PASS (except V2, V3)

- `engine/json-output.ps1`: `New-JsonEnvelope` includes all 8 required envelope fields
- `engine/apply.ps1`: Uses `New-JsonEnvelope` for structured output
- `engine/verify.ps1`: Includes error object with VERIFY_FAILED code
- `engine/report.ps1`: Uses `Format-ReportJson` -> `New-JsonEnvelope`

### Capture Artifact Contract (capture-artifact-contract.md) -- PASS (except V2 overlap)

- INV-CAPTURE-5 (zip bundle output): `bundle.ps1` `New-CaptureBundle` produces zip with manifest.jsonc + metadata.json
- INV-CAPTURE-6 (config failures don't block): `capture.ps1` captures config errors in `ConfigsCaptureErrors` but still reports success
- INV-CAPTURE-7 (install-only valid): Supported -- zip without configs/ is valid

### GUI Integration Contract (gui-integration-contract.md) -- PASS (except V3 overlap)

- Thin GUI principle enforced by architecture (CLI does all work)
- Capabilities handshake implemented via `Get-CapabilitiesData`
- Schema versioning present in envelope

---

## Engine Files With No Direct Contract Obligations

The following files were reviewed and have no contract surface area. They are compliant by default:

| File | Purpose |
|------|---------|
| diff.ps1 | Internal provisioning artifact comparison |
| discovery.ps1 | Software discovery via PATH and registry |
| external.ps1 | Mockable wrappers for winget/registry calls |
| logging.ps1 | Human-readable console/file logging |
| parallel.ps1 | RunspacePool-based parallel app install |
| progress.ps1 | Live progress UI rendering |
| shim-template.ps1 | CLI shim for global install |
| trace.ps1 | File tracing for automated module generation |
| snapshot.ps1 | Filesystem snapshot/diff helpers |
| paths.ps1 | Path token expansion and platform detection |
| config-modules.ps1 | Config module catalog and schema validation |
| profile-commands.ps1 | Profile CRUD commands (delegates to manifest.ps1) |

---

## Prioritized Recommendations

### Priority 1 -- Fix Before Next Release

1. **[V1]** Fix ArtifactEvent Kind ValidateSet in `events.ps1:308` -- add `"bundle"` to ValidateSet, update event-contract.md to document bundle as allowed kind.

2. **[V2]** Add 4 missing error codes to `json-output.ps1:271-286` -- ENGINE_CLI_NOT_FOUND, CAPTURE_FAILED, CAPTURE_BLOCKED, MANIFEST_WRITE_FAILED.

### Priority 2 -- Fix Before GUI Integration Ships

3. **[V4]** Implement atomic state writes -- extract temp+move pattern from bundle.ps1 into a shared helper, apply to state.ps1, plan.ps1, restore.ps1.

4. **[V5]** Implement `onConflict` field in restore.ps1 -- required for restore-safety-contract.md GUI flows (per-app toggle, skip-by-default).

### Priority 3 -- Documentation Alignment

5. **[V3]** Update gui-integration-contract.md capabilities example to show `"streaming": true`.

6. **[V6]** Update restore-safety-contract.md to reference config-portability-contract.md as journal schema authority.

---

## Methodology

- All 26 engine files in `engine/` were read in full
- All 7 contract files in `docs/contracts/` were read in full
- Each engine file was checked against every contract for:
  - Required field presence and types
  - Error code completeness
  - Behavioral invariant compliance
  - Schema/format alignment
- Line numbers reference the source as of 2026-02-22
