## ADDED Requirements

### Requirement: Capture records the engine-provisioned home-manager config

The engine SHALL record, in a captured manifest, the home-manager configuration that the engine
itself activated, so that applying the captured manifest re-activates the same configuration. The
configuration reference SHALL be recovered from the engine's provisioning history (the recorded
config activation), not from the live system, and SHALL be emitted in the manifest field the apply
config stage consumes. When no engine-activated configuration is on record, the manifest SHALL omit
the home-manager field.

#### Scenario: Captured manifest carries the activated config flake

- **WHEN** `capture` runs on a realizer host and the engine's provisioning history records an
  activated home-manager configuration
- **THEN** the written manifest SHALL include the home-manager configuration reference from that
  history
- **AND** that reference SHALL be the one the apply config stage uses to re-activate the configuration

#### Scenario: Most recently activated configuration is used

- **WHEN** the provisioning history contains a later generation that activated no home-manager
  configuration and an earlier generation that did
- **THEN** `capture` SHALL emit the configuration from the earlier generation (the one still in
  effect), not omit it

#### Scenario: No recorded configuration omits the field

- **WHEN** `capture` runs and the provisioning history records no activated home-manager configuration
- **THEN** the written manifest SHALL NOT include a home-manager field

#### Scenario: Capture then apply re-activates the same configuration

- **WHEN** a manifest produced by `capture` (carrying a home-manager configuration reference) is
  applied on a realizer host with configuration enabled
- **THEN** the engine SHALL re-activate the same home-manager configuration

### Requirement: Home-manager config recovery is best-effort and non-destructive

Recovering the home-manager configuration for capture SHALL NOT fail the capture command. If the
provisioning history cannot be read, capture SHALL still produce a valid package manifest and simply
omit the home-manager field. Capture SHALL remain read-only with respect to system and home-manager
state.

#### Scenario: Provisioning history unreadable

- **WHEN** `capture` runs and the provisioning history cannot be read
- **THEN** the engine SHALL still write a valid manifest of the captured packages
- **AND** it SHALL omit the home-manager field rather than returning an error

#### Scenario: Updating a manifest preserves an existing config reference

- **WHEN** `capture` updates an existing manifest that declares a home-manager configuration, and the
  provisioning history records no activated configuration
- **THEN** the engine SHALL preserve the existing manifest's home-manager configuration reference

### Requirement: Home-manager config content is out of scope for capture

Capture SHALL NOT capture home-manager configuration *content* (the managed files or a generated
configuration). It SHALL record only the configuration reference the engine activated. Capture of
configuration content is explicitly out of scope.

#### Scenario: No config content in the capture output

- **WHEN** `capture` records a home-manager configuration
- **THEN** the output SHALL contain only the configuration reference (the flake the engine activated)
- **AND** it SHALL NOT contain captured home-manager file content or a generated home-manager
  configuration
