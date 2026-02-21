# CLI JSON Consistency Audit Report
## Date: 2026-02-22

## Summary
- Contract fields checked: 8 (envelope) + 5 (error) + per-command data fields
- Output paths audited: 55 (all `Write-JsonEnvelope` call sites in bin/endstate.ps1 + engine JSON output paths)
- Conformance issues: 10
- Undocumented fields: 12+

## Contract Requirements

`docs/contracts/cli-json-contract.md` specifies:

1. **Standard Envelope**: Every `--json` output must include `schemaVersion`, `cliVersion`, `command`, `runId`, `timestampUtc`, `success`, `data`, `error` (all required except `error` which is required only when `success` is false).
2. **Error Object**: When `success` is false, `error` must contain `code` (SCREAMING_SNAKE_CASE), `message` (required), and optionally `detail`, `remediation`, `docsKey`.
3. **Error Codes**: 16 documented codes from `MANIFEST_NOT_FOUND` through `SCHEMA_INCOMPATIBLE`.
4. **Schema Version**: Currently `"1.0"`, additive changes are backward-compatible.
5. **Command-specific data shapes**: Defined for `capabilities`, `capture`, `apply`, `verify`, `report`.
6. **RunId format**: `yyyyMMdd-HHmmss`.

## Audit Results

### Envelope Structure

- **Required fields per contract**: `schemaVersion`, `cliVersion`, `command`, `runId`, `timestampUtc`, `success`, `data`, `error`
- **Status**: FAIL

There are two separate JSON envelope implementations:

#### Implementation A: `engine/json-output.ps1:64` (`New-JsonEnvelope`)
Produces: `schemaVersion`, `cliVersion`, `command`, `runId`, `timestampUtc`, `success`, `data`, `error`
- **Status**: PASS -- all 8 required fields present.
- Used by: `engine/apply.ps1:320`, `engine/apply.ps1:712`, `engine/verify.ps1:235`, `engine/report.ps1:376`

#### Implementation B: `bin/endstate.ps1:3564` (`Write-JsonEnvelope`)
Produces: `schemaVersion`, `cliVersion`, `command`, `timestampUtc`, `success`, `data`, `error`
- **Status**: FAIL -- **`runId` field is MISSING**. This is a required field per the contract.
- Used by: 45+ call sites in bin/endstate.ps1 (capture, apply error paths, verify error paths, report, validate, module draft, module snapshot, profile subcommands, capabilities)

**Impact**: The majority of CLI JSON output paths go through `Write-JsonEnvelope` and are missing the `runId` field. Only the engine-level apply/verify success paths (which use `New-JsonEnvelope`) include it.

### Timestamp Format

- **Contract example**: `"2024-12-20T14:30:52Z"`
- **`engine/json-output.ps1:105`**: `.ToString("yyyy-MM-ddTHH:mm:ssZ")` -- matches contract
- **`bin/endstate.ps1:3593`**: `.ToString("o")` -- produces `"2024-12-20T14:30:52.1234567Z"` with fractional seconds
- **`bin/endstate.ps1:3150`** (Invoke-ReportCore file write): `.ToString("o")` -- same fractional seconds
- **Status**: WARN -- technically valid ISO 8601 but inconsistent with contract examples and between the two implementations.

### RunId Format

- **Contract**: `yyyyMMdd-HHmmss`
- **`engine/json-output.ps1:49-62`** (`Get-RunId`): Produces `yyyyMMdd-HHmmss-MACHINE` (appends uppercase machine name)
- **`bin/endstate.ps1:Write-JsonEnvelope`**: Does not produce `runId` at all
- **Status**: FAIL -- format deviates from contract by appending machine suffix.

### Error Codes

- **Contract requirement**: SCREAMING_SNAKE_CASE from the documented set of 16 codes.
- **Status**: FAIL -- multiple undocumented codes in use, several documented codes missing from constant map.

#### Undocumented error codes used in `bin/endstate.ps1`:
| Code | Location |
|------|----------|
| `BUNDLE_EXTRACT_FAILED` | bin/endstate.ps1:3858, 4014 |
| `ENGINE_SCRIPT_NOT_FOUND` | bin/endstate.ps1:3883, 4036, 4236, 4525, 4357 |
| `MISSING_PATH` | bin/endstate.ps1:4215 |
| `MISSING_TRACE_PATH` | bin/endstate.ps1:4312 |
| `MISSING_OUT_PATH` | bin/endstate.ps1:4326 |
| `MISSING_NAME` | bin/endstate.ps1:4573 |

