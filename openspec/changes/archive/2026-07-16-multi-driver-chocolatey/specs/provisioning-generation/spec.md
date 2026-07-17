## MODIFIED Requirements

### Requirement: Apply persists a Provisioning Generation
After a successful `apply` that advances the committed package set, the engine SHALL write a numbered Provisioning Generation for every backend that committed packages, including the Nix realizer and per-package Winget, Chocolatey, and Brew drivers. Mixed-driver runs SHALL write separate backend-scoped generations. A Provisioning Generation is an install-stage record only; it does not represent configuration or restore state.

#### Scenario: Successful apply commits a Provisioning Generation
- **WHEN** `apply` completes a non-dry-run that newly installs at least one package
- **THEN** the engine SHALL write a numbered Provisioning Generation under the resolved state directory
- **AND** the record SHALL include the committed package set, the run identifier, and the backend name

#### Scenario: Mixed per-package drivers write separate generations
- **WHEN** one apply installs Winget and Chocolatey packages
- **THEN** the engine SHALL write backend-scoped generations whose added refs contain only packages installed by the named backend

#### Scenario: Atomic backend writes a generation only on full success
- **WHEN** an `apply` through the Nix realizer does not advance the profile generation
- **THEN** no Provisioning Generation SHALL be written for Nix
- **AND** the failure SHALL be surfaced through the existing error path

#### Scenario: Non-atomic backend records the installed subset
- **WHEN** an apply through a per-package driver installs some packages and fails others
- **THEN** the engine SHALL write that backend's Provisioning Generation containing the successfully installed subset
- **AND** the generation SHALL be marked partial

#### Scenario: Idempotent re-run writes no new generation
- **WHEN** `apply` runs and installs no new packages because every declared package is already present
- **THEN** no new Provisioning Generation SHALL be written

#### Scenario: Added references record only packages installed this run
- **WHEN** a Provisioning Generation is written
- **THEN** its added references SHALL contain only packages whose status this run was `installed`
- **AND** packages that were already present SHALL NOT appear in the added references

