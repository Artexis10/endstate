## ADDED Requirements

### Requirement: Dry-Run State Is Disclosed In Result Surfaces

A run that changed nothing SHALL be distinguishable from one that did. Any consumer presenting apply results SHALL read the envelope's `dryRun` field and SHALL NOT report completion, installs, or success for a dry run. The engine already emits `dryRun` on every apply envelope; this requirement binds consumers to it.

#### Scenario: Dry-run results are not labelled as completed setup

- **WHEN** a consumer presents results from an apply envelope where `data.dryRun` is `true`
- **THEN** the surface SHALL NOT state that setup completed
- **AND** the surface SHALL indicate that this was a preview and nothing was installed

#### Scenario: Real apply results are labelled as completed setup

- **WHEN** a consumer presents results from an apply envelope where `data.dryRun` is `false`
- **THEN** the surface MAY state that setup completed
- **AND** installed counts SHALL be presented as actual installs

#### Scenario: Dry run reports no installs

- **WHEN** `apply --dry-run --json` is run against a manifest containing an app that is not present
- **THEN** the action's status SHALL be `to_install`
- **AND** `summary.success` SHALL be `0`
- **AND** no package installation SHALL be attempted

#### Scenario: to_install never appears as a final status on a real apply

- **WHEN** `apply --json` (non-dry-run) completes for an app that was missing
- **THEN** that app's final status SHALL be `installed` or `failed`
- **AND** SHALL NOT be `to_install`

#### Scenario: Provisioning action defaults to a real apply

- **WHEN** a consumer's primary provisioning action is invoked without the user having explicitly opted into dry run
- **THEN** the consumer SHALL invoke apply without `--dry-run`

#### Scenario: Explicit dry-run preference is honored

- **WHEN** the user has explicitly enabled a dry-run preference
- **THEN** the consumer SHALL invoke apply with `--dry-run`
- **AND** the results surface SHALL disclose the dry run per the scenarios above