#### Undocumented error codes in `engine/json-output.ps1` constant map:
| Code | Location |
|------|----------|
| `INVALID_ARGUMENT` | engine/json-output.ps1:284 |
| `RUN_NOT_FOUND` | engine/json-output.ps1:285 |

#### Contract error codes NOT in `engine/json-output.ps1` `$script:ErrorCodes` map:
| Code | Contract Description |
|------|---------------------|
| `MANIFEST_WRITE_FAILED` | Manifest file could not be written or is empty |
| `ENGINE_CLI_NOT_FOUND` | Engine CLI not found |
| `CAPTURE_FAILED` | Capture operation failed |
| `CAPTURE_BLOCKED` | Capture blocked by guardrail |

Note: `CAPTURE_FAILED` and `CAPTURE_BLOCKED` are used inline in bin/endstate.ps1:3772 as string literals but are not registered in the `$script:ErrorCodes` map.

#### Error construction pattern inconsistency
- `engine/json-output.ps1` provides `New-JsonError` with `code`, `message`, `detail`, `remediation`, `docsKey` fields and only includes optional fields when non-null.
- `bin/endstate.ps1` constructs error objects as raw hashtables (e.g., `@{ code = "..."; message = "..." }`) without using `New-JsonError`, bypassing optional field filtering. None of the 45+ error paths in bin/endstate.ps1 include `remediation` or `docsKey`.

### --json Flag Threading

- **Parsing**: `bin/endstate.ps1:111` declares `[switch]$Json`. Additional parsing at line 192 (`--json` in manual arg loop) and line 286 (regex fallback on `$MyInvocation.Line`).
- **Threading**: `$Json` is checked inline via `if ($Json)` in each command handler block. It is NOT passed to engine functions -- instead, engine functions (apply, verify) receive their own `$OutputJson` parameter from the calling code.
- **Status**: PASS for parsing; the flag is correctly detected. However, the dual-path architecture (bin generates envelope for errors, engine generates envelope for success) creates inconsistency.

### Capabilities Command

- **Contract**: Specifies structured `supportedSchemaVersions` object, `commands` map with per-command `{ supported, flags }` objects, `features` object, and `platform` object.
- **Status**: FAIL -- severe divergence.

#### Contract-specified structure (from `engine/json-output.ps1:202-268`, `Get-CapabilitiesData`):
```
data.supportedSchemaVersions = { min: "1.0", max: "1.0" }
data.commands.<cmd> = { supported: true, flags: [...] }
data.features = { streaming: true, streamingFormat: "jsonl", parallelInstall: true, configModules: true, jsonOutput: true }
data.platform = { os: "windows", drivers: ["winget"] }
```
This function exists but is **never called** by the CLI.

#### Actual output (`bin/endstate.ps1:4825-4855`):
```
data.commands = ["bootstrap", "capture", "apply", ...] (flat string array)
data.version = "0.1.0-dev"
data.supportedFlags = { apply: [...], verify: [...], ... }
```
MISSING: `supportedSchemaVersions`, `features`, `platform` sections.

### Capture Command Data Shape

- **Contract**: `outputPath`, `outputFormat`, `sanitized`, `isExample`, `counts` (with specific sub-fields), `appsIncluded`, `configsIncluded`, `configsSkipped`, `configsCaptureErrors`, `captureWarnings`
- **Implementation** (`bin/endstate.ps1:3732-3777`): Largely matches, with `outputPath`, `outputFormat`, `sanitized`, `isExample`, `counts`, `appsIncluded`, `captureWarnings`, `configsIncluded`, `configsSkipped`, `configsCaptureErrors`
- **Status**: PASS (data shape for capture is the closest to contract compliance)
- **Note**: The `counts` sub-field structure depends on what `Invoke-CaptureCore` returns; contract specifies `totalFound`, `included`, `skipped`, `filteredRuntimes`, `filteredStoreApps`, `sensitiveExcludedCount` but actual sub-fields were not verified against capture engine internals.

### Apply Command Data Shape

- **Contract**: `dryRun`, `manifest` `{ path, name, hash }`, `summary` `{ total, success, skipped, failed }`, `actions[]` `{ type, id, ref, status, message }`, `runId`, `stateFile`, `logFile`, `eventsFile` (optional)
- **Implementation** (`engine/apply.ps1:275-318`): Matches contract fields AND adds undocumented fields.
- **Status**: WARN

