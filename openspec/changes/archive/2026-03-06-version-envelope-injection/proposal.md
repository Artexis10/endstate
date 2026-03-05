## Why

The engine still has hardcoded version fallbacks in `engine/bundle.ps1` (`"0.1.0"` and `"1.0"`) and `engine/json-output.ps1` (fallback `"1.0"`, hardcoded `supportedSchemaVersions`). While the main envelope builder (`json-output.ps1`) already reads from `VERSION` and `SCHEMA_VERSION` files, the capture bundle metadata constructor and capabilities data use stale hardcoded values. This change eliminates all hardcoded version strings so the version files are the true single source of truth, and adds tests to enforce the contract.

## What Changes

- Remove hardcoded version fallbacks from `engine/bundle.ps1` — use shared `Get-EndstateVersion` and `Get-SchemaVersion` functions instead
- Make `supportedSchemaVersions` in capabilities data derive from `SCHEMA_VERSION` file rather than hardcoded `"1.0"`
- Add unit tests verifying version injection from files into the JSON envelope and capture bundle metadata
- Validate that no hardcoded version assignments remain in envelope-constructing code paths

## Capabilities

### New Capabilities
- `version-envelope-injection`: Defines how CLI and schema versions are sourced at runtime and injected into the JSON envelope, ensuring VERSION and SCHEMA_VERSION files are the single sources of truth

### Modified Capabilities

## Impact

- `engine/bundle.ps1` — replace hardcoded versions with function calls from `json-output.ps1`
- `engine/json-output.ps1` — derive `supportedSchemaVersions` from `SCHEMA_VERSION` file
- `tests/unit/` — new test file for version injection contract
- `scripts/bump-version.ps1` — no changes needed (already correct)
- `VERSION`, `SCHEMA_VERSION` — no changes (already exist and are correct)
