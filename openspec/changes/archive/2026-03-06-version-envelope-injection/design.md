## Context

The engine has two code paths that inject version information into output:

1. **JSON envelope** (`engine/json-output.ps1`): Already reads from `VERSION` and `SCHEMA_VERSION` files via `Get-EndstateVersion` and `Get-SchemaVersion` functions. Has a hardcoded `"1.0"` fallback for SCHEMA_VERSION if the file is missing, and hardcoded `"1.0"` in `supportedSchemaVersions` capabilities range.

2. **Capture bundle metadata** (`engine/bundle.ps1` `New-CaptureMetadata`): Uses hardcoded `"0.1.0"` and `"1.0"` as defaults, then conditionally reads files. Does not reuse the shared version functions from `json-output.ps1`.

The `VERSION` file, `SCHEMA_VERSION` file, `bump-version.ps1` script, CHANGELOG, lefthook validation, and npm scripts are all already in place.

## Goals / Non-Goals

**Goals:**
- Eliminate all hardcoded version strings from envelope-constructing code paths
- Make `engine/bundle.ps1` reuse `Get-EndstateVersion` / `Get-SchemaVersion` from `json-output.ps1`
- Derive `supportedSchemaVersions` range from the `SCHEMA_VERSION` file
- Add unit tests enforcing version injection contract

**Non-Goals:**
- Changing the bump-version script (already correct)
- Modifying VERSION or SCHEMA_VERSION file contents
- Adding version validation beyond what lefthook already checks
- GUI-side compatibility changes (separate repo)

## Decisions

### 1. Reuse existing version functions in bundle.ps1

**Decision**: Import `json-output.ps1` in `bundle.ps1` and call `Get-EndstateVersion` / `Get-SchemaVersion` instead of duplicating file-read logic with hardcoded fallbacks.

**Rationale**: DRY — single code path for version resolution. The functions already handle missing-file fallback gracefully.

**Alternative considered**: Keep separate reads but remove hardcoded defaults. Rejected because it still duplicates logic and diverges over time.

### 2. Derive supportedSchemaVersions from SCHEMA_VERSION

**Decision**: Read `SCHEMA_VERSION` at module load time (already done for `$script:SchemaVersion`) and use it for both `min` and `max` in `supportedSchemaVersions`.

**Rationale**: Currently hardcoded to `"1.0"`. When schema is bumped, this would go stale. Using the file value keeps it correct automatically.

**Note**: Both min and max are set to the current SCHEMA_VERSION because the engine only supports one schema version at a time. If multi-version support is ever needed, this can be split into separate files.

### 3. Keep fallback for missing VERSION/SCHEMA_VERSION

**Decision**: Retain the fallback chain in `Get-EndstateVersion` (VERSION file → git SHA → `"0.0.0-dev"`) and the fallback `"1.0"` for missing SCHEMA_VERSION. These are safety nets for dev environments, not production paths.

**Rationale**: Bootstrapped copies and edge cases (no git, no files) need graceful degradation.

## Risks / Trade-offs

- **[Risk] bundle.ps1 importing json-output.ps1 creates a dependency** → Acceptable: bundle.ps1 is already engine-internal and json-output.ps1 is the canonical version source. The import is dot-sourced at function call time to avoid circular loading issues.
- **[Risk] supportedSchemaVersions min=max limits future flexibility** → Mitigated: this matches current behavior and can be split when multi-version support is needed. YAGNI until then.