#### Undocumented fields in apply output (`engine/apply.ps1:289-297`):
| Field | Description |
|-------|-------------|
| `counts` | GUI-specific counters: `total`, `installed`, `alreadyInstalled`, `skippedFiltered`, `failed` |
| `items` | GUI-specific per-app array with `id`, `driver`, `status`, `reason`, `message` |

Additionally, the bin/endstate.ps1 apply success path (`bin/endstate.ps1:3920-3938`) constructs its OWN data object with different fields: `manifestPath`, `installed`, `upgraded`, `skipped`, `failed`, `dryRun`, `counts`, `items`. This differs from both the contract AND the engine output.

### Verify Command Data Shape

- **Contract**: `manifest` `{ path, name }`, `summary` `{ total, pass, fail }`, `results[]` `{ type, id/ref/verifyType/path, status, message }`, `runId`, `stateFile`, `logFile`, `eventsFile` (optional)
- **Implementation**: TWO different verify JSON paths exist.

#### Path 1: `engine/verify.ps1:168-236` (called when engine handles JSON)
Produces: `manifest`, `summary`, `results`, `runId`, `stateFile`, `logFile` -- **matches contract**.

#### Path 2: `bin/endstate.ps1:4073-4085` (called when bin handles JSON after `Invoke-VerifyCore`)
Produces: `manifestPath`, `okCount`, `missingCount`, `versionMismatches`, `extraCount`, `missingApps`, `versionMismatchApps`, `items`
- **Status**: FAIL -- completely different field names and structure from contract.

### Report Command Data Shape

- **Contract**: `data.reports[]` with `runId`, `timestamp`, `command`, `dryRun`, `manifest` `{ name, path, hash }`, `summary` `{ success, skipped, failed }`, `stateFile`
- **Implementation** (`bin/endstate.ps1:3085-3172`, `Invoke-ReportCore`): Produces `hasState`, `state` (with `schemaVersion`, `lastApplied`, `lastVerify`, `appsObserved`), `manifest` (optional), `drift` (optional)
- **Status**: FAIL -- completely different structure. No `reports` array, no per-run entries.

Note: `engine/report.ps1:340-378` (`Write-ReportJson`) does produce the contract-compliant `reports` array structure, but this function is not called by the bin/endstate.ps1 report handler.

### Doctor Command

- **Contract**: Lists `doctor` with `--json` flag support.
- **Implementation** (`bin/endstate.ps1:4143-4148`): No `$Json` check, no JSON output path.
- **Status**: FAIL -- `--json` flag is ignored; no JSON envelope emitted.

### Schema Versioning

- **Contract**: Schema version `"1.0"`, consistent across all outputs.
- **`engine/json-output.ps1:14`**: `$script:SchemaVersion = "1.0"` -- consistent.
- **`bin/endstate.ps1:3590`**: Hardcoded `"1.0"` -- consistent.
- **`bin/endstate.ps1:3147`** (Invoke-ReportCore file write): Hardcoded `"1.0"` -- consistent.
- **Status**: PASS -- schema version string is consistent.

### Capabilities Feature Flags

- **Contract**: `streaming: false`, `parallelInstall: true`, `configModules: true`
- **`engine/json-output.ps1:254-259`**: `streaming: true`, `streamingFormat: "jsonl"`, `parallelInstall: true`, `configModules: true`, `jsonOutput: true`
- **Status**: FAIL (in `Get-CapabilitiesData`) -- `streaming` changed from `false` to `true`, added `streamingFormat` and `jsonOutput` not in contract. But this function is never called, so the practical impact is nil. The actual capabilities output (`bin/endstate.ps1:4825-4855`) omits `features` entirely.

### JSON Serialization Depth

- **`engine/json-output.ps1:183`**: `-Depth 20`
- **`bin/endstate.ps1:3600`** (`Write-JsonEnvelope`): `-Depth 10 -Compress`
- **`bin/endstate.ps1:3155`** (Invoke-ReportCore file write): `-Depth 10`
- **Status**: WARN -- inconsistent depth. `-Depth 10` may truncate deeply nested data; `-Compress` produces single-line output vs. pretty-printed.

### Undocumented Commands Producing JSON

The following commands produce `--json` output but are NOT documented in the contract:
| Command | Envelope `command` field value |
|---------|-------------------------------|
| validate | `"validate"` |
| module draft | `"module draft"` |
| module snapshot | `"module snapshot"` |
| profile new | `"profile new"` |
| profile exclude | `"profile exclude"` |
| profile exclude-config | `"profile exclude-config"` |
| profile add | `"profile add"` |
| profile show | `"profile show"` |
| profile list | `"profile list"` |

