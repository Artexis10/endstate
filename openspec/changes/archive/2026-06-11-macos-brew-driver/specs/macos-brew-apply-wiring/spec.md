## ADDED Requirements

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

## MODIFIED Requirements

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

## REMOVED Requirements

### Requirement: A cask ref requires the brew driver

**Reason**: Superseded by Cask auto-routing (shipped with the engine-backend-bootstrap arc, PR #125,
reconciled in PR #127): a `cask:` darwin reference without `driver: "brew"` now routes to the brew
lane by default instead of being rejected at load. The still-valid validation half (a `driver: "brew"`
declaration requires a `darwin` reference, host-independently) moves to its own requirement above.
