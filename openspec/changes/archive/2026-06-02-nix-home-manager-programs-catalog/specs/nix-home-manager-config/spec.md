## ADDED Requirements

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
