## ADDED Requirements

### Requirement: Apply activates a declared home-manager config

The engine SHALL activate a declared home-manager configuration as a configuration stage of `apply`
on a realizer backend, when configuration changes are enabled. The home-manager tool SHALL be
invoked by the engine itself (the user does not install or run it), at a version the engine controls.

#### Scenario: Declared config is activated when config is enabled

- **WHEN** `apply` runs on the Nix realizer with configuration changes enabled and the manifest
  declares a home-manager config reference
- **THEN** the engine SHALL activate that home-manager configuration (a `home-manager switch`)
- **AND** it SHALL do so without requiring the user to have installed home-manager

#### Scenario: No declared config is a no-op

- **WHEN** `apply` runs and the manifest declares no home-manager config
- **THEN** the engine SHALL NOT run any home-manager activation
- **AND** the apply behavior SHALL be unchanged from a package-only apply

### Requirement: The config stage is opt-in and scoped to the realizer

Activating a home-manager configuration SHALL require the configuration-changes flag, and SHALL apply
only on a backend that supports it (the Nix realizer). A backend without home-manager support, or an
apply without the configuration flag, SHALL NOT activate any configuration.

#### Scenario: Without the config flag, nothing is activated

- **WHEN** `apply` runs with a declared home-manager config but without the configuration-changes flag
- **THEN** the engine SHALL NOT activate the home-manager configuration

#### Scenario: The driver (winget) path does not run home-manager

- **WHEN** `apply` runs through the winget driver path
- **THEN** the engine SHALL NOT attempt any home-manager activation

### Requirement: Existing files are backed up before being overwritten

The engine SHALL back up any existing file that activating the home-manager configuration would
replace, rather than failing or destroying it, honoring the backup-before-overwrite guarantee.

#### Scenario: A clobbered file is backed up

- **WHEN** activating the home-manager configuration would replace a file the user already has
- **THEN** the engine SHALL preserve the existing file as a backup
- **AND** the activation SHALL proceed rather than aborting on the conflict

### Requirement: The applied config is recorded in the Provisioning Generation

After activating a home-manager configuration, the engine SHALL record the applied configuration in
the Provisioning Generation, so configuration is part of the same audit trail as packages.

#### Scenario: Generation records the activated config

- **WHEN** `apply` activates a home-manager configuration
- **THEN** the engine SHALL write a Provisioning Generation that records the activated configuration
  reference and its resulting home-manager generation

#### Scenario: Raw backend text stays out of the user message

- **WHEN** a home-manager activation fails
- **THEN** the engine SHALL report a stable error with a human-readable message
- **AND** the raw home-manager / Nix output SHALL appear only in the error detail, not the message
