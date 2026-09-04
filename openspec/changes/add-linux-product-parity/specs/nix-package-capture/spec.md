## MODIFIED Requirements

### Requirement: Capture works on realizer backends
The engine SHALL support `capture` on Linux and Darwin without requiring Winget and SHALL combine available realizer inventory with platform discovery. On Linux, capture SHALL remain available when the Nix realizer or Endstate-managed profile is absent by using the ordinary-machine discovery sources. When a working realizer is present, its Endstate-managed package set SHALL remain authoritative evidence for that profile and SHALL be reconciled with, rather than substituted for, the wider discovered machine state. Windows capture SHALL continue to use its package-driver path unchanged.

#### Scenario: Capture combines the managed realizer set and ordinary-machine discovery
- **WHEN** Linux capture runs with a working Endstate Nix profile and supported applications are also found through other discovery sources
- **THEN** the engine SHALL write one manifest containing the reconciled resolved package intents
- **AND** it SHALL preserve source-qualified evidence for the managed and external items

#### Scenario: Capture succeeds without Nix
- **WHEN** Linux capture runs with trustworthy supported native/desktop/config evidence but no Nix executable or Endstate profile
- **THEN** the engine SHALL produce a valid capture from the resolved discovery results
- **AND** it SHALL NOT return `REALIZER_UNAVAILABLE` solely because Nix is absent

#### Scenario: No resolved package but supported config exists
- **WHEN** all available capture sources report no resolved applications but selected trusted platform modules capture useful configuration
- **THEN** the engine SHALL write a valid settings-only artifact with an empty apps list
- **AND** it SHALL report the source and unresolved/ignored counts

#### Scenario: Discovery finds no useful state
- **WHEN** all available capture sources succeed but report no resolved applications and no selected configuration can be captured
- **THEN** the engine SHALL return the governed zero-useful-state capture error
- **AND** it SHALL NOT write a misleading empty artifact

#### Scenario: No trustworthy source can be read
- **WHEN** every selected available capture source fails with a systemic, permission, or malformed-output error
- **THEN** the engine SHALL return a top-level capture error rather than writing a misleading empty manifest

### Requirement: Realizer capture is package-scoped
The Nix realizer inventory adapter SHALL enumerate installed packages only and SHALL NOT infer application configuration from Nix store contents. After package discovery and reconciliation, the platform capture orchestrator MAY attach configuration modules and a configuration bundle selected from trusted platform variants and live config evidence. Configuration capture SHALL remain owned by the config-module pipeline, not the realizer implementation.

#### Scenario: Realizer inventory contains packages only
- **WHEN** the Nix inventory adapter reads the current profile
- **THEN** its direct output SHALL contain only package elements and package provenance
- **AND** it SHALL NOT scan Nix store outputs for mutable user configuration

#### Scenario: Linux capture finalization attaches supported config
- **WHEN** reconciled Linux applications or config-only evidence match trusted Linux platform variants and capture is not sanitized
- **THEN** capture finalization SHALL attach the supported config modules and isolated configuration payloads
- **AND** the package realizer SHALL remain uninvolved in collecting those payloads

#### Scenario: Sanitized capture remains package-only
- **WHEN** Linux capture runs in sanitize mode
- **THEN** the produced artifact SHALL omit configuration modules and payloads
- **AND** package discovery SHALL remain available

## ADDED Requirements

### Requirement: Discovered external packages round-trip through the Nix realizer
For every non-Nix inventory item resolved through the reviewed package catalog, capture SHALL emit a Linux package reference that the Nix apply path can consume. The emitted desired reference SHALL use the effective immutable Nixpkgs input and portable attribute; the original manager/reference/version SHALL remain discovery evidence and SHALL NOT be passed to Nix.

#### Scenario: Native package maps to a pinned Nix installable
- **WHEN** discovery resolves a native package to portable attribute `ripgrep` under the release Nixpkgs revision
- **THEN** the captured app SHALL carry a Linux ref that resolves that revision's `ripgrep`
- **AND** apply SHALL install it through the Nix realizer rather than the source native manager

#### Scenario: Original source remains inspectable
- **WHEN** a mapped package is captured from dpkg, RPM, pacman, Flatpak, or Snap evidence
- **THEN** the discovery result/provenance SHALL retain the original source namespace, ref, and observed version
- **AND** the desired manifest ref SHALL remain the separate Nix intent

#### Scenario: Mapping disappears or becomes ambiguous
- **WHEN** current catalog validation cannot establish one trusted Nix intent for an external inventory item
- **THEN** the item SHALL remain unresolved and SHALL NOT be written as an installable app
