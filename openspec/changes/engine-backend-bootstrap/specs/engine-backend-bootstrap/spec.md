## ADDED Requirements

> SKETCH — this delta-spec is part of a DESIGN-ONLY change. The requirements below define the behavior
> the eventual implementation MUST satisfy; no engine code implements them yet. They are stated
> behavior-level and backend-agnostic so they survive the human's decisions on phasing, consent UX, and
> installer flavor (see `design.md` Open Questions). The **Homebrew-specific** bootstrap requirement is
> owned by `macos-brew-driver` §8; the requirements here are the shared contract that requirement is one
> instance of, plus the Nix-specific footprint.

### Requirement: A missing backend is bootstrapped only with explicit consent

The engine SHALL bootstrap a backend a run needs (the Nix realizer or the Homebrew driver) when it is
absent on macOS or Linux only after explicit, plain-language user consent, offering to install it and
proceeding only once the user agrees. The consent prompt SHALL describe the action in plain language
without requiring the user to know the backend's product name. The engine SHALL NOT install a backend
silently or without consent. When the backend is already present and working, the engine SHALL use it
directly without prompting or re-installing. When the user declines, the engine SHALL skip that
backend's lane with a clear message and SHALL still run the rest of the apply.

#### Scenario: Backend already present — no prompt, no install

- **WHEN** a run needs a backend that is already installed and working
- **THEN** the engine SHALL use it directly
- **AND** it SHALL NOT prompt for consent or attempt a bootstrap

#### Scenario: Backend absent — one plain-language consent before any install

- **WHEN** a run needs a backend that is not installed
- **THEN** the engine SHALL request consent in plain language describing the action without requiring
  knowledge of the backend's product name
- **AND** it SHALL install the backend only after the user explicitly consents
- **AND** it SHALL NOT install the backend silently

#### Scenario: Consent declined — lane skipped, run continues

- **WHEN** the user declines the bootstrap for a backend
- **THEN** the engine SHALL NOT install that backend
- **AND** it SHALL skip that backend's lane with a clear message
- **AND** it SHALL still run the remaining lanes and configuration of the apply

### Requirement: A bootstrapped backend is verified working before use

After installing a backend, the engine SHALL verify the backend actually works before provisioning any
package or configuration through it. A backend whose installer completes but whose verification probe
fails SHALL be treated as unavailable, and the engine SHALL surface a clear error rather than proceeding
as if the backend were present. A bootstrap that fails SHALL NOT leave the run silently half-completed.

#### Scenario: Post-install verification gates use

- **WHEN** the engine has just installed a backend
- **THEN** it SHALL run a verification probe (for example, the backend's version command or an
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
than vendoring, forking, or re-implementing it. The privileged, system-modifying step SHALL be explicit
and inspectable, consistent with the engine's non-destructive, no-silent-mutation posture. The engine
SHALL NOT suppress operating-system credential or component prompts (such as the macOS administrator
password or Xcode Command Line Tools install) that the official installer requires.

#### Scenario: Nix bootstrap runs the official installer

- **WHEN** the engine bootstraps the Nix backend
- **THEN** it SHALL run the official Determinate Nix installer
- **AND** it SHALL NOT use a vendored or re-implemented installer

#### Scenario: Homebrew bootstrap runs the official installer

- **WHEN** the engine bootstraps the Homebrew backend
- **THEN** it SHALL run the official upstream Homebrew installer
- **AND** it SHALL NOT use a vendored or re-implemented installer

#### Scenario: Operating-system prompts are not suppressed

- **WHEN** the official installer triggers an operating-system credential or component prompt
- **THEN** the engine SHALL let that prompt proceed
- **AND** it SHALL NOT attempt to suppress or bypass it

### Requirement: Nix backend bootstrap accounts for its heavier footprint

The engine SHALL treat bootstrapping the Nix backend as a heavier, privileged system change than the
Homebrew bootstrap: a multi-user installation with a background daemon and, on macOS, a dedicated store
volume. The engine SHALL NOT silently uninstall a backend it installed; removing a backend SHALL be a
separate, explicit, user-owned action. On Windows the engine SHALL NOT attempt a backend bootstrap,
because the platform package backend ships with the operating system.

#### Scenario: Nix is bootstrapped as a multi-user system change

- **WHEN** the engine bootstraps Nix on macOS or Linux after consent
- **THEN** it SHALL use the multi-user installation (background daemon, and on macOS the dedicated store
  volume) provided by the official installer

#### Scenario: The engine does not silently remove a backend it installed

- **WHEN** a backend was bootstrapped by the engine
- **THEN** the engine SHALL NOT silently uninstall it as part of any later run
- **AND** removing the backend SHALL require a separate, explicit, user-owned action

#### Scenario: Windows needs no backend bootstrap

- **WHEN** a run executes on Windows
- **THEN** the engine SHALL NOT attempt to bootstrap a package backend
- **AND** it SHALL use the operating-system-provided backend directly
