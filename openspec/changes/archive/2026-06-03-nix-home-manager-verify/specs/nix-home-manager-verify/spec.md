## ADDED Requirements

### Requirement: verify checks home-manager config when declared

The `verify` command on a realizer backend SHALL check the home-manager configuration when, and only when,
the manifest declares a home-manager input. MUST produce exactly one `home-manager` item in the results
with status `pass` when the active generation matches the recorded one, `fail` with reason `config_drift`
when the generations diverge, or `fail` with reason `missing` when no home-manager generation is active.

#### Scenario: Active generation matches recorded — pass

- **WHEN** `verify` runs on the Nix realizer and the manifest declares a home-manager input
- **AND** the active home-manager generation number equals the most-recently recorded generation number
- **THEN** the engine SHALL emit one result item with type `home-manager`, id `home-manager`, status `pass`
- **AND** that item SHALL be counted in the pass total

#### Scenario: Active generation differs from recorded — config_drift

- **WHEN** `verify` runs on the Nix realizer and the manifest declares a home-manager input
- **AND** the active home-manager generation number is greater than zero but differs from the recorded one
- **THEN** the engine SHALL emit one result item with type `home-manager`, id `home-manager`, status `fail`
- **AND** the reason SHALL be `config_drift`
- **AND** the item SHALL surface the active generation and the expected (recorded) generation
- **AND** that item SHALL be counted in the fail total

#### Scenario: No active generation — missing

- **WHEN** `verify` runs on the Nix realizer and the manifest declares a home-manager input
- **AND** the active home-manager generation number is zero (no generation is active)
- **THEN** the engine SHALL emit one result item with type `home-manager`, id `home-manager`, status `fail`
- **AND** the reason SHALL be `missing`
- **AND** that item SHALL be counted in the fail total

#### Scenario: No provisioning history — missing

- **WHEN** `verify` runs on the Nix realizer and the manifest declares a home-manager input
- **AND** there is no Provisioning Generation that recorded a home-manager configuration
- **AND** the active home-manager generation number is zero
- **THEN** the engine SHALL emit one result item with status `fail` and reason `missing`

### Requirement: verify skips home-manager check when manifest does not declare it

The engine SHALL NOT emit a home-manager result item when the manifest does not declare a home-manager
input. MUST leave existing package and manifest-verify behavior completely unchanged.

#### Scenario: No homeManager field — no hm item

- **WHEN** `verify` runs on the Nix realizer and the manifest declares no `homeManager` block
- **THEN** the engine SHALL NOT emit any result item with type `home-manager`
- **AND** all other verify items SHALL be unaffected

### Requirement: verify skips home-manager check when the backend cannot read hm state

The home-manager generation check SHALL be gated on the realizer implementing the `HomeGenerationReader`
capability. MUST be skipped when the realizer does not implement it, so non-Nix or stub backends are not
broken.

#### Scenario: Backend without HomeGenerationReader — check skipped

- **WHEN** `verify` runs on a realizer that does not implement `HomeGenerationReader`
- **AND** the manifest declares a home-manager input
- **THEN** the engine SHALL NOT emit any result item with type `home-manager`

### Requirement: home-manager verify is realizer-path-only and read-only

The home-manager generation check SHALL run only on the realizer path (Nix) and SHALL NOT modify any
system state. MUST not affect the driver (winget) verify path.

#### Scenario: Driver path is unchanged

- **WHEN** `verify` runs through the driver (winget) path
- **THEN** the engine SHALL NOT check or emit any home-manager result item
- **AND** the driver verify behavior SHALL be unchanged

#### Scenario: verify does not mutate state

- **WHEN** `verify` runs and checks the home-manager generation
- **THEN** the engine SHALL NOT modify the active home-manager configuration or any provisioning state
