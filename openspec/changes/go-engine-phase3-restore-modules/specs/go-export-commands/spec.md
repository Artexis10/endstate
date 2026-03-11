## ADDED Requirements

### Requirement: Export-config copies system configs to export directory
The Go engine SHALL implement an export-config command that reads manifest restore entries and copies from system target paths to portable source paths in an export directory (inverse of restore). The default export directory SHALL be <manifestDir>/export/. A manifest snapshot SHALL be copied as manifest.snapshot.jsonc. The command SHALL support --dry-run. The envelope data SHALL include exportPath, exportCount, skipCount, warnCount, and warnings.

#### Scenario: Export config files
- **WHEN** export-config is run with a manifest containing restore entries
- **THEN** each entry's target (system path) is copied to the export directory at the source relative path

#### Scenario: Export dry-run
- **WHEN** export-config is run with --dry-run
- **THEN** no files are copied but the envelope reports what would be exported

#### Scenario: Missing target on system
- **WHEN** a restore entry's target does not exist on the system
- **THEN** the entry is skipped and counted in skipCount

### Requirement: Validate-export checks export completeness
The Go engine SHALL implement a validate-export command that checks whether all restore entry sources exist in the export directory using Model B resolution. The envelope data SHALL include valid (bool), validCount, warnCount, failCount, warnings, and errors.

#### Scenario: All sources present
- **WHEN** validate-export is run and all restore entry sources exist in the export directory
- **THEN** the result has valid=true

#### Scenario: Missing source in export
- **WHEN** validate-export is run and a restore entry source is missing from the export directory
- **THEN** the result has valid=false with the missing source listed in errors
