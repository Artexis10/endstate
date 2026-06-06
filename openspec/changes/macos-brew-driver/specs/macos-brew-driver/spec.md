## ADDED Requirements

> SKETCH — this delta-spec is part of a DESIGN-ONLY change. The requirements below define the behavior
> the eventual implementation MUST satisfy; no engine code implements them yet. They are stated
> behavior-level so they survive the human's decisions on ref scheme and routing default (see
> `design.md` Open Questions).

### Requirement: Brew installs both formulae and Casks

The engine SHALL install macOS packages through Homebrew as a per-package driver, installing a CLI
**formula** (`brew install <name>`) or a GUI **Cask** (`brew install --cask <name>`) according to the
app's `darwin` reference, with Casks treated as a first-class package kind. The driver SHALL determine
formula-vs-Cask from the reference (recommended: a `cask:` prefix marks a Cask; a bare reference is a
formula) without requiring the manifest to encode any other macOS-specific package metadata.

#### Scenario: A Cask reference installs a GUI app

- **WHEN** `apply` runs on macOS for an app routed to the brew driver whose `darwin` reference marks it
  as a Cask
- **THEN** the engine SHALL install it as a Homebrew Cask (the `brew install --cask` path)
- **AND** the result SHALL be reported as installed (or present when it is already installed)

#### Scenario: A bare reference installs a formula

- **WHEN** `apply` runs on macOS for an app routed to the brew driver whose `darwin` reference is a
  bare name with no Cask marker
- **THEN** the engine SHALL install it as a Homebrew formula (the `brew install <name>` path)

#### Scenario: A failed install does not abort the remaining packages

- **WHEN** one brew package fails to install during an `apply`
- **THEN** the engine SHALL report that package as failed
- **AND** it SHALL continue attempting the remaining brew packages

### Requirement: Capture records both formulae and Casks

When capturing on macOS, the engine SHALL record installed Homebrew formulae and Casks into the
manifest as bare package names routed to the brew driver, recording top-level formulae (those a user
explicitly installed, excluding dependencies) and installed Casks, each with its installed version
recorded best-effort. A version the package manager does not expose SHALL be recorded as empty rather
than failing the capture.

#### Scenario: Capture records top-level formulae and Casks

- **WHEN** `capture` runs on a macOS host that has Homebrew formulae and Casks installed
- **THEN** the captured manifest SHALL include the top-level formulae (excluding dependency-only
  formulae) and the installed Casks
- **AND** each captured brew app SHALL be routed to the brew driver, with Casks marked as Casks

#### Scenario: A missing version does not fail capture

- **WHEN** Homebrew exposes no version for a captured package
- **THEN** the engine SHALL record an empty version for that package
- **AND** the capture SHALL NOT fail because a version was unavailable

#### Scenario: Captured brew apps round-trip back to brew

- **WHEN** a manifest captured from a brew-provisioned macOS host is re-applied
- **THEN** the previously captured formulae and Casks SHALL be installed again through the brew driver
- **AND** they SHALL NOT be mis-attributed to the Nix realizer

### Requirement: The brew backend is best-effort and non-atomic

The brew driver SHALL operate per package without a whole-set atomic generation and without native
rollback. Rollback for the brew backend SHALL reuse the engine's best-effort uninstall pattern —
uninstalling the packages recorded as added after the target generation — rather than a native
generation switch, and SHALL surface that package-manager-pulled transitive dependencies are not
tracked and may remain installed. The brew driver SHALL default to a non-destructive uninstall that
does not remove user data.

#### Scenario: Brew rollback uninstalls later additions best-effort

- **WHEN** `rollback` runs against the brew backend for a target generation
- **THEN** the engine SHALL uninstall the brew packages recorded as added after the target generation
- **AND** it SHALL continue past a per-package uninstall failure and report it
- **AND** it SHALL surface the untracked-transitive-dependency caveat

#### Scenario: Brew offers no native generation rollback

- **WHEN** a rollback is requested on the brew backend
- **THEN** the engine SHALL NOT attempt a native whole-set generation switch
- **AND** it SHALL use the best-effort uninstall path instead

### Requirement: Per-app driver selection routes brew versus nix on darwin

