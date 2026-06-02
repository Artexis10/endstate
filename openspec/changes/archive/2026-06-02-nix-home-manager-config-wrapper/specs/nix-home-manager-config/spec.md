## ADDED Requirements

### Requirement: Apply activates a home-manager config supplied as a config file

The engine SHALL accept a home-manager configuration supplied as a **config file** referenced by the
manifest, generate the surrounding flake itself, and activate it — so the user supplies only their
configuration, not the flake, inputs, pinning, identity, or activation wiring. The config-file input and the
direct flake-reference input SHALL be mutually exclusive.

#### Scenario: A config file is wrapped and activated

- **WHEN** `apply` runs on the Nix realizer with configuration changes enabled and the manifest references a
  home-manager config file
- **THEN** the engine SHALL generate a flake that wraps that config file
- **AND** it SHALL activate the resulting home-manager configuration
- **AND** the user SHALL NOT have to author any flake, inputs, or activation wiring

#### Scenario: The engine supplies machine identity

- **WHEN** the engine generates a flake from a home-manager config file
- **THEN** it SHALL supply the user identity (username, home directory, and a state version) itself
- **AND** the config file SHALL NOT need to hardcode machine-specific values

#### Scenario: Config file and flake reference are mutually exclusive

- **WHEN** a manifest declares both a home-manager config file and a home-manager flake reference
- **THEN** the engine SHALL reject the manifest with a clear error

#### Scenario: A direct flake reference is unchanged

- **WHEN** the manifest references a home-manager flake directly rather than a config file
- **THEN** the engine SHALL activate that flake as before, with no flake generation

### Requirement: Engine-generated configuration is inspectable

The engine SHALL make any configuration it generates inspectable and ejectable: it SHALL write the generated
flake to a stable, readable location, and SHALL be able to reveal what would be activated without activating
it — so the Nix layer is invisible by default but transparent on demand.

#### Scenario: The generated artifact is readable and persists

- **WHEN** the engine generates a flake from a home-manager config file
- **THEN** it SHALL write that flake to a stable engine-state location in plain, readable form
- **AND** the generated flake SHALL persist after the run so a power user can inspect or take it over

#### Scenario: Preview reveals without activating

- **WHEN** `apply` runs in preview (dry-run) with a home-manager config file
- **THEN** the engine SHALL report the generated configuration location and what would be activated
- **AND** it SHALL NOT activate anything
