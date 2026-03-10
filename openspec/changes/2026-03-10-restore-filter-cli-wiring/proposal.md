# Proposal: Wire --restore-filter through CLI entrypoint

## Problem
The restore-filter spec is implemented at the engine level (Invoke-Apply, Invoke-Restore) but the CLI entrypoint (bin/endstate.ps1) does not expose the --restore-filter flag. Users and the GUI cannot use per-module restore filtering via the CLI.

## What Changes
- Add --restore-filter parameter to bin/endstate.ps1 param block and GNU-style flag normalization
- Pass RestoreFilter to Invoke-ApplyCore in apply command dispatch
- Add standalone restore command dispatch that delegates to engine Invoke-Restore
- Add --restore-filter to capabilities output for both apply and restore commands
- Update apply/restore help text

## Capabilities

### Modified Capabilities
- `restore-filter`: CLI entrypoint now exposes the flag that was already implemented in engine functions

## Impact
- Modified file: bin/endstate.ps1 (CLI entrypoint — requires explicit instruction per PROJECT_RULES.md)
- Modified file: docs/contracts/cli-json-contract.md (add --restore-filter to apply and restore flags)
- Modified file: docs/contracts/gui-integration-contract.md (add restore command to supported commands table)
- No new engine logic; purely wiring
- Backward compatible: omitting the flag behaves identically to before
