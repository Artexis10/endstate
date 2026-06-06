## ADDED Requirements

### Requirement: A missing backend is bootstrapped only with explicit consent

The engine SHALL bootstrap a package backend a run needs (the Nix realizer or the Homebrew driver)
when it is absent on macOS or Linux only after explicit, plain-language user consent. When the backend
is already present and working, the engine SHALL use it directly without prompting or re-installing.
When a run needs more than one absent backend, the engine SHALL request consent **once** for the
combined set rather than once per backend. The engine SHALL NOT install a backend silently or without
consent. When the user declines, the engine SHALL skip that backend's lane with a clear message and
SHALL still run the rest of the apply. Installation SHALL be attempted only during the mutating
`apply` command; read-only commands SHALL detect the backend and use it or skip its lane without
attempting an install.

#### Scenario: Backend already present — no prompt, no install

- **WHEN** a run needs a backend that is already installed and working
- **THEN** the engine SHALL use it directly
- **AND** it SHALL NOT request consent or attempt a bootstrap

#### Scenario: Backend absent — one combined consent before any install

- **WHEN** an `apply` run needs one or more backends that are not installed
- **THEN** the engine SHALL request consent once, in plain language, describing the action without
  requiring knowledge of the backend's product name
- **AND** it SHALL install the needed backends only after the user explicitly consents
- **AND** it SHALL NOT install any backend silently

#### Scenario: Consent declined — lane skipped, run continues

- **WHEN** the user declines the bootstrap for a backend an `apply` needs
- **THEN** the engine SHALL NOT install that backend
- **AND** it SHALL skip that backend's lane with a clear message
- **AND** it SHALL still run the remaining lanes and configuration of the apply

#### Scenario: Read-only command does not install an absent backend

- **WHEN** a read-only command (for example `verify` or `plan`) runs and a backend it would use is absent
- **THEN** the engine SHALL NOT attempt to install the backend
- **AND** it SHALL NOT request install consent

### Requirement: A bootstrapped backend is verified working before use

After installing a backend, the engine SHALL verify the backend actually works before provisioning any
package or configuration through it. A backend whose installer completes but whose verification probe
fails SHALL be treated as unavailable, and the engine SHALL surface a clear error rather than
proceeding as if the backend were present. A bootstrap that fails SHALL NOT leave the run silently
half-completed.

#### Scenario: Post-install verification gates use

- **WHEN** the engine has just installed a backend
- **THEN** it SHALL run a verification probe (for example the backend's version command or an
  evaluation) confirming the backend works
- **AND** it SHALL provision through the backend only after that probe passes

#### Scenario: Verification failure is surfaced, not silent

- **WHEN** a backend installs but its verification probe fails
- **THEN** the engine SHALL treat the backend as unavailable
- **AND** it SHALL report a clear error
- **AND** it SHALL NOT proceed as if the backend were available

### Requirement: The engine orchestrates the official installer, never a vendored fork

When bootstrapping a backend, the engine SHALL run the backend's official upstream installer — the
Determinate installer for Nix and the upstream installer script for Homebrew — orchestrating it rather
than vendoring, forking, or re-implementing it. The privileged, system-modifying step SHALL be
inspectable. The engine SHALL NOT suppress operating-system credential or component prompts (such as
the macOS administrator password or the Xcode Command Line Tools install) that the official installer
requires.

#### Scenario: Homebrew bootstrap runs the official installer

- **WHEN** the engine bootstraps the Homebrew backend
- **THEN** it SHALL run the official upstream Homebrew installer
- **AND** it SHALL NOT use a vendored or re-implemented installer

#### Scenario: Nix bootstrap runs the official installer

- **WHEN** the engine bootstraps the Nix backend
- **THEN** it SHALL run the official Determinate Nix installer
- **AND** it SHALL NOT use a vendored or re-implemented installer

#### Scenario: Operating-system prompts are not suppressed

- **WHEN** the official installer triggers an operating-system credential or component prompt
- **THEN** the engine SHALL let that prompt proceed
- **AND** it SHALL NOT attempt to suppress or bypass it

### Requirement: Backend bootstrap consent is flag-driven with a streamed consent request

The engine SHALL accept the user's bootstrap consent through command-line flags — an opt-in flag that
authorizes installing absent backends and an opt-out flag that forces skipping them. When neither flag
is given and an `apply` needs an absent backend, the engine SHALL emit a single streamed
consent-request event describing the combined set of absent backends in plain language, including an
inspectable details field carrying the exact installer commands, and SHALL default to skipping the
lane with a clear message rather than installing. The engine SHALL NOT name the backend's product in
the plain-language portion of the consent.

#### Scenario: Opt-in flag authorizes the install

- **WHEN** `apply` runs with the bootstrap opt-in flag and a needed backend is absent
- **THEN** the engine SHALL install that backend via its official installer and verify it before use

#### Scenario: Opt-out flag forces a skip

- **WHEN** `apply` runs with the bootstrap opt-out flag and a needed backend is absent
- **THEN** the engine SHALL NOT install the backend
- **AND** it SHALL skip that backend's lane with a clear message and continue the run

#### Scenario: No flag — consent requested and lane skipped by default

- **WHEN** `apply` needs an absent backend and neither bootstrap flag is given
- **THEN** the engine SHALL emit one consent-request event covering the combined set of absent
  backends, with a plain-language message and an inspectable details field
- **AND** it SHALL default to skipping the lane with a clear message rather than installing silently

### Requirement: The Nix backend bootstrap accounts for its heavier footprint and is never silently removed

The engine SHALL treat bootstrapping the Nix realizer as a heavier, privileged system change than the
Homebrew bootstrap: a multi-user installation with a background daemon and, on macOS, a dedicated store
volume, installed by the official Determinate installer. When the Nix realizer is needed by a run but is
unavailable — the user declined the bootstrap, or it failed verification — the engine SHALL skip the
realizer lane with a clear message and SHALL still run any other available lane (for example, an
already-consented Homebrew lane), rather than aborting the run or leaving a half-done apply. The engine
SHALL NOT silently uninstall a backend it installed; removing a backend SHALL be a separate, explicit,
user-owned action. On Windows the engine SHALL NOT attempt a backend bootstrap, because the platform
package backend ships with the operating system.

#### Scenario: Nix is bootstrapped as a multi-user system change

- **WHEN** the engine bootstraps Nix on macOS or Linux after consent
- **THEN** it SHALL use the multi-user installation (background daemon, and on macOS the dedicated store
  volume) provided by the official Determinate installer

#### Scenario: Declined Nix skips the realizer lane but a consented brew lane still runs

- **WHEN** an `apply` needs both the Nix realizer and the Homebrew driver, the user consents, Homebrew
  installs and verifies, but Nix is unavailable (declined or failed)
- **THEN** the engine SHALL skip the realizer-lane apps with a clear message
- **AND** it SHALL still install the brew-routed apps through Homebrew
- **AND** the run SHALL NOT return a top-level error solely because the realizer lane was skipped

#### Scenario: The engine does not silently remove a backend it installed

- **WHEN** a backend was bootstrapped by the engine
- **THEN** the engine SHALL NOT silently uninstall it as part of any later run
- **AND** removing the backend SHALL require a separate, explicit, user-owned action

#### Scenario: Windows needs no backend bootstrap

- **WHEN** a run executes on Windows
- **THEN** the engine SHALL NOT attempt to bootstrap a package backend
- **AND** it SHALL use the operating-system-provided backend directly