On macOS, where both the Nix realizer and the brew driver are available, the engine SHALL route each
manifest app to a backend: an app declaring the brew driver — or whose `darwin` reference is a Homebrew
Cask (the `cask:` prefix) — SHALL be provisioned through Homebrew, and every other app together with all
home-manager configuration SHALL be provisioned through the Nix realizer, which remains the default. The
engine SHALL run both backends in the same `apply`, `capture`, `verify`, and `plan` invocation rather
than letting one backend exclude the other. The Cask auto-routing default (a `cask:` reference routes to
brew without `driver: "brew"`) is owned by the `engine-backend-bootstrap-impl` capability; this
requirement composes with it.

#### Scenario: A brew-driver app and a default app coexist in one apply

- **WHEN** `apply` runs on macOS for a manifest containing one app that declares the brew driver and
  one app (or home-manager configuration) with no driver declared
- **THEN** the engine SHALL provision the brew-driver app through Homebrew
- **AND** it SHALL provision the default app and the home-manager configuration through the Nix
  realizer in the same run

#### Scenario: The Nix realizer remains the darwin default

- **WHEN** an app on macOS declares no driver
- **THEN** the engine SHALL provision it through the Nix realizer
- **AND** the behavior of a manifest that declares no brew-driver apps SHALL be unchanged from the
  realizer-only path

#### Scenario: A Cask reference without the brew driver auto-routes to brew

- **WHEN** a manifest declares an app whose `darwin` reference marks it as a Cask but does not declare
  the brew driver
- **THEN** the engine SHALL route that app to the brew lane by default (the `cask:` prefix is the
  routing signal) rather than rejecting the manifest at load
- **AND** it SHALL NOT pass the Cask reference to the Nix realizer

### Requirement: Brew verifies presence and best-effort version

The engine SHALL verify a brew app by its installed presence (`brew list`) and SHALL treat a declared
version as a best-effort pin, given Homebrew's weak version-selection model. Verify SHALL report
version drift when the installed version differs from a declared version, but `apply` SHALL NOT
downgrade or reinstall a present package solely to match a declared version that Homebrew cannot
select.

#### Scenario: Presence is the verify check

- **WHEN** `verify` runs for a brew app that is installed
- **THEN** the engine SHALL report it as present based on its Homebrew installation
- **AND** a brew app that is not installed SHALL be reported as missing

#### Scenario: A declared version is advisory for formulae brew cannot version-select

- **WHEN** a brew app declares a version that Homebrew cannot select for that formula
- **THEN** `verify` SHALL report version drift if the installed version differs
- **AND** `apply` SHALL NOT downgrade or reinstall the present package solely to match the declared
  version

### Requirement: Homebrew is bootstrapped when absent, with consent

When the brew backend is needed on macOS and Homebrew is not installed, the engine SHALL offer to install
Homebrew via its official installer, proceed only after explicit user consent, and verify Homebrew works
before using it. The engine SHALL NOT install Homebrew silently or without consent, SHALL surface a clear
error rather than a silently half-completed state if the bootstrap fails, and SHALL — if the user
declines — skip the brew backend with a clear message while continuing the rest of the run.

#### Scenario: Homebrew absent — bootstrapped after consent

- **WHEN** `apply` needs the brew backend on macOS and Homebrew is not installed
- **THEN** the engine SHALL explain in plain language that it will set up the installer and request consent
- **AND** on consent it SHALL run the official Homebrew installer and verify Homebrew works before
  installing any packages

#### Scenario: Consent declined — brew lane skipped, run continues

- **WHEN** the user declines the Homebrew bootstrap
- **THEN** the engine SHALL NOT install Homebrew
- **AND** it SHALL skip the brew-routed apps with a clear message and still run the rest of the apply

#### Scenario: Bootstrap failure is surfaced, not silent

- **WHEN** the Homebrew bootstrap is attempted and fails
- **THEN** the engine SHALL report a clear error
- **AND** it SHALL NOT proceed as if the brew backend were available

#### Scenario: Homebrew already present — no bootstrap

- **WHEN** the brew backend is needed and Homebrew is already installed and working
- **THEN** the engine SHALL use it directly without attempting a bootstrap or prompting
