# restore-opt-in Specification

## Purpose
Ensures that configuration restoration never executes unless the user explicitly opts in via the -EnableRestore flag.

## Requirements
### Requirement: Restore Requires Explicit Flag

Restore operations SHALL NOT execute unless the `-EnableRestore` flag is provided on the command line.

#### Scenario: Apply without EnableRestore skips all restore entries
- **WHEN** `apply` is run without `--EnableRestore`
- **THEN** all restore entries in the manifest are skipped
- **AND** no config files are written to disk by the restore stage

#### Scenario: Apply with EnableRestore executes restore entries
- **WHEN** `apply --EnableRestore` is run
- **THEN** restore entries in the manifest are executed
- **AND** config files are written according to restore strategies

#### Scenario: Standalone restore requires EnableRestore
- **WHEN** `restore` is run without `--EnableRestore`
- **THEN** no restore entries are executed
- **AND** the command exits without modifying config files

### Requirement: Flag Cannot Be Defaulted or Inferred

The `-EnableRestore` flag SHALL NOT be set by default, by environment variable, or by manifest content.

#### Scenario: Environment variable does not enable restore
- **WHEN** `apply` is run without `--EnableRestore` but with any environment variables set
- **THEN** restore is not triggered
- **AND** only the explicit CLI flag activates restore
