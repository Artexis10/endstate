## Context

`sandbox-validate.ps1` runs inside Windows Sandbox to validate module capture/restore cycles. The winget install path (lines ~1254–1366) uses `Start-Process -PassThru` and reads `$proc.ExitCode` after `WaitForExit()`. Despite pinning the process handle at line 1267, the exit code sometimes returns a value that is not `$null`, not `0`, and stringifies to empty — likely `AutomationNull` or a stale handle artifact. The current logic checks exit code first and only falls back to output matching when exit code is `$null`, so this edge case falls through to the failure branch.

Additionally, `$ProgressPreference` is never set to `SilentlyContinue`, which can cause spurious progress-bar rendering issues in non-interactive sandbox environments.

## Goals / Non-Goals

**Goals:**
- Eliminate false install failures caused by non-standard exit code values from `$proc.ExitCode`
- Make stdout content ("Successfully installed") the primary success signal
- Add defensive exit code reading with type diagnostics for future debugging
- Set `$ProgressPreference = 'SilentlyContinue'` at script scope

**Non-Goals:**
- Refactoring the polling loop or process startup logic
- Changing the offline installer path
- Modifying helper functions (Write-FatalError, Write-Pass, etc.)
- Changing the host-side script (`scripts/sandbox-validate.ps1`)

## Decisions

### 1. Output-based primary, exit-code secondary

**Decision:** Check `$combinedOutput -match 'Successfully installed'` before checking exit code value.

**Rationale:** Winget's stdout is the most reliable success signal in sandbox environments. Exit codes can be corrupted by .NET GC, AutomationNull, or sandbox process isolation. The previous approach (exit code primary, output fallback only for `$null`) missed the case where exit code is non-null but non-integer.

**Alternative considered:** Stronger handle pinning or GC suppression — rejected because the root cause is environmental and not reliably fixable at the .NET level.

### 2. Defensive try/catch around ExitCode read

**Decision:** Wrap `$proc.ExitCode` in try/catch, defaulting to `$null` on failure.

**Rationale:** If the handle is truly gone, accessing `.ExitCode` can throw. Catching this prevents unhandled exceptions while the output-based detection handles success determination.

### 3. Type-aware exit code normalization

**Decision:** Only treat exit code as zero if it is non-null, is `[int]`, and equals `0`. Everything else is "unknown".

**Rationale:** This catches AutomationNull, empty strings, and other edge-case types that bypass `$null -eq` checks in PowerShell.

## Risks / Trade-offs

- **Risk:** An app that prints "Successfully installed" in error output but actually failed → **Mitigation:** This string is winget's canonical success message; false positives are extremely unlikely. The exit code is still logged for diagnostics.
- **Risk:** Overriding exit code to 0 when output says success could mask real issues → **Mitigation:** A `NOTE:` log line is emitted when override occurs, preserving the original value for debugging.
