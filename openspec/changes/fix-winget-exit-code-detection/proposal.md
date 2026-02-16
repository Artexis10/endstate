## Why

The sandbox validation harness falsely reports install failure for apps like PowerToys despite winget printing "Successfully installed" in stdout. The exit code detection at line 1296 returns a value that is not `$null` (bypasses the null guard), not `0` (triggers failure), and stringifies to empty — likely `AutomationNull` or a GC'd handle artifact. The current logic gates on exit code first and only falls back to output matching for `$null`, which doesn't catch this edge case. Additionally, `$ProgressPreference` is never set to `SilentlyContinue`, a known PowerShell footgun in non-interactive scripts.

## What Changes

- Restructure winget install success detection to be **output-based primary, exit-code secondary** (inverted from current logic)
- Wrap `$proc.ExitCode` read in try/catch for defensive access
- Normalize exit code: treat null/empty/non-integer as unknown rather than failure
- Add diagnostic logging of exit code value and type
- Add `$ProgressPreference = 'SilentlyContinue'` at script scope

## Capabilities

### New Capabilities

_(none)_

### Modified Capabilities

- `sandbox-validation`: The install success detection requirement changes — output content ("Successfully installed") becomes the primary success signal, with exit code as secondary confirmation. This affects the "Required Artifacts on PASS" scenario's `installExitCode` field behavior (may be overridden to 0 when output confirms success).

## Impact

- **File:** `sandbox-tests/discovery-harness/sandbox-validate.ps1` (lines ~1295–1355)
- **Behavior:** Apps that install successfully but return non-standard exit codes will now be correctly detected as PASS
- **No API/contract changes:** `result.json` schema unchanged; `installExitCode` field may now contain `0` (overridden) where it previously contained an empty/unknown value
