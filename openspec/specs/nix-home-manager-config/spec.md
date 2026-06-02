# nix-home-manager-config Specification

## Purpose
TBD - created by archiving change nix-home-manager-config. Update Purpose after archive.
## Requirements
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

### Requirement: Apply activates a home-manager configuration declared in the Endstate catalog

The engine SHALL accept a home-manager configuration declared in **Endstate's own format** (not Nix and not a
`home.nix`), compile it into a `home.nix` itself, and activate it through the existing generation path — so the
user declares configuration in Endstate's format and never writes Nix. The declarative catalog input, the
config-file input, and the direct flake-reference input SHALL be mutually exclusive.

The catalog input SHALL be a **hybrid**: a curated set of Endstate-native concepts that the engine maps to the
correct home-manager options, plus a raw passthrough block forwarded to home-manager unchanged.

#### Scenario: A declared catalog is compiled and activated

- **WHEN** `apply` runs on the Nix realizer with configuration changes enabled and the manifest declares a
  home-manager catalog
- **THEN** the engine SHALL compile the declaration into a `home.nix`
- **AND** it SHALL generate the surrounding flake and activate the resulting home-manager configuration
- **AND** the user SHALL NOT have to author any Nix, `home.nix`, flake, or activation wiring

#### Scenario: Curated concepts are mapped to home-manager options

- **WHEN** the declaration uses a curated Endstate concept
- **THEN** the engine SHALL map it to the corresponding home-manager option(s)
- **AND** the mapping SHALL shield the declaration from home-manager option renames, so a curated concept keeps
  working when the underlying option changes

#### Scenario: A raw passthrough is forwarded unchanged

- **WHEN** the declaration includes a raw home-manager passthrough block
- **THEN** the engine SHALL forward it to the generated `home.nix` unchanged

#### Scenario: Unknown curated keys are rejected

- **WHEN** the declaration uses a curated concept with an unrecognized key
- **THEN** the engine SHALL reject the manifest with a clear error

#### Scenario: Catalog, config file, and flake reference are mutually exclusive

- **WHEN** a manifest declares more than one of a home-manager catalog, a home-manager config file, and a
  home-manager flake reference
- **THEN** the engine SHALL reject the manifest with a clear error

#### Scenario: The compiled configuration is inspectable and reveals on preview

- **WHEN** the engine compiles a declared catalog into a `home.nix`
- **THEN** it SHALL write the generated `home.nix` and flake to a stable, readable location that persists after
  the run
- **AND** on a preview (dry-run) run it SHALL report the generated location and what would be activated without
  activating anything

### Requirement: The catalog places declared files of any kind

The engine SHALL let the declaration place arbitrary files — text or binary — into the user's home, matching the
breadth of the Windows configuration catalog. It SHALL do so by staging each declared file so it is part of the
generated configuration and placing it through home-manager.

#### Scenario: A declared file is staged and placed

- **WHEN** the declaration maps a target path to a source file (resolved relative to the manifest)
- **THEN** the engine SHALL stage that file as part of the generated configuration
- **AND** it SHALL place the file at the declared target through home-manager on activation

#### Scenario: Binary files are preserved

- **WHEN** a declared file is binary rather than text
- **THEN** the engine SHALL place it without corruption

#### Scenario: A missing source is a clear error

- **WHEN** a declared file references a source that does not exist
- **THEN** the engine SHALL fail with a clear error rather than silently omitting the file

