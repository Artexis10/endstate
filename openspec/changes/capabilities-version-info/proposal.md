## Why

The GUI needs to detect stale engine installations. Currently, the `capabilities` command returns schema and CLI versions but no git commit, working-tree cleanliness, or bootstrap timestamp. Without these fields the GUI cannot warn users when the engine copy is outdated or dirty.

## What Changes

- Add `gitCommit` (string | null) to the capabilities `data` object -- short SHA of the current HEAD
- Add `gitDirty` (boolean) to the capabilities `data` object -- whether the working tree has uncommitted changes
- Add `bootstrapTimestamp` (string | null) to the capabilities `data` object -- ISO 8601 timestamp of when the engine was last bootstrapped
- Update `docs/contracts/cli-json-contract.md` with the new fields in the capabilities response documentation

## Capabilities

### New Capabilities

- `capabilities-version-info`: Expose gitCommit, gitDirty, and bootstrapTimestamp in the capabilities response data

### Modified Capabilities

(none -- additive fields only, no existing spec behavior changes)

## Impact

- `engine/json-output.ps1` -- `Get-CapabilitiesData` gains three new fields
- `docs/contracts/cli-json-contract.md` -- additive documentation update
- No schema version bump (additive, backward-compatible per contract rules)
- No breaking changes for existing GUI consumers (unknown fields are ignored per contract)
