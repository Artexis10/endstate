# Consolidated Engine Audit Findings
## Date: 2026-02-22

Cross-referenced findings from 5 audit agents, deduplicated and prioritized.

---

## Priority 0 — Runtime Bugs (Fix Immediately)

### F-001 [CRITICAL] ArtifactEvent ValidateSet rejects "bundle"
- **Source**: ContractGuard V1
- **Files**: `engine/events.ps1:308`, `engine/capture.ps1:630`
- **Issue**: `Write-ArtifactEvent` parameter `[ValidateSet("manifest")]` rejects `"bundle"` passed by `capture.ps1:630`. Runtime crash during capture bundle artifact emission.
- **Fix**: Expand ValidateSet to `("manifest", "bundle")`, update `docs/contracts/event-contract.md` to document `"bundle"` as allowed kind.

### F-002 [CRITICAL] CLI Write-JsonEnvelope missing runId field
- **Source**: ConsistencyChecker V-001
- **Files**: `bin/endstate.ps1:3564-3597`
- **Issue**: `Write-JsonEnvelope` in bin/ omits the contract-required `runId` field. Affects 45+ JSON output paths. The contract-compliant `New-JsonEnvelope` in `engine/json-output.ps1` is bypassed.
- **Fix**: Add `runId` to `Write-JsonEnvelope`, or retire it in favor of `engine/json-output.ps1:New-JsonEnvelope`.

### F-003 [CRITICAL] Capabilities --json diverges from contract
- **Source**: ConsistencyChecker V-002
- **Files**: `bin/endstate.ps1:4825-4855` vs `engine/json-output.ps1:202-268`
- **Issue**: CLI returns flat command list + `supportedFlags` map. Contract specifies per-command `{ supported, flags }` objects + `supportedSchemaVersions` + `features` + `platform`. The compliant `Get-CapabilitiesData` function exists but is never called.
- **Fix**: Wire `Get-CapabilitiesData` into the capabilities command handler.

### F-004 [P0] Module apps.powertoys broken restore path
- **Source**: ModuleValidator P0
- **Files**: `modules/apps/powertoys/module.jsonc`
- **Issue**: `restore[0].source` uses `./apps/powertoys` instead of `./payload/apps/powertoys`. Restore will fail at runtime.
- **Fix**: Change source prefix to `./payload/apps/powertoys`.

### F-005 [P0] Module apps.msi-afterburner hardcoded paths + wrong prefix
- **Source**: ModuleValidator P0
- **Files**: `modules/apps/msi-afterburner/module.jsonc`
- **Issue**: Hardcoded `C:\Program Files (x86)` paths, wrong `./configs/` source prefix (should be `./payload/apps/`), missing `excludeGlobs`.
- **Fix**: Full module overhaul — use `%ProgramFiles(x86)%`, fix source prefix, add `excludeGlobs`.

---

## Priority 1 — Correctness & Safety (Fix Before Next Release)

### F-006 [MEDIUM] Missing 4 error codes in json-output.ps1
- **Source**: ContractGuard V2, ConsistencyChecker V-007
- **Files**: `engine/json-output.ps1:271-286`
- **Issue**: Missing `ENGINE_CLI_NOT_FOUND`, `CAPTURE_FAILED`, `CAPTURE_BLOCKED`, `MANIFEST_WRITE_FAILED` from `$script:ErrorCodes`. Fallback to `INTERNAL_ERROR` masks capture-specific failures.
- **Fix**: Add 4 missing codes to the hashtable.

### F-007 [MEDIUM] Non-atomic state writes
- **Source**: ContractGuard V4, LandmineHunter #3
- **Files**: `engine/state.ps1:168`, `engine/plan.ps1:~138`, `engine/restore.ps1:967,1033`
- **Issue**: Direct `Out-File` writes without temp+move pattern. Crash during write corrupts state/journal files.
- **Fix**: Extract `Write-AtomicFile` helper from the pattern in `engine/bundle.ps1:380-394`, apply to all state/journal writes.

