# separation-of-concerns Specification

## Purpose
Enforces that install, configure, and verify are distinct pipeline stages that do not conflate responsibilities.
## Requirements
### Requirement: Distinct Pipeline Stages

The system SHALL maintain clear separation between installation, configuration (restore), and verification as independent stages.

#### Scenario: Install does not configure
- **WHEN** `apply` runs the install stage for a package
- **THEN** only package installation is performed
- **AND** no config files are written or modified during the install stage

#### Scenario: Restore does not install
- **WHEN** `apply --EnableRestore` runs the restore stage
- **THEN** restore operates on config files only
- **AND** no package install or uninstall operations are triggered by the restore stage

#### Scenario: Verify is read-only
- **WHEN** the verification stage runs
- **THEN** it inspects system state without modifying it
- **AND** no files are created, modified, or deleted by verifiers

#### Scenario: Provisioning Generation is install-only
- **WHEN** `apply` writes a Provisioning Generation for the install stage
- **THEN** the generation SHALL record package state only
- **AND** it SHALL NOT read or write the config backup directory (`state/backups/`) or the restore revert journal
- **AND** the verification stage SHALL NOT write Provisioning Generations

### Requirement: Stages Are Independently Addressable

Each stage SHALL be reachable without requiring the others to execute first in the same invocation.

#### Scenario: Restore can run standalone
- **WHEN** `restore --EnableRestore` is run
- **THEN** restore entries execute without re-running the install stage

#### Scenario: Capture is independent of apply
- **WHEN** `capture` is run
- **THEN** it reads current system state without triggering install or restore

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

