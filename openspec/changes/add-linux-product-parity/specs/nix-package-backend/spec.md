## MODIFIED Requirements

### Requirement: Nix package references resolve to pinned installables
The engine SHALL resolve `App.Refs["linux"]`/`App.Refs["darwin"]` through an immutable effective Nixpkgs input on the realizer path. Release defaults SHALL name a revision-locked input rather than a floating registry alias or branch. A bare portable attribute SHALL expand against that input; an explicit flakeref installable SHALL pass through after its effective revision is resolved for provenance. Apps with no Nix reference SHALL be skipped rather than receiving a non-Nix reference.

#### Scenario: Bare attribute uses the immutable release input
- **WHEN** a Nix ref is a bare attribute name under release defaults
- **THEN** it SHALL resolve against the revision-locked Nixpkgs input embedded for that release
- **AND** the effective input revision SHALL be available to the provisioning record

#### Scenario: Explicit flakeref passes through
- **WHEN** a Nix ref is already a flakeref installable
- **THEN** it SHALL be passed to `nix profile add` without changing its requested target
- **AND** the engine SHALL record the effective locked revision when the input can be resolved

#### Scenario: Advanced override is recorded honestly
- **WHEN** an advanced user overrides the Nixpkgs input
- **THEN** the engine SHALL use the override
- **AND** it SHALL record the effective resolved input rather than claiming the release default was used

#### Scenario: App with no Nix ref is skipped
- **WHEN** an app has no `linux`/`darwin` ref on the realizer path
- **THEN** the app SHALL be skipped
- **AND** no Winget, native-Linux-manager, Flatpak, Snap, or Brew reference SHALL be passed to Nix

## ADDED Requirements

### Requirement: Provisioning records portable intent and effective Nix resolution separately
For every Nix package operation, the engine SHALL retain the portable package attribute or explicit requested installable separately from the immutable input/revision that resolved it. Provisioning Generation and inspection output SHALL expose enough data to reproduce the effective resolution without treating an observed source-manager version as a Nix pin.

#### Scenario: Mapped external package is applied
- **WHEN** a portable attribute captured from an external Linux manager is applied through the release Nixpkgs input
- **THEN** the provisioning record SHALL identify both the portable attribute and the effective immutable input
- **AND** it SHALL retain the source-manager version only as capture evidence

#### Scenario: Exact resolution cannot be established
- **WHEN** a requested input cannot be resolved to immutable provenance before mutation
- **THEN** the engine SHALL fail or require an explicit non-reproducible override according to the command contract
- **AND** it SHALL NOT label the operation reproducible