### F-008 [MEDIUM] Hash normalization mismatch between engine and bin
- **Source**: LandmineHunter #9
- **Files**: `engine/state.ps1:34` vs `bin/endstate.ps1:450`
- **Issue**: `engine/state.ps1:Get-ManifestHash` uses raw `Get-FileHash` (includes CRLF). `bin/endstate.ps1:Get-ManifestHash` normalizes CRLF→LF first. Different hashes for the same file → false drift detection.
- **Fix**: Add CRLF normalization to `engine/state.ps1:Get-ManifestHash`, or consolidate into one shared function.

### F-009 [MEDIUM] Bootstrap Copy-Item -Recurse nesting
- **Source**: LandmineHunter #4
- **Files**: `bin/endstate.ps1:870,905,938,971`
- **Issue**: 4 `Copy-Item -Recurse` calls in bootstrap update path without `Remove-Item` guard. Re-bootstrap creates nested directories (e.g., `bin/engine/engine/`).
- **Fix**: Add `Remove-Item -Recurse -Force` before each `Copy-Item -Recurse`.

### F-010 [MAJOR] Verify JSON data shape diverges from contract
- **Source**: ConsistencyChecker V-003
- **Files**: `bin/endstate.ps1:4073-4085`
- **Issue**: Bin-level verify handler outputs `okCount`, `missingCount`, `versionMismatches` etc. instead of contract-specified `manifest`, `summary`, `results[]`.
- **Fix**: Route verify JSON through `engine/verify.ps1` contract-compliant output path.

### F-011 [MAJOR] Report JSON data shape diverges from contract
- **Source**: ConsistencyChecker V-004
- **Files**: `bin/endstate.ps1:3104-3172`
- **Issue**: Outputs `hasState`/`state`/`drift` instead of contract-specified `reports[]` array. Contract-compliant `Write-ReportJson` in `engine/report.ps1` exists but is not called.
- **Fix**: Wire `engine/report.ps1:Write-ReportJson` into report command handler.

### F-012 [P1] Module apps.ableton-live capture/restore asymmetry
- **Source**: ModuleValidator P1
- **Files**: `modules/apps/ableton-live/module.jsonc`
- **Issue**: Capture grabs 2 subdirectories (Presets, Defaults) but restore overwrites entire `User Library/` directory. Round-trip causes data loss.
- **Fix**: Either split restore into 2 subdirectory entries, or expand capture to whole directory.

---

## Priority 2 — Consistency & Hygiene

### F-013 [MODERATE] 8 undocumented error codes in use
- **Source**: ConsistencyChecker V-006
- **Files**: `bin/endstate.ps1` (various), `engine/json-output.ps1:284-285`
- **Issue**: `BUNDLE_EXTRACT_FAILED`, `ENGINE_SCRIPT_NOT_FOUND`, `MISSING_PATH`, `MISSING_TRACE_PATH`, `MISSING_OUT_PATH`, `MISSING_NAME`, `INVALID_ARGUMENT`, `RUN_NOT_FOUND` used but not in contract.
- **Fix**: Add to contract or map to standard codes.

### F-014 [MODERATE] Apply bin-level handler uses non-contract fields
- **Source**: ConsistencyChecker V-005
- **Files**: `bin/endstate.ps1:3920-3938`
- **Fix**: Align with `engine/apply.ps1` output or contract spec.

### F-015 [LOW] Capabilities streaming field stale in contract
- **Source**: ContractGuard V3
- **Files**: `docs/contracts/gui-integration-contract.md:194`
- **Fix**: Update contract example to `"streaming": true`.

### F-016 [LOW] Restore safety contract schema differences
- **Source**: ContractGuard V6
- **Fix**: Update `docs/contracts/restore-safety-contract.md` to reference `config-portability-contract.md`.

