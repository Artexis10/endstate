# non-destructive-defaults Specification

## Purpose
Ensures that Endstate never silently deletes or overwrites user data. Destructive operations require explicit opt-in flags.

## Requirements
### Requirement: No Silent Deletions

Default command execution SHALL NOT delete files, uninstall packages, or remove configuration without an explicit destructive flag.

#### Scenario: Apply does not remove unlisted packages
- **WHEN** `apply` is run and the system has packages installed that are not in the manifest
- **THEN** those packages are left untouched
- **AND** no uninstall operations are executed

#### Scenario: Restore does not delete files absent from payload
- **WHEN** `apply --EnableRestore` is run and the target directory contains files not present in the restore payload
- **THEN** those extra files are not deleted
- **AND** only files present in the payload are written

### Requirement: Destructive Operations Require Explicit Flags

Any operation that removes, overwrites, or resets existing state SHALL require an explicit command-line flag.

#### Scenario: Overwrite requires EnableRestore
- **WHEN** `apply` is run without `--EnableRestore`
- **THEN** no config files are overwritten on disk
- **AND** restore entries in the manifest are ignored

#### Scenario: Destructive flag is not inferred from environment
- **WHEN** `apply` is run without `--EnableRestore` but with restore entries in the manifest
- **THEN** the system does not infer destructive intent from manifest content alone
- **AND** restore is skipped entirely
