## ADDED Requirements

### Requirement: Capture preserves the originally-declared home-manager input

When capture records a home-manager configuration, it SHALL emit the input the user originally
declared — a configuration-file reference when the configuration was applied from a user
configuration file, or a flake reference when it was applied from a flake — rather than an
engine-generated artifact. A configuration applied from a user configuration file SHALL NOT be
captured as an engine-generated, machine-local flake reference, so that the captured manifest
round-trips through the apply config stage on another machine.

#### Scenario: Config-declared configuration is captured as a config reference

- **WHEN** a home-manager configuration was applied from a user configuration file (a `home.nix` the
  engine wrapped into a generated flake) and `capture` then runs
- **THEN** the written manifest SHALL carry the user's declared configuration-file reference
- **AND** it SHALL NOT carry the engine-generated, machine-local flake reference

#### Scenario: Flake-declared configuration is captured as a flake reference

- **WHEN** a home-manager configuration was applied directly from a flake and `capture` then runs
- **THEN** the written manifest SHALL carry that flake reference (unchanged)

#### Scenario: Captured config-declared manifest re-applies

- **WHEN** a manifest captured from a config-declared machine is applied on a realizer host with
  configuration enabled
- **THEN** the engine SHALL re-activate the same home-manager configuration from the declared
  configuration file
