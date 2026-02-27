## 1. Engine Implementation

- [x] 1.1 Add `gitCommit` field to `Get-CapabilitiesData` in `engine/json-output.ps1` -- query `git rev-parse --short HEAD` with null fallback
- [x] 1.2 Add `gitDirty` field to `Get-CapabilitiesData` in `engine/json-output.ps1` -- query `git status --porcelain` with `$false` fallback
- [x] 1.3 Add `bootstrapTimestamp` field to `Get-CapabilitiesData` in `engine/json-output.ps1` -- read from `engine/version-info.json` with null fallback

## 2. Contract Documentation

- [x] 2.1 Update `docs/contracts/cli-json-contract.md` capabilities response example to include `gitCommit`, `gitDirty`, and `bootstrapTimestamp`
- [x] 2.2 Add field descriptions to the capabilities data fields table in the contract doc

## 3. Verification

- [x] 3.1 Add unit tests for `gitCommit`, `gitDirty`, and `bootstrapTimestamp` fields in `tests/unit/JsonSchema.Tests.ps1`
- [x] 3.2 Run `scripts/test-unit.ps1` to verify no regressions