### F-017 [LOW] PS 5.1 null comparison in test helper
- **Source**: LandmineHunter #2
- **Files**: `tests/test-gui-contract.ps1:97,120,124`
- **Fix**: Change `$value -eq $null` to `$null -eq $value`.

### F-018 [LOW] Hardcoded path in utility script
- **Source**: LandmineHunter #8
- **Files**: `scripts/update-ruleset-bundle.ps1:3`
- **Fix**: Use `$PSScriptRoot`-relative path.

### F-019 [LOW] Fragile manual JSONC stripping
- **Source**: LandmineHunter #1
- **Files**: `scripts/batch-validate.ps1:160`
- **Fix**: Use `Read-JsoncFile` instead of manual regex.

### F-020 [LOW] 3 modules missing `winget: []` in matches
- **Source**: ModuleValidator P1
- **Files**: `modules/apps/premiere-pro/`, `modules/apps/after-effects/`, `modules/apps/evga-precision-x1/`
- **Fix**: Add `"winget": []` to matches object.

### F-021 [LOW] doctor --json not implemented
- **Source**: ConsistencyChecker V-008
- **Files**: `bin/endstate.ps1:4143-4148`
- **Fix**: Add JSON output path or remove from contract.

### F-022 [INFO] RunId format includes machine suffix
- **Source**: ConsistencyChecker V-009
- **Fix**: Update contract to document `-MACHINE` suffix or remove it.

### F-023 [INFO] Timestamp format inconsistency
- **Source**: ConsistencyChecker V-010
- **Fix**: Standardize on `yyyy-MM-ddTHH:mm:ssZ` across both envelope implementations.

### F-024 [INFO] JSON serialization depth inconsistency
- **Source**: ConsistencyChecker
- **Fix**: Standardize on `-Depth 20` across all paths.

---

## Test Coverage Gaps (Separate Track)

| Priority | Engine File | Functions Untested | Risk |
|----------|------------|-------------------|------|
| **P0** | `apply.ps1` | `Invoke-Apply`, `Invoke-ApplyFromPlan` | Core install pipeline — zero tests |
| **P0** | `verify.ps1` | `Invoke-Verify`, `Invoke-VerifyItem` | Core verification — zero tests |
| **P1** | `state.ps1` | `Save-RunState`, `Get-LastRunState`, `Get-RunHistory`, `Get-ExpandedManifestHash` | State persistence — 1/5 tested |
| **P1** | `restore.ps1` | `Test-SensitivePath`, `Backup-RestoreTarget`, `Copy-DirectoryWithExcludes` + 4 more | Safety functions — 6/13 tested |
| **P1** | `capture.ps1` | `Invoke-Capture`, `Test-WingetAvailable` + 4 more | Orchestrator — 4/11 tested |
| **P1** | `export-validate.ps1` | `Test-PathWritable`, `Invoke-ExportValidate` | Zero tests |
| **P2** | `parallel.ps1` | All 4 functions | Parallel safety — zero tests |
| **P2** | `report.ps1` | 7/9 untested | Presentation functions |

---

## Root Cause: Dual Implementation Pattern

The single biggest systemic issue is the **dual implementation** in `bin/endstate.ps1` vs `engine/`:

- `Write-JsonEnvelope` (bin) vs `New-JsonEnvelope` (engine) — different fields, depth, format
- `capabilities` handler (bin) vs `Get-CapabilitiesData` (engine) — completely different shape
- `verify` handler (bin) vs `engine/verify.ps1` JSON output — different field names
- `report` handler (bin) vs `engine/report.ps1:Write-ReportJson` — different structure

**Recommended systemic fix**: Retire all JSON output construction in `bin/endstate.ps1` and route through `engine/json-output.ps1` as the single source of truth. This would fix F-002, F-003, F-010, F-011, F-014, F-022, F-023, F-024 in one architectural change.

---

*Consolidated from 5 agent reports: ContractGuard, ModuleValidator, ConsistencyChecker, LandmineHunter, TestCoverage.*
