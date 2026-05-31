## MODIFIED Requirements

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
