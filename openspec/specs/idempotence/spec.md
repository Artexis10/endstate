# idempotence Specification

## Purpose
Guarantees that re-running any Endstate command converges to the desired state without duplicating work or producing side effects.

## Requirements
### Requirement: Command Convergence

Re-running any Endstate command with the same inputs SHALL produce the same end state and SHALL NOT duplicate previously completed work.

#### Scenario: Apply is idempotent on already-installed packages
- **WHEN** `apply` is run and all packages are already installed at the correct version
- **THEN** no install operations are executed
- **AND** the plan reports zero pending actions

#### Scenario: Restore is idempotent on already-restored files
- **WHEN** `apply --EnableRestore` is run and all config files already match the desired state
- **THEN** no file copy or merge operations are executed
- **AND** no backup is created (nothing was overwritten)

#### Scenario: Capture is idempotent across consecutive runs
- **WHEN** `capture` is run twice without changes to the source files
- **THEN** the second capture produces an identical bundle
- **AND** no duplicate entries appear in the manifest

### Requirement: No Cumulative Side Effects

Repeated execution SHALL NOT accumulate artifacts, log entries, or state records beyond what a single execution produces.

#### Scenario: State file does not grow on repeated apply
- **WHEN** `apply` is run N times with no changes to the manifest
- **THEN** the state file contains the same number of entries after each run
- **AND** timestamps reflect the latest run only
