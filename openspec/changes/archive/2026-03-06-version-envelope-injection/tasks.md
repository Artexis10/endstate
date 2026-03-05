## 1. Eliminate hardcoded versions in bundle.ps1

- [x] 1.1 Import `Get-EndstateVersion` and `Get-SchemaVersion` into `engine/bundle.ps1` by dot-sourcing `engine/json-output.ps1` (or referencing the already-loaded functions)
- [x] 1.2 Replace hardcoded `$endstateVersion = "0.1.0"` and inline file-read logic in `New-CaptureMetadata` with `Get-EndstateVersion` call
- [x] 1.3 Replace hardcoded `$schemaVer = "1.0"` and inline file-read logic in `New-CaptureMetadata` with `Get-SchemaVersion` call

## 2. Derive supportedSchemaVersions from file

- [x] 2.1 Update `Get-CapabilitiesData` in `engine/json-output.ps1` to use `$script:SchemaVersion` for `supportedSchemaVersions.min` and `supportedSchemaVersions.max` instead of hardcoded `"1.0"`

## 3. Unit tests

- [x] 3.1 Create `tests/unit/VersionInjection.Tests.ps1` with tests for:
  - `Get-EndstateVersion` returns content of VERSION file
  - `Get-SchemaVersion` returns content of SCHEMA_VERSION file
  - `New-JsonEnvelope` uses file-based versions
  - `New-CaptureMetadata` uses shared version functions (no hardcoded values)
  - `Get-CapabilitiesData` `supportedSchemaVersions` matches SCHEMA_VERSION
- [x] 3.2 Run `.\scripts\test-unit.ps1 -Path tests\unit\VersionInjection.Tests.ps1` and verify all tests pass

## 4. Verification

- [x] 4.1 Grep engine/ for remaining hardcoded version assignments to envelope/metadata fields — confirm none exist outside of fallback chains in `Get-EndstateVersion` / `Get-SchemaVersion`
