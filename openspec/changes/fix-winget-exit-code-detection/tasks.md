## 1. Script-Level Fix

- [ ] 1.1 Add `$ProgressPreference = 'SilentlyContinue'` after the param block in `sandbox-tests/discovery-harness/sandbox-validate.ps1`

## 2. Exit Code Detection Restructure

- [ ] 2.1 Replace exit code read (line ~1296) with defensive try/catch that defaults to `$null` on failure
- [ ] 2.2 Add exit code type/value diagnostic logging
- [ ] 2.3 Replace the exit-code-first success detection block (lines ~1306–1355) with output-based-primary logic: check "Successfully installed" and "already installed" in combined output before checking exit code
- [ ] 2.4 Add exit code normalization: only treat as zero if non-null, is `[int]`, and equals 0

## 3. Verification

- [ ] 3.1 Verify file changes are written correctly by reading back the modified sections
- [ ] 3.2 Provide copy-pastable sandbox validation command for manual testing
