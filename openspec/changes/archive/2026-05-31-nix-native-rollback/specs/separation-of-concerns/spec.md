## ADDED Requirements

### Requirement: Rollback operates on packages only

The package `rollback` command SHALL operate on the package Provisioning Generation only, never on configuration state, preserving the separation between `rollback` (packages) and `revert` (configuration). The two reversal verbs remain distinct commands over distinct logs.

This is an ADDED requirement (a new, distinct requirement) rather than a modification of *Distinct Pipeline Stages*, so it composes cleanly with any concurrent change that modifies that requirement.

#### Scenario: Rollback does not touch configuration state

- **WHEN** `rollback` runs
- **THEN** it SHALL NOT read or write the config backup directory (`state/backups/`) or the restore revert journal
- **AND** it SHALL NOT invoke configuration restore

#### Scenario: Rollback and revert are distinct verbs

- **WHEN** a host has both package Provisioning Generations and a restore revert journal
- **THEN** `rollback` SHALL act only on the package generation
- **AND** `revert` SHALL act only on the configuration restore journal
