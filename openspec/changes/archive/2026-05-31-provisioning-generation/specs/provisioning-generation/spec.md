## ADDED Requirements

### Requirement: Apply persists a Provisioning Generation

After a successful `apply` that advances the committed package set, the engine SHALL write a numbered Provisioning Generation that records what was committed, for **both** package backends (the Nix realizer and the winget driver). A Provisioning Generation is an install-stage record only; it does not represent configuration or restore state.

#### Scenario: Successful apply commits a Provisioning Generation

- **WHEN** `apply` completes a non-dry-run that newly installs at least one package
- **THEN** the engine SHALL write a numbered Provisioning Generation under the resolved state directory
- **AND** the record SHALL include the committed package set, the run identifier, and the backend name

#### Scenario: Atomic backend writes a generation only on full success

- **WHEN** an `apply` through the Nix realizer does not advance the profile generation (the atomic switch did not commit)
- **THEN** no Provisioning Generation SHALL be written
- **AND** the failure SHALL be surfaced through the existing error path

#### Scenario: Non-atomic backend records the installed subset

- **WHEN** an `apply` through the winget driver installs some packages and fails others
- **THEN** the engine SHALL write a Provisioning Generation containing the successfully installed subset
- **AND** the generation SHALL be marked partial

#### Scenario: Idempotent re-run writes no new generation

- **WHEN** `apply` runs and installs no new packages because every declared package is already present
- **THEN** no new Provisioning Generation SHALL be written

#### Scenario: Added references record only packages installed this run

- **WHEN** a Provisioning Generation is written
- **THEN** its added references SHALL contain only packages whose status this run was "installed"
- **AND** packages that were already present SHALL NOT appear in the added references

### Requirement: Provisioning Generation record format

A Provisioning Generation SHALL be a self-describing, versioned record stored as an individual file, written with the same durability guarantees as other engine state.

#### Scenario: Generation carries its own schema version

- **WHEN** a Provisioning Generation is written
- **THEN** the record SHALL include a schema version owned by the provisioning layer
- **AND** that schema version SHALL be independent of the manifest and envelope schema versions

#### Scenario: Generation is numbered monotonically by the engine

- **WHEN** a new Provisioning Generation is written
- **THEN** its number SHALL be one greater than the highest existing Provisioning Generation number on the host
- **AND** the number SHALL be owned by the engine, independent of any backend-native generation number recorded alongside it

#### Scenario: Generations are stored via the resolved state path

- **WHEN** the engine writes a Provisioning Generation
- **THEN** it SHALL resolve the storage location under the state directory via the configuration path resolver
- **AND** it SHALL NOT use a hardcoded absolute path

#### Scenario: Generation writes are atomic

- **WHEN** the engine writes a Provisioning Generation file
- **THEN** it SHALL write to a temporary file and rename it into place
- **AND** no partially written record SHALL ever be observable

#### Scenario: Generation records package facts only

- **WHEN** a Provisioning Generation is written
- **THEN** it SHALL record package facts: identifiers, references, status, and version when the backend exposes it
- **AND** it SHALL NOT record config-module identifiers, restore results, or backup state

### Requirement: The generations command lists Provisioning Generations

The engine SHALL provide a read-only `generations` command that lists the recorded Provisioning Generations.

#### Scenario: generations lists committed generations newest first

- **WHEN** `generations` is run on a host with one or more recorded Provisioning Generations
- **THEN** the engine SHALL list them ordered newest first
- **AND** each entry SHALL expose its number, run identifier, backend, partial flag, and added references

#### Scenario: generations returns an empty list when none exist

- **WHEN** `generations` is run and no Provisioning Generation has been recorded
- **THEN** the engine SHALL return an empty list
- **AND** it SHALL NOT report an error

#### Scenario: generations is read-only

- **WHEN** `generations` is run
- **THEN** it SHALL NOT create, modify, or delete any package, configuration, or generation state
