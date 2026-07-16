## MODIFIED Requirements

### Requirement: A missing backend is bootstrapped only with explicit consent
The engine SHALL bootstrap a package backend a run needs (the Nix realizer, the Homebrew driver, or the Chocolatey driver) only after explicit, plain-language user consent. An already working backend SHALL be used without prompting or reinstalling it. When more than one backend is absent, the engine SHALL request consent once for the combined set rather than once per backend. The engine SHALL NOT install a backend silently or without consent. When consent is declined or unanswered, the engine SHALL skip each absent backend's lane visibly and continue any available lanes and configuration. Installation SHALL be attempted only during mutating apply/rebuild execution; read-only commands, including plan, verify, and dry-run, SHALL surface unavailability without installing or requesting consent.

#### Scenario: Backend already present — no prompt, no install
- **WHEN** a run needs a backend that is already installed and working
- **THEN** the engine SHALL use it directly
- **AND** it SHALL NOT request consent or attempt bootstrap

#### Scenario: Backend absent — one combined consent before any install
- **WHEN** an apply or rebuild needs one or more absent backends
- **THEN** the engine SHALL request consent once, in plain language, for the combined set without requiring knowledge of or naming a backend product in the plain-language portion
- **AND** it SHALL install them only after explicit consent
- **AND** it SHALL NOT install any backend silently

#### Scenario: Consent declined — lanes skipped, run continues
- **WHEN** consent is declined for absent backends
- **THEN** the engine SHALL NOT install them
- **AND** it SHALL visibly skip their lanes and continue available lanes and configuration

#### Scenario: Read-only command does not install an absent backend
- **WHEN** verify, plan, or dry-run encounters an absent backend
- **THEN** it SHALL NOT install the backend or request mutating consent

### Requirement: The engine orchestrates the official installer, never a vendored fork
When bootstrapping a backend, the engine SHALL orchestrate its official upstream installer: the Determinate installer for Nix, the upstream installer script for Homebrew, or Chocolatey's official PowerShell installation endpoint. It SHALL NOT vendor, fork, or reimplement those installers. The privileged, system-modifying step SHALL be inspectable. The engine SHALL NOT suppress or bypass operating-system credential or component prompts, such as the macOS administrator password or Xcode Command Line Tools installation, and SHALL NOT mutate Chocolatey source configuration.

#### Scenario: Homebrew bootstrap runs the official installer
- **WHEN** the engine bootstraps Homebrew
- **THEN** it SHALL run the official upstream installer
- **AND** it SHALL NOT use a vendored, forked, or reimplemented installer

#### Scenario: Nix bootstrap runs the official installer
- **WHEN** the engine bootstraps Nix
- **THEN** it SHALL run the official Determinate installer
- **AND** it SHALL NOT use a vendored, forked, or reimplemented installer

#### Scenario: Chocolatey bootstrap runs the official installer
- **WHEN** the engine bootstraps Chocolatey
- **THEN** it SHALL invoke Chocolatey's official PowerShell bootstrap path
- **AND** it SHALL NOT use a vendored, forked, or reimplemented installer
- **AND** it SHALL NOT add or change package sources

#### Scenario: Operating-system prompts are not suppressed
- **WHEN** an official installer triggers a credential or component prompt
- **THEN** the engine SHALL let that prompt proceed
- **AND** it SHALL NOT suppress or bypass the prompt

### Requirement: Backend bootstrap consent is flag-driven with a streamed consent request
The engine SHALL use the existing `--bootstrap-backends` opt-in and `--no-bootstrap` opt-out flags for all supported backends, including Chocolatey. When neither is given and apply needs absent backends, the engine SHALL emit one streamed consent-request event covering the combined set in plain, product-neutral language. The event SHALL include an inspectable details field carrying the exact installer commands and SHALL default to visibly skipping those lanes rather than installing. Rebuild SHALL propagate the same flags to its apply stage.

#### Scenario: Opt-in flag authorizes installation
- **WHEN** apply or rebuild runs with `--bootstrap-backends` and a needed backend is absent
- **THEN** the engine SHALL install and verify that backend before use

#### Scenario: Opt-out flag forces a skip
- **WHEN** apply or rebuild runs with `--no-bootstrap` and a needed backend is absent
- **THEN** it SHALL skip the absent backend's lane and continue without installing

#### Scenario: No flag — combined consent requested and lanes skipped
- **WHEN** apply needs absent backends and neither flag is given
- **THEN** it SHALL emit one consent-request event covering the combined set with a plain-language, product-neutral message
- **AND** the inspectable details SHALL include the exact installer commands
- **AND** it SHALL default to skipping the absent lanes rather than installing silently

### Requirement: The Nix backend bootstrap accounts for its heavier footprint and is never silently removed
The engine SHALL treat bootstrapping the Nix realizer as a heavier, privileged system change than Homebrew or Chocolatey: a multi-user installation with a background daemon and, on macOS, a dedicated store volume, installed by the official Determinate installer. When Nix remains unavailable because bootstrap was declined or verification failed, the engine SHALL visibly skip the realizer lane and continue any other available lane, including Brew, without a top-level error solely for the skipped Nix lane. The engine SHALL NOT silently uninstall any backend it installed; removal SHALL be a separate explicit user-owned action. Winget SHALL never be bootstrapped because it is operating-system provided. Chocolatey MAY be bootstrapped on Windows only when a selected manifest lane needs it and the existing consent flow authorizes it.

#### Scenario: Nix is bootstrapped as a multi-user system change
- **WHEN** the engine bootstraps Nix on macOS or Linux
- **THEN** it SHALL use the official multi-user installation with a background daemon
- **AND** on macOS it SHALL use the installer's dedicated store volume

#### Scenario: Declined Nix skips the realizer lane but available Brew still runs
- **WHEN** Nix remains unavailable while Brew is available in the same apply
- **THEN** the engine SHALL skip Nix apps and continue Brew apps
- **AND** the run SHALL NOT return a top-level error solely because the Nix lane was skipped

#### Scenario: The engine does not silently remove a backend it installed
- **WHEN** a backend was bootstrapped by the engine
- **THEN** the engine SHALL NOT silently uninstall it as part of a later run
- **AND** removing it SHALL require a separate explicit user-owned action

#### Scenario: Windows bootstraps only optional Chocolatey
- **WHEN** a Windows run needs Winget and Chocolatey but Chocolatey is absent
- **THEN** Winget SHALL be used directly
- **AND** only Chocolatey SHALL enter the consent-gated bootstrap flow
