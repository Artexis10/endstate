## MODIFIED Requirements

### Requirement: Capture enumerates brew formulae and casks into the manifest
When capturing on macOS, the engine SHALL enumerate installed Homebrew top-level formulae and Casks and SHALL record them in the manifest as `driver: "brew"` apps — formulae as bare `darwin` references and Casks as `cask:`-prefixed `darwin` references — each with its installed version recorded best-effort. A version Homebrew does not expose SHALL be recorded as empty rather than failing capture. Capture identity SHALL be `(driver, ref)`: a Brew identifier colliding with a realizer identifier SHALL remain a separate entry and receive a deterministic manifest-ID suffix if needed. Realizer-captured apps SHALL carry no driver field, so a captured manifest re-applies each app through its original backend.

#### Scenario: Capture records formulae and casks routed to Brew
- **WHEN** `capture` runs on a macOS host with installed Homebrew formulae and Casks
- **THEN** the captured manifest SHALL include each top-level formula as a `driver: "brew"` app with a bare `darwin` reference
- **AND** it SHALL include each Cask as a `cask:`-prefixed `darwin` reference

#### Scenario: Cross-backend identifier collision preserves both entries
- **WHEN** Brew and the realizer enumerate the same identifier text
- **THEN** capture SHALL preserve both entries with their original backend provenance
- **AND** their manifest IDs SHALL remain unique and deterministic

#### Scenario: A missing version does not fail capture
- **WHEN** Homebrew exposes no version for a captured Brew package
- **THEN** the engine SHALL record an empty version for that package
- **AND** capture SHALL NOT fail because a version was unavailable

#### Scenario: A captured brew manifest round-trips to brew
- **WHEN** a manifest captured from a Brew-provisioned macOS host is re-applied
- **THEN** the previously captured formulae and Casks SHALL be installed again through the Brew driver
- **AND** they SHALL NOT be mis-attributed to the Nix realizer

