## ADDED Requirements

### Requirement: Rollback optionally reverts the recorded home-manager config

The `rollback` command SHALL revert the home-manager configuration recorded in the target Provisioning
Generation when, and only when, configuration changes are enabled by the same opt-in used by apply. Without
that opt-in, `rollback` SHALL remain package-only and SHALL NOT change the home-manager configuration. When
the target generation recorded no home-manager configuration, the configuration SHALL be left unchanged.

#### Scenario: Config is reverted when configuration changes are enabled

- **WHEN** `rollback --to <N>` runs on the realizer with configuration changes enabled and Provisioning
  Generation N recorded a home-manager configuration
- **THEN** the engine SHALL revert the home-manager configuration to the one recorded by generation N
- **AND** it SHALL also perform the package rollback for generation N

#### Scenario: Config is untouched by default

- **WHEN** `rollback --to <N>` runs without configuration changes enabled
- **THEN** the engine SHALL roll back the package set only
- **AND** it SHALL NOT change the active home-manager configuration

#### Scenario: A generation without a recorded config leaves config unchanged

- **WHEN** `rollback --to <N>` runs with configuration changes enabled but generation N recorded no
  home-manager configuration
- **THEN** the engine SHALL leave the active home-manager configuration unchanged
- **AND** it SHALL NOT fail on account of the absent configuration

### Requirement: Config rollback re-activates the recorded generation append-only

To revert the configuration, the engine SHALL re-activate the home-manager generation recorded in the target
Provisioning Generation. Because the backend provides no arbitrary version pointer-move, re-activation SHALL
produce a new home-manager generation that reproduces the recorded configuration (append-only: the newest
generation is the active one). The user SHALL NOT be required to reference any backend-native generation
number.

#### Scenario: Re-activation reproduces the recorded configuration

- **WHEN** the engine reverts the home-manager configuration to the one recorded by generation N
- **THEN** the now-active home-manager configuration SHALL match the configuration recorded by generation N
- **AND** a new home-manager generation SHALL become the active one
- **AND** the user SHALL NOT have to reference any backend-native generation number

### Requirement: An unavailable snapshot is handled non-destructively

The engine SHALL handle an unavailable recorded snapshot non-destructively. When the target generation's
recorded home-manager snapshot is no longer available (for example, garbage collected), the engine SHALL fall
back to re-activating the recorded configuration source when it can do so faithfully, and SHALL otherwise
refuse without changing the active configuration, reporting how to recover.

#### Scenario: Directly-referenced flake falls back to re-activation

- **WHEN** the recorded snapshot for generation N is unavailable and generation N recorded a directly
  referenced home-manager flake
- **THEN** the engine SHALL re-activate that recorded flake
- **AND** the now-active configuration SHALL reflect generation N's configuration

#### Scenario: A generated-config snapshot that is gone is refused

- **WHEN** the recorded snapshot for generation N is unavailable and generation N recorded an
  engine-generated configuration (not a directly referenced flake)
- **THEN** the engine SHALL refuse with a stable error and SHALL NOT change the active home-manager configuration
- **AND** it SHALL report how to recover the desired configuration

### Requirement: Config rollback requires confirmation and supports preview

Because reverting the configuration changes system state, the config rollback SHALL be governed by the same
confirmation and preview behavior as package rollback: it SHALL require explicit confirmation, and a preview
mode SHALL report the configuration target without activating anything.

#### Scenario: Preview reports the config target without mutating

- **WHEN** `rollback --to <N>` runs in preview mode with configuration changes enabled and generation N
  recorded a home-manager configuration
- **THEN** the engine SHALL report the home-manager configuration target it would re-activate
- **AND** it SHALL NOT activate any home-manager configuration
- **AND** it SHALL NOT require confirmation

#### Scenario: Confirmed config rollback executes

- **WHEN** `rollback --to <N>` runs with confirmation and configuration changes enabled and a valid recorded config
- **THEN** the engine SHALL perform the config rollback

### Requirement: A backend that cannot re-activate config is refused without mutation

The engine SHALL refuse a config rollback the backend cannot perform, before mutating anything. When
configuration changes are enabled and the target generation recorded a home-manager configuration, but the
active package backend cannot re-activate a home-manager configuration, the engine SHALL refuse with a stable
error before performing any rollback, and SHALL NOT modify the package set or the configuration.

#### Scenario: Unsupported config rollback refuses up front

- **WHEN** `rollback --to <N>` runs with configuration changes enabled, generation N recorded a home-manager
  configuration, and the backend cannot re-activate it
- **THEN** the engine SHALL refuse with a stable error
- **AND** it SHALL NOT roll back the package set
- **AND** it SHALL NOT change the active home-manager configuration

### Requirement: The appended generation records the reverted config

After a successful config rollback, the engine SHALL record the now-active home-manager configuration in the
appended rollback Provisioning Generation, so the append-only history's newest record reflects the active
configuration as well as the active package set.

#### Scenario: The rollback generation records the now-active config

- **WHEN** a config rollback completes successfully
- **THEN** the appended rollback Provisioning Generation SHALL record the now-active home-manager
  configuration, including the resulting home-manager generation
- **AND** that generation SHALL be distinguishable as produced by a rollback

### Requirement: Config rollback stays separate from file restore and confines diagnostics

Config rollback SHALL be confined to home-manager generation re-activation: it SHALL NOT read or write the
configuration-file backup directory or the restore revert journal. Any raw backend output produced during a
config rollback SHALL be confined to the error detail channel and SHALL NOT appear in the user-facing message.

#### Scenario: Raw backend text is confined to detail

- **WHEN** a config rollback fails with backend diagnostic output
- **THEN** the raw backend text SHALL appear only in the error detail
- **AND** the user-facing message SHALL NOT contain the raw backend text

#### Scenario: Config rollback does not touch the file-restore layer

- **WHEN** a config rollback runs
- **THEN** the engine SHALL NOT read or write the configuration-file backup directory or the restore revert journal
