## MODIFIED Requirements

### Requirement: A missing backend is bootstrapped only with explicit consent
The engine SHALL bootstrap a package backend a run needs (Nix, Homebrew, or Chocolatey) only after explicit consent. An already working backend SHALL be used without reinstalling it. When more than one backend is absent, the engine SHALL request consent once for the combined set. When consent is declined or unanswered, the engine SHALL skip each absent backend's lane visibly and continue any available lanes. Installation SHALL be attempted only during mutating apply/rebuild execution; read-only commands SHALL surface unavailability without installing or requesting consent.

#### Scenario: Backend already present — no prompt, no install
- **WHEN** a run needs a backend that is already installed and working
- **THEN** the engine SHALL use it directly
- **AND** it SHALL NOT request consent or attempt bootstrap

#### Scenario: Backend absent — one combined consent before any install
- **WHEN** an apply or rebuild needs one or more absent backends
- **THEN** the engine SHALL request consent once for the combined set
- **AND** it SHALL install them only after explicit consent

#### Scenario: Consent declined — lanes skipped, run continues
- **WHEN** consent is declined for absent backends
- **THEN** the engine SHALL NOT install them
- **AND** it SHALL visibly skip their lanes and continue available work

#### Scenario: Read-only command does not install an absent backend
- **WHEN** verify, plan, or dry-run encounters an absent backend
- **THEN** it SHALL NOT install the backend or request mutating consent

### Requirement: The engine orchestrates the official installer, never a vendored fork
When bootstrapping a backend, the engine SHALL orchestrate its official upstream installer: Determinate Nix, upstream Homebrew, or Chocolatey's official PowerShell installation endpoint. It SHALL NOT vendor, fork, or reimplement those installers, suppress operating-system credential prompts, or mutate Chocolatey source configuration.

#### Scenario: Homebrew bootstrap runs the official installer
- **WHEN** the engine bootstraps Homebrew
- **THEN** it SHALL run the official upstream installer

#### Scenario: Nix bootstrap runs the official installer
- **WHEN** the engine bootstraps Nix
- **THEN** it SHALL run the official Determinate installer

#### Scenario: Chocolatey bootstrap runs the official installer
- **WHEN** the engine bootstraps Chocolatey
- **THEN** it SHALL invoke Chocolatey's official PowerShell bootstrap path
- **AND** it SHALL NOT add or change package sources

#### Scenario: Operating-system prompts are not suppressed
- **WHEN** an official installer triggers a credential or component prompt
- **THEN** the engine SHALL let that prompt proceed

### Requirement: Backend bootstrap consent is flag-driven with a streamed consent request
The engine SHALL use the existing `--bootstrap-backends` opt-in and `--no-bootstrap` opt-out flags for all supported backends, including Chocolatey. When neither is given and apply needs absent backends, the engine SHALL emit one consent-request event covering the combined set and default to visibly skipping those lanes. Rebuild SHALL propagate the same flags to its apply stage.

#### Scenario: Opt-in flag authorizes installation
- **WHEN** apply or rebuild runs with `--bootstrap-backends` and a needed backend is absent
- **THEN** the engine SHALL install and verify that backend before use

#### Scenario: Opt-out flag forces a skip
- **WHEN** apply or rebuild runs with `--no-bootstrap` and a needed backend is absent
- **THEN** it SHALL skip the absent backend's lane and continue without installing

#### Scenario: No flag — combined consent requested and lanes skipped
- **WHEN** apply needs absent backends and neither flag is given
- **THEN** it SHALL emit one consent-request event with inspectable installer details
- **AND** default to skipping the absent lanes

### Requirement: The Nix backend bootstrap accounts for its heavier footprint and is never silently removed
The engine SHALL retain Nix's heavier multi-user bootstrap treatment and SHALL never silently uninstall any backend it installed. Winget SHALL never be bootstrapped because it is operating-system provided. Chocolatey MAY be bootstrapped on Windows only when a selected manifest lane needs it and the existing consent flow authorizes it.

#### Scenario: Nix is bootstrapped as a multi-user system change
- **WHEN** the engine bootstraps Nix on macOS or Linux
- **THEN** it SHALL use the official multi-user installation

#### Scenario: Declined Nix skips the realizer lane but available Brew still runs
- **WHEN** Nix remains unavailable while Brew is available in the same apply
- **THEN** the engine SHALL skip Nix apps and continue Brew apps

#### Scenario: The engine does not silently remove a backend it installed
- **WHEN** a backend was bootstrapped by the engine
- **THEN** removing it SHALL require a separate explicit user-owned action

#### Scenario: Windows bootstraps only optional Chocolatey
- **WHEN** a Windows run needs Winget and Chocolatey but Chocolatey is absent
- **THEN** Winget SHALL be used directly
- **AND** only Chocolatey SHALL enter the consent-gated bootstrap flow
