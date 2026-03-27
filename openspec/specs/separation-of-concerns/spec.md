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

### Requirement: Stages Are Independently Addressable

Each stage SHALL be reachable without requiring the others to execute first in the same invocation.

#### Scenario: Restore can run standalone
- **WHEN** `restore --EnableRestore` is run
- **THEN** restore entries execute without re-running the install stage

#### Scenario: Capture is independent of apply
- **WHEN** `capture` is run
- **THEN** it reads current system state without triggering install or restore
