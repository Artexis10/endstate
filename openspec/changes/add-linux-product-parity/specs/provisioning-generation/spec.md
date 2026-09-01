## MODIFIED Requirements

### Requirement: Apply persists a Provisioning Generation
After a successful `apply` that advances committed package state or an Endstate-owned generated Home Manager activation, the engine SHALL write a numbered Provisioning Generation for every provisioning backend that committed state. Mixed package-driver runs SHALL write separate backend-scoped generations. A Provisioning Generation SHALL record provisioning identity and effective immutable inputs; it SHALL NOT duplicate direct file/value restore journals, backup payloads, or restore results.

#### Scenario: Successful package apply commits a Provisioning Generation
- **WHEN** `apply` completes a non-dry-run that newly installs at least one package
- **THEN** the engine SHALL write a numbered Provisioning Generation under the resolved state directory
- **AND** the record SHALL include the committed package set, run identifier, backend name, portable intent, and effective resolution input when applicable

#### Scenario: Generated Home Manager activation commits a Provisioning Generation
- **WHEN** `apply` advances an Endstate-owned generated Home Manager activation
- **THEN** the engine SHALL write a Provisioning Generation containing the platform/config identity, effective Nixpkgs and Home Manager inputs, and resulting Home Manager generation
- **AND** it SHALL NOT embed captured settings payloads, direct-restore backup contents, or a user-owned Home Manager source graph

#### Scenario: Mixed per-package drivers write separate generations
- **WHEN** one apply installs Winget and Chocolatey packages
- **THEN** the engine SHALL write backend-scoped generations whose added refs contain only packages installed by the named backend

#### Scenario: Atomic backend writes a generation only on committed success
- **WHEN** a Nix package or generated Home Manager operation does not advance its backend-native generation
- **THEN** no Provisioning Generation SHALL be written for that uncommitted operation
- **AND** the failure SHALL be surfaced through the existing error path

#### Scenario: Non-atomic backend records the installed subset
- **WHEN** an apply through a per-package driver installs some packages and fails others
- **THEN** the engine SHALL write that backend's Provisioning Generation containing the successfully installed subset
- **AND** the generation SHALL be marked partial

#### Scenario: Idempotent re-run writes no new generation
- **WHEN** `apply` runs and advances no package or Endstate-owned Home Manager generation because desired provisioning state is already present
- **THEN** no new Provisioning Generation SHALL be written

#### Scenario: Added references record only packages installed this run
- **WHEN** a package Provisioning Generation is written
- **THEN** its added references SHALL contain only packages whose status this run was `installed`
- **AND** packages that were already present SHALL NOT appear in the added references

### Requirement: Provisioning Generation record format
A Provisioning Generation SHALL be a self-describing, versioned record stored as an individual file, written with the same durability guarantees as other engine state. It SHALL distinguish package facts from Endstate-owned generated-configuration identity and SHALL retain the exact effective immutable inputs used for either lane.

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

#### Scenario: Package generation records package facts only
- **WHEN** a Provisioning Generation is written for package provisioning without a generated Home Manager activation
- **THEN** it SHALL record package identifiers, requested refs, portable intent, effective resolution input, status, and version when exposed by the backend
- **AND** it SHALL NOT fabricate config-module identity, restore results, or backup state

#### Scenario: Home Manager generation records configuration identity only
- **WHEN** a Provisioning Generation is written for an Endstate-owned Home Manager activation without package changes
- **THEN** it SHALL record platform-qualified config identity, effective Nixpkgs/Home Manager inputs, and the resulting backend generation
- **AND** it SHALL NOT fabricate package additions or store captured configuration contents

## ADDED Requirements

### Requirement: Nix provisioning records portable intent and effective immutable inputs
A Provisioning Generation written for Nix package or Endstate-generated Home Manager work SHALL record the portable requested attributes/config identity separately from the effective immutable Nixpkgs and Home Manager inputs used to resolve and activate them. Every accepted override SHALL itself resolve to immutable provenance before mutation; the record SHALL NOT infer reproducibility from a parsed installed version alone.

#### Scenario: Release-default package generation
- **WHEN** apply installs a bare portable package attribute through the release-default Nixpkgs revision
- **THEN** the generation SHALL record the portable attribute and effective immutable Nixpkgs input
- **AND** an observed installed version MAY be recorded separately for inspection

#### Scenario: Generated Home Manager activation
- **WHEN** apply activates an Endstate-generated Home Manager configuration
- **THEN** the generation SHALL record the platform/config identity, effective immutable Nixpkgs and Home Manager inputs, and resulting Home Manager generation

#### Scenario: Advanced immutable input override
- **WHEN** an advanced user applies with an input override that resolves to immutable provenance
- **THEN** the generation SHALL record the effective resolved override
- **AND** it SHALL NOT claim the Endstate release default was used

#### Scenario: Override cannot be resolved immutably
- **WHEN** an input override cannot be resolved to immutable provenance before mutation
- **THEN** apply SHALL fail before mutation with a stable input-provenance error
- **AND** no Provisioning Generation SHALL be written for that operation
