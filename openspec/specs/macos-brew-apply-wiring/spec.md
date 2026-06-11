# macos-brew-apply-wiring Specification

## Purpose
Defines the two-lane darwin apply: the Nix realizer default lane plus the per-app Homebrew driver lane — routing (incl. Cask auto-routing), install, capture, verify/plan, and manifest validation.
## Requirements
### Requirement: Two-lane apply provisions brew and nix in one run on darwin

On macOS the engine SHALL provision a manifest through two lanes in a single `apply`: the Nix realizer lane (the default) and the Homebrew driver lane (apps that explicitly declare `driver: "brew"`). The Nix realizer lane SHALL run first and commit its atomic generation; the brew lane SHALL run second, best-effort and per package. A brew install failure SHALL be recorded as a per-item failure and SHALL NOT roll back or abort the committed Nix generation. The Nix realizer SHALL never receive a brew or `cask:` reference. A manifest that declares no brew-driver apps SHALL behave identically to the realizer-only path, writing exactly one Nix provisioning generation.

#### Scenario: A brew-driver app and a default app coexist in one apply

- **WHEN** `apply` runs on macOS for a manifest with one `driver: "brew"` app and one app with no driver declared
- **THEN** the engine SHALL provision the default app through the Nix realizer in one atomic generation
- **AND** it SHALL provision the brew-driver app through Homebrew in the same run
- **AND** it SHALL record the brew installs in a separate provisioning generation whose backend is `brew`

#### Scenario: A brew failure does not abort the committed nix generation

- **WHEN** a `driver: "brew"` app fails to install during an `apply` whose Nix lane already committed
- **THEN** the engine SHALL report that brew app as failed
- **AND** the committed Nix generation SHALL stand
- **AND** the run SHALL NOT return a top-level error solely because of the per-item brew failure

#### Scenario: A no-brew manifest is unchanged from the realizer-only path

- **WHEN** `apply` runs on macOS for a manifest that declares no `driver: "brew"` apps
- **THEN** the engine SHALL produce the same result and event stream as the realizer-only path
- **AND** it SHALL write exactly one provisioning generation whose backend is `nix`

#### Scenario: Brew items interleave into the single per-phase event stream

- **WHEN** `apply` runs both lanes on macOS
- **THEN** the brew per-item events SHALL interleave inside the realizer's existing plan, apply, and verify phases
- **AND** the engine SHALL emit exactly one summary event per phase, with the brew counts folded in

### Requirement: Brew installs formulae and casks via the driver lane

The engine SHALL install a `driver: "brew"` app through Homebrew, selecting a CLI formula (`brew install <name>`) for a bare `darwin` reference and a GUI Cask (`brew install --cask <name>`) for a reference marked with the `cask:` prefix. A `driver: "brew"` app on a non-macOS host SHALL be surfaced as a visible skipped item rather than silently dropped or installed.

#### Scenario: A bare reference installs a formula

- **WHEN** `apply` installs a `driver: "brew"` app whose `darwin` reference is a bare name
- **THEN** the engine SHALL install it as a Homebrew formula
- **AND** the result SHALL be reported as installed, or present when already installed

#### Scenario: A cask reference installs a GUI app

- **WHEN** `apply` installs a `driver: "brew"` app whose `darwin` reference carries the `cask:` prefix
- **THEN** the engine SHALL install it as a Homebrew Cask

#### Scenario: A brew app on a non-macOS host is a visible skip

- **WHEN** `apply` encounters a `driver: "brew"` app on a host where Homebrew is unavailable
- **THEN** the engine SHALL emit a skipped item for that app
- **AND** it SHALL NOT attempt to install the app through the Nix realizer

### Requirement: Capture enumerates brew formulae and casks into the manifest

When capturing on macOS, the engine SHALL enumerate installed Homebrew top-level formulae and Casks and SHALL record them in the manifest as `driver: "brew"` apps — formulae as bare `darwin` references and Casks as `cask:`-prefixed `darwin` references — each with its installed version recorded best-effort. A version Homebrew does not expose SHALL be recorded as empty rather than failing the capture. A captured brew app whose identifier collides with a realizer-captured app SHALL NOT be duplicated. Realizer-captured apps SHALL carry no driver field, so a captured manifest re-applies each app through its original backend.

#### Scenario: Capture records formulae and casks routed to brew

