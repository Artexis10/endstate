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

### Requirement: Convergence is opt-in and confirmed

Removal of undeclared (drift) packages via convergence SHALL require an explicit opt-in flag (`--prune`) AND explicit confirmation (`--confirm`). This is a new, distinct requirement that does not relax the default-safe guarantees: without `--prune`, `apply` removes nothing.

#### Scenario: Default apply removes nothing

- **WHEN** `apply` runs without `--prune`
- **THEN** no package SHALL be uninstalled
- **AND** undeclared installed packages SHALL be left untouched

#### Scenario: Convergence is not inferred from the manifest

- **WHEN** `apply` is run without `--prune` against a manifest that declares fewer packages than are installed
- **THEN** the engine SHALL NOT infer removal intent from the manifest
- **AND** no package SHALL be uninstalled

#### Scenario: Prune requires both opt-in and confirmation

- **WHEN** `apply --prune` runs without `--confirm` and not in preview mode
- **THEN** the engine SHALL NOT uninstall any package
- **AND** removal SHALL proceed only when both `--prune` and `--confirm` are present

