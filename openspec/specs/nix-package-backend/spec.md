# nix-package-backend Specification

## Purpose
Defines the Nix realizer — the package backend on Linux and macOS: platform selection, whole-set atomic generation apply, per-item event fan-out, pinned installable resolution, and stable error-code classification.
## Requirements
### Requirement: Nix backend is selected on Linux and macOS
The engine SHALL select a Nix package realizer on Linux and macOS hosts, and SHALL leave winget selection on Windows unchanged.

#### Scenario: Linux selects the Nix realizer
- **WHEN** the engine selects a backend on a Linux host
- **THEN** the Nix realizer SHALL be used
- **AND** no winget driver SHALL be selected

#### Scenario: macOS selects the Nix realizer
- **WHEN** the engine selects a backend on a macOS host
- **THEN** the Nix realizer SHALL be used

#### Scenario: Windows selection unchanged
- **WHEN** the engine selects a backend on a Windows host
- **THEN** the winget driver SHALL be used
- **AND** the Nix realizer SHALL NOT be selected

### Requirement: Apply realizes the desired package set as one atomic generation switch
Apply SHALL compute a single plan over the desired Nix package set and SHALL realize the packages to add with one operation that advances the profile generation only on full success, using `nix profile add` (not the deprecated `install` alias).

#### Scenario: Full success advances the generation
- **WHEN** every package to add builds and commits successfully
- **THEN** the profile generation SHALL advance
- **AND** each added package SHALL be reported with status `installed`

#### Scenario: Any failure leaves the prior generation intact
- **WHEN** at least one package in the operation fails
- **THEN** the profile generation SHALL NOT advance
- **AND** no package SHALL be installed by that run
- **AND** the prior generation SHALL remain the current generation

#### Scenario: Already-present packages are not re-added
- **WHEN** a desired package is already in the current generation
- **THEN** it SHALL be reported with status `present` and reason `already_installed`
- **AND** it SHALL NOT be passed to `nix profile add`

#### Scenario: Dry run makes no changes
- **WHEN** apply runs with `--dry-run`
- **THEN** the plan SHALL be emitted
- **AND** no generation switch SHALL be performed

### Requirement: The realizer result is fanned into the per-item event stream
The realizer apply path SHALL emit plan, apply, and verify phase events and one item event per package using the existing item-event vocabulary, so the event contract is preserved.

#### Scenario: Per-item plan events
- **WHEN** the realizer plan is computed
- **THEN** one item event SHALL be emitted per package with status `present` or `to_install`
- **AND** exactly one `plan` summary event SHALL be emitted

#### Scenario: Installing precedes a terminal status
- **WHEN** a package is applied
- **THEN** an `installing` item event SHALL precede its terminal `installed` or `failed` item event
- **AND** the item `driver` field SHALL be `nix`

#### Scenario: One summary per phase
- **WHEN** a phase completes
- **THEN** exactly one summary event SHALL be emitted for that phase
- **AND** the last event in the stream SHALL be a summary event, except when a systemic error truncates the stream

### Requirement: Nix package references resolve to pinned installables
The engine SHALL resolve `App.Refs["linux"]`/`App.Refs["darwin"]` to a pinned nixpkgs installable on the realizer path, and SHALL skip apps that have no Nix reference rather than passing a non-Nix reference to Nix.

#### Scenario: Bare attribute is pinned
- **WHEN** a Nix ref is a bare attribute name
- **THEN** it SHALL resolve to a pinned nixpkgs flake installable

#### Scenario: Explicit flakeref passes through
- **WHEN** a Nix ref is already a flakeref installable
- **THEN** it SHALL be passed to `nix profile add` unmodified

#### Scenario: App with no Nix ref is skipped
- **WHEN** an app has no `linux`/`darwin` ref on the realizer path
- **THEN** the app SHALL be skipped
- **AND** no winget-style reference SHALL be passed to `nix profile add`

### Requirement: Nix failures translate to stable engine error codes
The engine SHALL classify Nix outcomes into stable engine error codes from the process exit code, internal-json activity, and whether the generation advanced, and SHALL place raw Nix text only in `error.detail`.

#### Scenario: Daemon or store unavailable
- **WHEN** the Nix daemon or store is unreachable
- **THEN** the engine SHALL return error code `REALIZER_UNAVAILABLE`
- **AND** the engine SHALL surface remediation without requiring the user to interpret Nix output

#### Scenario: Nix binary missing or unrunnable
- **WHEN** the `nix` binary cannot be spawned
- **THEN** the engine SHALL return error code `REALIZER_UNAVAILABLE`

#### Scenario: Permission or read-only store failure
- **WHEN** Nix reports a permission-denied or read-only-store failure
- **THEN** the engine SHALL return error code `PERMISSION_DENIED`

#### Scenario: Evaluation or attribute failure
- **WHEN** a Nix attribute is undefined or a package is not found
- **THEN** the package SHALL fail with error code `INSTALL_FAILED`
- **AND** the detail SHALL carry subcode `eval`

#### Scenario: Unrecognised failure
- **WHEN** no anchor matches the Nix output
- **THEN** the error code SHALL be `INSTALL_FAILED`
- **AND** the raw Nix message SHALL appear only in `error.detail`
- **AND** the raw Nix message SHALL NOT appear in `error.message` or any item-event `message`

### Requirement: Nix error classification is locked by a contract test
The engine SHALL derive the top-level error class from a fixed anchor table validated against captured real-Nix output, and SHALL derive subcode and pipeline stage from structural signals.

#### Scenario: Anchors are validated against real output
- **WHEN** the classifier is tested
- **THEN** each anchor SHALL be asserted against a captured real-Nix stderr fixture for its expected code and subcode
- **AND** the test SHALL fail if an anchor stops matching its fixture or its class changes

#### Scenario: Structural signal carries the stage
- **WHEN** Nix emits build or fetch activity
- **THEN** the pipeline stage and subcode SHALL be derived from activity type and generation-advance without relying on message text

### Requirement: Verify reflects the current generation
Verify on the realizer path SHALL report each desired package present or missing by reading the current profile generation.

#### Scenario: Present package passes verify
- **WHEN** a desired package is in the current generation during verify
- **THEN** it SHALL be reported `present`

#### Scenario: Missing package fails verify
- **WHEN** a desired package is absent from the current generation during verify
- **THEN** it SHALL be reported `failed` with reason `missing`

### Requirement: Capabilities advertises the Nix backend on Linux and macOS
The `capabilities` data SHALL include `nix` among the available drivers on Linux and macOS, and SHALL remain unchanged on Windows.

#### Scenario: Nix advertised on Linux and macOS
- **WHEN** `capabilities` runs on a Linux or macOS host
- **THEN** the drivers list SHALL include `nix`

#### Scenario: Windows capabilities unchanged
- **WHEN** `capabilities` runs on a Windows host
- **THEN** the operating system SHALL be `windows`
- **AND** the drivers list SHALL be `["winget"]`

### Requirement: Nix selection does not change Windows behavior
The introduction of the Nix backend SHALL NOT alter Windows package selection, installation, verification, or the per-item event stream.

#### Scenario: Windows runs the existing per-package path
- **WHEN** apply or verify runs on a Windows host
- **THEN** no Nix code path SHALL execute
- **AND** the winget per-package install/detect path SHALL run unchanged