- **WHEN** `capture` runs on a macOS host with installed Homebrew formulae and Casks
- **THEN** the captured manifest SHALL include each top-level formula as a `driver: "brew"` app with a bare `darwin` reference
- **AND** it SHALL include each Cask as a `driver: "brew"` app with a `cask:`-prefixed `darwin` reference

#### Scenario: A missing version does not fail capture

- **WHEN** Homebrew exposes no version for a captured brew package
- **THEN** the engine SHALL record an empty version for that package
- **AND** the capture SHALL NOT fail because a version was unavailable

#### Scenario: A captured brew manifest round-trips to brew

- **WHEN** a manifest captured from a brew-provisioned macOS host is re-applied
- **THEN** the previously captured formulae and Casks SHALL be installed again through the brew driver
- **AND** they SHALL NOT be mis-attributed to the Nix realizer

### Requirement: Verify and plan report brew presence on darwin

On macOS the engine SHALL verify each `driver: "brew"` app by its installed presence (`brew list`) and SHALL plan it as an install when missing or a no-op when present, folding the brew results into the single verify or plan summary alongside the realizer results. Verify SHALL report version drift when an installed brew version differs from a declared version, treating a declared brew version as advisory. `apply` SHALL NOT downgrade or reinstall a present brew package solely to match a declared version that Homebrew cannot select.

#### Scenario: Presence is the brew verify check

- **WHEN** `verify` runs on macOS for a `driver: "brew"` app that is installed
- **THEN** the engine SHALL report it as passing based on its Homebrew installation
- **AND** a `driver: "brew"` app that is not installed SHALL be reported as failing

#### Scenario: Plan reports a missing brew app as an install

- **WHEN** `plan` runs on macOS for a `driver: "brew"` app that is not installed
- **THEN** the engine SHALL report a planned install for that app routed to the brew driver
- **AND** a present brew app SHALL be reported as a no-op

#### Scenario: A declared version is advisory for formulae brew cannot version-select

- **WHEN** a brew app declares a version that Homebrew cannot select for that formula
- **THEN** `verify` SHALL report version drift if the installed version differs
- **AND** `apply` SHALL NOT downgrade or reinstall the present package solely to match the declared
  version

### Requirement: A Cask reference routes to the brew lane by default

The engine SHALL route an app whose `darwin` reference is a Homebrew Cask (the `cask:` prefix,
case-insensitive) to the brew lane by default on macOS, without the user also declaring
`driver: "brew"`. The engine SHALL NOT reject a manifest solely because a `cask:` darwin reference
omits `driver: "brew"`. The Nix realizer SHALL never receive a `cask:` reference, and the engine SHALL
uphold that invariant by routing the Cask to the brew lane rather than by rejecting the manifest. The
engine SHALL still route an app that declares `driver: "brew"` to the brew lane regardless of its
reference shape, and SHALL continue to route a bare (non-Cask) darwin reference without `driver: "brew"`
to the Nix realizer (the default lane), so a manifest that declares no Cask and no brew driver behaves
identically to the realizer-only path.

#### Scenario: A Cask reference without the brew driver auto-routes to brew

- **WHEN** a manifest declares an app whose `darwin` reference is a `cask:` Cask and does not declare
  `driver: "brew"`
- **THEN** the engine SHALL accept the manifest
- **AND** it SHALL provision that app through the brew lane
- **AND** it SHALL NOT pass the `cask:` reference to the Nix realizer

#### Scenario: A bare darwin reference without a driver stays on the realizer

- **WHEN** a manifest declares an app with a bare (non-`cask:`) `darwin` reference and no `driver`
- **THEN** the engine SHALL route it to the Nix realizer (the default lane)
- **AND** the behavior SHALL be unchanged from before the Cask auto-routing

### Requirement: A brew driver declaration requires a darwin reference

The engine SHALL reject, at manifest load, an app that declares `driver: "brew"` without a `darwin`
reference (a formula name or a `cask:` reference), because brew installs only through a darwin
reference. This check SHALL run host-independently so a manifest is validated the same way on every
operating system.

#### Scenario: A brew driver without a darwin ref is rejected

- **WHEN** a manifest declares an app with `driver: "brew"` but no `darwin` reference
- **THEN** the engine SHALL reject the manifest at load with a clear error

#### Scenario: Brew driver validation is host-independent

- **WHEN** a manifest with a `driver: "brew"` app lacking a `darwin` reference is loaded on a
  non-macOS host
- **THEN** the engine SHALL reject it the same way it would on macOS

