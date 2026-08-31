## ADDED Requirements

### Requirement: Nix provisioning records portable intent and effective immutable inputs
A Provisioning Generation written for Nix package or Endstate-generated Home Manager work SHALL record the portable requested attributes/config identity separately from the effective immutable Nixpkgs and Home Manager inputs used to resolve and activate them. The record SHALL retain an advanced override honestly and SHALL NOT infer reproducibility from a parsed installed version alone.

#### Scenario: Release-default package generation
- **WHEN** apply installs a bare portable package attribute through the release-default Nixpkgs revision
- **THEN** the generation SHALL record the portable attribute and effective immutable Nixpkgs input
- **AND** an observed installed version MAY be recorded separately for inspection

#### Scenario: Generated Home Manager activation
- **WHEN** apply activates an Endstate-generated Home Manager configuration
- **THEN** the generation SHALL record the effective immutable Nixpkgs and Home Manager inputs plus the resulting Home Manager generation

#### Scenario: Advanced input override
- **WHEN** an advanced user applies with an input override
- **THEN** the generation SHALL record the effective resolved override
- **AND** it SHALL NOT claim the Endstate release default was used

#### Scenario: Package-only generation remains package-scoped
- **WHEN** a Nix apply commits packages but activates no Home Manager configuration
- **THEN** the generation SHALL record package intent/resolution data only
- **AND** it SHALL not fabricate configuration identity