## Violations

1. **V-001** [CRITICAL] `bin/endstate.ps1:3589-3597` -- `Write-JsonEnvelope` omits required `runId` field from envelope. Affects 45+ output paths.

2. **V-002** [CRITICAL] `bin/endstate.ps1:4825-4855` -- `capabilities --json` output structure completely diverges from contract. Returns flat command list and `supportedFlags` map instead of per-command `{ supported, flags }` objects. Missing `supportedSchemaVersions`, `features`, `platform`.

3. **V-003** [MAJOR] `bin/endstate.ps1:4073-4085` -- Verify success JSON data shape uses non-contract field names (`okCount`, `missingCount`, `versionMismatches`, `extraCount`, `missingApps`, `versionMismatchApps`, `items`) instead of contract-specified `manifest`, `summary`, `results`.

4. **V-004** [MAJOR] `bin/endstate.ps1:3104-3172` -- Report JSON data shape uses `hasState`/`state`/`drift` structure instead of contract-specified `reports[]` array.

5. **V-005** [MODERATE] `bin/endstate.ps1:3920-3938` -- Apply success JSON from bin-level handler uses non-contract fields (`manifestPath`, `installed`, `upgraded`, `skipped`, `failed`) instead of contract-specified `manifest`, `summary`, `actions[]`.

6. **V-006** [MODERATE] Multiple undocumented error codes: `BUNDLE_EXTRACT_FAILED`, `ENGINE_SCRIPT_NOT_FOUND`, `MISSING_PATH`, `MISSING_TRACE_PATH`, `MISSING_OUT_PATH`, `MISSING_NAME`, `INVALID_ARGUMENT`, `RUN_NOT_FOUND`.

7. **V-007** [MODERATE] Contract error codes `MANIFEST_WRITE_FAILED`, `ENGINE_CLI_NOT_FOUND`, `CAPTURE_FAILED`, `CAPTURE_BLOCKED` missing from `engine/json-output.ps1:271-286` error code constant map.

8. **V-008** [MINOR] `bin/endstate.ps1:4143-4148` -- `doctor --json` not implemented; `$Json` flag is ignored.

9. **V-009** [MINOR] `engine/json-output.ps1:54` -- `Get-RunId` produces `yyyyMMdd-HHmmss-MACHINE` format, deviating from contract-specified `yyyyMMdd-HHmmss`.

10. **V-010** [MINOR] `bin/endstate.ps1:3593` vs `engine/json-output.ps1:105` -- Timestamp format inconsistency: `.ToString("o")` vs `.ToString("yyyy-MM-ddTHH:mm:ssZ")`.

## Recommendations

1. **Unify envelope creation**: Retire `Write-JsonEnvelope` in `bin/endstate.ps1` and route all JSON output through `engine/json-output.ps1` functions (`New-JsonEnvelope` + `Write-JsonOutput`). This eliminates V-001 and V-010.

2. **Wire `Get-CapabilitiesData`**: Replace the ad-hoc capabilities data at `bin/endstate.ps1:4829-4853` with a call to `Get-CapabilitiesData` from `engine/json-output.ps1`. Update either the implementation or the contract to align. This fixes V-002.

3. **Align verify and report data shapes**: Either update `bin/endstate.ps1` verify/report handlers to use the contract-compliant engine functions (`engine/verify.ps1`, `engine/report.ps1:Write-ReportJson`), or update the contract to document the actual output shapes. This fixes V-003 and V-004.

4. **Align apply bin-level handler**: The bin-level apply success path should produce the same shape as `engine/apply.ps1` or the contract. This fixes V-005.

5. **Document or register all error codes**: Either add the 8 undocumented codes to the contract, or map them to existing standard codes. Add the 4 missing contract codes to the `$script:ErrorCodes` map. Use `New-JsonError` consistently instead of raw hashtables. This fixes V-006 and V-007.

6. **Implement `doctor --json`**: Add JSON envelope output for the doctor command. This fixes V-008.

7. **Standardize RunId format**: Either update the contract to document the `-MACHINE` suffix or remove it from `Get-RunId`. This fixes V-009.

8. **Document new commands**: Add contract sections for `validate`, `module draft`, `module snapshot`, and `profile` subcommands, or explicitly note them as unstable/uncontracted.

9. **Standardize serialization depth**: Use consistent `-Depth 20` across all paths for safety with nested data structures.
