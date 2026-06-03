## ADDED Requirements

### Requirement: Capture records the engine-provisioned home-manager catalog settings

The engine SHALL record, in a captured manifest, the `homeManager.settings` catalog the user
declared when the engine compiled and activated it, so that applying the captured manifest
re-activates the same catalog configuration. The settings SHALL be recovered from the engine's
provisioning history (the generation recorded on apply), not from the live system.

#### Scenario: Captured manifest carries the declared settings

- **WHEN** `capture` runs on a realizer host and the engine's provisioning history records an
  activated home-manager configuration compiled from a declared catalog
- **THEN** the written manifest SHALL include the `homeManager.settings` block from that history
- **AND** applying the captured manifest SHALL re-activate the same home-manager configuration
  through the catalog stage

#### Scenario: Settings take precedence over config and flake in capture

- **WHEN** the provisioning history contains a generation that activated a settings-compiled
  configuration
- **THEN** `capture` SHALL emit `homeManager.settings` rather than `homeManager.config` or
  `homeManager.flake`

#### Scenario: No recorded settings falls through to config and flake

- **WHEN** the provisioning history records no settings but does record a config or flake
- **THEN** `capture` SHALL emit the config or flake as before, without change

#### Scenario: Capture then apply re-activates the same settings configuration

- **WHEN** a manifest produced by `capture` (carrying `homeManager.settings`) is applied on a
  realizer host with configuration enabled
- **THEN** the engine SHALL re-activate the same home-manager configuration from the catalog

### Requirement: Settings capture is best-effort and non-destructive

Recovering the home-manager catalog settings for capture SHALL NOT fail the capture command. If
the provisioning history cannot be read or records no settings, capture SHALL still produce a
valid package manifest and omit the home-manager field.

#### Scenario: Provisioning history unreadable — settings omitted, capture succeeds

- **WHEN** `capture` runs and the provisioning history cannot be read
- **THEN** the engine SHALL still write a valid manifest of the captured packages
- **AND** it SHALL omit the home-manager field rather than returning an error

#### Scenario: No settings in history — config or flake preserved

- **WHEN** `capture` runs and the provisioning history records a config or flake but no settings
- **THEN** the engine SHALL emit the config or flake in the captured manifest unchanged

### Requirement: Apply records the declared settings in the provisioning generation

The engine SHALL record the declared `homeManager.settings` in the Provisioning Generation after
activating a home-manager configuration compiled from them, so that capture can round-trip them.

#### Scenario: Generation records the declared settings on a settings apply

- **WHEN** `apply` activates a home-manager configuration compiled from `homeManager.settings`
- **THEN** the engine SHALL write a Provisioning Generation whose `HomeManager.Settings` equals
  the declared catalog settings

#### Scenario: Config and flake generation records are unchanged

- **WHEN** `apply` activates a home-manager configuration from a `homeManager.config` or
  `homeManager.flake`
- **THEN** the Provisioning Generation SHALL record `Config` or `Flake` as before
- **AND** `Settings` SHALL be absent from those generations
