# Landmine Hunter Audit Report
## Date: 2026-02-22

## Summary
- Patterns searched: 10
- Total hits investigated: 168
- Confirmed violations: 7
- False positives: 161

## Findings by Landmine Type

### 1. Raw ConvertFrom-Json on JSONC Files
- **Risk**: Parse failure on JSONC comments (// and /* */)
- **Documented in**: CLAUDE.md (Critical Landmines #2), AGENTS.md (EngineDev Landmines #2)
- **Hits investigated**: 168 across all .ps1 files
- **Confirmed violations**: 1

| File | Line | Code | Verdict |
|------|------|------|---------|
| `scripts/batch-validate.ps1` | 160 | `$module = $moduleContent \| ConvertFrom-Json` | **VIOLATION** - parses JSONC module files with manual comment stripping (`-replace '//.*$'`) which is fragile (misses strings containing //) |
| `bin/endstate.ps1` | 391 | `return $content \| ConvertFrom-Json` | False positive - `Read-EndstateState` reads `state.json` (plain JSON, not JSONC) |
| `bin/endstate.ps1` | 2262 | `return $jsonContent \| ConvertFrom-Json` | False positive - comments already stripped via `-replace` before parsing |
| `bin/endstate.ps1` | 2593, 2744 | `$rawManifest = $jsonContent \| ConvertFrom-Json` | False positive - comments stripped before parsing |
| `bin/endstate.ps1` | 3414 | `$incoming = $incomingContent \| ConvertFrom-Json` | False positive - reads `state.json` import file (plain JSON) |
| `engine/state.ps1` | 186, 207 | `ConvertFrom-Json` on state files | False positive - state files are plain JSON |
| `engine/manifest.ps1` | 475, 478, 515, 518 | `ConvertFrom-Json` inside `Read-JsoncFile`/`ConvertFrom-Jsonc` | False positive - this IS the safe wrapper itself; comments stripped first |
| `engine/capture.ps1` | 867 | `ConvertFrom-Json` on winget-export.json | False positive - winget export is plain JSON |
| `engine/bundle.ps1` | 478 | `ConvertFrom-Json` on metadata.json | False positive - metadata files are plain JSON |
| `restorers/merge-json.ps1` | 261 | `ConvertFrom-Json` inside `ConvertFrom-JsoncContent` | False positive - this IS a JSONC-aware wrapper |
| `engine/trace.ps1` | 118 | `ConvertFrom-Json` on snapshot JSON | False positive - snapshot files are plain JSON |
| `engine/report.ps1` | 108 | `ConvertTo-Json \| ConvertFrom-Json` roundtrip | False positive - converting in-memory object |
| `tests/**/*.ps1` | various | `ConvertFrom-Json` in test assertions | False positive - tests parse known JSON strings/outputs |
| `sandbox-tests/**/*.ps1` | various | `ConvertFrom-Json` on result/diff JSON files | False positive - these are plain JSON files |

**Note**: `bin/endstate.ps1` lines 2262, 2593, 2744 use manual comment stripping (`-replace '//.*$', '' -replace '/\*[\s\S]*?\*/', ''`) before `ConvertFrom-Json`. While functional, this is fragile -- the regex `//.*$` can incorrectly strip `//` inside JSON string values (e.g., URLs). These should ideally use `Read-JsoncFile` or `ConvertFrom-Jsonc` from `engine/manifest.ps1`, but they work correctly for the current manifest schemas that don't contain `//` in string values. Classified as **low-risk technical debt** rather than violations.

---

### 2. PS 5.1 Null Comparison ($value -eq $null)
- **Risk**: When `$value` is an array, `$value -eq $null` filters the array instead of scalar null check
- **Documented in**: AGENTS.md (EngineDev Landmines #8)
- **Hits investigated**: 3
- **Confirmed violations**: 3

| File | Line | Code | Verdict |
|------|------|------|---------|
| `tests/test-gui-contract.ps1` | 97 | `$expectedValue -ne $null` | **VIOLATION** - should be `$null -ne $expectedValue` |
| `tests/test-gui-contract.ps1` | 120 | `$json.error -ne $null` | **VIOLATION** - should be `$null -ne $json.error` |
| `tests/test-gui-contract.ps1` | 124 | `$json.error -eq $null` | **VIOLATION** - should be `$null -eq $json.error` |

**Note**: All 3 hits are in `tests/test-gui-contract.ps1`, a test helper script. The risk is lower in test code (values are unlikely to be arrays), but it still violates the documented convention. No violations found in engine code.

---

### 3. State Atomicity (Direct Writes Without temp+move)
- **Risk**: Crash during write corrupts state file
- **Documented in**: CLAUDE.md (Critical Landmines #6), AGENTS.md (EngineDev Landmines #6)
- **Hits investigated**: 2 functions in engine/state.ps1
- **Confirmed violations**: 1

| File | Line | Code | Verdict |
|------|------|------|---------|
| `engine/state.ps1` | 168 | `$state \| ConvertTo-Json -Depth 10 \| Out-File -FilePath $stateFile` | **VIOLATION** - `Save-RunState` writes directly to `$stateFile` without temp+move pattern |
| `bin/endstate.ps1` | 398-419 | `Write-EndstateStateAtomic` | False positive - correctly uses temp file + `Move-Item` pattern |

**Analysis**: `engine/state.ps1:Save-RunState` writes run history to `state/$RunId.json` directly via `Out-File`. While each run file is unique (keyed by RunId), a crash during write could leave a corrupted partial file. The companion function `Write-EndstateStateAtomic` in `bin/endstate.ps1` correctly implements the temp+move pattern for the main `state.json`.

---

### 4. Copy-Item -Recurse Without Remove-Item Guard
- **Risk**: When destination directory exists, `Copy-Item -Recurse` nests source inside dest
- **Documented in**: CLAUDE.md (Critical Landmines #5), AGENTS.md (EngineDev Landmines #5)
- **Hits investigated**: 14
- **Confirmed violations**: 1 (with 4 instances)

| File | Line | Code | Verdict |
|------|------|------|---------|
| `bin/endstate.ps1` | 870 | `Copy-Item -Path $sourceEngineDir -Destination $binDir -Recurse -Force` | **VIOLATION** - bootstrap copies engine/ to bin/ without Remove-Item when dest exists |
| `bin/endstate.ps1` | 905 | `Copy-Item -Path $sourceModulesDir -Destination $binDir -Recurse -Force` | **VIOLATION** - same pattern for modules/ |
| `bin/endstate.ps1` | 938 | `Copy-Item -Path $sourcePayloadDir -Destination $binDir -Recurse -Force` | **VIOLATION** - same pattern for payload/ |
| `bin/endstate.ps1` | 971 | `Copy-Item -Path $sourceRestorersDir -Destination $binDir -Recurse -Force` | **VIOLATION** - same pattern for restorers/ |
| `engine/restore.ps1` | 705-708 | `Remove-Item` then `Copy-Item -Recurse` | False positive - correctly guarded |
| `engine/export-revert.ps1` | 257-260 | `Remove-Item` then `Copy-Item -Recurse` | False positive - correctly guarded |
| `engine/export-capture.ps1` | 231-234 | `Remove-Item` then `Copy-Item -Recurse` | False positive - correctly guarded |
| `engine/config-modules.ps1` | 689-690 | `Remove-Item` then `Copy-Item -Recurse` | False positive - correctly guarded |
| `engine/bundle.ps1` | 191-192 | `Remove-Item` then `Copy-Item -Recurse` | False positive - correctly guarded |
| `restorers/helpers.ps1` | 188 | `Copy-Item -Path $Target -Destination $backupPath -Recurse` | False positive - backup copy to new path |
| `restorers/copy.ps1` | 311 | `Copy-Item -Path $expandedTarget -Destination $backupPath -Recurse` | False positive - backup copy to new path |
| `engine/restore.ps1` | 765 | `Copy-Item -Path $Target -Destination $backupPath -Recurse` | False positive - backup copy to new path |
| `sandbox-tests/.../sandbox-validate.ps1` | 1719 | `Copy-Item -Recurse` without Remove-Item guard | Low risk - sandbox test harness, not production code |
| `sandbox-tests/.../sandbox-validate.ps1` | 1780 | `Copy-Item -Recurse` for backup | False positive - backup copy to new path |

**Analysis**: The bootstrap function in `bin/endstate.ps1` has 4 instances where it copies directories (engine, modules, payload, restorers) to the bin directory during `bootstrap` command. When `$destEngineDir` already exists (the `elseif` branch at line 868), it copies without first removing the destination. This could cause nesting: `bin/engine/engine/`. However, the destination is `$binDir` (not `$destEngineDir`), and `Copy-Item` copies the source directory as a child of the destination, so this actually creates `bin/engine/` correctly on first install. On update, since `bin/engine/` already exists, this WOULD nest to `bin/engine/engine/`. This is a confirmed violation.

---

### 5. $? Instead of $LASTEXITCODE for External Commands
- **Risk**: `$?` is unreliable for external process exit codes in PS 5.1
- **Documented in**: AGENTS.md (EngineDev Landmines #12)
- **Hits investigated**: Full codebase search
- **Confirmed violations**: 0

No instances of `$?` found anywhere in the codebase. The codebase correctly uses `$LASTEXITCODE` for external command exit code checking.

---

### 6. Write-Error for Event Emission
- **Risk**: Events should use `[Console]::Error.WriteLine()`, not `Write-Error` which adds error records
- **Documented in**: AGENTS.md (EngineDev Landmines #9)
- **Hits investigated**: 19
- **Confirmed violations**: 0

| File | Line | Code | Verdict |
|------|------|------|---------|
| `engine/events.ps1` | 126 | `[Console]::Error.WriteLine($json)` | False positive - correctly uses Console.Error |
| `engine/events.ps1` | 255 | `function Write-ErrorEvent` | False positive - this is a function NAME, it internally calls `Write-StreamingEvent` which uses `[Console]::Error.WriteLine()` |
| `engine/apply.ps1` | 71 | `Write-ErrorEvent` | False positive - calls the correctly-implemented event function |
| `scripts/*.ps1` | various | `Write-Error` for actual errors | False positive - legitimate error reporting, not event emission |
| `sandbox-tests/**/*.ps1` | various | `Write-Error` for actual errors | False positive - legitimate error reporting |
| `modules/apps/*/seed.ps1` | various | `Write-Error` in seed scripts | False positive - legitimate error reporting |

**Analysis**: The event system is correctly implemented. `Write-StreamingEvent` at `engine/events.ps1:126` uses `[Console]::Error.WriteLine()`. All `Write-Error` usage elsewhere is for actual error reporting, not event emission.

---

### 7. -EnableRestore Wiring Gaps
- **Risk**: Flag parsed at CLI but not threaded through, causing silent restore skipping
- **Documented in**: CLAUDE.md (Critical Landmines #3), AGENTS.md (EngineDev Landmines #3)
- **Hits investigated**: Full call chain traced
- **Confirmed violations**: 0

**Call chain verified**:
1. `bin/endstate.ps1:99` - declares `[switch]$EnableRestore` parameter
2. `bin/endstate.ps1:3918` - passes `$EnableRestore.IsPresent` to `Invoke-ApplyCore`
3. `bin/endstate.ps1:1455` - `Invoke-ApplyCore` accepts `[bool]$EnableRestore`
4. `bin/endstate.ps1:1659` - checks `if ($EnableRestore -and $manifest.restore...)`
5. `engine/apply.ps1:32` - `Invoke-Apply` accepts `[switch]$EnableRestore`
6. `engine/apply.ps1:136` - checks `if (-not $EnableRestore)`
7. `engine/apply.ps1:392` - `Invoke-ApplyFromPlan` accepts `[switch]$EnableRestore`
8. `engine/apply.ps1:528` - checks `if (-not $EnableRestore)`
9. `engine/restore.ps1:794` - `Invoke-Restore` accepts `[switch]$EnableRestore`
10. `engine/restore.ps1:848` - checks `if (-not $EnableRestore)`
11. `bin/cli.ps1:788` - passes `$EnableRestore.IsPresent` correctly

**Analysis**: The `-EnableRestore` flag is correctly wired through all call paths. Both the main CLI (`bin/endstate.ps1`) and the alternate CLI (`bin/cli.ps1`) correctly thread the flag to `Invoke-ApplyCore`, `Invoke-Apply`, `Invoke-ApplyFromPlan`, and `Invoke-Restore`.

---

### 8. Hardcoded Absolute Paths
- **Risk**: Breaks portability across machines
- **Documented in**: CLAUDE.md (Forbidden Patterns), AGENTS.md (ModuleValidator Path Validity)
- **Hits investigated**: 60+
- **Confirmed violations**: 1

| File | Line | Code | Verdict |
|------|------|------|---------|
| `scripts/update-ruleset-bundle.ps1` | 3 | `$Path = "C:\Users\win-laptop\Desktop\projects\endstate\.windsurf\rules\project-ruleset.md"` | **VIOLATION** - hardcoded user-specific absolute path |
| `tests/unit/Trace.Tests.ps1` | many | `"C:\Users\Test\AppData\Local"` | False positive - test fixture data with fake paths |
| `tests/unit/Discovery.Tests.ps1` | 32, 52, etc. | `"C:\Program Files\Git\cmd\git.exe"` | False positive - test fixture data |
| `tests/unit/PathResolver.Tests.ps1` | 141, 184 | `"C:\Users\test\file.txt"` | False positive - test fixture data |
| `tests/unit/Events.Tests.ps1` | 563 | `"C:\Users\test\.ssh"` | False positive - test fixture data |
| `tests/unit/Restore.Tests.ps1` | 221 | `"C:\Users\test\.ssh\id_rsa"` | False positive - test fixture data |
| `tests/Endstate.Tests.ps1` | 1285, 1300 | `"C:\Users\john\AppData\test.exe"`, `"C:\Program Files\App\app.exe"` | False positive - test input for path detection function |
| `engine/paths.ps1` | 110, 113, 116, 208 | `C:\Users\username\...` | False positive - documentation comments showing example output |
| `modules/apps/*/seed.ps1` | various | `C:\Users\placeholder\...` | False positive - placeholder paths in seed scripts that get replaced at runtime |
| `sandbox-tests/powertoys-afterburner/run.ps1` | 28, 29 | `"C:\Program Files (x86)\MSI Afterburner\..."` | False positive - MSI Afterburner always installs to this fixed path |
| `sandbox-tests/.../sandbox-validate.ps1` | 286 | `C:\Users\User\AppData\...` | False positive - sandbox environment documentation |

**Analysis**: Only `scripts/update-ruleset-bundle.ps1` has a true hardcoded path violation. It references the developer's personal machine path. This is a one-off utility script, not production engine code, but still violates the "no hardcoded absolute paths" rule.

---

### 9. Missing Line Ending Normalization in Hash Computations
- **Risk**: Hash mismatch between CRLF and LF environments
- **Documented in**: CLAUDE.md (Critical Landmines #7), AGENTS.md (EngineDev Landmines #7)
- **Hits investigated**: 3 hash computation sites
- **Confirmed violations**: 1

| File | Line | Code | Verdict |
|------|------|------|---------|
| `bin/endstate.ps1` | 450 | `$normalized = $content -replace "\`r\`n", "\`n"` then hash | False positive - correctly normalizes CRLF to LF |
| `engine/state.ps1` | 34 | `$hash = Get-FileHash -Path $ManifestPath -Algorithm SHA256` | **VIOLATION** - `Get-ManifestHash` hashes the raw file bytes without CRLF normalization |
| `engine/state.ps1` | 99-102 | SHA256 hash of JSON-serialized manifest | False positive - hashes in-memory JSON (deterministic output from `ConvertTo-Json`) |
| `sandbox-tests/.../curate-git.ps1` | 330, 334, etc. | `Get-FileHash` for file comparison | False positive - comparing files on same system, not cross-platform |

**Analysis**: `engine/state.ps1:Get-ManifestHash` uses `Get-FileHash` directly on the file, which hashes raw bytes including `\r\n`. The companion function in `bin/endstate.ps1:Get-ManifestHash` (line 440-458) correctly normalizes CRLF to LF before hashing. These two functions compute DIFFERENT hashes for the same file on Windows. If both are used for drift detection, they will produce false-positive drift signals.

---

### 10. Hashtable Key Checks via Truthy/Falsy Instead of .ContainsKey()
- **Risk**: Truthy/falsy on hashtable values gives wrong results for `$null`, `$false`, `0`, or empty string values
- **Documented in**: AGENTS.md (EngineDev Landmines #8)
- **Hits investigated**: Spot-checked across engine code
- **Confirmed violations**: 0

**Analysis**: The engine code consistently uses `.ContainsKey()` for hashtable membership checks (e.g., `engine/events.ps1:112` uses `$Event.ContainsKey('version')`). No instances of truthy/falsy hashtable key checks were found in production engine code.

---

## Risk Assessment

| # | Landmine | Severity | Location | Risk |
|---|----------|----------|----------|------|
| 1 | State atomicity violation | **HIGH** | `engine/state.ps1:168` | Crash during `Save-RunState` could corrupt run history file |
| 2 | Hash normalization mismatch | **HIGH** | `engine/state.ps1:34` vs `bin/endstate.ps1:450` | Two `Get-ManifestHash` functions produce different hashes for same file on Windows |
| 3 | Bootstrap Copy-Item nesting | **MEDIUM** | `bin/endstate.ps1:870,905,938,971` | `bootstrap` update path nests directories when destination exists |
| 4 | PS 5.1 null comparison | **LOW** | `tests/test-gui-contract.ps1:97,120,124` | Wrong comparison order in test helper (unlikely to hit array case) |
| 5 | Hardcoded absolute path | **LOW** | `scripts/update-ruleset-bundle.ps1:3` | Utility script only usable on one developer's machine |
| 6 | Fragile JSONC stripping | **LOW** | `scripts/batch-validate.ps1:160` | Manual regex comment strip could break on URLs in JSON strings |

## Recommendations (Prioritized)

### Priority 1 (High Risk)
1. **Fix hash normalization in `engine/state.ps1:Get-ManifestHash`**: Add CRLF-to-LF normalization before hashing, matching the pattern in `bin/endstate.ps1:Get-ManifestHash`. Alternatively, consolidate into a single shared function.
2. **Fix state atomicity in `engine/state.ps1:Save-RunState`**: Replace direct `Out-File` with temp file + `Move-Item` pattern, matching the `Write-EndstateStateAtomic` implementation.

### Priority 2 (Medium Risk)
3. **Fix bootstrap Copy-Item nesting in `bin/endstate.ps1`**: Add `Remove-Item -Recurse -Force` before each `Copy-Item -Recurse` in the update branch of the bootstrap function (lines 869-870, 903-905, 936-938, 969-971).

### Priority 3 (Low Risk)
4. **Fix null comparison order in `tests/test-gui-contract.ps1`**: Change `$value -eq $null` to `$null -eq $value` at lines 97, 120, 124.
5. **Fix hardcoded path in `scripts/update-ruleset-bundle.ps1`**: Replace with `$PSScriptRoot`-relative or env var-based path.
6. **Replace manual JSONC stripping in `scripts/batch-validate.ps1`**: Use `Read-JsoncFile` or `ConvertFrom-Jsonc` from `engine/manifest.ps1`.

### No Action Required
- `-EnableRestore` wiring: Correctly threaded through all call paths
- `$?` usage: Not used anywhere; `$LASTEXITCODE` used correctly
- Event emission: Correctly uses `[Console]::Error.WriteLine()`
- Hashtable key checks: Correctly uses `.ContainsKey()`
